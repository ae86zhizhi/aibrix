# Task 001: ZMQ Client Implementation for KV Event Subscription

## Overview
Implement a ZMQ client library for subscribing to vLLM KV cache events. This client will be integrated into the AIBrix gateway plugin cache system.

## Background
vLLM publishes KV cache events via ZMQ on ports 5557 (PUB) and 5558 (ROUTER for replay). We need a Go client that can subscribe to these events and handle them reliably.

## Requirements

### Functional Requirements
1. Connect to vLLM ZMQ endpoints (PUB and ROUTER)
2. Subscribe to all KV cache events 
3. Request and process event replay on startup
4. Handle MessagePack deserialization
5. Implement reconnection logic with exponential backoff
6. Support graceful shutdown

### Non-Functional Requirements
1. Thread-safe operation
2. Minimal memory footprint
3. Low latency event processing (<1ms)
4. Comprehensive error handling and logging

## Technical Specification

### Dependencies
```go
// Add to go.mod
require (
    github.com/pebbe/zmq4 v1.2.10
    github.com/shamaton/msgpack/v2 v2.1.1
)
```

### File Structure
```
pkg/cache/kvcache/
├── zmq_client.go         // Core ZMQ client implementation
├── zmq_client_test.go    // Unit tests
├── event_types.go        // Event type definitions
└── msgpack_decoder.go    // MessagePack decoding utilities
```

### Core Types

```go
// pkg/cache/kvcache/event_types.go
package kvcache

import "time"

// EventType represents the type of KV cache event
type EventType string

// Note on Token Representation:
// - vLLM sends token IDs as []int32 arrays
// - Gateway expects tokens as []byte for hashing
// - Conversion: Each int32 is encoded as 4 bytes in big-endian format
// - Example: []int32{1, 2} becomes []byte{0, 0, 0, 1, 0, 0, 0, 2}

const (
    EventTypeBlockStored   EventType = "BLOCK_STORED"
    EventTypeBlockRemoved  EventType = "BLOCK_REMOVED"
    EventTypeAllCleared    EventType = "ALL_BLOCKS_CLEARED"
)

// KVEvent is the base interface for all KV cache events
type KVEvent interface {
    GetType() EventType
    GetTimestamp() time.Time
}

// BlockStoredEvent represents blocks being stored in KV cache
type BlockStoredEvent struct {
    Type            EventType `msgpack:"type"`
    Timestamp       time.Time `msgpack:"timestamp"`
    BlockHashes     []int64   `msgpack:"block_hashes"`
    TokenIDs        [][]int32 `msgpack:"token_ids"` // One array per block
    ParentBlockHash *int64    `msgpack:"parent_block_hash,omitempty"` // Parent hash for chaining
    ModelName       string    `msgpack:"model_name"`
    PodName         string    `msgpack:"-"` // Set by subscriber
}

// BlockRemovedEvent represents blocks being removed from KV cache
type BlockRemovedEvent struct {
    Type        EventType `msgpack:"type"`
    Timestamp   time.Time `msgpack:"timestamp"`
    BlockHashes []int64   `msgpack:"block_hashes"`
    ModelName   string    `msgpack:"model_name"`
    PodName     string    `msgpack:"-"` // Set by subscriber
}

// AllBlocksClearedEvent represents all blocks being cleared
type AllBlocksClearedEvent struct {
    Type      EventType `msgpack:"type"`
    Timestamp time.Time `msgpack:"timestamp"`
    ModelName string    `msgpack:"model_name"`
    PodName   string    `msgpack:"-"` // Set by subscriber
}

// EventBatch represents a batch of events from vLLM
type EventBatch struct {
    Events []KVEvent `msgpack:"events"`
}
```

### ZMQ Client Implementation

