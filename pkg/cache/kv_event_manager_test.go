//go:build zmq || !nozmq
// +build zmq !nozmq

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

package cache

import (
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/assert"
	"github.com/vllm-project/aibrix/pkg/cache/kvcache"
	syncindexer "github.com/vllm-project/aibrix/pkg/utils/syncprefixcacheindexer"
)

// TestKVEventManagerCreation tests the creation of KV event manager
func TestKVEventManagerCreation(t *testing.T) {
	tests := []struct {
		name            string
		kvSyncEnabled   string
		remoteTokenizer string
		expectedEnabled bool
	}{
		{
			name:            "both features enabled",
			kvSyncEnabled:   "true",
			remoteTokenizer: "true",
			expectedEnabled: true,
		},
		{
			name:            "kv sync disabled",
			kvSyncEnabled:   "false",
			remoteTokenizer: "true",
			expectedEnabled: false,
		},
		{
			name:            "remote tokenizer disabled",
			kvSyncEnabled:   "true",
			remoteTokenizer: "false",
			expectedEnabled: false,
		},
		{
			name:            "both features disabled",
			kvSyncEnabled:   "false",
			remoteTokenizer: "false",
			expectedEnabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			t.Setenv("AIBRIX_KV_EVENT_SYNC_ENABLED", tt.kvSyncEnabled)
			t.Setenv("AIBRIX_USE_REMOTE_TOKENIZER", tt.remoteTokenizer)

			store := &Store{}
			manager := NewKVEventManager(store)
			assert.NotNil(t, manager)
			assert.Equal(t, tt.expectedEnabled, manager.enabled)
		})
	}
}

// TestShouldSubscribe tests the pod subscription eligibility logic
func TestShouldSubscribe(t *testing.T) {
	store := &Store{}
	manager := NewKVEventManager(store)
	manager.enabled = true

	tests := []struct {
		name     string
		pod      *v1.Pod
		expected bool
	}{
		{
			name: "eligible pod",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"model.aibrix.ai/name":              "test-model",
						"model.aibrix.ai/kv-events-enabled": "true",
					},
				},
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
					PodIP: "10.0.0.1",
				},
			},
			expected: true,
		},
		{
			name: "kv events not enabled",
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
			expected: false,
		},
		{
			name: "pod not running",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"model.aibrix.ai/name":              "test-model",
						"model.aibrix.ai/kv-events-enabled": "true",
					},
				},
				Status: v1.PodStatus{
					Phase: v1.PodPending,
				},
			},
			expected: false,
		},
		{
			name: "no pod IP",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"model.aibrix.ai/name":              "test-model",
						"model.aibrix.ai/kv-events-enabled": "true",
					},
				},
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
				},
			},
			expected: false,
		},
		{
			name: "no model name",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"model.aibrix.ai/kv-events-enabled": "true",
					},
				},
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
					PodIP: "10.0.0.1",
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manager.shouldSubscribe(tt.pod)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestPodLifecycle tests pod add/update/delete lifecycle
func TestPodLifecycle(t *testing.T) {
	// Setup environment
	t.Setenv("AIBRIX_KV_EVENT_SYNC_ENABLED", "true")
	t.Setenv("AIBRIX_USE_REMOTE_TOKENIZER", "true")
	t.Setenv("AIBRIX_PREFIX_CACHE_TOKENIZER_TYPE", "remote")
	t.Setenv("AIBRIX_REMOTE_TOKENIZER_ENDPOINT", "http://test:8000")

	// Create store with sync indexer
	store := &Store{
		syncPrefixIndexer: syncindexer.NewSyncPrefixHashTable(),
	}

	manager := NewKVEventManager(store)
	assert.True(t, manager.enabled)

	// Test pod that should trigger subscription
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Labels: map[string]string{
				"model.aibrix.ai/name":              "test-model",
				"model.aibrix.ai/kv-events-enabled": "true",
			},
		},
		Status: v1.PodStatus{
			Phase: v1.PodRunning,
			PodIP: "10.0.0.1",
		},
	}

	// Note: We can't test actual ZMQ subscription without mocking the ZMQ client
	// This test verifies the logic flow

	// Test OnPodAdd
	manager.OnPodAdd(pod)
	// In real implementation, this would create a ZMQ subscriber

	// Test OnPodUpdate with IP change
	oldPod := pod.DeepCopy()
	newPod := pod.DeepCopy()
	newPod.Status.PodIP = "10.0.0.2"
	manager.OnPodUpdate(oldPod, newPod)

	// Test OnPodDelete
	manager.OnPodDelete(pod)

	// Clean up
	store.Close()
}

