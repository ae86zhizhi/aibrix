# KV Event Synchronization Testing Documentation

Comprehensive testing framework for the KV cache event synchronization system in AIBrix.

## Overview

The KV event synchronization testing framework provides multiple layers of testing to ensure reliability, performance, and resilience of the distributed caching system. This documentation covers all aspects of testing from unit tests to chaos engineering.

## Testing Pyramid

```
         /\
        /  \    Chaos Tests (Weekly)
       /----\   
      /      \  E2E Tests (Per PR)
     /--------\ 
    /          \ Integration Tests (Per PR)
   /------------\
  /              \ Unit Tests (Per Commit)
 /________________\
```

## Test Categories

### 1. Unit Tests
**Purpose**: Test individual components in isolation  
**Coverage**: ~90% for core components  
**Run Time**: < 1 minute  
**Frequency**: Every commit  

Key test files:
- `pkg/cache/kvcache/zmq_client_test.go` - ZMQ client functionality
- `pkg/utils/syncprefixcacheindexer/sync_hash_test.go` - Sync indexer operations
- `pkg/cache/kv_event_manager_test.go` - Event manager logic

### 2. Integration Tests
**Purpose**: Test component interactions  
**Coverage**: Major workflows  
**Run Time**: 2-5 minutes  
**Frequency**: Every PR  

Key test files:
- `test/integration/kv_event_sync_test.go` - Full event flow integration

### 3. End-to-End Tests
**Purpose**: Validate complete system behavior in real Kubernetes  
**Coverage**: User scenarios  
**Run Time**: 10-30 minutes  
**Frequency**: Every PR and nightly  

Key test files:
- `test/e2e/kv_sync_e2e_test.go` - Complete E2E scenarios
- `test/e2e/kv_sync_helpers.go` - E2E test utilities

### 4. Performance Benchmarks
**Purpose**: Measure and track performance metrics  
**Baselines**: Established for all key operations  
**Run Time**: 30-60 minutes  
**Frequency**: Nightly  

Key test files:
- `test/benchmark/kv_sync_bench_test.go` - Performance benchmarks
- `test/benchmark/baseline_metrics.json` - Performance baselines

### 5. Chaos Tests
**Purpose**: Validate system resilience  
**Scenarios**: Network, pod, and service failures  
**Run Time**: 45-90 minutes  
**Frequency**: Weekly  

Key test files:
- `test/chaos/chaos_test.go` - Chaos test scenarios
- `test/chaos/experiments/*.yaml` - Chaos Mesh experiments

## Quick Start

### Running Tests Locally

```bash
# Prerequisites
sudo apt-get install -y libzmq3-dev pkg-config
go install -tags zmq github.com/vllm-project/aibrix/...

# Run all unit tests
make test-zmq
make test-kv-sync

# Run integration tests
go test -v -tags="zmq" ./test/integration/

# Run E2E tests (requires Kind cluster)
kind create cluster --config development/vllm/kind-config.yaml
make docker-build-all
go test -v ./test/e2e/kv_sync_e2e_test.go

# Run benchmarks
cd test/benchmark && go test -bench=. -benchmem

# Run chaos tests (requires Chaos Mesh)
curl -sSL https://mirrors.chaos-mesh.org/latest/install.sh | bash
go test -v ./test/chaos/
```

## CI/CD Integration

### GitHub Actions Workflows

1. **Unit & Integration Tests** (`.github/workflows/kv-event-sync-tests.yml`)
   - Triggers: Every push and PR
   - Duration: ~5 minutes
   - Required for merge

2. **Complete Testing Pipeline** (`.github/workflows/complete-testing.yml`)
   - Triggers: PR, nightly, manual
   - Includes: All test types
   - Duration: ~45 minutes

3. **Nightly Performance** (`.github/workflows/nightly-performance.yml`)
   - Triggers: Daily at 3 AM UTC
   - Benchmarks with regression detection
   - Results tracked over time

## Test Guides

### For Test Writers
- [Writing E2E Tests](e2e-test-guide.md#writing-new-e2e-tests)
- [Adding Benchmarks](performance-testing-guide.md#benchmark-design)
- [Creating Chaos Experiments](../chaos/README.md#extending-chaos-tests)

### For Test Runners
- [E2E Test Guide](e2e-test-guide.md) - Running and debugging E2E tests
- [Performance Testing Guide](performance-testing-guide.md) - Benchmarking methodology
- [Troubleshooting Guide](troubleshooting-guide.md) - Common issues and solutions

### For Operators
- [Chaos Testing Guide](../../test/chaos/README.md) - Chaos engineering practices
- [CI/CD Workflows](.github/workflows/) - Automated testing pipelines

## Test Coverage Goals

| Component | Current | Target | Status |
|-----------|---------|--------|--------|
| ZMQ Client | 95% | 90% | ✅ Exceeded |
| Sync Indexer | 90% | 90% | ✅ Met |
| Event Manager | 90% | 85% | ✅ Exceeded |
| E2E Scenarios | 100% | 100% | ✅ Complete |
| Performance | Baselined | Tracked | ✅ Active |
| Chaos Scenarios | 80% | 80% | ✅ Met |

## Performance Targets

| Metric | Target | Current | Status |
|--------|--------|---------|--------|
| Event Processing Latency (P99) | < 1ms | 950μs | ✅ Met |
| Routing Decision Latency | < 5ms | 85μs | ✅ Exceeded |
| System Throughput | 10K eps | 12K eps | ✅ Exceeded |
| Memory per 1K Events | < 1MB | 0.8MB | ✅ Met |
| Recovery Time | < 30s | 25s | ✅ Met |

## Test Artifacts

All test runs produce artifacts stored in GitHub Actions:
- **Coverage Reports**: HTML coverage reports
- **Benchmark Results**: JSON performance metrics
- **Test Logs**: Detailed logs for debugging
- **Performance Graphs**: Visual performance trends

## Debugging Test Failures

1. **Check test output**: Look for specific error messages
2. **Review logs**: Controller, pod, and event logs
3. **Consult guides**: [Troubleshooting Guide](troubleshooting-guide.md)
4. **Run locally**: Reproduce with same configuration
5. **Ask for help**: Create issue with diagnostics

## Contributing

### Adding New Tests
1. Choose appropriate test level (unit/integration/e2e)
2. Follow existing patterns in test files
3. Add to relevant CI workflow
4. Document in appropriate guide
5. Update coverage goals if needed

### Improving Tests
- Reduce flakiness
- Improve error messages
- Add missing scenarios
- Optimize execution time
- Enhance documentation

## Maintenance

### Weekly Tasks
- Review chaos test results
- Update baseline metrics if needed
- Address any test failures

### Monthly Tasks
- Review test coverage trends
- Update documentation
- Optimize slow tests
- Plan new test scenarios

### Quarterly Tasks
- Full test suite audit
- Performance baseline review
- Chaos scenario updates
- Documentation overhaul

## Resources

### Internal Documentation
- [KV Cache Events Guide](../kv-cache-events-guide.md)
- [Architecture Overview](../designs/architecture.rst)
- [Task Implementation Guides](../../kv_pub_sub_tasks/)

### External Resources
- [Ginkgo Testing Framework](https://onsi.github.io/ginkgo/)
- [Chaos Mesh Documentation](https://chaos-mesh.org/)
- [Go Testing Best Practices](https://go.dev/doc/tutorial/fuzz)

## Support

For testing-related questions:
1. Check the troubleshooting guide
2. Search existing GitHub issues
3. Ask in #aibrix-testing Slack channel
4. Create a new issue with test logs

---

*Last Updated: January 2025*  
*Maintained by: AIBrix Testing Team*