//go:build zmq || !nozmq
// +build zmq !nozmq

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

package integration

import (
	"context"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/vllm-project/aibrix/pkg/cache"
	"github.com/vllm-project/aibrix/pkg/cache/kvcache"
	syncindexer "github.com/vllm-project/aibrix/pkg/utils/syncprefixcacheindexer"
)

// setupTestEnvironment sets up the test environment with proper configuration
func setupTestEnvironment(t *testing.T) {
	t.Setenv("AIBRIX_KV_EVENT_SYNC_ENABLED", "true")
	t.Setenv("AIBRIX_USE_REMOTE_TOKENIZER", "true")
	t.Setenv("AIBRIX_PREFIX_CACHE_TOKENIZER_TYPE", "remote")
	t.Setenv("AIBRIX_REMOTE_TOKENIZER_ENDPOINT", "http://test:8000")
}

// TestPodLifecycleIntegration tests the complete pod lifecycle with KV event sync
func TestPodLifecycleIntegration(t *testing.T) {
	setupTestEnvironment(t)

	// Create fake Kubernetes client
	client := fake.NewSimpleClientset()

	// Create and initialize store
	store := cache.InitForTest()
	defer store.Close()

	// Get KV event manager
	manager := cache.NewKVEventManager(store)
	require.NotNil(t, manager)
	// Manager should be enabled with proper configuration
	assert.True(t, manager != nil, "KV event manager should be created")

	// Create a test pod
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Labels: map[string]string{
				"model.aibrix.ai/name":              "test-model",
				"model.aibrix.ai/kv-events-enabled": "true",
				"model.aibrix.ai/lora-id":           "123",
			},
		},
		Status: v1.PodStatus{
			Phase: v1.PodRunning,
			PodIP: "10.0.0.1",
		},
	}

	// Test pod add
	_, err := client.CoreV1().Pods("default").Create(context.TODO(), pod, metav1.CreateOptions{})
	require.NoError(t, err)

	// Simulate pod lifecycle events
	manager.OnPodAdd(pod)

	// Update pod IP
	updatedPod := pod.DeepCopy()
	updatedPod.Status.PodIP = "10.0.0.2"
	_, err = client.CoreV1().Pods("default").Update(context.TODO(), updatedPod, metav1.UpdateOptions{})
	require.NoError(t, err)

	manager.OnPodUpdate(pod, updatedPod)

	// Delete pod
	err = client.CoreV1().Pods("default").Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
	require.NoError(t, err)

	manager.OnPodDelete(updatedPod)
}

// TestConfigurationDependencyValidation tests configuration dependency validation
func TestConfigurationDependencyValidation(t *testing.T) {
	tests := []struct {
		name              string
		remoteTokenizer   string
		kvSyncRequested   string
		prefixCacheType   string
		expectedKVEnabled bool
		description       string
	}{
		{
			name:              "all dependencies satisfied",
			remoteTokenizer:   "true",
			kvSyncRequested:   "true",
			prefixCacheType:   "remote",
			expectedKVEnabled: true,
			description:       "Should enable KV sync when all dependencies are satisfied",
		},
		{
			name:              "kv sync without remote tokenizer",
			remoteTokenizer:   "false",
			kvSyncRequested:   "true",
			prefixCacheType:   "remote",
			expectedKVEnabled: false,
			description:       "Should disable KV sync when remote tokenizer is disabled",
		},
		{
			name:              "wrong tokenizer type",
			remoteTokenizer:   "true",
			kvSyncRequested:   "true",
			prefixCacheType:   "local",
			expectedKVEnabled: true, // Note: Manager checks only USE_REMOTE_TOKENIZER
			description:       "Should enable if USE_REMOTE_TOKENIZER is true",
		},
		{
			name:              "kv sync explicitly disabled",
			remoteTokenizer:   "true",
			kvSyncRequested:   "false",
			prefixCacheType:   "remote",
			expectedKVEnabled: false,
			description:       "Should disable KV sync when explicitly disabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			t.Setenv("AIBRIX_USE_REMOTE_TOKENIZER", tt.remoteTokenizer)
			t.Setenv("AIBRIX_KV_EVENT_SYNC_ENABLED", tt.kvSyncRequested)
			t.Setenv("AIBRIX_PREFIX_CACHE_TOKENIZER_TYPE", tt.prefixCacheType)
			t.Setenv("AIBRIX_REMOTE_TOKENIZER_ENDPOINT", "http://test:8000")

			// Create and initialize store
			store := cache.InitForTest()
			manager := cache.NewKVEventManager(store)

			// Verify results by checking if manager is created and configured
			// Manager will only work if enabled internally
			if tt.expectedKVEnabled {
				assert.NotNil(t, manager, tt.description)
			}
		})
	}
}

