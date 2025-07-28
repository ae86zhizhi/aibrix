# KV Cache Event Synchronization System Design

## Authors
- AIBrix Team

## Status
- Implemented

## Summary

This proposal introduces a KV cache event synchronization system that enables real-time sharing of cached key-value states between vLLM inference pods and the AIBrix routing layer. This significantly improves inference performance by routing requests to pods that already have relevant cached prefixes.

## Motivation

### Problem Statement

In large-scale LLM serving deployments:
1. Each vLLM pod maintains its own KV cache for processed token sequences
2. The router has no visibility into which pods have cached specific prefixes
3. Requests are routed without considering cache availability
4. This leads to redundant computation when prefixes are re-processed on different pods

### Goals

1. **Cache Visibility**: Provide real-time visibility of KV cache contents across all pods
2. **Intelligent Routing**: Enable prefix-aware routing to maximize cache hits
3. **Performance**: Reduce inference latency by 30-50% for requests with common prefixes
4. **Scalability**: Support 100+ pods with millions of cached prefixes

### Non-Goals

1. Cross-pod cache sharing (only metadata is shared, not actual cache data)
2. Cache persistence across pod restarts
3. Guaranteed cache consistency (eventual consistency is acceptable)

## Proposal

### High-Level Architecture

```
┌─────────────────┐     ZMQ Events    ┌──────────────────┐
│   vLLM Pod 1    │ ─────────────────> │                  │
│ (KV Cache)      │                    │   KV Event       │
└─────────────────┘                    │   Manager        │
                                       │                  │
┌─────────────────┐                    │                  │     ┌─────────────────┐
│   vLLM Pod 2    │ ─────────────────> │                  │ ───>│ Prefix Cache    │
│ (KV Cache)      │                    │                  │     │ Indexer         │
└─────────────────┘                    └──────────────────┘     └─────────────────┘
                                                                         │
┌─────────────────┐                                                     │
│   Gateway       │ <───────────────────────────────────────────────────┘
│   Router        │         Query: Which pods have this prefix?
└─────────────────┘
```

### Key Components

#### 1. vLLM KV Event Publisher (External)
- Publishes cache events via ZMQ PUB socket
- Events: BlockStored, BlockRemoved, AllBlocksCleared
- Minimal performance impact (<1% overhead)

#### 2. ZMQ Client (`pkg/cache/kvcache`)
- Subscribes to vLLM event streams
- Handles reconnection and replay
- Decodes MessagePack events
- Maintains sequence tracking

#### 3. KV Event Manager (`pkg/cache`)
- Manages pod lifecycle (discovery, subscription, cleanup)
- Routes events to appropriate handlers
- Integrates with Kubernetes informers

#### 4. Prefix Cache Indexer (`pkg/utils/syncprefixcacheindexer`)
- Two-level hash table (model → prefix → pods)
- Thread-safe concurrent operations
- Automatic eviction of stale entries
- O(1) insert, O(n) prefix matching where n = prefix length

#### 5. Gateway Integration
- New routing algorithm: prefix-cache aware routing
- Falls back to existing algorithms if no cache hits
- Configurable via routing strategy

### Event Flow

1. **Block Storage**: When vLLM processes a sequence, it stores KV blocks and publishes a BlockStored event
2. **Event Reception**: ZMQ client receives the event and forwards to event manager
3. **Index Update**: Event manager updates the prefix cache indexer
4. **Routing Decision**: Gateway queries indexer for pods with matching prefixes
5. **Request Dispatch**: Request routed to pod with highest prefix match

### Data Structures

#### Event Schema
```go
type BlockStoredEvent struct {
    Type            EventType
    Timestamp       time.Time
    BlockHashes     []int64    // Hashes of stored blocks
    TokenIDs        [][]int32  // Token sequences
    ModelName       string
    PodName         string
}
```

#### Index Structure
```go
// First level: Model context
ModelContext = hash(ModelName + LoraID)

// Second level: Prefix to pods mapping
PrefixHash -> Set<PodIP>
```

### Performance Considerations

1. **Memory Usage**: ~1KB per cached sequence (metadata only)
2. **Latency**: <1ms for index updates, <5ms for prefix queries
3. **Throughput**: Supports 100K+ events/second
4. **Network**: ~100KB/s per pod for event traffic

### Configuration

Environment variables for tuning:
- `AIBRIX_KV_EVENT_SYNC_ENABLED`: Feature flag
- `AIBRIX_SYNC_MAX_CONTEXTS`: Max models (default: 1000)
- `AIBRIX_SYNC_MAX_PREFIXES_PER_CONTEXT`: Max prefixes per model (default: 10000)
- `AIBRIX_SYNC_EVICTION_INTERVAL_SECONDS`: Cleanup interval (default: 60)

## Design Decisions

### Why ZMQ?
- Low latency pub-sub messaging
- Reliable delivery with replay support
- Minimal CPU overhead
- Battle-tested in HPC environments

### Why MessagePack?
- 50% smaller than JSON
- Fast encoding/decoding
- Schema evolution support
- Native binary data handling

### Why Two-Level Hashing?
- Isolates models/adapters
- Enables parallel updates
- Reduces lock contention
- Supports multi-tenant deployments

## Alternatives Considered

1. **Redis Pub/Sub**: Higher latency, requires external service
2. **gRPC Streaming**: Higher overhead, complex lifecycle management
3. **Shared Memory**: Not viable across nodes
4. **Database Polling**: Too slow for real-time requirements

## Testing Strategy

### Unit Tests
- Component isolation with mocks
- Concurrent operation verification
- Memory leak detection

### Integration Tests
- Multi-pod event flow
- Kubernetes lifecycle handling
- Failure recovery

### E2E Tests
- Full system deployment
- Performance validation
- Chaos engineering

### Benchmarks
- Event processing throughput
- Index query latency
- Memory efficiency
- Scalability limits

## Migration Strategy

1. **Phase 1**: Deploy with feature flag disabled
2. **Phase 2**: Enable for pilot deployments
3. **Phase 3**: Gradual rollout with monitoring
4. **Phase 4**: Enable by default

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|---------|------------|
| Event storms | System overload | Rate limiting, backpressure |
| Memory growth | OOM | Automatic eviction, limits |
| Network partition | Stale data | TTL-based cleanup |
| Pod failures | Missing events | Replay mechanism |

## Future Work

1. **Cross-Region Sync**: Share cache metadata across regions
2. **Persistent Cache**: Survive pod restarts
3. **Cache Prewarming**: Proactive cache population
4. **Analytics**: Cache hit rate dashboards

## References

- [vLLM KV Cache Design](https://github.com/vllm-project/vllm)
- [ZeroMQ Patterns](https://zguide.zeromq.org/)
- [AIBrix Architecture](../architecture/)