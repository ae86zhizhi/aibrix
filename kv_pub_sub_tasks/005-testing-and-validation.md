# Task 005: Testing and Validation Framework

## Overview
Implement comprehensive testing framework for the KV cache event synchronization system, including unit tests, integration tests, and end-to-end validation.

## Background
The KV event sync system involves multiple components across different codebases. We need thorough testing to ensure reliability, performance, and correctness.

## Requirements

### Functional Requirements
1. Unit tests for all new components
2. Integration tests for component interactions
3. End-to-end tests for complete flow
4. Performance benchmarks
5. Chaos testing for fault tolerance

### Non-Functional Requirements
1. 90%+ code coverage
2. Tests must run in CI/CD pipeline
3. Performance regression detection
4. Clear test documentation

## Test Structure

### Unit Tests

#### 1. ZMQ Client Tests
```go
// pkg/cache/kvcache/zmq_client_test.go
package kvcache

import (
    "context"
    "testing"
    "time"
    
    zmq "github.com/pebbe/zmq4"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/mock"
)

// MockEventHandler for testing
type MockEventHandler struct {
    mock.Mock
}

func (m *MockEventHandler) HandleEvent(event KVEvent) error {
    args := m.Called(event)
    return args.Error(0)
}

// TestZMQClientConnect tests client connection
func TestZMQClientConnect(t *testing.T) {
    // Create mock ZMQ publisher
    publisher := createMockPublisher(t, 15557, 15558)
    defer publisher.Close()
    
    handler := new(MockEventHandler)
    client := NewZMQClient("test-pod", "127.0.0.1:15557", "test-model", handler)
    
    // Test connection
    err := client.Connect()
    assert.NoError(t, err)
    assert.True(t, client.connected)
    
    // Test duplicate connection
    err = client.Connect()
    assert.NoError(t, err)
    
    client.Stop()
}

// TestZMQClientEventProcessing tests event reception
func TestZMQClientEventProcessing(t *testing.T) {
    publisher := createMockPublisher(t, 15557, 15558)
    defer publisher.Close()
    
    handler := new(MockEventHandler)
    client := NewZMQClient("test-pod", "127.0.0.1", "test-model", handler)
    
    // Set up handler expectations
    parentHash := int64(9999)
    blockStored := &BlockStoredEvent{
        Type:            EventTypeBlockStored,
        Timestamp:       time.Now(),
        BlockHashes:     []int64{1234, 5678},
        TokenIDs:        [][]int32{{1, 2, 3}, {4, 5, 6}},
        ParentBlockHash: &parentHash,
        ModelName:       "test-model",
    }
    
    handler.On("HandleEvent", mock.MatchedBy(func(e KVEvent) bool {
        bs, ok := e.(*BlockStoredEvent)
        return ok && len(bs.BlockHashes) == 2
    })).Return(nil)
    
    // Start client
    err := client.Start()
    assert.NoError(t, err)
    
    // Publish test event
    publisher.PublishEvent(blockStored)
    
    // Wait for processing
    time.Sleep(200 * time.Millisecond)
    
    // Verify handler was called
    handler.AssertExpectations(t)
    
    client.Stop()
}

// TestZMQClientReconnection tests reconnection logic
func TestZMQClientReconnection(t *testing.T) {
    // Test scenario: publisher goes down and comes back
    publisher := createMockPublisher(t, 15557, 15558)
    
    handler := new(MockEventHandler)
    client := NewZMQClient("test-pod", "127.0.0.1", "test-model", handler)
    
    err := client.Start()
    assert.NoError(t, err)
    
    // Stop publisher
    publisher.Close()
    
    // Wait and restart publisher
    time.Sleep(100 * time.Millisecond)
    publisher = createMockPublisher(t, 15557, 15558)
    defer publisher.Close()
    
    // Publish event after reconnection
    event := &BlockRemovedEvent{
        Type:        EventTypeBlockRemoved,
        Timestamp:   time.Now(),
        BlockHashes: []int64{1234},
        ModelName:   "test-model",
    }
    
    handler.On("HandleEvent", mock.Anything).Return(nil)
    publisher.PublishEvent(event)
    
    time.Sleep(200 * time.Millisecond)
    handler.AssertExpectations(t)
    
    client.Stop()
}

// Mock publisher helper
type mockPublisher struct {
    ctx      context.Context
    cancel   context.CancelFunc
    pubSock  *zmq.Socket
    repSock  *zmq.Socket
    sequence int64
}

func createMockPublisher(t *testing.T, pubPort, repPort int) *mockPublisher {
    ctx, cancel := context.WithCancel(context.Background())
    
    // Create PUB socket
    pubSock, err := zmq.NewSocket(zmq.PUB)
    assert.NoError(t, err)
    
    err = pubSock.Bind(fmt.Sprintf("tcp://*:%d", pubPort))
    assert.NoError(t, err)
    
    // Create ROUTER socket for replay
    repSock, err := zmq.NewSocket(zmq.ROUTER)
    assert.NoError(t, err)
    
    err = repSock.Bind(fmt.Sprintf("tcp://*:%d", repPort))
    assert.NoError(t, err)
    
    mp := &mockPublisher{
        ctx:     ctx,
        cancel:  cancel,
        pubSock: pubSock,
        repSock: repSock,
    }
    
    // Start replay handler
    go mp.handleReplay()
    
    return mp
}

func (mp *mockPublisher) PublishEvent(event KVEvent) error {
    // Encode event
    batch := &EventBatch{Events: []KVEvent{event}}
    data, err := EncodeEventBatch(batch)
    if err != nil {
        return err
    }
    
    // Send multipart message
    mp.sequence++
    seqBytes := make([]byte, 8)
    binary.BigEndian.PutUint64(seqBytes, uint64(mp.sequence))
    
    _, err = mp.pubSock.SendMessage("", seqBytes, data)
    return err
}

func (mp *mockPublisher) Close() {
    mp.cancel()
    mp.pubSock.Close()
    mp.repSock.Close()
}

func (mp *mockPublisher) handleReplay() {
    for {
        select {
        case <-mp.ctx.Done():
            return
        default:
            // Handle replay requests
            msg, err := mp.repSock.RecvMessageBytes(zmq.DONTWAIT)
            if err != nil {
                time.Sleep(10 * time.Millisecond)
                continue
            }
            
            if len(msg) >= 2 {
                // Send acknowledgment
                mp.repSock.SendMessage(msg[0], "OK")
            }
        }
    }
}
```

