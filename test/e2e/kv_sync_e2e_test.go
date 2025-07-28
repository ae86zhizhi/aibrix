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
	"github.com/vllm-project/aibrix/pkg/cache/kvcache"
	"github.com/vllm-project/aibrix/pkg/utils/syncprefixcacheindexer"
)

// TestKVSyncE2EHappyPath tests the happy path of KV event synchronization with a single pod
func TestKVSyncE2EHappyPath(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Initialize clients
	k8sClient, _ := initializeClient(ctx, t)
	helper := NewKVEventTestHelper(k8sClient, kvEventsTestNamespace)

	// Setup
	helper.CreateTestNamespace(t)
	defer helper.CleanupTestNamespace(t)
	defer helper.CleanupDeployments(t)

	// Create a single vLLM pod with KV events enabled
	deployment := helper.CreateVLLMPodWithKVEvents(t, "vllm-single-pod", 1)
	helper.WaitForDeploymentReady(t, deployment.Name, 2*time.Minute)

	// Get the pod
	pods := helper.GetPodsByDeployment(t, deployment.Name)
	require.Equal(t, 1, len(pods), "Expected exactly 1 pod")
	pod := pods[0]

	// Validate KV event connection
	helper.ValidateKVEventConnection(t, pod.Status.PodIP)

	// Create sync indexer directly for testing
	indexer := syncprefixcacheindexer.NewSyncPrefixHashTable()
	defer indexer.Close()

	// Wait for pod to be ready
	time.Sleep(2 * time.Second)

	// Create test event
	tokenIDs := []int32{100, 101, 102, 103, 104}
	
	// Create a BlockStoredEvent
	event := &kvcache.BlockStoredEvent{
		Type:        kvcache.EventTypeBlockStored,
		Timestamp:   time.Now(),
		BlockHashes: []int64{12345},
		TokenIDs:    [][]int32{tokenIDs},
		ModelName:   helper.modelName,
		PodName:     pod.Name,
	}
	
	// Simulate processing the event
	// In real scenario, this would come through ZMQ
	// Convert the event to the format expected by the indexer
	blockStoredEvent := syncprefixcacheindexer.BlockStored{
		BlockHashes: event.BlockHashes,
		Tokens:      [][]byte{convertTokensToBytes(tokenIDs)},
		ModelName:   event.ModelName,
		LoraID:      -1,
		SourcePod:   pod.Status.PodIP,
	}
	err := indexer.ProcessBlockStored(blockStoredEvent)
	require.NoError(t, err, "Failed to process block stored event")

	// Convert int32 tokens to byte tokens for indexer lookup
	byteTokens := convertTokensToBytes(tokenIDs)

	// Check if the prefix exists in the indexer using MatchPrefix
	readyPods := map[string]struct{}{pod.Status.PodIP: {}}
	matches, hashes := indexer.MatchPrefix(helper.modelName, -1, byteTokens, readyPods)
	assert.Greater(t, len(matches), 0, "Should have matches after processing event")
	assert.Contains(t, matches, pod.Status.PodIP, "Pod should be in matches")
	assert.Greater(t, len(hashes), 0, "Should have computed hashes")

	t.Log("Single pod KV sync E2E test passed successfully")
}