```go
// pkg/cache/kvcache/zmq_client.go
package kvcache

import (
    "context"
    "encoding/binary"
    "fmt"
    "sync"
    "time"

    zmq "github.com/pebbe/zmq4"
    "k8s.io/klog/v2"
)

// ZMQClient manages ZMQ connections to vLLM KV event publishers
type ZMQClient struct {
    podKey       string
    podIP        string
    modelName    string
    
    // ZMQ sockets
    subSocket    *zmq.Socket
    replaySocket *zmq.Socket
    
    // Event handler
    eventHandler EventHandler
    
    // State management
    mu           sync.RWMutex
    connected    bool
    lastSeq      int64
    
    // Configuration
    subEndpoint    string
    replayEndpoint string
    pollTimeout    time.Duration
    
    // Lifecycle
    ctx    context.Context
    cancel context.CancelFunc
    wg     sync.WaitGroup
}

// EventHandler processes received KV events
type EventHandler interface {
    HandleEvent(event KVEvent) error
}

// NewZMQClient creates a new ZMQ client for a vLLM pod
func NewZMQClient(podKey, podIP, modelName string, handler EventHandler) *ZMQClient {
    ctx, cancel := context.WithCancel(context.Background())
    
    return &ZMQClient{
        podKey:         podKey,
        podIP:          podIP,
        modelName:      modelName,
        eventHandler:   handler,
        subEndpoint:    fmt.Sprintf("tcp://%s:5557", podIP),
        replayEndpoint: fmt.Sprintf("tcp://%s:5558", podIP),
        pollTimeout:    100 * time.Millisecond,
        lastSeq:        -1,
        ctx:            ctx,
        cancel:         cancel,
    }
}

// Connect establishes ZMQ connections
func (c *ZMQClient) Connect() error {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    if c.connected {
        return nil
    }
    
    // Create SUB socket
    subSocket, err := zmq.NewSocket(zmq.SUB)
    if err != nil {
        return fmt.Errorf("failed to create SUB socket: %w", err)
    }
    
    if err := subSocket.Connect(c.subEndpoint); err != nil {
        subSocket.Close()
        return fmt.Errorf("failed to connect to %s: %w", c.subEndpoint, err)
    }
    
    // Subscribe to all messages
    if err := subSocket.SetSubscribe(""); err != nil {
        subSocket.Close()
        return fmt.Errorf("failed to subscribe: %w", err)
    }
    
    // Create DEALER socket for replay (to communicate with ROUTER)
    replaySocket, err := zmq.NewSocket(zmq.DEALER)
    if err != nil {
        subSocket.Close()
        return fmt.Errorf("failed to create DEALER socket: %w", err)
    }
    
    if err := replaySocket.Connect(c.replayEndpoint); err != nil {
        subSocket.Close()
        replaySocket.Close()
        return fmt.Errorf("failed to connect to replay endpoint: %w", err)
    }
    
    c.subSocket = subSocket
    c.replaySocket = replaySocket
    c.connected = true
    
    return nil
}

// Start begins event consumption
func (c *ZMQClient) Start() error {
    if err := c.Connect(); err != nil {
        return err
    }
    
    // Request full replay
    if err := c.requestReplay(0); err != nil {
        klog.Warningf("Failed to request replay for %s: %v", c.podKey, err)
    }
    
    c.wg.Add(1)
    go c.consumeEvents()
    
    return nil
}

// Stop gracefully shuts down the client
func (c *ZMQClient) Stop() {
    c.cancel()
    c.wg.Wait()
    
    c.mu.Lock()
    defer c.mu.Unlock()
    
    if c.subSocket != nil {
        c.subSocket.Close()
    }
    if c.replaySocket != nil {
        c.replaySocket.Close()
    }
    
    c.connected = false
}

// consumeEvents is the main event loop
func (c *ZMQClient) consumeEvents() {
    defer c.wg.Done()
    
    poller := zmq.NewPoller()
    poller.Add(c.subSocket, zmq.POLLIN)
    
    for {
        select {
        case <-c.ctx.Done():
            return
        default:
            // Poll with timeout
            polled, err := poller.Poll(c.pollTimeout)
            if err != nil {
                klog.Errorf("Poll error for %s: %v", c.podKey, err)
                continue
            }
            
            if len(polled) == 0 {
                continue
            }
            
            // Process message
            if err := c.processMessage(); err != nil {
                klog.Errorf("Failed to process message from %s: %v", c.podKey, err)
            }
        }
    }
}

// processMessage reads and processes a single message
func (c *ZMQClient) processMessage() error {
    // Receive multipart message: [topic, sequence, payload]
    topic, err := c.subSocket.RecvBytes(0)
    if err != nil {
        return fmt.Errorf("failed to receive topic: %w", err)
    }
    
    seqBytes, err := c.subSocket.RecvBytes(0)
    if err != nil {
        return fmt.Errorf("failed to receive sequence: %w", err)
    }
    
    payload, err := c.subSocket.RecvBytes(0)
    if err != nil {
        return fmt.Errorf("failed to receive payload: %w", err)
    }
    
    // Parse sequence number
    seq := int64(binary.BigEndian.Uint64(seqBytes))
    
    // Check for missed events
    c.mu.RLock()
    lastSeq := c.lastSeq
    c.mu.RUnlock()
    
    if lastSeq >= 0 && seq > lastSeq+1 {
        klog.Warningf("Missed %d events for %s (last=%d, current=%d)",
            seq-lastSeq-1, c.podKey, lastSeq, seq)
        // Could trigger replay here if needed
    }
    
    // Decode and process events
    batch, err := DecodeEventBatch(payload)
    if err != nil {
        return fmt.Errorf("failed to decode event batch: %w", err)
    }
    
    // Process each event
    for _, event := range batch.Events {
        // Add pod information
        switch e := event.(type) {
        case *BlockStoredEvent:
            e.PodName = c.podKey
        case *BlockRemovedEvent:
            e.PodName = c.podKey
        case *AllBlocksClearedEvent:
            e.PodName = c.podKey
        }
        
        if err := c.eventHandler.HandleEvent(event); err != nil {
            klog.Errorf("Failed to handle event: %v", err)
        }
    }
    
    // Update sequence
    c.mu.Lock()
    c.lastSeq = seq
    c.mu.Unlock()
    
    klog.V(5).Infof("Processed event batch %d from %s (%d events)",
        seq, c.podKey, len(batch.Events))
    
    return nil
}

// requestReplay requests event replay from a specific sequence
func (c *ZMQClient) requestReplay(fromSeq int64) error {
    // Send replay request
    reqData := make([]byte, 8)
    binary.BigEndian.PutUint64(reqData, uint64(fromSeq))
    
    if _, err := c.replaySocket.SendBytes(reqData, 0); err != nil {
        return fmt.Errorf("failed to send replay request: %w", err)
    }
    
    // Set receive timeout
    c.replaySocket.SetRcvtimeo(5 * time.Second)
    
    // Receive response
    _, err := c.replaySocket.RecvBytes(0)
    if err != nil {
        return fmt.Errorf("failed to receive replay response: %w", err)
    }
    
    klog.Infof("Successfully requested replay from seq %d for %s", fromSeq, c.podKey)
    return nil
}
```