#### 2. Sync Hash Indexer Tests
```go
// pkg/utils/syncprefixcacheindexer/sync_hash_test.go
// (Already exists, ensure new event processing methods are tested)

func TestProcessBlockStoredEvent(t *testing.T) {
    indexer := NewSyncPrefixHashTable()
    defer indexer.Close()
    
    // Test basic block storage
    event := BlockStored{
        BlockHashes: []int64{1001, 1002, 1003},
        Tokens: [][]byte{
            {0, 0, 0, 1, 0, 0, 0, 2}, // 2 int32s
            {0, 0, 0, 3, 0, 0, 0, 4},
            {0, 0, 0, 5, 0, 0, 0, 6},
        },
        ModelName: "test-model",
        LoraID:    -1,
        SourcePod: "pod-1",
    }
    
    err := indexer.ProcessBlockStored(event)
    assert.NoError(t, err)
    
    // Verify mapping exists
    ctx := ModelContext{ModelName: "test-model", LoraID: -1}
    contextData, exists := indexer.contextMap.Load(ctx)
    assert.True(t, exists)
    
    cd := contextData.(*ContextData)
    cd.mappingMu.RLock()
    assert.Len(t, cd.hashMapping.engineToAibrix, 3)
    cd.mappingMu.RUnlock()
}

func TestProcessBlockRemovedEvent(t *testing.T) {
    indexer := NewSyncPrefixHashTable()
    defer indexer.Close()
    
    // First add blocks
    storeEvent := BlockStored{
        BlockHashes: []int64{2001, 2002},
        Tokens: [][]byte{
            {0, 0, 0, 1}, 
            {0, 0, 0, 2},
        },
        ModelName: "test-model",
        LoraID:    -1,
        SourcePod: "pod-1",
    }
    
    err := indexer.ProcessBlockStored(storeEvent)
    assert.NoError(t, err)
    
    // Remove one block
    removeEvent := BlockRemoved{
        BlockHashes: []int64{2001},
        ModelName:   "test-model",
        LoraID:      -1,
        SourcePod:   "pod-1",
    }
    
    err = indexer.ProcessBlockRemoved(removeEvent)
    assert.NoError(t, err)
    
    // Verify only one block remains
    ctx := ModelContext{ModelName: "test-model", LoraID: -1}
    contextData, _ := indexer.contextMap.Load(ctx)
    cd := contextData.(*ContextData)
    
    cd.mappingMu.RLock()
    assert.Len(t, cd.hashMapping.engineToAibrix, 1)
    _, exists := cd.hashMapping.engineToAibrix[2002]
    assert.True(t, exists)
    cd.mappingMu.RUnlock()
}

func TestConcurrentEventProcessing(t *testing.T) {
    indexer := NewSyncPrefixHashTable()
    defer indexer.Close()
    
    // Test concurrent events from multiple pods
    var wg sync.WaitGroup
    numPods := 10
    numEventsPerPod := 100
    
    for i := 0; i < numPods; i++ {
        wg.Add(1)
        go func(podID int) {
            defer wg.Done()
            
            for j := 0; j < numEventsPerPod; j++ {
                event := BlockStored{
                    BlockHashes: []int64{int64(podID*1000 + j)},
                    Tokens:      [][]byte{{byte(j), byte(j), byte(j), byte(j)}},
                    ModelName:   "test-model",
                    LoraID:      -1,
                    SourcePod:   fmt.Sprintf("pod-%d", podID),
                }
                
                err := indexer.ProcessBlockStored(event)
                assert.NoError(t, err)
            }
        }(i)
    }
    
    wg.Wait()
    
    // Verify all events were processed
    ctx := ModelContext{ModelName: "test-model", LoraID: -1}
    contextData, exists := indexer.contextMap.Load(ctx)
    assert.True(t, exists)
    
    cd := contextData.(*ContextData)
    cd.mappingMu.RLock()
    assert.Len(t, cd.hashMapping.engineToAibrix, numPods*numEventsPerPod)
    cd.mappingMu.RUnlock()
}
```

