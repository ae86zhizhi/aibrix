# Task 002: Cache System Integration for KV Event Processing

## Overview
Integrate the ZMQ client (from Task 001) into the existing AIBrix cache system. This includes pod lifecycle management, event routing to sync hash indexer, and maintaining subscriber state.

## Background
The AIBrix cache system already monitors pods via Kubernetes informers. We need to extend this to:
1. Detect vLLM pods with KV events enabled
2. Create and manage ZMQ subscribers per pod
3. Route events to the sync hash indexer

## Requirements

### Functional Requirements
1. Detect pods with `model.aibrix.ai/kv-events-enabled: "true"` label
2. Create ZMQ subscribers when pods become ready
3. Clean up subscribers when pods are deleted
4. Route KV events to sync hash indexer
5. Handle pod IP changes (restart scenarios)
6. Support multiple models concurrently

### Non-Functional Requirements
1. Zero impact on existing cache functionality
2. Efficient resource usage (connection pooling)
3. Fault tolerance (handle pod failures gracefully)
4. Observable (metrics and logging)

## Technical Specification

### Architecture Changes

```
Cache System (pkg/cache/)
├── Existing Components
│   ├── Pod Informers (watch all pods)
│   ├── Model Management 
│   └── Metrics Collection
└── New Components (this task)
    ├── KV Event Manager
    ├── ZMQ Subscriber Pool
    └── Event Router
```

### File Structure
```
pkg/cache/
├── kv_event_manager.go      // Main KV event management
├── kv_event_manager_test.go // Unit tests
├── kv_event_handler.go      // Event handler implementation
└── kvcache/                 // From Task 001
    └── ...
```

### Core Implementation