### MessagePack Decoder

```go
// pkg/cache/kvcache/msgpack_decoder.go
package kvcache

import (
    "fmt"
    
    msgpack "github.com/shamaton/msgpack/v2"
)

// DecodeEventBatch decodes a MessagePack encoded event batch
func DecodeEventBatch(data []byte) (*EventBatch, error) {
    var raw map[string]interface{}
    if err := msgpack.Unmarshal(data, &raw); err != nil {
        return nil, fmt.Errorf("failed to unmarshal event batch: %w", err)
    }
    
    // Parse events array
    eventsRaw, ok := raw["events"].([]interface{})
    if !ok {
        return nil, fmt.Errorf("missing or invalid events field")
    }
    
    batch := &EventBatch{
        Events: make([]KVEvent, 0, len(eventsRaw)),
    }
    
    for _, eventRaw := range eventsRaw {
        event, err := parseEvent(eventRaw)
        if err != nil {
            return nil, fmt.Errorf("failed to parse event: %w", err)
        }
        batch.Events = append(batch.Events, event)
    }
    
    return batch, nil
}

// parseEvent parses a single event from raw data
func parseEvent(raw interface{}) (KVEvent, error) {
    eventMap, ok := raw.(map[string]interface{})
    if !ok {
        return nil, fmt.Errorf("invalid event format")
    }
    
    eventType, ok := eventMap["type"].(string)
    if !ok {
        return nil, fmt.Errorf("missing event type")
    }
    
    switch EventType(eventType) {
    case EventTypeBlockStored:
        return parseBlockStoredEvent(eventMap)
    case EventTypeBlockRemoved:
        return parseBlockRemovedEvent(eventMap)
    case EventTypeAllCleared:
        return parseAllBlocksClearedEvent(eventMap)
    default:
        return nil, fmt.Errorf("unknown event type: %s", eventType)
    }
}

// Implementation of individual event parsers...
```

## Testing Plan

### Unit Tests
1. Test ZMQ connection establishment
2. Test message decoding
3. Test event handling
4. Test reconnection logic
5. Test graceful shutdown

### Integration Tests
1. Test with mock vLLM publisher
2. Test replay functionality
3. Test high-volume event processing
4. Test network failure scenarios

## Implementation Steps

1. **Setup** (Day 1)
   - Add dependencies to go.mod
   - Create package structure
   - Define event types

2. **Core Implementation** (Day 2-3)
   - Implement ZMQ client
   - Implement MessagePack decoder
   - Add error handling and logging

3. **Testing** (Day 4)
   - Write unit tests
   - Create mock vLLM publisher for testing
   - Integration testing

4. **Documentation** (Day 5)
   - API documentation
   - Usage examples
   - Troubleshooting guide

## Success Criteria

1. Successfully connect to vLLM ZMQ endpoints
2. Process events with <1ms latency
3. Handle 10,000+ events/second
4. 90%+ test coverage
5. No memory leaks under sustained load

## Dependencies

- Task 002: Integration with cache system (blocked by this task)
- Task 003: Sync hash indexer integration (can proceed in parallel)

## Risks and Mitigations

1. **ZMQ Library Compatibility**
   - Risk: Go ZMQ bindings may have issues
   - Mitigation: Thoroughly test with vLLM, have fallback to pure Go implementation if needed

2. **MessagePack Format Changes**
   - Risk: vLLM event format may change
   - Mitigation: Version detection and flexible parsing

3. **Performance**
   - Risk: High event volume may cause bottlenecks
   - Mitigation: Implement batching and async processing