#### 3. Cache Integration Tests
```go
// pkg/cache/kv_event_manager_test.go
package cache

import (
    "testing"
    "time"
    
    v1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "github.com/stretchr/testify/assert"
)

func TestKVEventManagerPodLifecycle(t *testing.T) {
    // Create test store
    store := &Store{
        initialized: true,
    }
    
    manager := NewKVEventManager(store)
    manager.enabled = true
    
    // Test pod addition
    pod := &v1.Pod{
        ObjectMeta: metav1.ObjectMeta{
            Name:      "test-pod",
            Namespace: "default",
            Labels: map[string]string{
                "model.aibrix.ai/name":           "test-model",
                "model.aibrix.ai/kv-events-enabled": "true",
            },
        },
        Status: v1.PodStatus{
            Phase: v1.PodRunning,
            PodIP: "10.0.0.1",
        },
    }
    
    // Should subscribe
    manager.OnPodAdd(pod)
    
    // Verify subscription
    _, exists := manager.subscribers.Load("default/test-pod")
    assert.True(t, exists)
    
    // Test pod update with IP change
    newPod := pod.DeepCopy()
    newPod.Status.PodIP = "10.0.0.2"
    
    manager.OnPodUpdate(pod, newPod)
    
    // Should have resubscribed
    client, exists := manager.subscribers.Load("default/test-pod")
    assert.True(t, exists)
    assert.NotNil(t, client)
    
    // Test pod deletion
    manager.OnPodDelete(newPod)
    
    // Should be unsubscribed
    _, exists = manager.subscribers.Load("default/test-pod")
    assert.False(t, exists)
    
    manager.Stop()
}

func TestKVEventManagerConfigurationDependencies(t *testing.T) {
    tests := []struct {
        name               string
        remoteTokenizer    string
        kvSyncRequested    string
        expectedKVEnabled  bool
        expectedWarning    bool
    }{
        {
            name:              "both enabled",
            remoteTokenizer:   "true",
            kvSyncRequested:   "true",
            expectedKVEnabled: true,
            expectedWarning:   false,
        },
        {
            name:              "kv sync without remote tokenizer",
            remoteTokenizer:   "false",
            kvSyncRequested:   "true",
            expectedKVEnabled: false,
            expectedWarning:   true,
        },
        {
            name:              "remote tokenizer only",
            remoteTokenizer:   "true",
            kvSyncRequested:   "false",
            expectedKVEnabled: false,
            expectedWarning:   false,
        },
        {
            name:              "both disabled",
            remoteTokenizer:   "false",
            kvSyncRequested:   "false",
            expectedKVEnabled: false,
            expectedWarning:   false,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Set environment variables
            os.Setenv("AIBRIX_USE_REMOTE_TOKENIZER", tt.remoteTokenizer)
            os.Setenv("AIBRIX_KV_EVENT_SYNC_ENABLED", tt.kvSyncRequested)
            defer os.Unsetenv("AIBRIX_USE_REMOTE_TOKENIZER")
            defer os.Unsetenv("AIBRIX_KV_EVENT_SYNC_ENABLED")
            
            // Create manager
            store := &Store{initialized: true}
            manager := NewKVEventManager(store)
            
            // Check results
            assert.Equal(t, tt.expectedKVEnabled, manager.enabled)
        })
    }
}

func TestKVEventManagerShouldSubscribe(t *testing.T) {
    manager := &KVEventManager{enabled: true}
    
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
                        "model.aibrix.ai/name":           "model",
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
            name: "no kv events label",
            pod: &v1.Pod{
                ObjectMeta: metav1.ObjectMeta{
                    Labels: map[string]string{
                        "model.aibrix.ai/name": "model",
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
            name: "not running",
            pod: &v1.Pod{
                ObjectMeta: metav1.ObjectMeta{
                    Labels: map[string]string{
                        "model.aibrix.ai/name":           "model",
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
            name: "no IP",
            pod: &v1.Pod{
                ObjectMeta: metav1.ObjectMeta{
                    Labels: map[string]string{
                        "model.aibrix.ai/name":           "model",
                        "model.aibrix.ai/kv-events-enabled": "true",
                    },
                },
                Status: v1.PodStatus{
                    Phase: v1.PodRunning,
                    PodIP: "",
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
```

