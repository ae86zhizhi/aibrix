# End-to-End Testing Guide for KV Event Synchronization

## Overview

This guide provides comprehensive instructions for running and debugging E2E tests for the KV cache event synchronization system in AIBrix. The tests validate the complete event flow from vLLM to gateway routing decisions.

## Prerequisites

### Local Development

1. **Go 1.22+** with ZMQ support
2. **Docker** for building images
3. **Kind** or **K3s** for local Kubernetes cluster
4. **kubectl** configured to access the cluster
5. **ZMQ libraries**:
   - Ubuntu/Debian: `sudo apt-get install -y libzmq3-dev pkg-config`
   - macOS: `brew install zeromq pkg-config`

## Test Environment Setup

### 1. Create Kind Cluster

```bash
# Create cluster with specific configuration
kind create cluster --name aibrix-e2e --config kind-config.yaml

# Example kind-config.yaml:
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  extraPortMappings:
  - containerPort: 30000
    hostPort: 30000
    protocol: TCP
```

### 2. Build Test Images

```bash
# Build with ZMQ support
make docker-build-all

# Or specific components
make docker-build-gateway-plugins   # Requires ZMQ
make docker-build-controller-manager # No ZMQ needed
```

### 3. Load Images to Kind

```bash
kind load docker-image aibrix/controller-manager:nightly --name aibrix-e2e
kind load docker-image aibrix/gateway-plugins:nightly --name aibrix-e2e
kind load docker-image aibrix/vllm-mock:nightly --name aibrix-e2e
```

### 4. Deploy AIBrix Components

```bash
# Install CRDs
kubectl apply -k config/dependency --server-side

# Deploy controllers
kubectl apply -k config/test

# Wait for readiness
kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=controller-manager \
  -n aibrix-system --timeout=300s
```

## Running E2E Tests

### Full Test Suite

```bash
# Run all KV sync E2E tests with ZMQ build tag
go test -v -tags="zmq" ./test/e2e/kv_sync_e2e_test.go -timeout 30m
```

### Specific Test Scenarios

```bash
# Single pod happy path
go test -v -tags="zmq" ./test/e2e -run TestKVSyncE2EHappyPath

# Multi-pod synchronization
go test -v -tags="zmq" ./test/e2e -run TestKVSyncE2EMultiPod

# Pod lifecycle handling
go test -v -tags="zmq" ./test/e2e -run TestKVSyncE2EPodLifecycle

# Large scale test
go test -v -tags="zmq" ./test/e2e -run TestKVSyncE2ELargeScale
```

### Simple Integration Test

```bash
# Quick validation test
go test -v -tags="zmq" ./test/e2e/kv_sync_e2e_simple_test.go
```

## Test Structure

### Test Categories

1. **Unit Tests** (`pkg/cache/kvcache/*_test.go`)
   - ZMQ client functionality
   - Event encoding/decoding
   - Connection management

2. **Integration Tests** (`test/integration/kv_event_sync_test.go`)
   - Component interactions
   - Event flow validation
   - Mock vLLM responses

3. **E2E Tests** (`test/e2e/kv_sync_e2e_test.go`)
   - Full system deployment
   - Real Kubernetes resources
   - End-to-end event flow

### Key Test Helpers

```go
// KVEventTestHelper provides utilities for E2E tests
type KVEventTestHelper struct {
    k8sClient kubernetes.Interface
    namespace string
    modelName string
}

// Common test operations
helper.CreateVLLMPodWithKVEvents(t, "deployment-name", replicas)
helper.ValidateKVEventConnection(t, podIP)
helper.SimulateKVCacheEvents(t, pod, numEvents)
helper.ValidatePrefixCacheHit(t, expectedHitRate)
```

## Debugging Test Failures

### 1. Enable Debug Logging

```bash
# Set log level for tests
export AIBRIX_LOG_LEVEL=debug
export AIBRIX_KV_EVENT_DEBUG=true

# Run tests with verbose output
go test -v -tags="zmq" ./test/e2e -run TestName
```