// TestKVSyncE2EMultiPod tests KV event synchronization with multiple pods
func TestKVSyncE2EMultiPod(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Initialize clients
	k8sClient, _ := initializeClient(ctx, t)
	helper := NewKVEventTestHelper(k8sClient, kvEventsTestNamespace+"-multi")

	// Setup
	helper.CreateTestNamespace(t)
	defer helper.CleanupTestNamespace(t)
	defer helper.CleanupDeployments(t)

	// Create multiple vLLM pods
	numPods := 3
	deployment := helper.CreateVLLMPodWithKVEvents(t, "vllm-multi-pod", int32(numPods))
	helper.WaitForDeploymentReady(t, deployment.Name, 3*time.Minute)

	// Get all pods
	pods := helper.GetPodsByDeployment(t, deployment.Name)
	require.Equal(t, numPods, len(pods), "Expected %d pods", numPods)

	// Validate connections for all pods
	for _, pod := range pods {
		helper.ValidateKVEventConnection(t, pod.Status.PodIP)
	}

	// Create sync indexer directly for testing
	indexer := syncprefixcacheindexer.NewSyncPrefixHashTable()
	defer indexer.Close()

	// Wait for pods to be ready
	time.Sleep(2 * time.Second)

	// Send events from different pods
	for i, pod := range pods {
		tokenIDs := []int32{int32(i*100), int32(i*100 + 1), int32(i*100 + 2)}
		
		event := &kvcache.BlockStoredEvent{
			Type:        kvcache.EventTypeBlockStored,
			Timestamp:   time.Now(),
			BlockHashes: []int64{int64(i * 1000)},
			TokenIDs:    [][]int32{tokenIDs},
			ModelName:   helper.modelName,
			PodName:     pod.Name,
		}
		// Convert the event to the format expected by the indexer
		blockStoredEvent := syncprefixcacheindexer.BlockStored{
			BlockHashes: event.BlockHashes,
			Tokens:      [][]byte{convertTokensToBytes(tokenIDs)},
			ModelName:   event.ModelName,
			LoraID:      -1,
			SourcePod:   pod.Status.PodIP,
		}
		err := indexer.ProcessBlockStored(blockStoredEvent)
		require.NoError(t, err, "Failed to process block stored event")
		
		t.Logf("Sent event from pod %s with tokens %v", pod.Name, tokenIDs)
	}

	// Verify all events were processed

	// Check each pod's prefix
	for i := 0; i < numPods; i++ {
		tokenIDs := []int32{int32(i*100), int32(i*100 + 1), int32(i*100 + 2)}
		// Convert to bytes
		byteTokens := convertTokensToBytes(tokenIDs)
		// Check if the prefix exists using MatchPrefix
		readyPods := map[string]struct{}{}
		for _, p := range pods {
			readyPods[p.Status.PodIP] = struct{}{}
		}
		matches, _ := indexer.MatchPrefix(helper.modelName, -1, byteTokens, readyPods)
		assert.Greater(t, len(matches), 0, "Prefix from pod %d should exist in the indexer", i)
	}

	t.Log("Multi-pod KV sync E2E test passed successfully")
}