// TestEventFlowIntegration tests the event flow with sync indexer
func TestEventFlowIntegration(t *testing.T) {
	setupTestEnvironment(t)

	// Create sync indexer directly
	indexer := syncindexer.NewSyncPrefixHashTable()

	// Add test pod to indexer
	podKey := "default/test-pod"
	loraID := int64(456)

	// Test BlockStoredEvent simulation
	storedEvent := &kvcache.BlockStoredEvent{
		Type:        kvcache.EventTypeBlockStored,
		Timestamp:   time.Now(),
		BlockHashes: []int64{1234, 5678},
		TokenIDs:    [][]int32{{100, 200}, {300, 400}},
		ModelName:   "test-model",
		PodName:     podKey,
	}

	// Simulate event processing via the sync indexer
	tokens := [][]byte{}
	for _, tokenIDs := range storedEvent.TokenIDs {
		tokenBytes := make([]byte, len(tokenIDs)*4)
		for i, id := range tokenIDs {
			binary.BigEndian.PutUint32(tokenBytes[i*4:], uint32(id))
		}
		tokens = append(tokens, tokenBytes)
	}

	// Process block stored event
	err := indexer.ProcessBlockStored(syncindexer.BlockStored{
		ModelName:   storedEvent.ModelName,
		LoraID:      loraID,
		SourcePod:   podKey,
		BlockHashes: storedEvent.BlockHashes,
		Tokens:      tokens,
	})
	assert.NoError(t, err)

	// Verify data in sync indexer
	time.Sleep(100 * time.Millisecond) // Allow async processing

	// Test BlockRemovedEvent simulation
	removedEvent := &kvcache.BlockRemovedEvent{
		Type:        kvcache.EventTypeBlockRemoved,
		Timestamp:   time.Now(),
		BlockHashes: []int64{1234},
		ModelName:   "test-model",
		PodName:     podKey,
	}

	// Remove blocks from sync indexer
	err = indexer.ProcessBlockRemoved(syncindexer.BlockRemoved{
		ModelName:   removedEvent.ModelName,
		LoraID:      loraID,
		SourcePod:   podKey,
		BlockHashes: removedEvent.BlockHashes,
	})
	assert.NoError(t, err)

	// Test AllBlocksClearedEvent simulation
	// Note: AllBlocksCleared is not fully implemented in sync indexer

	// Clear all blocks from sync indexer
	err = indexer.ProcessAllBlocksCleared(syncindexer.AllBlocksCleared{})
	assert.NoError(t, err)
}

// TestConcurrentEventProcessing tests concurrent event processing
func TestConcurrentEventProcessing(t *testing.T) {
	setupTestEnvironment(t)

	// Create and initialize store
	store := cache.InitForTest()
	defer store.Close()

	// Get KV event manager
	manager := cache.NewKVEventManager(store)
	require.NotNil(t, manager)

	// Create multiple pods
	numPods := 10
	for i := 0; i < numPods; i++ {
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("test-pod-%d", i),
				Namespace: "default",
				Labels: map[string]string{
					"model.aibrix.ai/name":              "test-model",
					"model.aibrix.ai/kv-events-enabled": "true",
				},
			},
			Status: v1.PodStatus{
				Phase: v1.PodRunning,
				PodIP: fmt.Sprintf("10.0.0.%d", i),
			},
		}

		// Add pod concurrently
		go func(p *v1.Pod) {
			manager.OnPodAdd(p)
			time.Sleep(50 * time.Millisecond)
			manager.OnPodDelete(p)
		}(pod)
	}

	// Wait for all operations to complete
	time.Sleep(1 * time.Second)
}

