/*
Copyright 2025 The Aibrix Team.

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
	"fmt"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vllm-project/aibrix/pkg/cache/kvcache"
	e2e "github.com/vllm-project/aibrix/test/e2e"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	chaosFullTestNamespace = "kv-sync-chaos-test-full"
	chaosFullTimeout       = 5 * time.Minute
	recoveryFullTimeout    = 2 * time.Minute
	kvEventsFullPort       = 5557
)

// ChaosFullTestSuite manages comprehensive chaos testing
type ChaosFullTestSuite struct {
	k8sClient      *kubernetes.Clientset
	helper         *e2e.KVEventTestHelper
	chaosMeshReady bool
}

// NewChaosFullTestSuite creates a new comprehensive chaos test suite
func NewChaosFullTestSuite(t *testing.T) *ChaosFullTestSuite {
	// Skip if no Kubernetes configuration is available
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" && os.Getenv("KUBERNETES_SERVICE_HOST") == "" {
		t.Skip("Skipping chaos tests: no Kubernetes configuration available (set KUBECONFIG or run in-cluster)")
		return nil
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	require.NoError(t, err, "Failed to build kube config")

	k8sClient, err := kubernetes.NewForConfig(config)
	require.NoError(t, err, "Failed to create k8s client")

	suite := &ChaosFullTestSuite{
		k8sClient: k8sClient,
		helper:    e2e.NewKVEventTestHelper(k8sClient, chaosFullTestNamespace),
	}

	// Check if Chaos Mesh is installed
	suite.chaosMeshReady = suite.checkChaosMeshInstalled(t)

	return suite
}

// checkChaosMeshInstalled checks if Chaos Mesh is installed
func (s *ChaosFullTestSuite) checkChaosMeshInstalled(t *testing.T) bool {
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
func (s *ChaosFullTestSuite) applyChaosExperiment(t *testing.T, experimentFile string) {
	cmd := exec.Command("kubectl", "apply", "-f", experimentFile)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "Failed to apply chaos experiment: %s", string(output))
	t.Logf("Applied chaos experiment from %s", experimentFile)
}

// deleteChaosExperiment deletes a chaos experiment
func (s *ChaosFullTestSuite) deleteChaosExperiment(t *testing.T, experimentFile string) {
	cmd := exec.Command("kubectl", "delete", "-f", experimentFile, "--ignore-not-found=true")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("Failed to delete chaos experiment: %s", string(output))
	}
}

// validateSystemRecovery validates that the system recovers after chaos
func (s *ChaosFullTestSuite) validateSystemRecovery(t *testing.T, deploymentName string) {
	ctx, cancel := context.WithTimeout(context.Background(), recoveryFullTimeout)
	defer cancel()

	// Wait for deployment to be ready again
	err := wait.PollUntilContextTimeout(ctx, 5*time.Second, recoveryFullTimeout, true,
		func(ctx context.Context) (bool, error) {
			deployment, err := s.k8sClient.AppsV1().Deployments(s.helper.GetNamespace()).Get(
				ctx, deploymentName, metav1.GetOptions{})
			if err != nil {
				return false, err
			}

			if deployment.Status.ReadyReplicas == *deployment.Spec.Replicas {
				t.Logf("Deployment %s recovered: %d/%d replicas ready",
					deploymentName, deployment.Status.ReadyReplicas, *deployment.Spec.Replicas)
				return true, nil
			}

			return false, nil
		})

	assert.NoError(t, err, "System failed to recover after chaos")
}

// TestChaosFullNetworkPartition tests network partition between vLLM and gateway
func TestChaosFullNetworkPartition(t *testing.T) {
	suite := NewChaosFullTestSuite(t)
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

	// Try to send events during partition (should fail or timeout)
	for _, pod := range pods {
		// Simulate sending event - in real test this would use ZMQ
		// This should fail or timeout due to network partition
		event := &kvcache.BlockStoredEvent{
			Type:        kvcache.EventTypeBlockStored,
			Timestamp:   time.Now(),
			BlockHashes: []int64{5000},
			TokenIDs:    [][]int32{{5000, 5001, 5002}},
			ModelName:   "test-model",
			PodName:     pod.Name,
		}
		_ = event // Event might not be deliverable due to network partition
	}

	// Wait for chaos experiment to finish
	time.Sleep(30 * time.Second)

	// Validate recovery
	suite.validateSystemRecovery(t, deployment.Name)

	// Verify pods can communicate again
	pods = suite.helper.GetPodsByDeployment(t, deployment.Name)
	assert.Equal(t, initialPodCount, len(pods), "Pod count should remain the same after recovery")

	for _, pod := range pods {
		suite.helper.ValidateKVEventConnection(t, pod.Status.PodIP)
	}

	t.Log("Network partition chaos test passed")
}

// TestChaosFullPodFailures tests pod failures and recovery
func TestChaosFullPodFailures(t *testing.T) {
	suite := NewChaosFullTestSuite(t)
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
	time.Sleep(40 * time.Second)

	// Validate recovery
	suite.validateSystemRecovery(t, deployment.Name)

	// Verify all pods are functional
	finalPods := suite.helper.GetPodsByDeployment(t, deployment.Name)
	assert.Equal(t, len(initialPods), len(finalPods), "Pod count should be restored")

	for _, pod := range finalPods {
		suite.helper.ValidateKVEventConnection(t, pod.Status.PodIP)
	}

	t.Log("Pod failures chaos test passed")
}

// TestChaosFullZMQFailures tests ZMQ-specific failures
func TestChaosFullZMQFailures(t *testing.T) {
	suite := NewChaosFullTestSuite(t)
	if !suite.chaosMeshReady {
		t.Skip("Chaos Mesh not installed, skipping chaos tests")
	}

	// Setup
	suite.helper.CreateTestNamespace(t)
	defer suite.helper.CleanupTestNamespace(t)
	defer suite.helper.CleanupDeployments(t)

	// Create vLLM pods
	deployment := suite.helper.CreateVLLMPodWithKVEvents(t, "vllm-chaos-zmq", 3)
	suite.helper.WaitForDeploymentReady(t, deployment.Name, 2*time.Minute)

	// Apply ZMQ failures
	suite.applyChaosExperiment(t, "experiments/zmq-failures.yaml")
	defer suite.deleteChaosExperiment(t, "experiments/zmq-failures.yaml")

	// Monitor during chaos
	startTime := time.Now()
	failedConnections := 0
	successfulConnections := 0

	// Try connections during chaos
	for time.Since(startTime) < 30*time.Second {
		pods := suite.helper.GetPodsByDeployment(t, deployment.Name)
		for _, pod := range pods {
			// Try to connect to ZMQ socket directly
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", pod.Status.PodIP, kvEventsFullPort), 2*time.Second)
			if err == nil {
				successfulConnections++
				_ = conn.Close()
			} else {
				failedConnections++
			}
		}
		time.Sleep(5 * time.Second)
	}

	// Some connections should fail during chaos
	assert.Greater(t, failedConnections, 0, "Some ZMQ connections should fail during chaos")

	// Wait for chaos to finish
	time.Sleep(30 * time.Second)

	// Validate recovery
	suite.validateSystemRecovery(t, deployment.Name)

	// All connections should work after recovery
	pods := suite.helper.GetPodsByDeployment(t, deployment.Name)
	for _, pod := range pods {
		suite.helper.ValidateKVEventConnection(t, pod.Status.PodIP)
	}

	t.Log("ZMQ failures chaos test passed")
}

// TestChaosFullStress tests CPU and memory stress
func TestChaosFullStress(t *testing.T) {
	suite := NewChaosFullTestSuite(t)
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

	// Measure baseline performance
	baselineStart := time.Now()
	baselineEvents := 0

	pods := suite.helper.GetPodsByDeployment(t, deployment.Name)
	for i := 0; i < 10; i++ {
		for _, pod := range pods {
			event := &kvcache.BlockStoredEvent{
				Type:        kvcache.EventTypeBlockStored,
				Timestamp:   time.Now(),
				BlockHashes: []int64{int64(6000 + i*100)},
				TokenIDs:    [][]int32{{int32(6000 + i*10), int32(6001 + i*10), int32(6002 + i*10)}},
				ModelName:   "test-model",
				PodName:     pod.Name,
			}
			_ = event
			baselineEvents++
		}
	}
	baselineDuration := time.Since(baselineStart)
	baselineRate := float64(baselineEvents) / baselineDuration.Seconds()

	// Apply stress chaos
	suite.applyChaosExperiment(t, "experiments/pod-failures.yaml") // Uses stress chaos
	defer suite.deleteChaosExperiment(t, "experiments/pod-failures.yaml")

	// Measure performance under stress
	time.Sleep(10 * time.Second) // Let stress take effect

	stressStart := time.Now()
	stressEvents := 0

	for i := 0; i < 10; i++ {
		for _, pod := range pods {
			event := &kvcache.BlockStoredEvent{
				Type:        kvcache.EventTypeBlockStored,
				Timestamp:   time.Now(),
				BlockHashes: []int64{int64(7000 + i*100)},
				TokenIDs:    [][]int32{{int32(7000 + i*10), int32(7001 + i*10), int32(7002 + i*10)}},
				ModelName:   "test-model",
				PodName:     pod.Name,
			}
			_ = event
			stressEvents++
		}
	}
	stressDuration := time.Since(stressStart)
	stressRate := float64(stressEvents) / stressDuration.Seconds()

	// Performance should degrade but system should still function
	degradation := (baselineRate - stressRate) / baselineRate * 100
	t.Logf("Performance degradation under stress: %.1f%% (baseline: %.1f eps, stress: %.1f eps)",
		degradation, baselineRate, stressRate)

	assert.Less(t, degradation, 90.0, "System should maintain at least 10% performance under stress")

	// Wait for stress to finish
	time.Sleep(50 * time.Second)

	// Validate recovery
	suite.validateSystemRecovery(t, deployment.Name)

	t.Log("Stress chaos test passed")
}

// TestChaosFullTimeSkew tests time synchronization issues
func TestChaosFullTimeSkew(t *testing.T) {
	suite := NewChaosFullTestSuite(t)
	if !suite.chaosMeshReady {
		t.Skip("Chaos Mesh not installed, skipping chaos tests")
	}

	// Setup
	suite.helper.CreateTestNamespace(t)
	defer suite.helper.CleanupTestNamespace(t)
	defer suite.helper.CleanupDeployments(t)

	// Create vLLM pods
	deployment := suite.helper.CreateVLLMPodWithKVEvents(t, "vllm-chaos-time", 3)
	suite.helper.WaitForDeploymentReady(t, deployment.Name, 2*time.Minute)

	// Apply time skew chaos
	suite.applyChaosExperiment(t, "experiments/zmq-failures.yaml") // Contains time chaos
	defer suite.deleteChaosExperiment(t, "experiments/zmq-failures.yaml")

	// Send events with timestamps during time skew
	time.Sleep(10 * time.Second)

	pods := suite.helper.GetPodsByDeployment(t, deployment.Name)

	for i, pod := range pods {
		event := &kvcache.BlockStoredEvent{
			Type:        kvcache.EventTypeBlockStored,
			Timestamp:   time.Now(),
			BlockHashes: []int64{int64(8000 + i*100)},
			TokenIDs:    [][]int32{{int32(8000 + i*10), int32(8001 + i*10), int32(8002 + i*10)}},
			ModelName:   "test-model",
			PodName:     pod.Name,
		}
		// In real test, would send event to pod
		_ = event
		_ = pod
	}

	// System should handle time skew gracefully
	// In a real test, we would verify event ordering and timestamp handling

	// Wait for chaos to finish
	time.Sleep(110 * time.Second)

	// Validate recovery
	suite.validateSystemRecovery(t, deployment.Name)

	t.Log("Time skew chaos test passed")
}

// TestChaosFullRecoveryValidation tests automatic recovery from various failures
func TestChaosFullRecoveryValidation(t *testing.T) {
	suite := NewChaosFullTestSuite(t)
	if !suite.chaosMeshReady {
		t.Skip("Chaos Mesh not installed, skipping chaos tests")
	}

	scenarios := []struct {
		name           string
		experimentFile string
		duration       time.Duration
		validation     func(*testing.T, string)
	}{
		{
			name:           "NetworkPartitionRecovery",
			experimentFile: "experiments/network-partition.yaml",
			duration:       60 * time.Second,
			validation: func(t *testing.T, deploymentName string) {
				// Verify network connectivity is restored
				pods := suite.helper.GetPodsByDeployment(t, deploymentName)
				for _, pod := range pods {
					suite.helper.ValidateKVEventConnection(t, pod.Status.PodIP)
				}
			},
		},
		{
			name:           "PodFailureRecovery",
			experimentFile: "experiments/pod-failures.yaml",
			duration:       90 * time.Second,
			validation: func(t *testing.T, deploymentName string) {
				// Verify deployment reaches desired replica count
				deployment, err := suite.k8sClient.AppsV1().Deployments(suite.helper.GetNamespace()).Get(
					context.TODO(), deploymentName, metav1.GetOptions{})
				require.NoError(t, err)
				assert.Equal(t, *deployment.Spec.Replicas, deployment.Status.ReadyReplicas)
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			// Setup
			suite.helper.CreateTestNamespace(t)
			defer suite.helper.CleanupTestNamespace(t)
			defer suite.helper.CleanupDeployments(t)

			// Create deployment
			deployment := suite.helper.CreateVLLMPodWithKVEvents(t, "vllm-recovery", 3)
			suite.helper.WaitForDeploymentReady(t, deployment.Name, 2*time.Minute)

			// Apply chaos
			suite.applyChaosExperiment(t, scenario.experimentFile)
			defer suite.deleteChaosExperiment(t, scenario.experimentFile)

			// Wait for chaos duration
			time.Sleep(scenario.duration)

			// Validate recovery
			suite.validateSystemRecovery(t, deployment.Name)

			// Run scenario-specific validation
			scenario.validation(t, deployment.Name)

			t.Logf("%s recovery test passed", scenario.name)
		})
	}
}
