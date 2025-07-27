# AIBrix KV Cache Event Synchronization Design

## Executive Summary

This document describes the design and implementation of the KV cache event synchronization system between vLLM inference engines and AIBrix gateway. The system ensures real-time consistency between the prefix cache states in vLLM pods and the gateway's routing decisions.

## Architecture Overview

### Key Components

1. **vLLM Engine Side**
   - KV cache event publisher (ZMQ-based)
   - Publishing port: 5557 (tcp://*:5557)
   - Replay port: 5558 (tcp://*:5558)
   - Events: BlockStored, BlockRemoved, AllBlocksCleared

2. **AIBrix Gateway Side**
   - Remote tokenizer for consistent tokenization
   - Sync prefix hash indexer (sync_hash.go) - **always present**
   - KV event subscriber (optional enhancement)
   - Pod lifecycle management

3. **Hash Synchronization**
   - Gateway hash: Computed from tokenized input
   - Engine hash: Received from vLLM KV events
   - Mapping: Unidirectional engine → gateway hash mapping

### Backward Compatibility

The design ensures full backward compatibility:
- **Original indexer is used** when KV sync is disabled (`prefixcacheindexer.PrefixHashTable`)
- **Sync indexer is only created** when KV sync is properly enabled (`syncprefixcacheindexer.SyncPrefixHashTable`)
- The router automatically selects the appropriate indexer based on configuration
- This ensures the system behaves identically to the original when both features are disabled

## Current Implementation Status

### Completed Components

1. **Remote Tokenizer Integration**
   - Generic remote tokenizer interface
   - Support for vLLM and SGLang engines
   - HTTP client with retry logic
   - Engine-specific adapters

2. **Sync Prefix Hash Indexer**
   - Two-level hash structure: ModelContext → PrefixStore
   - Engine hash mapping management
   - Lock separation for concurrent access
   - Eviction and cleanup mechanisms

3. **Event Structures**
   - BlockStored: Contains engine hashes, tokens, parent hash
   - BlockRemoved: Contains engine hashes to remove
   - AllBlocksCleared: Placeholder for future use

### Components to be Implemented

1. **KV Event Subscriber Service**
   - ZMQ subscriber connections to vLLM pods
   - Event processing pipeline
   - Pod discovery and lifecycle management

2. **Gateway Integration**
   - Connection between cache system and sync hash indexer
   - Real-time prefix cache updates

## Data Flow

### 1. Request Processing Flow (Current)

```
User Request → Gateway
    ↓
Remote Tokenizer (vLLM endpoint)
    ↓
Tokenize Request → Token IDs
    ↓
Gateway Hash Computation (GetPrefixHashes)
    ↓
Add to sync_hash indexer via AddPrefix()
    ↓
Route to best matching pod
```

### 2. KV Event Flow (To be implemented)

```
vLLM Pod (KV operations)
    ↓
Generate BlockStored/BlockRemoved events
    ↓
Publish via ZMQ (port 5557)
    ↓
Gateway KV Event Subscriber
    ↓
Process events → Update sync_hash indexer
    - BlockStored → ProcessBlockStored()
    - BlockRemoved → ProcessBlockRemoved()
    ↓
Maintain engine → gateway hash mapping
```

## Detailed Design

### Remote Tokenizer Integration

The remote tokenizer ensures tokenization consistency between gateway and vLLM:

```go
// Configuration
type RemoteTokenizerConfig struct {
    Engine             string        // "vllm" or "sglang"
    Endpoint           string        // vLLM service endpoint
    Model              string        // Model identifier
    Timeout            time.Duration
    MaxRetries         int
    AddSpecialTokens   bool
}

// Usage in prefix cache router
tokenizer := tokenizer.NewRemoteTokenizer(config)
tokens, err := tokenizer.TokenizeInputText(prompt)
```

### Sync Hash Indexer Architecture

The sync hash indexer maintains prefix cache state with the following structure:

```go
// Two-level hierarchy
SyncPrefixHashTable
├── contextMap (sync.Map): ModelContext → ContextData
│   └── ContextData
│       ├── prefixStore (RWMutex protected)
│       │   └── prefixMap: gateway_hash → pods → PodInfo
│       └── hashMapping (RWMutex protected)
│           └── engineToAibrix: engine_hash → gateway_hash
└── blockIndex: engine_hash → []ModelContext (reverse index)
```

Key features:
- Lock-free context lookup using sync.Map
- Separate locks for prefix store and hash mapping
- Atomic operations for access time tracking
- Efficient eviction using background worker

### Hash Computation

Gateway uses xxhash with chained parent hashes:

```go
func (s *SyncPrefixHashTable) GetPrefixHashes(tokens []byte) []uint64 {
    prefixHashes := make([]uint64, 0)
    parentHash := s.seed // Initial parent hash
    
    for i := 0; i < len(tokens); i += s.blockSize {
        // Hash = xxhash(parentHash || blockTokens)
        digest.ResetWithSeed(s.seed)
        digest.Write(parentHashBytes)
        digest.Write(tokens[i:i+blockSize])
        
        currentHash := digest.Sum64()
        prefixHashes = append(prefixHashes, currentHash)
        parentHash = currentHash // Chain hashes
    }
    return prefixHashes
}
```

### Event Processing

#### BlockStored Event Processing

```go
func (s *SyncPrefixHashTable) ProcessBlockStored(event BlockStored) error {
    // 1. Get or create context
    contextData := s.getOrCreateContextData(ctx)
    
    // 2. Process blocks with mapping lock
    contextData.mappingMu.Lock()
    for i, engineBlockHash := range event.BlockHashes {
        // Determine parent hash for this block
        var parentAibrixHash uint64
        if i == 0 {
            // First block uses provided parent or seed
            if event.ParentBlockHash != nil {
                if mappedParent, exists := hashMapping.engineToAibrix[*event.ParentBlockHash]; exists {
                    parentAibrixHash = mappedParent
                } else {
                    // Parent not found, use seed
                    parentAibrixHash = s.seed
                }
            } else {
                parentAibrixHash = s.seed
            }
        } else {
            // Subsequent blocks chain from previous block
            parentAibrixHash = hashMapping.engineToAibrix[event.BlockHashes[i-1]]
        }
        
        // Compute gateway hash
        aibrixHash := s.computeHash(parentAibrixHash, event.Tokens[i])
        
        // Store mapping
        hashMapping.engineToAibrix[engineBlockHash] = aibrixHash
    }
    contextData.mappingMu.Unlock()
    
    // 3. Update prefix store
    contextData.prefixMu.Lock()
    // Add pod to prefix entries
    contextData.prefixMu.Unlock()
}
```

#### BlockRemoved Event Processing

```go
func (s *SyncPrefixHashTable) ProcessBlockRemoved(event BlockRemoved) error {
    // 1. Find affected contexts using reverse index
    // 2. Remove from hash mapping
    // 3. Remove from prefix store
}
```

## Future KV Event Subscriber Design

### Pod Discovery and Subscription

```go
// Extend existing pod informers
func (s *Store) addPod(pod *v1.Pod) {
    // Existing logic...
    
    // Check for KV events enabled
    if pod.Labels["model.aibrix.ai/kv-events-enabled"] == "true" &&
       pod.Status.Phase == v1.PodRunning && 
       pod.Status.PodIP != "" {
        s.startKVEventSubscriber(pod)
    }
}

// KV Event Subscriber
type kvEventSubscriber struct {
    podKey       string
    modelName    string
    subSocket    *zmq.Socket    // Port 5557
    replaySocket *zmq.Socket    // Port 5558
    indexer      *syncprefixcacheindexer.SyncPrefixHashTable
}
```

### Event Processing Pipeline

```go
func (s *Store) consumeKVEvents(ctx context.Context, subscriber *kvEventSubscriber) {
    // 1. Request full replay on startup
    // 2. Process event stream
    for {
        // Receive multipart message: [topic, sequence, payload]
        payload := subscriber.subSocket.RecvBytes()
        
        // Decode MessagePack batch
        var batch KVEventBatch
        decoder.Decode(payload, &batch)
        
        // Process each event
        for _, event := range batch.Events {
            switch e := event.(type) {
            case BlockStored:
                subscriber.indexer.ProcessBlockStored(e)
            case BlockRemoved:
                subscriber.indexer.ProcessBlockRemoved(e)
            }
        }
    }
}
```

## Configuration

### Feature Dependencies

**Important**: The KV event sync feature requires remote tokenizer to be enabled. This ensures tokenization consistency between gateway and vLLM engines.

```
Remote Tokenizer (required) → KV Event Sync (optional)
```

### vLLM Configuration

```yaml
env:
  - name: VLLM_ENABLE_KV_CACHE_EVENTS
    value: "true"
  - name: VLLM_KV_EVENTS_PUBLISHER
    value: "zmq"
  - name: VLLM_KV_EVENTS_ENDPOINT
    value: "tcp://*:5557"
  - name: VLLM_KV_EVENTS_REPLAY_ENDPOINT
    value: "tcp://*:5558"

labels:
  model.aibrix.ai/kv-events-enabled: "true"

ports:
  - name: kv-events
    port: 5557
    protocol: TCP
  - name: kv-replay
    port: 5558
    protocol: TCP
```

### Gateway Configuration

```yaml
# Feature control (ordered by dependency)
AIBRIX_USE_REMOTE_TOKENIZER: "true"          # Must be true to enable KV sync
AIBRIX_KV_EVENT_SYNC_ENABLED: "true"         # Requires remote tokenizer

# Remote tokenizer configuration
AIBRIX_PREFIX_CACHE_TOKENIZER_TYPE: "remote"
AIBRIX_REMOTE_TOKENIZER_ENGINE: "vllm"
AIBRIX_REMOTE_TOKENIZER_ENDPOINT: "http://vllm-service:8000"
AIBRIX_REMOTE_TOKENIZER_TIMEOUT: "30s"
AIBRIX_REMOTE_TOKENIZER_MAX_RETRIES: "3"

# Sync hash indexer configuration
AIBRIX_SYNC_MAX_CONTEXTS: 1000
AIBRIX_SYNC_MAX_PREFIXES_PER_CONTEXT: 10000
AIBRIX_SYNC_EVICTION_INTERVAL_SECONDS: 60
AIBRIX_SYNC_EVICTION_DURATION_MINUTES: 20
AIBRIX_PREFIX_CACHE_BLOCK_SIZE: 16
```

### Configuration Validation

The system enforces the following rules:
1. If `AIBRIX_KV_EVENT_SYNC_ENABLED=true` but `AIBRIX_USE_REMOTE_TOKENIZER=false`, KV sync will be automatically disabled with a warning
2. Remote tokenizer can be enabled independently without KV sync
3. Both features can be disabled for backward compatibility

## Benefits

1. **Real-time Synchronization**
   - Immediate prefix cache visibility
   - No polling or periodic syncs needed
   - Sub-millisecond update latency

2. **Scalability**
   - Efficient two-level hash structure
   - Lock-free lookups for hot paths
   - Automatic eviction for memory management

3. **Reliability**
   - Event replay for crash recovery
   - Idempotent event processing
   - Pod lifecycle management

4. **Performance**
   - Optimized concurrent access
   - Batch event processing
   - Minimal overhead on vLLM

## Implementation Plan

### Phase 1: Foundation (Completed)
- [x] Remote tokenizer implementation
- [x] Sync hash indexer with event processing
- [x] Event structure definitions

### Phase 2: KV Event Subscriber
- [ ] ZMQ client implementation
- [ ] Event decoder and processor
- [ ] Integration with pod informers

### Phase 3: Testing and Optimization
- [ ] Unit tests for event processing
- [ ] Integration tests with vLLM
- [ ] Performance benchmarking
- [ ] Memory usage optimization

### Phase 4: Production Readiness
- [ ] Monitoring and metrics
- [ ] Error handling and recovery
- [ ] Documentation and deployment guide

## Risks and Mitigations

1. **Network Failures**
   - Risk: ZMQ connection drops
   - Mitigation: Automatic reconnection with exponential backoff

2. **Memory Growth**
   - Risk: Unbounded prefix cache growth
   - Mitigation: Configurable limits and eviction policies

3. **Event Ordering**
   - Risk: Out-of-order event processing
   - Mitigation: Sequence number tracking and replay

4. **Pod Churn**
   - Risk: Frequent pod restarts causing cache invalidation
   - Mitigation: Graceful shutdown and cache persistence

## Conclusion

The KV cache event synchronization system provides a robust foundation for real-time prefix cache sharing between vLLM engines and AIBrix gateway. The current implementation of the sync hash indexer and remote tokenizer integration sets the stage for the final component - the KV event subscriber service.