// TestVerifyRemoteTokenizer tests remote tokenizer verification
func TestVerifyRemoteTokenizer(t *testing.T) {
	tests := []struct {
		name           string
		tokenizerType  string
		endpoint       string
		expectedResult bool
	}{
		{
			name:           "properly configured",
			tokenizerType:  "remote",
			endpoint:       "http://test:8000",
			expectedResult: true,
		},
		{
			name:           "wrong tokenizer type",
			tokenizerType:  "local",
			endpoint:       "http://test:8000",
			expectedResult: false,
		},
		{
			name:           "missing endpoint",
			tokenizerType:  "remote",
			endpoint:       "",
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AIBRIX_PREFIX_CACHE_TOKENIZER_TYPE", tt.tokenizerType)
			t.Setenv("AIBRIX_REMOTE_TOKENIZER_ENDPOINT", tt.endpoint)

			store := &Store{}
			manager := NewKVEventManager(store)
			result := manager.verifyRemoteTokenizer()
			assert.Equal(t, tt.expectedResult, result)
		})
	}
}

// TestKVEventHandler tests the event handler implementation
func TestKVEventHandler(t *testing.T) {
	// Create a mock sync indexer
	store := &Store{
		syncPrefixIndexer: syncindexer.NewSyncPrefixHashTable(),
	}
	store.metaPods.Store("default/test-pod", &Pod{
		Pod: &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"model.aibrix.ai/lora-id": "123",
				},
			},
		},
	})

	manager := NewKVEventManager(store)
	handler := &kvEventHandler{
		manager:   manager,
		podKey:    "default/test-pod",
		modelName: "test-model",
	}

	// Test handleBlockStored
	storedEvent := &kvcache.BlockStoredEvent{
		Type:            kvcache.EventTypeBlockStored,
		Timestamp:       time.Now(),
		BlockHashes:     []int64{1234, 5678},
		TokenIDs:        [][]int32{{100, 200}, {300, 400}},
		ParentBlockHash: nil,
		ModelName:       "test-model",
		PodName:         "default/test-pod",
	}

	err := handler.HandleEvent(storedEvent)
	assert.NoError(t, err)

	// Test handleBlockRemoved
	removedEvent := &kvcache.BlockRemovedEvent{
		Type:        kvcache.EventTypeBlockRemoved,
		Timestamp:   time.Now(),
		BlockHashes: []int64{1234},
		ModelName:   "test-model",
		PodName:     "default/test-pod",
	}

	err = handler.HandleEvent(removedEvent)
	assert.NoError(t, err)

	// Test handleAllBlocksCleared
	clearedEvent := &kvcache.AllBlocksClearedEvent{
		Type:      kvcache.EventTypeAllCleared,
		Timestamp: time.Now(),
		ModelName: "test-model",
		PodName:   "default/test-pod",
	}

	err = handler.HandleEvent(clearedEvent)
	assert.NoError(t, err)
}

// TestGetLoraID tests LoRA ID extraction
func TestGetLoraID(t *testing.T) {
	store := &Store{}
	manager := NewKVEventManager(store)

	tests := []struct {
		name     string
		pod      *Pod
		expected int64
	}{
		{
			name: "valid lora ID",
			pod: &Pod{
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"model.aibrix.ai/lora-id": "456",
						},
					},
				},
			},
			expected: 456,
		},
		{
			name: "invalid lora ID",
			pod: &Pod{
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"model.aibrix.ai/lora-id": "invalid",
						},
					},
				},
			},
			expected: -1,
		},
		{
			name: "missing lora ID",
			pod: &Pod{
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{},
					},
				},
			},
			expected: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store.metaPods.Store("test/pod", tt.pod)
			handler := &kvEventHandler{
				manager:   manager,
				podKey:    "test/pod",
				modelName: "test-model",
			}
			result := handler.getLoraID()
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestTokenIDsToBytes tests token ID conversion
func TestTokenIDsToBytes(t *testing.T) {
	tests := []struct {
		name     string
		tokenIDs []int32
		expected []byte
	}{
		{
			name:     "empty tokens",
			tokenIDs: []int32{},
			expected: []byte{},
		},
		{
			name:     "single token",
			tokenIDs: []int32{12345},
			expected: []byte{0, 0, 48, 57}, // 12345 in big-endian
		},
		{
			name:     "multiple tokens",
			tokenIDs: []int32{256, 512},
			expected: []byte{0, 0, 1, 0, 0, 0, 2, 0}, // 256 and 512 in big-endian
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tokenIDsToBytes(tt.tokenIDs)
			assert.Equal(t, tt.expected, result)
		})
	}
}