```go
// pkg/cache/kv_event_manager.go
package cache

import (
    "context"
    "fmt"
    "sync"
    
    v1 "k8s.io/api/core/v1"
    "k8s.io/klog/v2"
    
    "github.com/vllm-project/aibrix/pkg/cache/kvcache"
    "github.com/vllm-project/aibrix/pkg/utils"
    syncindexer "github.com/vllm-project/aibrix/pkg/utils/syncprefixcacheindexer"
)

// KVEventManager manages KV event subscriptions for vLLM pods
type KVEventManager struct {
    // Dependencies
    store       *Store
    
    // Subscriber management
    subscribers utils.SyncMap[string, *kvcache.ZMQClient] // podKey -> client
    
    // Configuration
    enabled bool
    
    // Lifecycle
    ctx    context.Context
    cancel context.CancelFunc
    wg     sync.WaitGroup
}

// NewKVEventManager creates a new KV event manager
func NewKVEventManager(store *Store) *KVEventManager {
    ctx, cancel := context.WithCancel(context.Background())
    
    // Check feature dependencies
    kvSyncRequested := utils.LoadEnvBool("AIBRIX_KV_EVENT_SYNC_ENABLED", false)
    remoteTokenizerEnabled := utils.LoadEnvBool("AIBRIX_USE_REMOTE_TOKENIZER", false)
    
    // Validate configuration
    enabled := kvSyncRequested
    if kvSyncRequested && !remoteTokenizerEnabled {
        klog.Warning("KV event sync requires remote tokenizer to be enabled. " +
            "Please set AIBRIX_USE_REMOTE_TOKENIZER=true to use KV event sync. " +
            "Disabling KV event sync.")
        enabled = false
    }
    
    return &KVEventManager{
        store:       store,
        enabled:     enabled,
        ctx:         ctx,
        cancel:      cancel,
    }
}

// Start initializes the KV event manager
func (m *KVEventManager) Start() error {
    if !m.enabled {
        klog.Info("KV event sync is disabled")
        return nil
    }
    
    // Double-check remote tokenizer is available
    if !m.verifyRemoteTokenizer() {
        klog.Error("Remote tokenizer not available, cannot start KV event sync")
        m.enabled = false
        return fmt.Errorf("remote tokenizer required for KV event sync")
    }
    
    klog.Info("Starting KV event manager with remote tokenizer support")
    
    // Process existing pods
    m.store.metaPods.Range(func(key string, pod *Pod) bool {
        if m.shouldSubscribe(pod.Pod) {
            if err := m.subscribeToPod(pod.Pod); err != nil {
                klog.Errorf("Failed to subscribe to existing pod %s: %v", key, err)
            }
        }
        return true
    })
    
    return nil
}

// verifyRemoteTokenizer checks if remote tokenizer is properly configured
func (m *KVEventManager) verifyRemoteTokenizer() bool {
    // Check if the cache/router has remote tokenizer configured
    if m.store == nil {
        return false
    }
    
    // Get the prefix cache router configuration
    tokenizerType := utils.LoadEnv("AIBRIX_PREFIX_CACHE_TOKENIZER_TYPE", "")
    if tokenizerType != "remote" {
        klog.Warning("AIBRIX_PREFIX_CACHE_TOKENIZER_TYPE must be 'remote' for KV sync")
        return false
    }
    
    // Check remote tokenizer endpoint
    endpoint := utils.LoadEnv("AIBRIX_REMOTE_TOKENIZER_ENDPOINT", "")
    if endpoint == "" {
        klog.Warning("AIBRIX_REMOTE_TOKENIZER_ENDPOINT not configured")
        return false
    }
    
    return true
}

// Stop gracefully shuts down the manager
func (m *KVEventManager) Stop() {
    klog.Info("Stopping KV event manager")
    
    m.cancel()
    
    // Stop all subscribers
    m.subscribers.Range(func(key string, client *kvcache.ZMQClient) bool {
        client.Stop()
        return true
    })
    
    m.wg.Wait()
    
    // Close sync indexer
    m.syncIndexer.Close()
}

// OnPodAdd handles new pod additions
func (m *KVEventManager) OnPodAdd(pod *v1.Pod) {
    if !m.enabled || !m.shouldSubscribe(pod) {
        return
    }
    
    if err := m.subscribeToPod(pod); err != nil {
        klog.Errorf("Failed to subscribe to pod %s: %v", 
            utils.GetPodKey(pod.Namespace, pod.Name), err)
    }
}

// OnPodUpdate handles pod updates
func (m *KVEventManager) OnPodUpdate(oldPod, newPod *v1.Pod) {
    if !m.enabled {
        return
    }
    
    podKey := utils.GetPodKey(newPod.Namespace, newPod.Name)
    shouldSubscribeOld := m.shouldSubscribe(oldPod)
    shouldSubscribeNew := m.shouldSubscribe(newPod)
    
    // Handle state transitions
    if !shouldSubscribeOld && shouldSubscribeNew {
        // Pod became eligible for subscription
        if err := m.subscribeToPod(newPod); err != nil {
            klog.Errorf("Failed to subscribe to pod %s: %v", podKey, err)
        }
    } else if shouldSubscribeOld && !shouldSubscribeNew {
        // Pod no longer eligible
        m.unsubscribeFromPod(podKey)
    } else if shouldSubscribeOld && shouldSubscribeNew {
        // Check if IP changed
        if oldPod.Status.PodIP != newPod.Status.PodIP {
            klog.Infof("Pod %s IP changed from %s to %s, resubscribing",
                podKey, oldPod.Status.PodIP, newPod.Status.PodIP)
            m.unsubscribeFromPod(podKey)
            if err := m.subscribeToPod(newPod); err != nil {
                klog.Errorf("Failed to resubscribe to pod %s: %v", podKey, err)
            }
        }
    }
}

// OnPodDelete handles pod deletion
func (m *KVEventManager) OnPodDelete(pod *v1.Pod) {
    if !m.enabled {
        return
    }
    
    podKey := utils.GetPodKey(pod.Namespace, pod.Name)
    m.unsubscribeFromPod(podKey)
    
    // Clean up from sync indexer
    syncIndexer := m.store.GetSyncPrefixIndexer()
    if syncIndexer != nil {
        modelName := pod.Labels["model.aibrix.ai/name"]
        if modelName != "" {
            loraID := int64(-1) // Default no LoRA
            if loraStr := pod.Labels["model.aibrix.ai/lora-id"]; loraStr != "" {
                // Parse LoRA ID if present
            }
            
            if err := syncIndexer.RemovePrefix(modelName, loraID, podKey); err != nil {
                klog.Errorf("Failed to remove prefix for pod %s: %v", podKey, err)
            }
        }
    }
}

// shouldSubscribe checks if a pod should have KV event subscription
func (m *KVEventManager) shouldSubscribe(pod *v1.Pod) bool {
    // Check if KV events are enabled
    if pod.Labels["model.aibrix.ai/kv-events-enabled"] != "true" {
        return false
    }
    
    // Check if pod is ready
    if pod.Status.Phase != v1.PodRunning || pod.Status.PodIP == "" {
        return false
    }
    
    // Check if it's a model pod
    if pod.Labels["model.aibrix.ai/name"] == "" {
        return false
    }
    
    return true
}

// subscribeToPod creates a ZMQ subscription for a pod
func (m *KVEventManager) subscribeToPod(pod *v1.Pod) error {
    podKey := utils.GetPodKey(pod.Namespace, pod.Name)
    modelName := pod.Labels["model.aibrix.ai/name"]
    
    // Check if already subscribed
    if _, exists := m.subscribers.Load(podKey); exists {
        return nil
    }
    
    // Create event handler
    handler := &kvEventHandler{
        manager:   m,
        podKey:    podKey,
        modelName: modelName,
    }
    
    // Create ZMQ client
    client := kvcache.NewZMQClient(podKey, pod.Status.PodIP, modelName, handler)
    
    // Start subscription
    if err := client.Start(); err != nil {
        return fmt.Errorf("failed to start ZMQ client: %w", err)
    }
    
    // Store subscriber
    m.subscribers.Store(podKey, client)
    
    klog.Infof("Subscribed to KV events for pod %s (model: %s, IP: %s)",
        podKey, modelName, pod.Status.PodIP)
    
    return nil
}

// unsubscribeFromPod removes a ZMQ subscription
func (m *KVEventManager) unsubscribeFromPod(podKey string) {
    client, exists := m.subscribers.LoadAndDelete(podKey)
    if !exists {
        return
    }
    
    client.Stop()
    
    klog.Infof("Unsubscribed from KV events for pod %s", podKey)
}
```

