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
	"testing"
	"time"

	"github.com/vllm-project/aibrix/pkg/types"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BenchmarkRoute_CompleteFlow benchmarks the complete routing flow
func BenchmarkRoute_CompleteFlow(b *testing.B) {
	router := createBenchmarkRouter(b)

	ctx := &types.RoutingContext{
		Context:   context.Background(),
		RequestID: "bench-001",
		Model:     "llama3-70b",
		Message:   "Test prompt for benchmarking performance",
	}

	podList := createBenchmarkPodList()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = router.Route(ctx, podList)
	}

	b.StopTimer()

	// Report stats
	stats := router.stats.(*routingStatistics).GetSnapshot()
	if stats.TotalRequests > 0 {
		b.Logf("Success rate: %.2f%%", stats.SuccessRate*100)
		b.Logf("Average latency: %v", stats.AvgLatency)
		b.Logf("P95 latency: %v", stats.P95Latency)
	}
}

// BenchmarkRoute_ConcurrentAccess benchmarks concurrent routing access
func BenchmarkRoute_ConcurrentAccess(b *testing.B) {
	router := createBenchmarkRouter(b)
	podList := createBenchmarkPodList()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			ctx := &types.RoutingContext{
				Context:   context.Background(),
				RequestID: fmt.Sprintf("bench-concurrent-%d", i),
				Model:     "llama3-70b",
				Message:   "Test prompt",
			}
			_, _ = router.Route(ctx, podList)
			i++
		}
	})

	// Report final stats
	stats := router.stats.(*routingStatistics).GetSnapshot()
	b.Logf("Total requests: %d", stats.TotalRequests)
	b.Logf("Success rate: %.2f%%", stats.SuccessRate*100)
}

// BenchmarkTTFTEstimation benchmarks TTFT estimation alone
func BenchmarkTTFTEstimation(b *testing.B) {
	config := &KVAwareConfig{
		EMAAlpha: 0.7,
		Models: []ModelKVSpec{
			{
				ModelName:       "llama3-70b",
				PerTokenKVBytes: 327680,
				BlockSizeTokens: 16,
			},
		},
		TransferBandwidthBps: 1.25e10,
	}
	estimator := NewTTFTEstimator(config)

	pod := PodRef{Name: "test-pod", Namespace: "default", Role: "prefill"}
	prefixMatch := &PrefixMatch{
		PodPrefixBlocks: map[string]int{"default/test-pod": 5},
		BestBlocks:      10,
		BestPod:         "default/test-pod",
	}
	meanPrefill := 0.5
	p95Queue := 0.2
	metrics := PodMetrics{
		NumWaiting:     3,
		PromptTokPerS:  1000,
		MeanPrefillSec: &meanPrefill,
		P95QueueSec:    &p95Queue,
	}
	tokens := make([]int, 100)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = estimator.EstimateTTFT(pod, prefixMatch, metrics, tokens)
	}
}

// BenchmarkDecodeSelection benchmarks decode pod selection
func BenchmarkDecodeSelection(b *testing.B) {
	config := &KVAwareConfig{
		TBTSLO: 200 * time.Millisecond,
		Models: []ModelKVSpec{
			{
				ModelName:       "llama3-70b",
				PerTokenKVBytes: 327680,
				BlockSizeTokens: 16,
			},
		},
	}
	selector := NewDecodeSelector(config)

	decodePods := []PodRef{
		{Name: "decode-1", Namespace: "default", Role: "decode"},
		{Name: "decode-2", Namespace: "default", Role: "decode"},
		{Name: "decode-3", Namespace: "default", Role: "decode"},
	}

	p95Tpot := 0.15
	avgTpot := 0.12
	metricsMap := map[string]PodMetrics{
		"default/decode-1": {NumRunning: 2, GenTokPerS: 100, GPUCacheUsage: 60, P95TPOTSec: &p95Tpot, AvgTPOTSec: &avgTpot},
		"default/decode-2": {NumRunning: 1, GenTokPerS: 120, GPUCacheUsage: 50, P95TPOTSec: &p95Tpot, AvgTPOTSec: &avgTpot},
		"default/decode-3": {NumRunning: 3, GenTokPerS: 80, GPUCacheUsage: 70, P95TPOTSec: &p95Tpot, AvgTPOTSec: &avgTpot},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _, _ = selector.SelectDecodePod(decodePods, metricsMap, config.TBTSLO, 100)
	}
}

// BenchmarkPrefixMatching benchmarks prefix matching computation
func BenchmarkPrefixMatching(b *testing.B) {
	matcher := &mockBenchPrefixMatcher{}

	tokens := make([]int, 500) // 500 token prompt
	for i := range tokens {
		tokens[i] = i
	}

	pods := []PodRef{
		{Name: "pod-1", Namespace: "default"},
		{Name: "pod-2", Namespace: "default"},
		{Name: "pod-3", Namespace: "default"},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = matcher.ComputePrefixMatch("llama3-70b", -1, tokens, pods, 16)
	}
}