### Backward Compatibility Tests

```go
// test/integration/backward_compatibility_test.go
package integration

import (
    "testing"
    
    "github.com/stretchr/testify/assert"
    "github.com/vllm-project/aibrix/pkg/plugins/gateway/algorithms"
)

func TestPrefixCacheRouterBackwardCompatibility(t *testing.T) {
    tests := []struct {
        name              string
        remoteTokenizer   string
        kvSyncEnabled     string
        expectedIndexer   string
    }{
        {
            name:            "both disabled - uses original indexer",
            remoteTokenizer: "false",
            kvSyncEnabled:   "false",
            expectedIndexer: "prefixcacheindexer",
        },
        {
            name:            "only remote tokenizer - uses original indexer",
            remoteTokenizer: "true",
            kvSyncEnabled:   "false",
            expectedIndexer: "prefixcacheindexer",
        },
        {
            name:            "both enabled - uses sync indexer",
            remoteTokenizer: "true",
            kvSyncEnabled:   "true",
            expectedIndexer: "syncprefixcacheindexer",
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Set environment
            os.Setenv("AIBRIX_USE_REMOTE_TOKENIZER", tt.remoteTokenizer)
            os.Setenv("AIBRIX_KV_EVENT_SYNC_ENABLED", tt.kvSyncEnabled)
            defer os.Unsetenv("AIBRIX_USE_REMOTE_TOKENIZER")
            defer os.Unsetenv("AIBRIX_KV_EVENT_SYNC_ENABLED")
            
            // Create router
            router, err := algorithms.NewPrefixCacheRouter()
            assert.NoError(t, err)
            
            // Verify correct indexer is used
            r := router.(*prefixCacheRouter)
            if tt.expectedIndexer == "prefixcacheindexer" {
                assert.NotNil(t, r.prefixCacheIndexer)
                assert.False(t, r.useKVSync)
            } else {
                assert.NotNil(t, r.syncIndexer)
                assert.True(t, r.useKVSync)
            }
        })
    }
}
```

