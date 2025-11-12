/*
Copyright 2024 The Aibrix Team.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

/*
Package kvaware implements KVCache-aware routing for AIBrix gateway.

This package provides intelligent routing based on KV cache state across
prefill and decode pods, implementing the algorithm from the Mooncake paper.

# Architecture

The KV-aware router consists of several integrated components:

  - Configuration: Load and validate routing parameters (Phase 001)
  - Metrics Collection: Gather real-time pod metrics (Phase 003)
  - Prefix Matching: Find cache hits across pods (Phase 004)
  - TTFT Estimation: Predict time to first token (Phase 005)
  - Decode Selection: Choose optimal decode pod (Phase 006)
  - SLO Enforcement: Reject requests that violate SLO (Phase 006)
  - Statistics Tracking: Monitor routing performance (Phase 007)

# Usage

Basic usage:

	// Create router
	router, err := NewKVAwareRouter()
	if err != nil {
	    log.Fatal(err)
	}

	// Route request
	ctx := &types.RoutingContext{
	    RequestID: "req-001",
	    Model:     "llama3-70b",
	    Message:   "Once upon a time",
	}

	address, err := router.Route(ctx, podList)
	if err != nil {
	    // Handle routing error or rejection
	    log.Printf("Routing failed: %v", err)
	    return
	}

	// Forward request to address
	forwardRequest(address, ctx)

# Configuration

The router is configured via environment variables:

	AIBRIX_KV_AWARE_ENABLED=true
	AIBRIX_KV_TRANSFER_ENABLED=false  # MVP constraint
	AIBRIX_KV_TRANSFER_BW_GBPS=100
	AIBRIX_TTFT_SLO_SECONDS=3
	AIBRIX_TBT_SLO_MS=200
	AIBRIX_PREFIX_CACHE_BLOCK_SIZE=16

See config.go for full configuration options.

# Routing Algorithm

The router implements a 13-step algorithm:

	1. Get all ready pods
	2. Separate prefill and decode pods by role label
	3. Convert to internal PodRef structures
	4. Tokenize the request prompt
	5. Compute prefix cache matches across prefill pods
	6. Fetch metrics for all pods
	7. Estimate TTFT for each prefill pod
	8. Select prefill pod with minimum TTFT
	9. Check TTFT against SLO (with relaxed fallback)
	10. Estimate output tokens for decode selection
	11. Select decode pod optimizing for TBT
	12. Final combined SLO check
	13. Return routing decision

# Monitoring

The router exports statistics via periodic logging:

	V(2): Overall stats (total, success/fallback/reject/error rates)
	V(3): Performance metrics (latency avg/P95, cache hit rate)
	V(4): Top reasons (fallback, rejection, error types)

Future: Prometheus metrics export (currently stubbed).

# Error Handling

The router implements graceful degradation:

  - Component failure: Falls back to LeastRequest algorithm
  - SLO violation: Returns RejectionError with HTTP 429
  - Missing pods: Returns error or falls back
  - System unavailable: Returns appropriate error

# Performance

Typical performance characteristics (from benchmarks):

  - Routing latency: ~200µs average, <500µs P95
  - Throughput: >1000 requests/second per router
  - Statistics overhead: ~127ns per operation
  - Memory overhead: <50MB for 100 concurrent requests

# MVP Limitations

This MVP implementation has the following constraints:

  - No actual KV transfer: Only estimates transfer time, doesn't execute
  - Routes to prefill pod only: Doesn't implement separate P/D routing
  - Simple output estimation: Uses prompt length heuristic
  - Static model config: Model specs loaded at startup
  - Constant bandwidth: Uses configured value, no dynamic measurement

See design docs in tasks/design_docs/ for post-MVP enhancements.

# Thread Safety

All components are designed for concurrent use:

  - Statistics tracking uses sync.RWMutex
  - Metrics cache is thread-safe with TTL
  - Router instance can handle concurrent Route() calls
  - Prefix indexer supports concurrent lookups

# Testing

The package includes comprehensive tests:

  - Unit tests: 42+ test cases covering all components
  - E2E tests: 5 scenarios with build tag 'e2e'
  - Performance benchmarks: 7 benchmarks for key operations
  - Coverage: 66% overall, >85% for critical paths

Run tests:

	go test ./pkg/plugins/gateway/algorithms/kv_aware/
	go test -tags=e2e ./pkg/plugins/gateway/algorithms/kv_aware/
	go test -bench=. ./pkg/plugins/gateway/algorithms/kv_aware/

# References

  - Mooncake paper: https://arxiv.org/abs/2407.00079
  - Design docs: tasks/design_docs/
  - Implementation notes: tasks/development_process_docs/
*/
package kvaware