### Event Handler Implementation

```go
// pkg/cache/kv_event_handler.go
package cache

import (
    "encoding/binary"
    "fmt"
    "strconv"
    
    "github.com/vllm-project/aibrix/pkg/cache/kvcache"
    syncindexer "github.com/vllm-project/aibrix/pkg/utils/syncprefixcacheindexer"
    "k8s.io/klog/v2"
)

// kvEventHandler implements the EventHandler interface
type kvEventHandler struct {
    manager   *KVEventManager
    podKey    string
    modelName string
}

// HandleEvent processes a KV cache event
func (h *kvEventHandler) HandleEvent(event kvcache.KVEvent) error {
    switch e := event.(type) {
    case *kvcache.BlockStoredEvent:
        return h.handleBlockStored(e)
    case *kvcache.BlockRemovedEvent:
        return h.handleBlockRemoved(e)
    case *kvcache.AllBlocksClearedEvent:
        return h.handleAllBlocksCleared(e)
    default:
        klog.Warningf("Unknown event type: %T", event)
        return nil
    }
}

// handleBlockStored processes BlockStored events
func (h *kvEventHandler) handleBlockStored(event *kvcache.BlockStoredEvent) error {
    // Get sync indexer from store
    syncIndexer := h.manager.store.GetSyncPrefixIndexer()
    if syncIndexer == nil {
        return fmt.Errorf("sync indexer not available")
    }
    
    // Convert to sync indexer event format
    syncEvent := syncindexer.BlockStored{
        BlockHashes:     event.BlockHashes,
        ModelName:       h.modelName,
        LoraID:          h.getLoraID(),
        SourcePod:       h.podKey,
        ParentBlockHash: event.ParentBlockHash, // Use the parent hash from event
    }
    
    // Convert token IDs to byte arrays
    syncEvent.Tokens = make([][]byte, len(event.TokenIDs))
    for i, tokenIDs := range event.TokenIDs {
        syncEvent.Tokens[i] = tokenIDsToBytes(tokenIDs)
    }
    
    // Process event
    if err := syncIndexer.ProcessBlockStored(syncEvent); err != nil {
        klog.Errorf("Failed to process BlockStored event: %v", err)
        return err
    }
    
    klog.V(4).Infof("Processed BlockStored event: %d blocks for pod %s",
        len(event.BlockHashes), h.podKey)
    
    return nil
}

// handleBlockRemoved processes BlockRemoved events
func (h *kvEventHandler) handleBlockRemoved(event *kvcache.BlockRemovedEvent) error {
    // Get sync indexer from store
    syncIndexer := h.manager.store.GetSyncPrefixIndexer()
    if syncIndexer == nil {
        return fmt.Errorf("sync indexer not available")
    }
    
    syncEvent := syncindexer.BlockRemoved{
        BlockHashes: event.BlockHashes,
        ModelName:   h.modelName,
        LoraID:      h.getLoraID(),
        SourcePod:   h.podKey,
    }
    
    if err := syncIndexer.ProcessBlockRemoved(syncEvent); err != nil {
        klog.Errorf("Failed to process BlockRemoved event: %v", err)
        return err
    }
    
    klog.V(4).Infof("Processed BlockRemoved event: %d blocks for pod %s",
        len(event.BlockHashes), h.podKey)
    
    return nil
}

// handleAllBlocksCleared processes AllBlocksCleared events
func (h *kvEventHandler) handleAllBlocksCleared(event *kvcache.AllBlocksClearedEvent) error {
    // Current implementation is a no-op as per requirements
    klog.V(4).Infof("Received AllBlocksCleared event for pod %s (not implemented)", h.podKey)
    return nil
}

// getLoraID extracts LoRA ID from pod metadata in a thread-safe manner
func (h *kvEventHandler) getLoraID() int64 {
    // Thread-safe access to pod metadata
    h.manager.mu.RLock()
    defer h.manager.mu.RUnlock()
    
    // Look up pod from cache
    if pod, exists := h.manager.store.metaPods.Load(h.podKey); exists {
        if metaPod, ok := pod.(*Pod); ok {
            // Extract LoRA ID from labels
            if loraStr, exists := metaPod.Pod.Labels["model.aibrix.ai/lora-id"]; exists {
                if loraID, err := strconv.ParseInt(loraStr, 10, 64); err == nil {
                    return loraID
                }
            }
        }
    }
    
    return -1 // Default: no LoRA adapter
}

// tokenIDsToBytes converts int32 token IDs to byte array
func tokenIDsToBytes(tokenIDs []int32) []byte {
    bytes := make([]byte, len(tokenIDs)*4)
    for i, id := range tokenIDs {
        binary.BigEndian.PutUint32(bytes[i*4:], uint32(id))
    }
    return bytes
}
```