### Integration Tests

#### End-to-End Test
```go
// test/e2e/kv_event_sync_test.go
package e2e

import (
    "context"
    "fmt"
    "testing"
    "time"
    
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    appsv1 "k8s.io/api/apps/v1"
    v1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes"
)

func TestKVEventSyncEndToEnd(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping e2e test in short mode")
    }
    
    // Setup
    ctx := context.Background()
    k8sClient := getK8sClient(t)
    namespace := "test-kv-sync"
    
    // Create namespace
    createNamespace(t, k8sClient, namespace)
    defer deleteNamespace(t, k8sClient, namespace)
    
    // Deploy vLLM with KV events enabled
    vllmDeployment := createVLLMDeployment(namespace, "test-model", true)
    _, err := k8sClient.AppsV1().Deployments(namespace).Create(ctx, vllmDeployment, metav1.CreateOptions{})
    require.NoError(t, err)
    
    // Wait for vLLM to be ready
    waitForDeploymentReady(t, k8sClient, namespace, "test-model", 5*time.Minute)
    
    // Get pod IP
    pods, err := k8sClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
        LabelSelector: "model.aibrix.ai/name=test-model",
    })
    require.NoError(t, err)
    require.Len(t, pods.Items, 1)
    
    podIP := pods.Items[0].Status.PodIP
    require.NotEmpty(t, podIP)
    
    // Test 1: Send request through gateway
    gatewayEndpoint := getGatewayEndpoint(t)
    response := sendChatRequest(t, gatewayEndpoint, "test-model", "Hello, world!")
    assert.NotEmpty(t, response)
    
    // Test 2: Verify prefix cache hit on second request
    response2 := sendChatRequest(t, gatewayEndpoint, "test-model", "Hello, world! How are you?")
    assert.NotEmpty(t, response2)
    
    // Check metrics for prefix cache hit
    metrics := getGatewayMetrics(t)
    assert.Contains(t, metrics, "aibrix_prefix_cache_routing_decisions_total")
    
    // Test 3: Scale deployment and verify new pod gets events
    scale := int32(2)
    vllmDeployment.Spec.Replicas = &scale
    _, err = k8sClient.AppsV1().Deployments(namespace).Update(ctx, vllmDeployment, metav1.UpdateOptions{})
    require.NoError(t, err)
    
    waitForDeploymentReady(t, k8sClient, namespace, "test-model", 2*time.Minute)
    
    // Send requests and verify distribution
    for i := 0; i < 10; i++ {
        response := sendChatRequest(t, gatewayEndpoint, "test-model", fmt.Sprintf("Request %d", i))
        assert.NotEmpty(t, response)
    }
    
    // Verify both pods received requests
    pods, err = k8sClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
        LabelSelector: "model.aibrix.ai/name=test-model",
    })
    require.NoError(t, err)
    require.Len(t, pods.Items, 2)
}

func createVLLMDeployment(namespace, modelName string, kvEventsEnabled bool) *appsv1.Deployment {
    replicas := int32(1)
    deployment := &appsv1.Deployment{
        ObjectMeta: metav1.ObjectMeta{
            Name:      modelName,
            Namespace: namespace,
            Labels: map[string]string{
                "model.aibrix.ai/name": modelName,
            },
        },
        Spec: appsv1.DeploymentSpec{
            Replicas: &replicas,
            Selector: &metav1.LabelSelector{
                MatchLabels: map[string]string{
                    "model.aibrix.ai/name": modelName,
                },
            },
            Template: v1.PodTemplateSpec{
                ObjectMeta: metav1.ObjectMeta{
                    Labels: map[string]string{
                        "model.aibrix.ai/name": modelName,
                    },
                },
                Spec: v1.PodSpec{
                    Containers: []v1.Container{
                        {
                            Name:  "vllm",
                            Image: "vllm/vllm-openai:v0.7.1",
                            Command: []string{
                                "python3", "-m", "vllm.entrypoints.openai.api_server",
                                "--host", "0.0.0.0",
                                "--port", "8000",
                                "--model", "facebook/opt-125m", // Small model for testing
                                "--served-model-name", modelName,
                            },
                            Ports: []v1.ContainerPort{
                                {Name: "api", ContainerPort: 8000},
                            },
                            Resources: v1.ResourceRequirements{
                                Limits: v1.ResourceList{
                                    "nvidia.com/gpu": resource.MustParse("1"),
                                },
                                Requests: v1.ResourceList{
                                    "nvidia.com/gpu": resource.MustParse("1"),
                                },
                            },
                        },
                    },
                },
            },
        },
    }
    
    if kvEventsEnabled {
        // Add KV events configuration
        deployment.ObjectMeta.Labels["model.aibrix.ai/kv-events-enabled"] = "true"
        deployment.Spec.Template.ObjectMeta.Labels["model.aibrix.ai/kv-events-enabled"] = "true"
        
        container := &deployment.Spec.Template.Spec.Containers[0]
        container.Command = append(container.Command,
            "--enable-kv-cache-events",
            "--kv-events-publisher", "zmq",
            "--kv-events-endpoint", "tcp://*:5557",
            "--kv-events-replay-endpoint", "tcp://*:5558",
        )
        
        container.Ports = append(container.Ports,
            v1.ContainerPort{Name: "kv-events", ContainerPort: 5557},
            v1.ContainerPort{Name: "kv-replay", ContainerPort: 5558},
        )
    }
    
    return deployment
}
```