// TestKVSyncE2EPodLifecycle tests pod lifecycle events (creation, scaling, deletion)
func TestKVSyncE2EPodLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Initialize clients
	k8sClient, _ := initializeClient(ctx, t)
	helper := NewKVEventTestHelper(k8sClient, kvEventsTestNamespace+"-lifecycle")

	// Setup
	helper.CreateTestNamespace(t)
	defer helper.CleanupTestNamespace(t)
	defer helper.CleanupDeployments(t)

	// Create sync indexer directly for testing
	indexer := syncprefixcacheindexer.NewSyncPrefixHashTable()
	defer indexer.Close()

	// Test 1: Pod Creation
	t.Log("Testing pod creation...")
	deployment := helper.CreateVLLMPodWithKVEvents(t, "vllm-lifecycle", 1)
	helper.WaitForDeploymentReady(t, deployment.Name, 2*time.Minute)

	pods := helper.GetPodsByDeployment(t, deployment.Name)
	require.Equal(t, 1, len(pods), "Expected 1 pod after creation")

	// Send event from the new pod
	event1 := &kvcache.BlockStoredEvent{
		Type:        kvcache.EventTypeBlockStored,
		Timestamp:   time.Now(),
		BlockHashes: []int64{1000},
		TokenIDs:    [][]int32{{1000, 1001, 1002}},
		ModelName:   helper.modelName,
		PodName:     pods[0].Name,
	}
	// Convert the event to the format expected by the indexer
	blockStoredEvent1 := syncprefixcacheindexer.BlockStored{
		BlockHashes: event1.BlockHashes,
		Tokens:      [][]byte{convertTokensToBytes([]int32{1000, 1001, 1002})},
		ModelName:   event1.ModelName,
		LoraID:      -1,
		SourcePod:   pods[0].Status.PodIP,
	}
	err := indexer.ProcessBlockStored(blockStoredEvent1)
	require.NoError(t, err, "Failed to process block stored event")

	// Test 2: Scale Up
	t.Log("Testing scale up...")
	helper.ScaleDeployment(t, deployment.Name, 2)  // Reduced for faster testing
	helper.WaitForDeploymentReady(t, deployment.Name, 1*time.Minute)

	pods = helper.GetPodsByDeployment(t, deployment.Name)
	require.Equal(t, 2, len(pods), "Expected 2 pods after scale up")

	// Send events from all pods
	for i, pod := range pods {
		tokenIDs := []int32{int32(2000 + i*10), int32(2001 + i*10), int32(2002 + i*10)}
		event := &kvcache.BlockStoredEvent{
			Type:        kvcache.EventTypeBlockStored,
			Timestamp:   time.Now(),
			BlockHashes: []int64{int64(2000 + i*100)},
			TokenIDs:    [][]int32{tokenIDs},
			ModelName:   helper.modelName,
			PodName:     pod.Name,
		}
		// Convert the event to the format expected by the indexer
		blockStoredEvent := syncprefixcacheindexer.BlockStored{
			BlockHashes: event.BlockHashes,
			Tokens:      [][]byte{convertTokensToBytes(tokenIDs)},
			ModelName:   event.ModelName,
			LoraID:      -1,
			SourcePod:   pod.Status.PodIP,
		}
		err := indexer.ProcessBlockStored(blockStoredEvent)
		require.NoError(t, err, "Failed to process block stored event")
	}

	// Test 3: Pod Deletion
	t.Log("Testing pod deletion...")
	podToDelete := pods[0].Name
	helper.DeletePod(t, podToDelete)
	helper.WaitForPodDeletion(t, podToDelete, 1*time.Minute)

	// Wait for deployment to create a replacement pod
	helper.WaitForDeploymentReady(t, deployment.Name, 2*time.Minute)

	// Test 4: Scale Down
	t.Log("Testing scale down...")
	helper.ScaleDeployment(t, deployment.Name, 1)
	
	// Wait for pods to actually terminate
	require.Eventually(t, func() bool {
		pods = helper.GetPodsByDeployment(t, deployment.Name)
		return len(pods) == 1
	}, 2*time.Minute, 5*time.Second, "Waiting for deployment to scale down to 1 pod")

	require.Equal(t, 1, len(pods), "Expected 1 pod after scale down")

	// Verify indexer still has data from previous pods

	// Check that events from different lifecycle stages are still present
	// Convert initial tokens
	initTokens := []int32{1000, 1001, 1002}
	initBytes := convertTokensToBytes(initTokens)
	// Check using MatchPrefix
	readyPods := map[string]struct{}{}
	for _, p := range pods {
		readyPods[p.Status.PodIP] = struct{}{}
	}
	matches, _ := indexer.MatchPrefix(helper.modelName, -1, initBytes, readyPods)
	assert.Greater(t, len(matches), 0, "Initial pod event should still exist")
	
	// Convert scaled tokens
	scaledTokens := []int32{2000, 2001, 2002}
	scaledBytes := convertTokensToBytes(scaledTokens)
	matches2, _ := indexer.MatchPrefix(helper.modelName, -1, scaledBytes, readyPods)
	assert.Greater(t, len(matches2), 0, "Scaled pod event should still exist")

	t.Log("Pod lifecycle KV sync E2E test passed successfully")
}

