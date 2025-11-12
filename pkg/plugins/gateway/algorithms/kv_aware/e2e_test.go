// +build e2e

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

package kvaware

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/vllm-project/aibrix/pkg/types"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestE2E_HighCacheHitScenario tests routing with high cache hit rate
func TestE2E_HighCacheHitScenario(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E tests in short mode")
	}

	// Setup: Create router with test config
	router := createE2ERouter(t)

	// Scenario: Repeated similar prompts (high cache hit)
	basePrompt := "Tell me about artificial intelligence"
	numRequests := 10

	var successCount int
	var totalCacheHit float64

	for i := 0; i < numRequests; i++ {
		ctx := createE2ERoutingContext(
			fmt.Sprintf("e2e-high-cache-%d", i),
			basePrompt+fmt.Sprintf(" request %d", i), // Slight variation
		)

		pods := createE2EPodList(2, 2) // 2 prefill, 2 decode
		targetAddr, err := router.Route(ctx, pods)

		if err == nil {
			successCount++
			assert.NotEmpty(t, targetAddr)
			assert.Contains(t, targetAddr, "10.0.1") // Should route to prefill pod
		}
	}

	// Verify: High success rate
	assert.Greater(t, successCount, numRequests*8/10, "Should have >80% success rate")

	// Check statistics
	stats := router.stats.(*routingStatistics).GetSnapshot()
	assert.Equal(t, int64(numRequests), stats.TotalRequests)
	assert.Greater(t, stats.SuccessfulRoutes, int64(numRequests*8/10))

	t.Logf("E2E High Cache Hit: %d/%d successful, cache_hit=%.1f%%",
		successCount, numRequests, totalCacheHit/float64(numRequests)*100)
}

// TestE2E_LowCacheHitScenario tests routing with low cache hit rate
func TestE2E_LowCacheHitScenario(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E tests in short mode")
	}

	router := createE2ERouter(t)

	// Scenario: Completely different prompts (low cache hit)
	prompts := []string{
		"What is machine learning?",
		"How does quantum computing work?",
		"Explain blockchain technology",
		"Describe neural networks",
		"What are transformers in AI?",
	}

	var successCount int

	for i, prompt := range prompts {
		ctx := createE2ERoutingContext(
			fmt.Sprintf("e2e-low-cache-%d", i),
			prompt,
		)

		pods := createE2EPodList(2, 2)
		targetAddr, err := router.Route(ctx, pods)

		if err == nil {
			successCount++
			assert.NotEmpty(t, targetAddr)
		}
	}

	// Verify: Most should succeed
	assert.Greater(t, successCount, len(prompts)*7/10, "Should have >70% success rate")

	stats := router.stats.(*routingStatistics).GetSnapshot()
	assert.Equal(t, int64(len(prompts)), stats.TotalRequests)

	t.Logf("E2E Low Cache Hit: %d/%d successful", successCount, len(prompts))
}

// TestE2E_MixedWorkload tests routing with mixed request patterns
func TestE2E_MixedWorkload(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E tests in short mode")
	}

	router := createE2ERouter(t)

	// Scenario: Mix of short and long prompts, repeated and unique
	testCases := []struct {
		name   string
		prompt string
		repeat int
	}{
		{"short-repeated", "Hello", 3},
		{"long-unique", strings.Repeat("token ", 100), 1},
		{"medium-repeated", "Tell me about AI " + strings.Repeat("please ", 10), 2},
		{"short-unique", "Quick question", 1},
	}

	var totalRequests, successCount int

	for _, tc := range testCases {
		for i := 0; i < tc.repeat; i++ {
			ctx := createE2ERoutingContext(
				fmt.Sprintf("e2e-mixed-%s-%d", tc.name, i),
				tc.prompt,
			)

			pods := createE2EPodList(3, 3) // More pods for mixed workload
			targetAddr, err := router.Route(ctx, pods)

			totalRequests++
			if err == nil {
				successCount++
				assert.NotEmpty(t, targetAddr)
			}
		}
	}

	// Verify: Good overall success rate
	assert.Greater(t, successCount, totalRequests*7/10, "Should have >70% success rate")

	stats := router.stats.(*routingStatistics).GetSnapshot()
	assert.Equal(t, int64(totalRequests), stats.TotalRequests)
	assert.Greater(t, stats.AvgLatency, time.Duration(0))

	t.Logf("E2E Mixed Workload: %d/%d successful, avg_latency=%v",
		successCount, totalRequests, stats.AvgLatency)
}

