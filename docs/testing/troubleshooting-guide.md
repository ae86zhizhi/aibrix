# Troubleshooting Guide for KV Event Sync Testing

This guide helps diagnose and resolve common issues encountered during KV event synchronization testing.

## Table of Contents
- [Common Test Failures](#common-test-failures)
- [Debugging Techniques](#debugging-techniques)
- [Environment Issues](#environment-issues)
- [Performance Problems](#performance-problems)
- [Chaos Test Issues](#chaos-test-issues)
- [CI/CD Failures](#cicd-failures)
- [Quick Reference](#quick-reference)

## Common Test Failures

### 1. ZMQ Connection Failures

#### Symptoms
```
Failed to connect to KV events endpoint on pod test-pod: dial tcp 10.0.0.1:5557: connection refused
```

#### Causes & Solutions

**Missing ZMQ libraries**
```bash
# Check if ZMQ is installed
pkg-config --modversion libzmq

# Install if missing
sudo apt-get install -y libzmq3-dev pkg-config
```

**Pod not ready**
```bash
# Check pod status
kubectl get pod <pod-name> -n kv-sync-test -o yaml

# Check readiness probe
kubectl describe pod <pod-name> -n kv-sync-test | grep -A5 "Readiness"

# Wait for pod to be ready
kubectl wait --for=condition=ready pod/<pod-name> -n kv-sync-test --timeout=300s
```

**Network policies blocking traffic**
```bash
# Check network policies
kubectl get networkpolicies -n kv-sync-test

# Temporarily disable for testing
kubectl delete networkpolicies --all -n kv-sync-test
```

### 2. Event Processing Failures

#### Symptoms
```
Expected prefix to exist in indexer after event processing
```

#### Causes & Solutions

**Event manager not running**
```bash
# Check controller logs
kubectl logs -n aibrix-system deployment/aibrix-controller-manager | grep -i "event manager"

# Restart controller if needed
kubectl rollout restart deployment/aibrix-controller-manager -n aibrix-system
```

**Incorrect pod labels**
```bash
# Verify required labels
kubectl get pods -n kv-sync-test -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.labels}{"\n"}{end}'

# Required label: model.aibrix.ai/kv-events-enabled=true
kubectl label pod <pod-name> model.aibrix.ai/kv-events-enabled=true
```

**Event format mismatch**
```go
// Check event structure in test
event := &kvcache.KVCacheEvent{
    EventType: kvcache.EventBlockStored,  // Must be valid type
    RequestID: "test-001",                 // Must be unique
    TokenIDs:  []int32{100, 101, 102},    // Must be valid tokens
    Metadata: &kvcache.EventMetadata{      // Required fields
        ModelName: "test-model",
        Timestamp: time.Now().Unix(),
    },
}
```

### 3. Test Timeouts

#### Symptoms
```
panic: test timed out after 10m0s
```

#### Causes & Solutions

**Insufficient timeout**
```bash
# Increase test timeout
go test -v ./test/e2e/ -timeout 30m

# For specific long-running tests
go test -v ./test/e2e/ -run TestKVSyncE2ELargeScale -timeout 60m
```

**Deployment taking too long**
```bash
# Check deployment progress
kubectl rollout status deployment/<name> -n kv-sync-test

# Check for pending pods
kubectl get pods -n kv-sync-test | grep Pending

# Check events for issues
kubectl get events -n kv-sync-test --sort-by='.lastTimestamp'
```

**Resource constraints**
```bash
# Check node resources
kubectl top nodes

# Check if pods are being evicted
kubectl get events --all-namespaces | grep -i evict

# Reduce test scale or increase cluster resources
```

### 4. Flaky Tests

#### Symptoms
- Tests pass sometimes, fail others
- Different results in CI vs local

#### Causes & Solutions

**Race conditions**
```bash
# Run with race detector
go test -race -v ./test/e2e/

# Add proper synchronization
time.Sleep(5 * time.Second)  // Bad
helper.WaitForDeploymentReady(...)  // Good
```

**Non-deterministic data**
```go
// Use fixed seeds for random data
rand.Seed(42)

// Use deterministic request IDs
requestID := fmt.Sprintf("test-%03d", i)  // Good
requestID := uuid.New().String()          // Bad for tests
```

**Environment differences**
```bash
# Ensure consistent environment
export KUBECONFIG="${HOME}/.kube/config"
export GOMAXPROCS=4

# Use same Go version as CI
go version  # Should match CI
```

## Debugging Techniques

### 1. Enable Debug Logging

**Controller debug logs**
```bash
# Edit controller deployment
kubectl edit deployment aibrix-controller-manager -n aibrix-system

# Add environment variable
env:
- name: LOG_LEVEL
  value: "debug"
```

**Test debug output**
```go
// Add debug logging in tests
t.Logf("Processing event: %+v", event)
t.Logf("Indexer state: %+v", indexer.GetStats())
```

### 2. Interactive Debugging

**Port forwarding for direct access**
```bash
# Forward ZMQ port
kubectl port-forward -n kv-sync-test pod/<pod-name> 5557:5557

# Test with netcat
nc -zv localhost 5557

# Test with ZMQ tools
zmq_sub tcp://localhost:5557
```

**Exec into pod**
```bash
# Get shell access
kubectl exec -it -n kv-sync-test <pod-name> -- /bin/bash

# Check processes
ps aux | grep vllm

# Check network connections
netstat -tulpn | grep 5557
```

### 3. Trace Event Flow

**Add tracing to event manager**
```go
// In test helper
func (h *KVEventTestHelper) TraceEvent(event *kvcache.KVCacheEvent) {
    fmt.Printf("[TRACE] Event sent: %s at %s\n", 
        event.RequestID, time.Now().Format(time.RFC3339))
}
```

**Use distributed tracing**
```bash
# Deploy Jaeger for tracing
kubectl apply -f https://raw.githubusercontent.com/jaegertracing/jaeger-operator/master/deploy/crds/jaegertracing.io_jaegers_crd.yaml

# Enable tracing in components
export JAEGER_ENDPOINT=http://jaeger-collector:14268/api/traces
```

## Environment Issues

### 1. Kind Cluster Problems

**Cluster not starting**
```bash
# Delete and recreate
kind delete cluster --name aibrix-e2e
kind create cluster --config development/vllm/kind-config.yaml --name aibrix-e2e

# Check Docker resources
docker system df
docker system prune -a  # Clean up if needed
```

**Image loading failures**
```bash
# Check if images exist
docker images | grep aibrix

# Rebuild if missing
make docker-build-all

# Load with explicit name
kind load docker-image aibrix/vllm-mock:nightly --name aibrix-e2e
```

### 2. Resource Limitations

**Insufficient memory**
```bash
# Check Docker memory limit (macOS/Windows)
docker info | grep -i memory

# Increase Docker resources in Docker Desktop settings
# Recommended: 8GB+ for full test suite
```

**CPU throttling**
```bash
# Check for CPU limits
kubectl top pods -n kv-sync-test

# Remove CPU limits for testing
kubectl patch deployment <name> -n kv-sync-test --type='json' \
  -p='[{"op": "remove", "path": "/spec/template/spec/containers/0/resources/limits/cpu"}]'
```

## Performance Problems

### 1. Slow Event Processing

**Identify bottleneck**
```bash
# Enable profiling in test
go test -bench=. -cpuprofile=cpu.prof ./test/benchmark/

# Analyze profile
go tool pprof -http=:8080 cpu.prof
```

**Common causes**
- Large token sequences without batching
- Synchronous processing instead of async
- Memory pressure causing GC

### 2. Memory Leaks

**Detection**
```bash
# Run test with memory profiling
go test -memprofile=mem.prof -benchtime=60s ./test/benchmark/

# Check for leaks
go tool pprof -alloc_space mem.prof
```

**Common leak sources**
- Unclosed ZMQ connections
- Growing maps without cleanup
- Goroutine leaks

## Chaos Test Issues

### 1. Chaos Mesh Not Working

**Installation issues**
```bash
# Check Chaos Mesh pods
kubectl get pods -n chaos-mesh

# Check webhook certificates
kubectl get validatingwebhookconfigurations
kubectl get mutatingwebhookconfigurations

# Reinstall if needed
kubectl delete ns chaos-mesh
curl -sSL https://mirrors.chaos-mesh.org/latest/install.sh | bash
```

**Experiments not applying**
```bash
# Check chaos experiment status
kubectl get chaos -A

# Check RBAC permissions
kubectl auth can-i create chaos --as=system:serviceaccount:chaos-mesh:chaos-controller-manager -n kv-sync-test

# Check target selector
kubectl get pods -n kv-sync-test -l model.aibrix.ai/kv-events-enabled=true
```

### 2. Recovery Validation Failures

**System not recovering**
```bash
# Check if chaos is still active
kubectl get networkchaos -n kv-sync-test

# Force delete stuck experiments
kubectl patch networkchaos <name> -n kv-sync-test \
  -p '{"metadata":{"finalizers":[]}}' --type=merge
kubectl delete networkchaos <name> -n kv-sync-test --force
```

## CI/CD Failures

### 1. GitHub Actions Issues

**Timeout in CI**
```yaml
# Increase job timeout in workflow
jobs:
  e2e-tests:
    timeout-minutes: 60  # Increase from default 30
```

**Artifact upload failures**
```yaml
# Reduce artifact size
- name: Collect logs on failure
  if: failure()
  run: |
    # Only last 1000 lines
    kubectl logs -n aibrix-system deployment/controller --tail=1000 > controller.log
```

### 2. Nightly Build Failures

**Performance regression detected**
```bash
# Check baseline metrics
cat test/benchmark/baseline_metrics.json

# Run comparison locally
benchstat baseline.txt current.txt

# Investigate specific regression
go test -bench=BenchmarkKVEventProcessingLatency -count=10
```

## Quick Reference

### Essential Commands

```bash
# Check all pods in test namespace
kubectl get pods -n kv-sync-test -o wide

# Get recent events
kubectl get events -n kv-sync-test --sort-by='.lastTimestamp' | tail -20

# Check controller logs
kubectl logs -n aibrix-system deployment/aibrix-controller-manager --tail=100

# Test ZMQ connectivity
kubectl exec -n kv-sync-test <pod> -- nc -zv localhost 5557

# Run single test with verbose output
go test -v -run TestKVSyncE2EHappyPath ./test/e2e/ -timeout 10m

# Clean up test resources
kubectl delete namespace kv-sync-test --grace-period=0 --force
```

### Log Locations

| Component | Log Command |
|-----------|------------|
| Controller | `kubectl logs -n aibrix-system deployment/aibrix-controller-manager` |
| Gateway | `kubectl logs -n aibrix-system deployment/aibrix-gateway` |
| vLLM Pod | `kubectl logs -n kv-sync-test <pod-name>` |
| Test Output | `go test -v 2>&1 | tee test.log` |

### Common Error Patterns

| Error | Likely Cause | Quick Fix |
|-------|--------------|-----------|
| `connection refused` | Service not ready | Wait for pods to be ready |
| `no such host` | DNS issue | Check service exists |
| `timeout` | Resource constraints | Increase limits/timeout |
| `permission denied` | RBAC issue | Check service account |
| `already exists` | Cleanup failure | Delete and retry |

## Getting Help

1. **Check existing issues**: Search GitHub issues for similar problems
2. **Collect diagnostics**: Run `kubectl cluster-info dump > cluster-dump.tar.gz`
3. **Ask for help**: Include test output, logs, and cluster dump

## Related Documentation
- [E2E Test Guide](e2e-test-guide.md)
- [Performance Testing Guide](performance-testing-guide.md)
- [Chaos Testing README](../../test/chaos/README.md)