### Integration Points

#### 1. Modify Store Structure (cache_init.go)
```go
// Add to Store struct
type Store struct {
    // ... existing fields ...
    
    // Sync prefix indexer - only created when KV sync is enabled
    syncPrefixIndexer *syncindexer.SyncPrefixHashTable
    
    // KV event management - optional enhancement
    kvEventManager *KVEventManager
}
```

#### 2. Initialize in Cache Creation
```go
// In InitForGateway or similar initialization function
func (s *Store) initCache() error {
    // Check if KV sync should be enabled
    kvSyncEnabled := utils.LoadEnvBool("AIBRIX_KV_EVENT_SYNC_ENABLED", false)
    remoteTokenizerEnabled := utils.LoadEnvBool("AIBRIX_USE_REMOTE_TOKENIZER", false)
    
    // Create sync indexer only if KV sync is properly configured
    if kvSyncEnabled && remoteTokenizerEnabled {
        s.syncPrefixIndexer = syncindexer.NewSyncPrefixHashTable()
        
        // Enable KV event sync
        s.kvEventManager = NewKVEventManager(s)
        if err := s.kvEventManager.Start(); err != nil {
            klog.Warningf("Failed to start KV event sync: %v", err)
            // Continue without KV sync
        }
    } else if kvSyncEnabled && !remoteTokenizerEnabled {
        klog.Warning("KV sync requires remote tokenizer, feature disabled")
    }
    
    return nil
}

// Cleanup
func (s *Store) Close() {
    if s.syncPrefixIndexer != nil {
        s.syncPrefixIndexer.Close()
    }
    if s.kvEventManager != nil {
        s.kvEventManager.Stop()
    }
}
```