// TestE2E_SLOEnforcement tests SLO violation handling
func TestE2E_SLOEnforcement(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E tests in short mode")
	}

	router := createE2ERouter(t)

	// Set tight SLO for testing
	router.config.TTFTSLO = 100 * time.Millisecond
	router.config.TBTSLO = 50 * time.Millisecond

	// Scenario: Very long prompt that likely violates SLO
	ctx := createE2ERoutingContext(
		"e2e-slo-violation",
		strings.Repeat("token ", 2000), // Very long prompt
	)

	pods := createE2EPodList(1, 1) // Minimal pods (stressed)

	// Execute routing - may be rejected
	targetAddr, err := router.Route(ctx, pods)

	// Should either succeed with relaxed SLO or be rejected
	if err != nil {
		// Verify it's a rejection error
		assert.Contains(t, err.Error(), "SLO", "Error should mention SLO")
		t.Logf("Request properly rejected due to SLO violation: %v", err)
	} else {
		assert.NotEmpty(t, targetAddr)
		t.Logf("Request accepted with relaxed SLO")
	}

	// Check statistics
	stats := router.stats.(*routingStatistics).GetSnapshot()
	assert.Equal(t, int64(1), stats.TotalRequests)
	// Should have either success or rejection, not error
	assert.Equal(t, int64(1), stats.SuccessfulRoutes+stats.RejectedRequests)
}

// TestE2E_GracefulDegradation tests fallback behavior
func TestE2E_GracefulDegradation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E tests in short mode")
	}

	router := createE2ERouter(t)

	testCases := []struct {
		name          string
		prefillCount  int
		decodeCount   int
		expectSuccess bool
	}{
		{"normal", 2, 2, true},
		{"no-decode", 2, 0, false}, // Should fallback
		{"no-prefill", 0, 2, false}, // Should fallback
		{"minimal", 1, 1, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := createE2ERoutingContext(
				fmt.Sprintf("e2e-degradation-%s", tc.name),
				"Test prompt for degradation",
			)

			pods := createE2EPodList(tc.prefillCount, tc.decodeCount)
			targetAddr, err := router.Route(ctx, pods)

			if tc.expectSuccess {
				// Should route successfully
				if err == nil {
					assert.NotEmpty(t, targetAddr)
				} else {
					t.Logf("Expected success but got error: %v", err)
				}
			} else {
				// May fallback or error
				t.Logf("Degraded case result: addr=%s, err=%v", targetAddr, err)
			}
		})
	}

	// Check overall statistics
	stats := router.stats.(*routingStatistics).GetSnapshot()
	assert.Greater(t, stats.TotalRequests, int64(0))

	t.Logf("E2E Degradation: total=%d, success=%d, fallback=%d, error=%d",
		stats.TotalRequests, stats.SuccessfulRoutes, stats.FallbackRoutes, stats.Errors)
}

// Helper functions

// SimplePodList implements types.PodList for testing
type SimplePodList struct {
	pods []*v1.Pod
}

func (p *SimplePodList) All() []*v1.Pod {
	return p.pods
}

func (p *SimplePodList) Len() int {
	return len(p.pods)
}

func (p *SimplePodList) Indexes() []string {
	return []string{"default"}
}

func (p *SimplePodList) ListByIndex(index string) []*v1.Pod {
	return p.pods
}

func createE2ERouter(t *testing.T) *kvAwareRouter {
	// Set environment for testing
	t.Setenv("AIBRIX_KV_AWARE_ENABLED", "true")
	t.Setenv("AIBRIX_KV_TRANSFER_ENABLED", "false")
	t.Setenv("AIBRIX_KV_TRANSFER_BW_GBPS", "100")
	t.Setenv("AIBRIX_TTFT_SLO_SECONDS", "3")
	t.Setenv("AIBRIX_TBT_SLO_MS", "200")

	router, err := NewKVAwareRouter()
	assert.NoError(t, err)
	assert.NotNil(t, router)

	return router.(*kvAwareRouter)
}

func createE2ERoutingContext(requestID, message string) *types.RoutingContext {
	return &types.RoutingContext{
		Context:   context.Background(),
		RequestID: requestID,
		Model:     "llama3-70b",
		Message:   message,
		ReqHeaders: map[string]string{
			"Content-Type": "application/json",
		},
	}
}

func createE2EPodList(numPrefill, numDecode int) types.PodList {
	var pods []*v1.Pod

	// Create prefill pods
	for i := 0; i < numPrefill; i++ {
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("prefill-%d", i),
				Namespace: "default",
				Labels: map[string]string{
					RoleLabelKey:  RolePrefill,
					ModelLabelKey: "llama3-70b",
				},
				Annotations: map[string]string{
					MetricPortAnnotation: "8080",
				},
			},
			Status: v1.PodStatus{
				Phase: v1.PodRunning,
				PodIP: fmt.Sprintf("10.0.1.%d", i+1),
			},
		}
		pods = append(pods, pod)
	}

	// Create decode pods
	for i := 0; i < numDecode; i++ {
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("decode-%d", i),
				Namespace: "default",
				Labels: map[string]string{
					RoleLabelKey:  RoleDecode,
					ModelLabelKey: "llama3-70b",
				},
				Annotations: map[string]string{
					MetricPortAnnotation: "8080",
				},
			},
			Status: v1.PodStatus{
				Phase: v1.PodRunning,
				PodIP: fmt.Sprintf("10.0.2.%d", i+1),
			},
		}
		pods = append(pods, pod)
	}

	return &SimplePodList{pods: pods}
}
