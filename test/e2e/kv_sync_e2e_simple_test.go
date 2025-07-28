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

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKVSyncE2EDeployment tests deployment of vLLM pods with KV events enabled
func TestKVSyncE2EDeployment(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Initialize clients
	k8sClient, _ := initializeClient(ctx, t)
	helper := NewKVEventTestHelper(k8sClient, kvEventsTestNamespace)

	// Setup
	helper.CreateTestNamespace(t)
	defer helper.CleanupTestNamespace(t)
	defer helper.CleanupDeployments(t)

	// Test single pod deployment
	t.Run("SinglePod", func(t *testing.T) {
		deployment := helper.CreateVLLMPodWithKVEvents(t, "vllm-single", 1)
		helper.WaitForDeploymentReady(t, deployment.Name, 2*time.Minute)
		
		pods := helper.GetPodsByDeployment(t, deployment.Name)
		require.Equal(t, 1, len(pods), "Expected exactly 1 pod")
		
		// Validate KV event port is accessible
		helper.ValidateKVEventConnection(t, pods[0].Status.PodIP)
	})

	// Test multi-pod deployment
	t.Run("MultiPod", func(t *testing.T) {
		deployment := helper.CreateVLLMPodWithKVEvents(t, "vllm-multi", 3)
		helper.WaitForDeploymentReady(t, deployment.Name, 2*time.Minute)
		
		pods := helper.GetPodsByDeployment(t, deployment.Name)
		require.Equal(t, 3, len(pods), "Expected exactly 3 pods")
		
		// Validate all pods have KV events enabled
		for _, pod := range pods {
			assert.Equal(t, "true", pod.Labels["model.aibrix.ai/kv-events-enabled"])
			helper.ValidateKVEventConnection(t, pod.Status.PodIP)
		}
	})

	// Test pod scaling
	t.Run("PodScaling", func(t *testing.T) {
		deployment := helper.CreateVLLMPodWithKVEvents(t, "vllm-scale", 1)
		helper.WaitForDeploymentReady(t, deployment.Name, 2*time.Minute)
		
		// Scale up
		helper.ScaleDeployment(t, deployment.Name, 5)
		helper.WaitForDeploymentReady(t, deployment.Name, 2*time.Minute)
		
		pods := helper.GetPodsByDeployment(t, deployment.Name)
		require.Equal(t, 5, len(pods), "Expected 5 pods after scale up")
		
		// Scale down
		helper.ScaleDeployment(t, deployment.Name, 2)
		time.Sleep(30 * time.Second) // Wait for scale down
		
		pods = helper.GetPodsByDeployment(t, deployment.Name)
		require.Equal(t, 2, len(pods), "Expected 2 pods after scale down")
	})

	t.Log("KV sync E2E deployment tests passed successfully")
}

// TestKVSyncE2EConnectivity tests ZMQ connectivity for KV events
func TestKVSyncE2EConnectivity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Initialize clients
	k8sClient, _ := initializeClient(ctx, t)
	helper := NewKVEventTestHelper(k8sClient, kvEventsTestNamespace+"-conn")

	// Setup
	helper.CreateTestNamespace(t)
	defer helper.CleanupTestNamespace(t)
	defer helper.CleanupDeployments(t)

	// Create multiple deployments
	numDeployments := 3
	for i := 0; i < numDeployments; i++ {
		name := fmt.Sprintf("vllm-conn-%d", i)
		deployment := helper.CreateVLLMPodWithKVEvents(t, name, 2)
		helper.WaitForDeploymentReady(t, deployment.Name, 2*time.Minute)
	}

	// Validate connectivity across all pods
	totalConnections := 0
	for i := 0; i < numDeployments; i++ {
		name := fmt.Sprintf("vllm-conn-%d", i)
		pods := helper.GetPodsByDeployment(t, name)
		
		for _, pod := range pods {
			helper.ValidateKVEventConnection(t, pod.Status.PodIP)
			totalConnections++
		}
	}

	assert.Equal(t, numDeployments*2, totalConnections, 
		"Should have validated connections for all pods")

	t.Log("KV sync E2E connectivity tests passed successfully")
}

// TestKVSyncE2ESimpleLargeScale tests large-scale deployment scenarios
func TestKVSyncE2ESimpleLargeScale(t *testing.T) {
	// Skip in CI due to resource constraints
	if testing.Short() {
		t.Skip("Skipping large scale test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	// Initialize clients
	k8sClient, _ := initializeClient(ctx, t)
	
	scales := []int32{10, 25, 50}
	
	for _, scale := range scales {
		t.Run(fmt.Sprintf("Scale%d", scale), func(t *testing.T) {
			helper := NewKVEventTestHelper(k8sClient, 
				fmt.Sprintf("%s-scale-%d", kvEventsTestNamespace, scale))
			
			// Setup
			helper.CreateTestNamespace(t)
			defer helper.CleanupTestNamespace(t)
			defer helper.CleanupDeployments(t)
			
			// Deploy at scale
			deployment := helper.CreateVLLMPodWithKVEvents(t, "vllm-scale", scale)
			
			// Wait with appropriate timeout
			timeout := time.Duration(scale/10+2) * time.Minute
			helper.WaitForDeploymentReady(t, deployment.Name, timeout)
			
			// Validate deployment
			pods := helper.GetPodsByDeployment(t, deployment.Name)
			require.Equal(t, int(scale), len(pods), 
				"Expected %d pods", scale)
			
			// Sample connectivity validation (not all pods to save time)
			sampleSize := 5
			if int(scale) < sampleSize {
				sampleSize = int(scale)
			}
			
			for i := 0; i < sampleSize; i++ {
				helper.ValidateKVEventConnection(t, pods[i].Status.PodIP)
			}
			
			t.Logf("Successfully deployed and validated %d pods", scale)
		})
	}
}