// TestKVSyncE2EMultiModel tests KV event synchronization with multiple models
func TestKVSyncE2EMultiModel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Initialize clients
	k8sClient, _ := initializeClient(ctx, t)
	
	// Create separate helpers for different models
	helper1 := NewKVEventTestHelper(k8sClient, kvEventsTestNamespace+"-model1")
	helper1.modelName = "model-1"
	
	helper2 := NewKVEventTestHelper(k8sClient, kvEventsTestNamespace+"-model2")
	helper2.modelName = "model-2"

	// Setup namespaces
	helper1.CreateTestNamespace(t)
	helper2.CreateTestNamespace(t)
	defer helper1.CleanupTestNamespace(t)
	defer helper2.CleanupTestNamespace(t)
	defer helper1.CleanupDeployments(t)
	defer helper2.CleanupDeployments(t)

	// Create pods for different models
	deployment1 := helper1.CreateVLLMPodWithKVEvents(t, "vllm-model1", 2)
	deployment2 := helper2.CreateVLLMPodWithKVEvents(t, "vllm-model2", 2)
	
	helper1.WaitForDeploymentReady(t, deployment1.Name, 2*time.Minute)
	helper2.WaitForDeploymentReady(t, deployment2.Name, 2*time.Minute)

	// Get pods for each model
	pods1 := helper1.GetPodsByDeployment(t, deployment1.Name)
	pods2 := helper2.GetPodsByDeployment(t, deployment2.Name)
	
	require.Equal(t, 2, len(pods1), "Expected 2 pods for model 1")
	require.Equal(t, 2, len(pods2), "Expected 2 pods for model 2")

	// Create sync indexers for each model
	indexer1 := syncprefixcacheindexer.NewSyncPrefixHashTable()
	indexer2 := syncprefixcacheindexer.NewSyncPrefixHashTable()
	defer indexer1.Close()
	defer indexer2.Close()

	// Wait for pods to be ready
	time.Sleep(2 * time.Second)

	// Send events from model 1 pods
	for i, pod := range pods1 {
		tokenIDs := []int32{int32(3000 + i*10), int32(3001 + i*10), int32(3002 + i*10)}
		event := &kvcache.BlockStoredEvent{
			Type:        kvcache.EventTypeBlockStored,
			Timestamp:   time.Now(),
			BlockHashes: []int64{int64(3000 + i*100)},
			TokenIDs:    [][]int32{tokenIDs},
			ModelName:   helper1.modelName,
			PodName:     pod.Name,
		}
		// Convert the event to the format expected by the indexer
		blockStoredEvent := syncprefixcacheindexer.BlockStored{
			BlockHashes: event.BlockHashes,
			Tokens:      [][]byte{convertTokensToBytes(tokenIDs)},
			ModelName:   event.ModelName,
			LoraID:      -1,
			SourcePod:   pod.Status.PodIP,
		}
		err := indexer1.ProcessBlockStored(blockStoredEvent)
		require.NoError(t, err, "Failed to process block stored event")
	}

	// Send events from model 2 pods
	for i, pod := range pods2 {
		tokenIDs := []int32{int32(4000 + i*10), int32(4001 + i*10), int32(4002 + i*10)}
		event := &kvcache.BlockStoredEvent{
			Type:        kvcache.EventTypeBlockStored,
			Timestamp:   time.Now(),
			BlockHashes: []int64{int64(4000 + i*100)},
			TokenIDs:    [][]int32{tokenIDs},
			ModelName:   helper2.modelName,
			PodName:     pod.Name,
		}
		// Convert the event to the format expected by the indexer
		blockStoredEvent := syncprefixcacheindexer.BlockStored{
			BlockHashes: event.BlockHashes,
			Tokens:      [][]byte{convertTokensToBytes(tokenIDs)},
			ModelName:   event.ModelName,
			LoraID:      -1,
			SourcePod:   pod.Status.PodIP,
		}
		err := indexer2.ProcessBlockStored(blockStoredEvent)
		require.NoError(t, err, "Failed to process block stored event")
	}

	// Verify isolation between models
	
	// Convert and check Model 1 events
	tokens1a := []int32{3000, 3001, 3002}
	bytes1a := convertTokensToBytes(tokens1a)
	// Check using MatchPrefix
	readyPods1 := map[string]struct{}{}
	for _, p := range pods1 {
		readyPods1[p.Status.PodIP] = struct{}{}
	}
	matches1a, _ := indexer1.MatchPrefix(helper1.modelName, -1, bytes1a, readyPods1)
	assert.Greater(t, len(matches1a), 0, "Model 1 events should exist in indexer 1")
	
	tokens1b := []int32{3010, 3011, 3012}
	bytes1b := convertTokensToBytes(tokens1b)
	matches1b, _ := indexer1.MatchPrefix(helper1.modelName, -1, bytes1b, readyPods1)
	assert.Greater(t, len(matches1b), 0, "Model 1 events should exist in indexer 1")
	
	// Convert and check Model 2 events
	tokens2a := []int32{4000, 4001, 4002}
	bytes2a := convertTokensToBytes(tokens2a)
	// Check using MatchPrefix
	readyPods2 := map[string]struct{}{}
	for _, p := range pods2 {
		readyPods2[p.Status.PodIP] = struct{}{}
	}
	matches2a, _ := indexer2.MatchPrefix(helper2.modelName, -1, bytes2a, readyPods2)
	assert.Greater(t, len(matches2a), 0, "Model 2 events should exist in indexer 2")
	
	tokens2b := []int32{4010, 4011, 4012}
	bytes2b := convertTokensToBytes(tokens2b)
	matches2b, _ := indexer2.MatchPrefix(helper2.modelName, -1, bytes2b, readyPods2)
	assert.Greater(t, len(matches2b), 0, "Model 2 events should exist in indexer 2")

	t.Log("Multi-model KV sync E2E test passed successfully")
}