#### 3. Hook into Pod Informers (informers.go)
```go
// Modify existing pod event handlers
func (s *Store) addPod(obj interface{}) {
    pod := obj.(*v1.Pod)
    
    // Existing logic...
    
    // New: Notify KV event manager
    if s.kvEventManager != nil {
        s.kvEventManager.OnPodAdd(pod)
    }
}

func (s *Store) updatePod(oldObj, newObj interface{}) {
    oldPod := oldObj.(*v1.Pod)
    newPod := newObj.(*v1.Pod)
    
    // Existing logic...
    
    // New: Notify KV event manager
    if s.kvEventManager != nil {
        s.kvEventManager.OnPodUpdate(oldPod, newPod)
    }
}

func (s *Store) deletePod(obj interface{}) {
    pod := obj.(*v1.Pod)
    
    // New: Notify KV event manager first (before cleanup)
    if s.kvEventManager != nil {
        s.kvEventManager.OnPodDelete(pod)
    }
    
    // Existing logic...
}
```

#### 4. Expose Sync Indexer for Routing
```go
// Add method to Cache interface
type Cache interface {
    // ... existing methods ...
    
    // GetSyncPrefixIndexer returns the sync prefix hash indexer
    GetSyncPrefixIndexer() *syncindexer.SyncPrefixHashTable
}

// Implement in Store
func (s *Store) GetSyncPrefixIndexer() *syncindexer.SyncPrefixHashTable {
    // Return sync indexer only if KV sync is enabled
    // Router will fall back to original indexer if this returns nil
    return s.syncPrefixIndexer
}
```

## Configuration

### Environment Variables
```bash
# Feature dependencies (order matters)
AIBRIX_USE_REMOTE_TOKENIZER=true        # REQUIRED for KV event sync
AIBRIX_KV_EVENT_SYNC_ENABLED=true       # Depends on remote tokenizer

# Remote tokenizer configuration (required when KV sync enabled)
AIBRIX_PREFIX_CACHE_TOKENIZER_TYPE=remote
AIBRIX_REMOTE_TOKENIZER_ENGINE=vllm
AIBRIX_REMOTE_TOKENIZER_ENDPOINT=http://vllm-service:8000

# ZMQ configuration
AIBRIX_KV_EVENT_POLL_TIMEOUT_MS=100
AIBRIX_KV_EVENT_REPLAY_TIMEOUT_S=5

# Sync indexer configuration (from existing)
AIBRIX_SYNC_MAX_CONTEXTS=1000
AIBRIX_SYNC_MAX_PREFIXES_PER_CONTEXT=10000
```

### Configuration Validation

The system enforces the following dependency:
- KV event sync can only be enabled if remote tokenizer is enabled
- If `AIBRIX_KV_EVENT_SYNC_ENABLED=true` but `AIBRIX_USE_REMOTE_TOKENIZER=false`, the system will:
  1. Log a warning explaining the dependency
  2. Automatically disable KV event sync
  3. Continue operating without KV sync (fallback to standard routing)

### Pod Labels
```yaml
labels:
  model.aibrix.ai/name: "model-name"
  model.aibrix.ai/kv-events-enabled: "true"
  model.aibrix.ai/lora-id: "-1" # Optional
```

## Testing Plan

### Unit Tests
1. Test pod eligibility detection
2. Test subscriber lifecycle management
3. Test event routing to sync indexer
4. Test configuration loading
5. Test error handling

### Integration Tests
1. Test with mock Kubernetes API
2. Test pod state transitions
3. Test concurrent pod operations
4. Test memory usage under load

## Implementation Steps

1. **Setup** (Day 1)
   - Create KV event manager structure
   - Add configuration loading

2. **Core Implementation** (Day 2-3)
   - Implement pod subscription logic
   - Implement event handler
   - Integrate with existing informers

3. **Testing** (Day 4)
   - Unit tests with mocks
   - Integration tests
   - Load testing

4. **Documentation** (Day 5)
   - Update cache documentation
   - Add operational guide
   - Performance tuning guide

## Success Criteria

1. Seamless integration with existing cache system
2. Automatic subscription to eligible pods
3. Proper cleanup on pod deletion
4. No memory leaks
5. Minimal performance impact (<1% CPU overhead)

## Dependencies

- Task 001: ZMQ client implementation (must be completed first)
- Task 003: Can proceed in parallel

## Risks and Mitigations

1. **Performance Impact**
   - Risk: Event processing may slow down cache operations
   - Mitigation: Async processing, separate goroutines

2. **Memory Usage**
   - Risk: Large number of pods may consume significant memory
   - Mitigation: Connection pooling, event batching

3. **Pod Churn**
   - Risk: Frequent pod restarts may cause subscription storms
   - Mitigation: Rate limiting, exponential backoff