// BenchmarkStatistics_Recording benchmarks statistics recording
func BenchmarkStatistics_Recording(b *testing.B) {
	stats := NewRoutingStatistics()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		stats.IncrementTotal()
		stats.RecordLatency(1 * time.Millisecond)
		stats.RecordCacheHit(0.5)
	}
}

// BenchmarkStatistics_GetSnapshot benchmarks snapshot generation
func BenchmarkStatistics_GetSnapshot(b *testing.B) {
	stats := NewRoutingStatistics().(*routingStatistics)

	// Populate with data
	for i := 0; i < 1000; i++ {
		stats.IncrementTotal()
		stats.IncrementSuccess()
		stats.RecordLatency(time.Duration(i) * time.Microsecond)
		stats.RecordCacheHit(float64(i) / 1000.0)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = stats.GetSnapshot()
	}
}

// Helper functions

func createBenchmarkRouter(b *testing.B) *kvAwareRouter {
	config := &KVAwareConfig{
		Enabled:              true,
		EnableKVTransfer:     false,
		TransferBandwidthBps: 1.25e10,
		TTFTSLO:              3 * time.Second,
		TBTSLO:               200 * time.Millisecond,
		EMAAlpha:             0.7,
		Models: []ModelKVSpec{
			{
				ModelName:       "llama3-70b",
				PerTokenKVBytes: 327680,
				BlockSizeTokens: 16,
			},
		},
	}

	// Use mock prefix matcher for benchmarking
	router := &kvAwareRouter{
		config:         config,
		tokenizer:      NewTokenizer(),
		prefixMatcher:  &mockBenchPrefixMatcher{},
		metricsReader:  &mockBenchMetricsReader{},
		metricsCache:   newMetricsCache(5 * time.Second),
		ttftEstimator:  NewTTFTEstimator(config),
		decodeSelector: NewDecodeSelector(config),
		sloChecker:     NewSLOChecker(),
		stats:          NewRoutingStatistics(),
		fallbackAlgo:   types.RoutingAlgorithm("least-request"),
	}

	return router
}

func createBenchmarkPodList() types.PodList {
	pods := make([]*v1.Pod, 20)
	for i := 0; i < 10; i++ {
		pods[i] = createBenchmarkPod(
			fmt.Sprintf("prefill-%d", i),
			RolePrefill,
			fmt.Sprintf("10.0.1.%d", i+1),
		)
		pods[i+10] = createBenchmarkPod(
			fmt.Sprintf("decode-%d", i),
			RoleDecode,
			fmt.Sprintf("10.0.2.%d", i+1),
		)
	}
	return &SimplePodList{pods: pods}
}

func createBenchmarkPod(name, role, ip string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels: map[string]string{
				RoleLabelKey:  role,
				ModelLabelKey: "llama3-70b",
			},
			Annotations: map[string]string{
				MetricPortAnnotation: "8080",
			},
		},
		Status: v1.PodStatus{
			Phase: v1.PodRunning,
			PodIP: ip,
		},
	}
}

// mockBenchPrefixMatcher provides mock prefix matching for benchmarking
type mockBenchPrefixMatcher struct{}

func (m *mockBenchPrefixMatcher) ComputePrefixMatch(
	modelName string, loraID int64, tokens []int, pods []PodRef, blockSize int,
) (PrefixMatch, error) {
	// Return mock prefix match - assume 50% cache hit
	matchBlocks := len(tokens) / blockSize / 2
	podPrefixBlocks := make(map[string]int)

	for _, pod := range pods {
		// Vary prefix blocks per pod for realistic testing
		podPrefixBlocks[pod.Key()] = matchBlocks
	}

	if len(pods) > 0 {
		return PrefixMatch{
			PodPrefixBlocks: podPrefixBlocks,
			BestPod:         pods[0].Key(),
			BestBlocks:      matchBlocks,
		}, nil
	}

	return PrefixMatch{
		PodPrefixBlocks: make(map[string]int),
		BestBlocks:      0,
	}, nil
}

// mockBenchMetricsReader provides mock metrics for benchmarking
type mockBenchMetricsReader struct{}

func (m *mockBenchMetricsReader) GetPodMetrics(pod PodRef) (PodMetrics, error) {
	meanPrefill := 0.5
	p95Queue := 0.2
	p95Tpot := 0.15
	avgTpot := 0.12

	return PodMetrics{
		NumWaiting:       2.0,
		NumRunning:       3.0,
		GPUCacheUsage:    60.0,
		CPUCacheUsage:    40.0,
		PromptTokPerS:    1000.0,
		GenTokPerS:       100.0,
		MeanPrefillSec:   &meanPrefill,
		P95QueueSec:      &p95Queue,
		P95TPOTSec:       &p95Tpot,
		AvgTPOTSec:       &avgTpot,
		LastUpdated:      time.Now(),
		MetricsAvailable: true,
	}, nil
}

func (m *mockBenchMetricsReader) BatchGetPodMetrics(pods []PodRef) map[string]PodMetrics {
	result := make(map[string]PodMetrics)
	for _, pod := range pods {
		metrics, _ := m.GetPodMetrics(pod)
		result[pod.Key()] = metrics
	}
	return result
}