### Performance Tests

```go
// test/benchmark/kv_sync_bench_test.go
package benchmark

import (
    "testing"
    "time"
    
    syncindexer "github.com/vllm-project/aibrix/pkg/utils/syncprefixcacheindexer"
)

func BenchmarkSyncIndexerEventProcessing(b *testing.B) {
    indexer := syncindexer.NewSyncPrefixHashTable()
    defer indexer.Close()
    
    // Prepare events
    events := make([]syncindexer.BlockStored, 1000)
    for i := range events {
        events[i] = syncindexer.BlockStored{
            BlockHashes: []int64{int64(i * 3), int64(i*3 + 1), int64(i*3 + 2)},
            Tokens: [][]byte{
                make([]byte, 64),
                make([]byte, 64),
                make([]byte, 64),
            },
            ModelName: "test-model",
            LoraID:    -1,
            SourcePod: "pod-1",
        }
    }
    
    b.ResetTimer()
    
    for i := 0; i < b.N; i++ {
        event := events[i%len(events)]
        indexer.ProcessBlockStored(event)
    }
}

func BenchmarkPrefixMatching(b *testing.B) {
    indexer := syncindexer.NewSyncPrefixHashTable()
    defer indexer.Close()
    
    // Pre-populate with data
    for i := 0; i < 1000; i++ {
        tokens := make([]byte, 256)
        for j := range tokens {
            tokens[j] = byte(j % 256)
        }
        
        hashes := indexer.GetPrefixHashes(tokens)
        indexer.AddPrefix("test-model", -1, fmt.Sprintf("pod-%d", i%10), hashes)
    }
    
    // Prepare test tokens
    testTokens := make([]byte, 256)
    for i := range testTokens {
        testTokens[i] = byte(i % 256)
    }
    
    readyPods := make(map[string]struct{})
    for i := 0; i < 10; i++ {
        readyPods[fmt.Sprintf("pod-%d", i)] = struct{}{}
    }
    
    b.ResetTimer()
    
    for i := 0; i < b.N; i++ {
        indexer.MatchPrefix("test-model", -1, testTokens, readyPods)
    }
}
```

### Chaos Testing

```yaml
# test/chaos/network-delay.yaml
apiVersion: chaos-mesh.org/v1alpha1
kind: NetworkChaos
metadata:
  name: kv-event-delay
  namespace: test
spec:
  selector:
    labelSelectors:
      "app": "gateway-plugins"
  mode: all
  action: delay
  delay:
    latency: "50ms"
    jitter: "10ms"
  duration: "5m"
  
---
# test/chaos/pod-failure.yaml
apiVersion: chaos-mesh.org/v1alpha1
kind: PodChaos
metadata:
  name: vllm-pod-failure
  namespace: test
spec:
  selector:
    labelSelectors:
      "model.aibrix.ai/name": "test-model"
  mode: one
  action: pod-failure
  duration: "30s"
```

