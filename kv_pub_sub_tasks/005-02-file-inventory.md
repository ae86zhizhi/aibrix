# Task 005-02 File Inventory

This document lists all files created for Task 005-02 with their status.

## E2E Test Files

| File | Status | Notes |
|------|--------|-------|
| `test/e2e/kv_sync_helpers.go` | ✅ Compiles | Helper functions for E2E tests |
| `test/e2e/kv_sync_e2e_test.go` | ❌ Needs fixes | Uses wrong event types, needs API updates |
| `test/e2e/kv_sync_e2e_simple_test.go` | ✅ Compiles | Simplified tests that work |
| `test/e2e/fixtures/vllm-kv-events-deployment.yaml` | ✅ Valid | Test deployment fixture |

## Performance Benchmark Files

| File | Status | Notes |
|------|--------|-------|
| `test/benchmark/kv_sync_bench_test.go` | ❌ Needs fixes | Uses non-existent KVCacheEvent type |
| `test/benchmark/kv_sync_indexer_bench_test.go` | ✅ Compiles | Uses correct SyncPrefixHashTable API |
| `test/benchmark/baseline_metrics.json` | ✅ Valid | Performance baseline data |

## Chaos Test Files

| File | Status | Notes |
|------|--------|-------|
| `test/chaos/chaos_test.go` | ❌ Needs fixes | Missing imports, wrong method calls |
| `test/chaos/chaos_simple_test.go` | ✅ Compiles | Simplified chaos tests |
| `test/chaos/experiments/network-partition.yaml` | ✅ Valid | Network chaos experiments |
| `test/chaos/experiments/pod-failures.yaml` | ✅ Valid | Pod failure experiments |
| `test/chaos/experiments/zmq-failures.yaml` | ✅ Valid | ZMQ-specific failures |
| `test/chaos/README.md` | ✅ Complete | Chaos testing guide |

## CI/CD Files

| File | Status | Notes |
|------|--------|-------|
| `.github/workflows/complete-testing.yml` | ✅ Valid | Comprehensive test pipeline |
| `.github/workflows/nightly-performance.yml` | ✅ Valid | Nightly benchmark runs |

## Documentation Files

| File | Status | Notes |
|------|--------|-------|
| `docs/testing/README.md` | ✅ Complete | Testing overview |
| `docs/testing/e2e-test-guide.md` | ✅ Complete | E2E testing guide |
| `docs/testing/performance-testing-guide.md` | ✅ Complete | Performance testing guide |
| `docs/testing/troubleshooting-guide.md` | ✅ Complete | Troubleshooting guide |

## Task Documentation

| File | Status | Notes |
|------|--------|-------|
| `kv_pub_sub_tasks/005-02-completeness-testing.md` | ✅ Original | Task requirements |
| `kv_pub_sub_tasks/005-02-implementation-guide.md` | ✅ Original | Implementation guidance |
| `kv_pub_sub_tasks/005-02-implementation-summary.md` | ✅ Updated | Current status summary |
| `kv_pub_sub_tasks/005-02-handover-document.md` | ✅ New | Detailed handover guide |
| `kv_pub_sub_tasks/005-02-file-inventory.md` | ✅ This file | File listing |

## Makefile Changes

**File**: `Makefile`

Added targets:
```makefile
.PHONY: test-kv-sync-e2e
test-kv-sync-e2e: ## Run KV sync E2E tests.
	go test -v -tags="zmq" ./test/e2e/kv_sync_e2e_simple_test.go ./test/e2e/kv_sync_helpers.go ./test/e2e/util.go -timeout 30m

.PHONY: test-kv-sync-benchmark
test-kv-sync-benchmark: ## Run KV sync performance benchmarks.
	go test -bench=. -benchmem -benchtime=10s -tags="zmq" ./test/benchmark/kv_sync_indexer_bench_test.go

.PHONY: test-kv-sync-chaos
test-kv-sync-chaos: ## Run KV sync chaos tests (requires Chaos Mesh).
	go test -v -tags="zmq" ./test/chaos/chaos_simple_test.go -timeout 45m

.PHONY: test-kv-sync-all
test-kv-sync-all: test-zmq test-kv-sync test-kv-sync-e2e ## Run all KV sync tests (unit, integration, E2E).
	@echo "All KV sync tests completed"
```

## Quick Test Commands

### What Works Now:
```bash
# Compile check
go test -tags="zmq" -c ./test/e2e/kv_sync_e2e_simple_test.go
go test -tags="zmq" -c ./test/benchmark/kv_sync_indexer_bench_test.go
go test -tags="zmq" -c ./test/chaos/chaos_simple_test.go

# Run working tests
make test-kv-sync-e2e        # Uses simple E2E test
make test-kv-sync-benchmark  # Uses indexer benchmark
make test-kv-sync-chaos      # Uses simple chaos test
```

### What Needs Fixing:
```bash
# These files need API corrections before they'll compile:
test/e2e/kv_sync_e2e_test.go
test/benchmark/kv_sync_bench_test.go
test/chaos/chaos_test.go
```

## Key API Discoveries

1. **Event Types** (from `pkg/cache/kvcache/event_types.go`):
   - `BlockStoredEvent` (not KVCacheEvent)
   - `BlockRemovedEvent`
   - `AllBlocksClearedEvent`

2. **Token Format**:
   - Events use `[][]int32` for tokens
   - Indexer expects `[]byte`
   - Need conversion function

3. **SyncPrefixHashTable Methods**:
   - `ProcessBlockStored(event BlockStored)` (not AddRequest)
   - `MatchPrefix(...)` (not LookupTokens)
   - `ProcessBlockRemoved(...)`

4. **Event Manager**:
   - Doesn't have `HandleEvent` method
   - Need to investigate actual integration pattern