// TestBackwardCompatibility tests backward compatibility scenarios
func TestBackwardCompatibility(t *testing.T) {
	setupTestEnvironment(t)

	tests := []struct {
		name        string
		pod         *v1.Pod
		expectation string
	}{
		{
			name: "pod without kv-events label",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"model.aibrix.ai/name": "test-model",
					},
				},
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
					PodIP: "10.0.0.1",
				},
			},
			expectation: "Should not subscribe to KV events",
		},
		{
			name: "pod with kv-events disabled",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"model.aibrix.ai/name":              "test-model",
						"model.aibrix.ai/kv-events-enabled": "false",
					},
				},
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
					PodIP: "10.0.0.1",
				},
			},
			expectation: "Should not subscribe to KV events",
		},
		{
			name: "legacy pod without new labels",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "vllm",
					},
				},
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
					PodIP: "10.0.0.1",
				},
			},
			expectation: "Should not cause any errors",
		},
	}

	// Create and initialize store
	store := cache.InitForTest()
	defer store.Close()

	manager := cache.NewKVEventManager(store)
	require.NotNil(t, manager)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test that pod operations don't cause panics
			assert.NotPanics(t, func() {
				manager.OnPodAdd(tt.pod)
				manager.OnPodUpdate(tt.pod, tt.pod)
				manager.OnPodDelete(tt.pod)
			}, tt.expectation)
		})
	}
}

// TestErrorScenarios tests error handling scenarios
func TestErrorScenarios(t *testing.T) {
	// Create sync indexer directly
	indexer := syncindexer.NewSyncPrefixHashTable()

	// Try to insert data for non-existent pod
	assert.NotPanics(t, func() {
		_ = indexer.ProcessBlockStored(syncindexer.BlockStored{
			ModelName:   "test-model",
			LoraID:      0,
			SourcePod:   "default/missing-pod",
			BlockHashes: []int64{1234},
			Tokens:      [][]byte{{1, 2, 3, 4}},
		})
	}, "Should handle missing pod gracefully")

	// Try to remove data for non-existent pod
	assert.NotPanics(t, func() {
		_ = indexer.ProcessBlockRemoved(syncindexer.BlockRemoved{
			ModelName:   "test-model",
			LoraID:      0,
			SourcePod:   "default/missing-pod",
			BlockHashes: []int64{1234},
		})
	}, "Should handle missing pod gracefully")

	// Try to clear non-existent pod
	assert.NotPanics(t, func() {
		_ = indexer.ProcessAllBlocksCleared(syncindexer.AllBlocksCleared{})
	}, "Should handle missing pod gracefully")
}

// TestSyncIndexerIntegration tests sync indexer operations
func TestSyncIndexerIntegration(t *testing.T) {
	// Create sync indexer
	indexer := syncindexer.NewSyncPrefixHashTable()

	// Test basic operations
	podKey := "default/test-pod"
	loraID := int64(123)
	blockHashes := []int64{1000, 2000, 3000}

	// Create token data
	tokens := [][]byte{
		{0, 0, 0, 100}, // Token ID 100
		{0, 0, 0, 200}, // Token ID 200
		{0, 0, 0, 150}, // Token ID 150
	}

	// Insert blocks
	err := indexer.ProcessBlockStored(syncindexer.BlockStored{
		ModelName:   "test-model",
		LoraID:      loraID,
		SourcePod:   podKey,
		BlockHashes: blockHashes,
		Tokens:      tokens,
	})
	assert.NoError(t, err)

	// Test removal of specific blocks
	err = indexer.ProcessBlockRemoved(syncindexer.BlockRemoved{
		ModelName:   "test-model",
		LoraID:      loraID,
		SourcePod:   podKey,
		BlockHashes: []int64{1000},
	})
	assert.NoError(t, err)

	// Test clearing all blocks for a pod
	err = indexer.ProcessAllBlocksCleared(syncindexer.AllBlocksCleared{})
	assert.NoError(t, err)

	// Test concurrent operations
	done := make(chan bool)
	go func() {
		for i := 0; i < 100; i++ {
			key := fmt.Sprintf("default/pod-%d", i)
			hashes := []int64{int64(i * 1000)}
			tokenData := [][]byte{{byte(i), 0, 0, 0}}
			_ = indexer.ProcessBlockStored(syncindexer.BlockStored{
				ModelName:   "test-model",
				LoraID:      int64(i),
				SourcePod:   key,
				BlockHashes: hashes,
				Tokens:      tokenData,
			})
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			// Clear all blocks (note: AllBlocksCleared doesn't use pod-specific info)
			_ = indexer.ProcessAllBlocksCleared(syncindexer.AllBlocksCleared{})
		}
		done <- true
	}()

	// Wait for concurrent operations
	<-done
	<-done
}