### 2. Check Component Logs

```bash
# Gateway logs
kubectl logs -n aibrix-system -l app.kubernetes.io/name=gateway-plugins --tail=100

# Controller logs
kubectl logs -n aibrix-system -l app.kubernetes.io/name=controller-manager --tail=100

# vLLM mock logs
kubectl logs -l app=vllm-mock --tail=100
```

### 3. Verify ZMQ Connectivity

```bash
# Test ZMQ connection from gateway pod
kubectl exec -it <gateway-pod> -n aibrix-system -- sh
# Inside pod:
nc -zv <vllm-pod-ip> 5557
```

### 4. Check Event Flow

```bash
# Monitor events in real-time
kubectl logs -f <gateway-pod> -n aibrix-system | grep "KV event"

# Check sync indexer state
kubectl exec <gateway-pod> -n aibrix-system -- curl localhost:8080/debug/sync-indexer
```

## CI/CD Integration

### GitHub Actions

```yaml
name: KV Sync E2E Tests

on: [push, pull_request]

jobs:
  e2e-tests:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v3
    
    - name: Install ZMQ
      run: |
        sudo apt-get update
        sudo apt-get install -y libzmq3-dev pkg-config
    
    - name: Setup Go
      uses: actions/setup-go@v4
      with:
        go-version: '1.22'
    
    - name: Create Kind cluster
      run: |
        kind create cluster --name test
        kubectl cluster-info
    
    - name: Run E2E tests
      run: |
        make test-kv-sync-e2e
```

### Makefile Targets

```makefile
# Run KV sync E2E tests
test-kv-sync-e2e:
	go test -v -tags="zmq" ./test/e2e/kv_sync_e2e_test.go -timeout 30m

# Run with coverage
test-kv-sync-e2e-coverage:
	go test -v -tags="zmq" -coverprofile=coverage.out ./test/e2e/kv_sync_e2e_test.go
	go tool cover -html=coverage.out -o coverage.html
```

## Performance Testing

### Benchmark Tests

```bash
# Run KV sync benchmarks
go test -bench=. -tags="zmq" ./test/benchmark/kv_sync_bench_test.go

# With memory profiling
go test -bench=. -benchmem -tags="zmq" ./test/benchmark/kv_sync_bench_test.go
```

### Load Testing

```go
// Example load test configuration
func TestKVSyncE2ELoad(t *testing.T) {
    config := LoadTestConfig{
        NumPods:        10,
        EventsPerPod:   1000,
        EventInterval:  100 * time.Millisecond,
        TestDuration:   5 * time.Minute,
    }
    RunLoadTest(t, config)
}
```

## Best Practices

1. **Test Isolation**: Use unique namespaces for each test
2. **Resource Cleanup**: Always defer cleanup operations
3. **Timeout Management**: Set appropriate timeouts for operations
4. **Mock Services**: Use mock vLLM for functional tests
5. **Real Services**: Test with actual vLLM for integration validation

## Common Issues and Solutions

### ZMQ Build Errors

```bash
# Ensure ZMQ is properly linked
export CGO_ENABLED=1
export PKG_CONFIG_PATH=/usr/local/lib/pkgconfig

# Verify installation
pkg-config --cflags --libs libzmq
```

### Kind Cluster Issues

```bash
# Reset Kind cluster
kind delete cluster --name aibrix-e2e
kind create cluster --name aibrix-e2e

# Check cluster status
kubectl cluster-info --context kind-aibrix-e2e
```

### Test Timeouts

- Increase timeout values for slow environments
- Check for resource constraints (CPU/Memory)
- Verify network connectivity between pods

## Test Coverage

Current E2E test coverage includes:

- ✅ Single pod KV event publishing
- ✅ Multi-pod synchronization
- ✅ Pod lifecycle (create/delete)
- ✅ Large scale deployments (10+ pods)
- ✅ LoRA adapter support
- ✅ Event replay functionality
- ✅ Connection resilience
- ✅ Metrics validation