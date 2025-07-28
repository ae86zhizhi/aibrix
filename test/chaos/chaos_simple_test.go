/*
Copyright 2024 The Aibrix Team.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package chaos

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	e2e "github.com/vllm-project/aibrix/test/e2e"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	chaosTestNamespace = "kv-sync-chaos-test"
	chaosTimeout       = 5 * time.Minute
	recoveryTimeout    = 2 * time.Minute
)

// ChaosTestSuite manages chaos testing
type ChaosTestSuite struct {
	k8sClient      *kubernetes.Clientset
	helper         *e2e.KVEventTestHelper
	chaosMeshReady bool
}

// NewChaosTestSuite creates a new chaos test suite
func NewChaosTestSuite(t *testing.T) *ChaosTestSuite {
	config, err := clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
	require.NoError(t, err, "Failed to build kube config")

	k8sClient, err := kubernetes.NewForConfig(config)
	require.NoError(t, err, "Failed to create k8s client")

	suite := &ChaosTestSuite{
		k8sClient: k8sClient,
		helper:    e2e.NewKVEventTestHelper(k8sClient, chaosTestNamespace),
	}

	// Check if Chaos Mesh is installed
	suite.chaosMeshReady = suite.checkChaosMeshInstalled(t)

	return suite
}

// checkChaosMeshInstalled checks if Chaos Mesh is installed
func (s *ChaosTestSuite) checkChaosMeshInstalled(t *testing.T) bool {
	// Check if chaos-mesh namespace exists
	_, err := s.k8sClient.CoreV1().Namespaces().Get(context.TODO(), "chaos-mesh", metav1.GetOptions{})
	if err != nil {
		t.Logf("Chaos Mesh not installed: %v", err)
		return false
	}

	// Check if controller manager is running
	pods, err := s.k8sClient.CoreV1().Pods("chaos-mesh").List(context.TODO(), metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/component=controller-manager",
	})
	if err != nil || len(pods.Items) == 0 {
		t.Logf("Chaos Mesh controller not running")
		return false
	}

	for _, pod := range pods.Items {
		if pod.Status.Phase != v1.PodRunning {
			t.Logf("Chaos Mesh controller pod %s not running", pod.Name)
			return false
		}
	}

	t.Log("Chaos Mesh is installed and ready")
	return true
}

// applyChaosExperiment applies a chaos experiment
func (s *ChaosTestSuite) applyChaosExperiment(t *testing.T, experimentFile string) {
	cmd := exec.Command("kubectl", "apply", "-f", experimentFile)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "Failed to apply chaos experiment: %s", string(output))
	t.Logf("Applied chaos experiment from %s", experimentFile)
}

// deleteChaosExperiment deletes a chaos experiment
func (s *ChaosTestSuite) deleteChaosExperiment(t *testing.T, experimentFile string) {
	cmd := exec.Command("kubectl", "delete", "-f", experimentFile, "--ignore-not-found=true")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("Failed to delete chaos experiment: %s", string(output))
	}
}

// validateSystemRecovery validates that the system recovers after chaos
func (s *ChaosTestSuite) validateSystemRecovery(t *testing.T, deploymentName string, namespace string) {
	ctx, cancel := context.WithTimeout(context.Background(), recoveryTimeout)
	defer cancel()

	// Wait for deployment to be ready again
	deadline := time.Now().Add(recoveryTimeout)
	for time.Now().Before(deadline) {
		deployment, err := s.k8sClient.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
		if err != nil {
			time.Sleep(5 * time.Second)
			continue
		}

		if deployment.Status.ReadyReplicas == *deployment.Spec.Replicas {
			t.Logf("Deployment %s recovered: %d/%d replicas ready",
				deploymentName, deployment.Status.ReadyReplicas, *deployment.Spec.Replicas)
			return
		}

		time.Sleep(5 * time.Second)
	}

	assert.Fail(t, "System failed to recover after chaos")
}

// TestChaosNetworkPartition tests network partition between vLLM and gateway
func TestChaosNetworkPartition(t *testing.T) {
	suite := NewChaosTestSuite(t)
	if !suite.chaosMeshReady {
		t.Skip("Chaos Mesh not installed, skipping chaos tests")
	}

	// Setup
	suite.helper.CreateTestNamespace(t)
	defer suite.helper.CleanupTestNamespace(t)
	defer suite.helper.CleanupDeployments(t)

	// Create vLLM pods
	deployment := suite.helper.CreateVLLMPodWithKVEvents(t, "vllm-chaos-network", 3)
	suite.helper.WaitForDeploymentReady(t, deployment.Name, 2*time.Minute)

	// Get initial pod count
	pods := suite.helper.GetPodsByDeployment(t, deployment.Name)
	initialPodCount := len(pods)

	// Apply network partition
	suite.applyChaosExperiment(t, "experiments/network-partition.yaml")
	defer suite.deleteChaosExperiment(t, "experiments/network-partition.yaml")

	// Wait for chaos to take effect
	time.Sleep(10 * time.Second)

	// During partition, pods should still be running
	pods = suite.helper.GetPodsByDeployment(t, deployment.Name)
	assert.Equal(t, initialPodCount, len(pods), "Pods should remain running during network partition")

	// Wait for chaos experiment to finish (30s duration)
	time.Sleep(30 * time.Second)

	// Validate recovery
	suite.validateSystemRecovery(t, deployment.Name, chaosTestNamespace)

	// Verify pods can communicate again
	pods = suite.helper.GetPodsByDeployment(t, deployment.Name)
	assert.Equal(t, initialPodCount, len(pods), "Pod count should remain the same after recovery")

	for _, pod := range pods {
		suite.helper.ValidateKVEventConnection(t, pod.Status.PodIP)
	}

	t.Log("Network partition chaos test passed")
}

// TestChaosPodFailures tests pod failures and recovery
func TestChaosPodFailures(t *testing.T) {
	suite := NewChaosTestSuite(t)
	if !suite.chaosMeshReady {
		t.Skip("Chaos Mesh not installed, skipping chaos tests")
	}

	// Setup
	suite.helper.CreateTestNamespace(t)
	defer suite.helper.CleanupTestNamespace(t)
	defer suite.helper.CleanupDeployments(t)

	// Create vLLM pods
	deployment := suite.helper.CreateVLLMPodWithKVEvents(t, "vllm-chaos-pods", 5)
	suite.helper.WaitForDeploymentReady(t, deployment.Name, 2*time.Minute)

	// Record initial state
	initialPods := suite.helper.GetPodsByDeployment(t, deployment.Name)
	initialPodNames := make(map[string]bool)
	for _, pod := range initialPods {
		initialPodNames[pod.Name] = true
	}

	// Apply pod kill chaos
	suite.applyChaosExperiment(t, "experiments/pod-failures.yaml")
	defer suite.deleteChaosExperiment(t, "experiments/pod-failures.yaml")

	// Wait for pods to be killed
	time.Sleep(20 * time.Second)

	// Check that some pods were killed and recreated
	currentPods := suite.helper.GetPodsByDeployment(t, deployment.Name)
	newPods := 0
	for _, pod := range currentPods {
		if !initialPodNames[pod.Name] {
			newPods++
		}
	}

	assert.Greater(t, newPods, 0, "Some pods should have been recreated")

	// Wait for chaos to finish
	time.Sleep(70 * time.Second) // 90s total duration

	// Validate recovery
	suite.validateSystemRecovery(t, deployment.Name, chaosTestNamespace)

	// Verify all pods are functional
	finalPods := suite.helper.GetPodsByDeployment(t, deployment.Name)
	assert.Equal(t, len(initialPods), len(finalPods), "Pod count should be restored")

	for _, pod := range finalPods {
		suite.helper.ValidateKVEventConnection(t, pod.Status.PodIP)
	}

	t.Log("Pod failures chaos test passed")
}

// TestChaosStress tests CPU and memory stress
func TestChaosStress(t *testing.T) {
	suite := NewChaosTestSuite(t)
	if !suite.chaosMeshReady {
		t.Skip("Chaos Mesh not installed, skipping chaos tests")
	}

	// Setup
	suite.helper.CreateTestNamespace(t)
	defer suite.helper.CleanupTestNamespace(t)
	defer suite.helper.CleanupDeployments(t)

	// Create vLLM pods
	deployment := suite.helper.CreateVLLMPodWithKVEvents(t, "vllm-chaos-stress", 3)
	suite.helper.WaitForDeploymentReady(t, deployment.Name, 2*time.Minute)

	// Get baseline state
	pods := suite.helper.GetPodsByDeployment(t, deployment.Name)
	for _, pod := range pods {
		suite.helper.ValidateKVEventConnection(t, pod.Status.PodIP)
	}

	// Apply stress chaos
	suite.applyChaosExperiment(t, "experiments/pod-failures.yaml") // Contains stress scenarios
	defer suite.deleteChaosExperiment(t, "experiments/pod-failures.yaml")

	// During stress, system should degrade gracefully
	time.Sleep(30 * time.Second)

	// Pods should still be running under stress
	stressPods := suite.helper.GetPodsByDeployment(t, deployment.Name)
	assert.Equal(t, len(pods), len(stressPods), "Pods should survive stress test")

	// Wait for stress to finish
	time.Sleep(30 * time.Second)

	// Validate recovery
	suite.validateSystemRecovery(t, deployment.Name, chaosTestNamespace)

	t.Log("Stress chaos test passed")
}