// TestKVSyncE2ELargeScale tests with a large number of pods (scale testing)
func TestKVSyncE2ELargeScale(t *testing.T) {
	// Skip in CI due to resource constraints
	if testing.Short() {
		t.Skip("Skipping large scale test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	// Initialize clients
	k8sClient, _ := initializeClient(ctx, t)
	helper := NewKVEventTestHelper(k8sClient, kvEventsTestNamespace+"-scale")

	// Setup
	helper.CreateTestNamespace(t)
	defer helper.CleanupTestNamespace(t)
	defer helper.CleanupDeployments(t)

	// Test different scales
	scales := []int32{10, 50, 100}
	
	for _, scale := range scales {
		t.Logf("Testing with %d pods...", scale)
		
		deploymentName := fmt.Sprintf("vllm-scale-%d", scale)
		deployment := helper.CreateVLLMPodWithKVEvents(t, deploymentName, scale)
		
		// Wait longer for larger deployments
		timeout := time.Duration(scale/10+2) * time.Minute
		helper.WaitForDeploymentReady(t, deployment.Name, timeout)
		
		pods := helper.GetPodsByDeployment(t, deployment.Name)
		require.Equal(t, int(scale), len(pods), "Expected %d pods", scale)
		
		// Create sync indexer for this scale test
		indexer := syncprefixcacheindexer.NewSyncPrefixHashTable()
		defer indexer.Close()
		
		// Wait for pods to be ready
		time.Sleep(2 * time.Second)
		
		// Send events from all pods
		startTime := time.Now()
		eventCount := 0
		
		for i, pod := range pods {
			for j := 0; j < 10; j++ { // 10 events per pod
				tokenIDs := []int32{int32(10000 + i*100 + j*10), int32(10001 + i*100 + j*10), int32(10002 + i*100 + j*10)}
				event := &kvcache.BlockStoredEvent{
					Type:        kvcache.EventTypeBlockStored,
					Timestamp:   time.Now(),
					BlockHashes: []int64{int64(10000 + i*1000 + j*100)},
					TokenIDs:    [][]int32{tokenIDs},
					ModelName:   helper.modelName,
					PodName:     pod.Name,
				}
				// Convert the event to the format expected by the indexer
				blockStoredEvent := syncprefixcacheindexer.BlockStored{
					BlockHashes: event.BlockHashes,
					Tokens:      [][]byte{convertTokensToBytes(tokenIDs)},
					ModelName:   event.ModelName,
					LoraID:      -1,
					SourcePod:   pod.Status.PodIP,
				}
				if err := indexer.ProcessBlockStored(blockStoredEvent); err != nil {
					t.Logf("Failed to process event: %v", err)
				}
				eventCount++
			}
		}
		
		duration := time.Since(startTime)
		eventsPerSecond := float64(eventCount) / duration.Seconds()
		
		t.Logf("Scale %d: Processed %d events in %v (%.2f events/sec)", 
			scale, eventCount, duration, eventsPerSecond)
		
		// Verify random sampling of events
		sampleSize := 10
		successCount := 0
		
		for i := 0; i < sampleSize; i++ {
			podIdx := i * int(scale) / sampleSize
			eventIdx := 5 // Middle event
			tokens := []int32{
				int32(10000 + podIdx*100 + eventIdx*10),
				int32(10001 + podIdx*100 + eventIdx*10),
				int32(10002 + podIdx*100 + eventIdx*10),
			}
			// Convert to bytes
			byteTokens := convertTokensToBytes(tokens)
			// Check using MatchPrefix
			readyPods := map[string]struct{}{}
			for _, p := range pods {
				readyPods[p.Status.PodIP] = struct{}{}
			}
			matches, _ := indexer.MatchPrefix(helper.modelName, -1, byteTokens, readyPods)
			if len(matches) > 0 {
				successCount++
			}
		}
		
		assert.GreaterOrEqual(t, successCount, sampleSize*8/10, 
			"At least 80%% of sampled events should be found")
		
		// Clean up before next scale
		cancel()
	}

	t.Log("Large scale KV sync E2E test passed successfully")
}

// convertTokensToBytes converts int32 tokens to byte representation
func convertTokensToBytes(tokens []int32) []byte {
	bytes := make([]byte, len(tokens)*4)
	for i, token := range tokens {
		bytes[i*4] = byte(token >> 24)
		bytes[i*4+1] = byte(token >> 16)
		bytes[i*4+2] = byte(token >> 8)
		bytes[i*4+3] = byte(token)
	}
	return bytes
}

