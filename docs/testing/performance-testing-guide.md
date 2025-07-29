# Performance Testing Guide for KV Event Synchronization

This guide covers performance benchmarking methodology, interpretation of results, and best practices for the KV event synchronization system.

## Table of Contents
- [Overview](#overview)
- [Benchmark Suite](#benchmark-suite)
- [Running Benchmarks](#running-benchmarks)
- [Interpreting Results](#interpreting-results)
- [Performance Baselines](#performance-baselines)
- [Regression Detection](#regression-detection)
- [Optimization Guide](#optimization-guide)

## Overview

Performance testing ensures the KV event synchronization system meets the following targets:
- **Event Processing Latency**: < 1ms per event
- **Routing Decision Update**: < 5ms
- **System Throughput**: 10,000 events/second
- **Memory Efficiency**: < 1KB per cached prefix
- **CPU Usage**: < 20% under normal load

## Benchmark Suite

### 1. Event Processing Latency (`BenchmarkKVEventProcessingLatency`)
Measures the time to process a single KV cache event through the sync indexer.

**Key Metrics:**
- Average latency (ns)
- P99 latency (ns)
- Operations per second

### 2. Event Throughput (`BenchmarkKVEventThroughput`)
Tests maximum sustainable event processing rate with concurrent workers.

**Key Metrics:**
- Events per second
- Error rate
- CPU cores utilized

### 3. Memory Usage (`BenchmarkKVEventMemoryUsage`)
Profiles memory consumption with different cache sizes.

**Key Metrics:**
- Memory per item (bytes)
- Total heap usage (MB)
- GC pressure

### 4. Concurrency Performance (`BenchmarkKVEventConcurrency`)
Evaluates performance under various concurrency levels (1-500 concurrent operations).

**Key Metrics:**
- Throughput vs concurrency
- Lock contention
- Response time distribution

### 5. Large Prefix Handling (`BenchmarkKVEventLargePrefix`)
Tests performance with very long token sequences (100-50,000 tokens).

**Key Metrics:**
- Latency vs prefix size
- Memory usage scaling
- Hash computation time

### 6. Burst Load (`BenchmarkKVEventBurstLoad`)
Simulates sudden traffic spikes (10,000 events in 5 seconds).

**Key Metrics:**
- Peak throughput
- Recovery time
- Queue depth

### 7. Routing Decision Speed (`BenchmarkKVEventRoutingDecision`)
Measures time to make routing decisions based on cached prefixes.

**Key Metrics:**
- Lookup latency
- Cache hit rate
- Decision accuracy

## Running Benchmarks

### Prerequisites
```bash
# Install dependencies
sudo apt-get install -y libzmq3-dev pkg-config

# Install benchmark tools
go install golang.org/x/perf/cmd/benchstat@latest
go install github.com/uber/go-torch@latest
```

### Run All Benchmarks
```bash
cd test/benchmark
go test -bench=. -benchmem -benchtime=10s -count=3 -cpu=1,2,4,8 -tags="zmq"
```

### Run Specific Benchmark
```bash
# Latency benchmark only
go test -bench=BenchmarkKVEventProcessingLatency -benchmem -benchtime=30s

# With CPU profiling
go test -bench=BenchmarkKVEventThroughput -cpuprofile=cpu.prof

# With memory profiling
go test -bench=BenchmarkKVEventMemoryUsage -memprofile=mem.prof
```

### Generate Flame Graphs
```bash
# CPU flame graph
go-torch -b cpu.prof

# Memory allocation flame graph
go tool pprof -alloc_space -output mem.svg mem.prof
```

### Compare Results
```bash
# Run baseline
go test -bench=. -tags="zmq" > baseline.txt

# Make changes and run again
go test -bench=. -tags="zmq" > new.txt

# Compare
benchstat baseline.txt new.txt
```

## Interpreting Results

### Understanding Benchmark Output
```
BenchmarkKVEventProcessingLatency-8   1000000   1050 ns/op   256 B/op   4 allocs/op
```
- `-8`: Number of CPU cores used
- `1000000`: Number of iterations
- `1050 ns/op`: Nanoseconds per operation
- `256 B/op`: Bytes allocated per operation
- `4 allocs/op`: Number of allocations per operation

### Performance Metrics

#### Latency Percentiles
- **P50 (Median)**: Typical performance
- **P90**: Performance for 90% of requests
- **P99**: Performance for 99% of requests
- **P99.9**: Worst-case scenarios

#### Throughput Calculation
```
Throughput (ops/sec) = 1,000,000,000 / latency_ns
```

#### Memory Efficiency
```
Memory per event = Total memory / Number of events
```

### Red Flags
- P99 latency > 10x P50 latency
- Memory allocations growing linearly with load
- CPU usage > 80% at target throughput
- Error rate > 0.1%

## Performance Baselines

### Current Baselines (as of v1.0)

| Metric | Target | Baseline | Unit |
|--------|--------|----------|------|
| Event Processing Latency (P50) | < 500μs | 450μs | microseconds |
| Event Processing Latency (P99) | < 1ms | 950μs | microseconds |
| Throughput (single-core) | > 2,000 | 2,200 | events/sec |
| Throughput (8-cores) | > 10,000 | 12,000 | events/sec |
| Memory per 1K events | < 1MB | 0.8MB | megabytes |
| Routing decision time | < 100μs | 85μs | microseconds |

### Establishing New Baselines
```bash
# Run comprehensive benchmark
./scripts/establish-baseline.sh

# Results saved to:
test/benchmark/baseline_metrics.json
```

## Regression Detection

### Automated Detection
The CI pipeline automatically:
1. Runs benchmarks nightly
2. Compares with 7-day rolling baseline
3. Fails if regression > 10%
4. Creates GitHub issue for regressions

### Manual Detection
```bash
# Check for regression
go run test/benchmark/check_regression.go \
  --baseline baseline_metrics.json \
  --current current_results.json \
  --threshold 0.1
```

### Regression Criteria
- **Critical**: > 20% performance degradation
- **Warning**: 10-20% degradation
- **Info**: 5-10% degradation

## Optimization Guide

### Common Bottlenecks

#### 1. Lock Contention
**Symptoms**: Poor scaling with CPU cores
**Solution**: Use sharded locks or lock-free data structures

#### 2. Memory Allocations
**Symptoms**: High GC pressure, allocations per op
**Solution**: Use object pools, preallocate buffers

#### 3. Hash Computation
**Symptoms**: High CPU in hash functions
**Solution**: Cache hash values, use faster algorithms

#### 4. Network I/O
**Symptoms**: High latency variance
**Solution**: Batch operations, use connection pooling

### Profiling Workflow

1. **Identify hotspot**:
   ```bash
   go test -bench=. -cpuprofile=cpu.prof
   go tool pprof -top cpu.prof
   ```

2. **Analyze allocations**:
   ```bash
   go test -bench=. -memprofile=mem.prof
   go tool pprof -alloc_objects mem.prof
   ```

3. **Check contention**:
   ```bash
   go test -bench=. -blockprofile=block.prof
   go tool pprof block.prof
   ```

4. **Trace execution**:
   ```bash
   go test -bench=. -trace=trace.out
   go tool trace trace.out
   ```

### Optimization Techniques

#### Memory Optimization
```go
// Use sync.Pool for temporary objects
var bufferPool = sync.Pool{
    New: func() interface{} {
        return make([]byte, 0, 1024)
    },
}

// Preallocate slices
tokens := make([]int32, 0, expectedSize)
```

#### CPU Optimization
```go
// Avoid interface{} and reflection
// Use concrete types where possible

// Inline hot functions
//go:inline
func fastHash(data []byte) uint64 {
    // ...
}
```

#### Concurrency Optimization
```go
// Use channels for coordination
// Avoid shared memory where possible

// Shard data structures
type ShardedIndex struct {
    shards [256]*IndexShard
}
```

## Continuous Monitoring

### Metrics to Track
1. **Latency**: P50, P90, P99
2. **Throughput**: Events/sec
3. **Errors**: Rate and types
4. **Resources**: CPU, Memory, Network

### Alerting Thresholds
- P99 latency > 2ms
- Throughput < 8,000 events/sec
- Error rate > 0.5%
- Memory usage > 80% of limit

### Dashboard Integration
Performance metrics are exported to:
- Prometheus for real-time monitoring
- Grafana for visualization
- GitHub Pages for historical trends

## Best Practices

### 1. Benchmark Design
- Use realistic workloads
- Test edge cases
- Include warm-up phase
- Run multiple iterations

### 2. Environment
- Dedicated hardware/VMs
- Consistent OS settings
- Disable CPU frequency scaling
- Control background processes

### 3. Data Collection
- Record environment details
- Save raw results
- Track code versions
- Document changes

### 4. Analysis
- Look for trends, not absolutes
- Consider variance
- Validate with production metrics
- Cross-reference different benchmarks

## Troubleshooting

### High Variance in Results
- Increase benchmark time: `-benchtime=60s`
- Disable GC during critical sections
- Check for background processes
- Use dedicated test hardware

### Unexpected Regressions
1. Git bisect to find commit
2. Profile before/after
3. Check for environment changes
4. Review recent PRs

### Memory Leaks
```bash
# Run with memory profiling
go test -bench=. -memprofile=mem.prof -benchtime=60s

# Check for growing allocations
go tool pprof -alloc_space mem.prof
```

## Related Documentation
- [E2E Testing Guide](e2e-test-guide.md)
- [Chaos Testing Guide](../../test/chaos/README.md)
- [KV Event Sync Architecture](../kv-cache-events-guide.md)