```bash
#!/bin/bash
# test/chaos/run-chaos-tests.sh

echo "Running chaos tests for KV event sync..."

# Test 1: Network delay
kubectl apply -f test/chaos/network-delay.yaml
sleep 30
./test/e2e/verify-sync-working.sh
kubectl delete -f test/chaos/network-delay.yaml

# Test 2: Pod failures
kubectl apply -f test/chaos/pod-failure.yaml
sleep 30
./test/e2e/verify-recovery.sh
kubectl delete -f test/chaos/pod-failure.yaml

# Test 3: High load
./test/load/high-volume-events.sh &
LOAD_PID=$!
sleep 60
./test/e2e/verify-performance.sh
kill $LOAD_PID

echo "Chaos tests completed"
```

## Test Coverage Requirements

### Unit Test Coverage
- ZMQ Client: 95%+
- Sync Hash Indexer: 90%+
- Cache Integration: 90%+
- Event Handlers: 95%+
- Router with both indexers: 90%+

### Integration Test Coverage
- Pod lifecycle scenarios: 100%
- Event flow scenarios: 90%+
- Error scenarios: 85%+

### E2E Test Coverage
- Basic functionality: 100%
- Scaling scenarios: 90%+
- Failure scenarios: 80%+

## CI/CD Integration

```yaml
# .github/workflows/kv-sync-tests.yml
name: KV Sync Tests

on:
  pull_request:
    paths:
      - 'pkg/cache/kvcache/**'
      - 'pkg/cache/kv_event_*.go'
      - 'pkg/utils/syncprefixcacheindexer/**'
      - 'pkg/plugins/gateway/algorithms/prefix_cache.go'

jobs:
  unit-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      
      - name: Install ZMQ
        run: |
          sudo apt-get update
          sudo apt-get install -y libzmq3-dev
      
      - name: Run unit tests
        run: |
          go test -v -cover ./pkg/cache/kvcache/...
          go test -v -cover ./pkg/cache/kv_event*_test.go
          go test -v -cover ./pkg/utils/syncprefixcacheindexer/...
      
      - name: Upload coverage
        uses: codecov/codecov-action@v3

  integration-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - name: Setup Kind cluster
        uses: helm/kind-action@v1.8.0
      
      - name: Deploy AIBrix
        run: |
          kubectl apply -k config/dependency
          kubectl apply -k config/default
      
      - name: Run integration tests
        run: |
          go test -v ./test/integration/kv_sync/...

  e2e-tests:
    runs-on: ubuntu-latest
    needs: [unit-tests, integration-tests]
    steps:
      - uses: actions/checkout@v3
      - name: Setup test environment
        run: ./test/e2e/setup.sh
      
      - name: Run E2E tests
        run: |
          go test -v -timeout 30m ./test/e2e/kv_event_sync_test.go
```

## Success Criteria

1. All tests pass consistently
2. Code coverage meets requirements
3. Performance benchmarks show no regression
4. Chaos tests demonstrate resilience
5. CI/CD pipeline integrates all tests

## Dependencies

- Tasks 001-004 must be completed
- Test infrastructure (Kind, etc.)
- Mock vLLM for testing

## Timeline

1. **Unit Tests** (Day 1-2)
   - ZMQ client tests
   - Integration tests
   - Mock implementations

2. **Integration Tests** (Day 3)
   - Pod lifecycle tests
   - Event flow tests

3. **E2E Tests** (Day 4)
   - Full system tests
   - Performance tests

4. **Chaos Tests** (Day 5)
   - Failure scenarios
   - Recovery testing

## Risks and Mitigations

1. **Test Flakiness**
   - Risk: Network-based tests may be flaky
   - Mitigation: Proper timeouts, retries, mock where possible

2. **Resource Requirements**
   - Risk: E2E tests need GPU nodes
   - Mitigation: Use CPU-only models for most tests

3. **Test Duration**
   - Risk: Full test suite takes too long
   - Mitigation: Parallelize, use test categories