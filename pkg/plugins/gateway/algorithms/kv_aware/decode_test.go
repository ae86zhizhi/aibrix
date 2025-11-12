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
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test helper: create test config
func createDecodeTestConfig() *KVAwareConfig {
	return &KVAwareConfig{
		Enabled:              true,
		TTFTSLO:              30 * time.Second,
		TBTSLO:               200 * time.Millisecond,
		TransferBandwidthBps: 100 * 1e9, // 100 Gbps
		Models: []ModelKVSpec{
			{
				ModelName:       "llama3-70b",
				PerTokenKVBytes: 327680,
				BlockSizeTokens: 16,
			},
		},
	}
}

// ============================================================================
// TestGetCurrentTBT - 4 test cases
// ============================================================================

func TestGetCurrentTBT(t *testing.T) {
	config := createDecodeTestConfig()
	selector := NewDecodeSelector(config).(*defaultDecodeSelector)

	t.Run("from_p95_tpot", func(t *testing.T) {
		p95 := 0.15
		metrics := PodMetrics{
			P95TPOTSec: &p95,
			GenTokPerS: 10,
		}
		result := selector.getCurrentTBT(metrics)
		assert.Equal(t, 0.15, result, "Should use P95 TPOT when available")
	})

	t.Run("from_avg_tpot", func(t *testing.T) {
		avg := 0.12
		metrics := PodMetrics{
			AvgTPOTSec: &avg,
			GenTokPerS: 10,
		}
		result := selector.getCurrentTBT(metrics)
		assert.Equal(t, 0.12, result, "Should use Avg TPOT when P95 unavailable")
	})

	t.Run("from_throughput", func(t *testing.T) {
		metrics := PodMetrics{
			GenTokPerS: 20, // 1/20 = 0.05s
		}
		result := selector.getCurrentTBT(metrics)
		assert.InDelta(t, 0.05, result, 0.001, "Should calculate from throughput")
	})

	t.Run("fallback_default", func(t *testing.T) {
		metrics := PodMetrics{}
		result := selector.getCurrentTBT(metrics)
		assert.Equal(t, 0.2, result, "Should use 200ms fallback when no metrics")
	})
}

// ============================================================================
// TestPredictTBTWithNewRequest - 6 test cases
// ============================================================================

func TestPredictTBTWithNewRequest(t *testing.T) {
	config := createDecodeTestConfig()
	selector := NewDecodeSelector(config).(*defaultDecodeSelector)

	t.Run("low_load_low_cache", func(t *testing.T) {
		p95 := 0.1
		metrics := PodMetrics{
			P95TPOTSec:    &p95,
			NumRunning:    1.0,
			GPUCacheUsage: 50.0,
		}
		result := selector.predictTBTWithNewRequest(metrics, 500)
		// Expected: 0.1 * (1 + 0.1*1) * 1.0 * 1.0 = 0.11
		assert.InDelta(t, 0.11, result, 0.001, "Low load should have minimal impact")
	})

	t.Run("high_load_moderate_cache", func(t *testing.T) {
		p95 := 0.1
		metrics := PodMetrics{
			P95TPOTSec:    &p95,
			NumRunning:    5.0,
			GPUCacheUsage: 75.0,
		}
		result := selector.predictTBTWithNewRequest(metrics, 500)
		// Expected: 0.1 * (1 + 0.1*5) * 1.0 * 1.0 = 0.15
		assert.InDelta(t, 0.15, result, 0.001, "High load should increase TBT")
	})

	t.Run("high_cache_pressure", func(t *testing.T) {
		p95 := 0.1
		metrics := PodMetrics{
			P95TPOTSec:    &p95,
			NumRunning:    2.0,
			GPUCacheUsage: 85.0, // > 80%
		}
		result := selector.predictTBTWithNewRequest(metrics, 500)
		// Expected: 0.1 * (1 + 0.1*2) * 1.2 * 1.0 = 0.144
		assert.InDelta(t, 0.144, result, 0.001, "Cache > 80% should apply 20% penalty")
	})

	t.Run("very_high_cache_pressure", func(t *testing.T) {
		p95 := 0.1
		metrics := PodMetrics{
			P95TPOTSec:    &p95,
			NumRunning:    2.0,
			GPUCacheUsage: 92.0, // > 90%
		}
		result := selector.predictTBTWithNewRequest(metrics, 500)
		// Expected: 0.1 * (1 + 0.1*2) * 1.5 * 1.0 = 0.18
		assert.InDelta(t, 0.18, result, 0.001, "Cache > 90% should apply 50% penalty")
	})

	t.Run("long_sequence", func(t *testing.T) {
		p95 := 0.1
		metrics := PodMetrics{
			P95TPOTSec:    &p95,
			NumRunning:    1.0,
			GPUCacheUsage: 50.0,
		}
		result := selector.predictTBTWithNewRequest(metrics, 1500) // > 1000
		// Expected: 0.1 * (1 + 0.1*1) * 1.0 * 1.1 = 0.121
		assert.InDelta(t, 0.121, result, 0.001, "Sequence > 1000 should apply 10% penalty")
	})

	t.Run("very_long_sequence", func(t *testing.T) {
		p95 := 0.1
		metrics := PodMetrics{
			P95TPOTSec:    &p95,
			NumRunning:    1.0,
			GPUCacheUsage: 50.0,
		}
		result := selector.predictTBTWithNewRequest(metrics, 2500) // > 2000
		// Expected: 0.1 * (1 + 0.1*1) * 1.0 * 1.2 = 0.132
		assert.InDelta(t, 0.132, result, 0.001, "Sequence > 2000 should apply 20% penalty")
	})
}

// ============================================================================
// TestCalculateScore - 3 test cases
// ============================================================================

func TestCalculateScore(t *testing.T) {
	config := createDecodeTestConfig()
	selector := NewDecodeSelector(config).(*defaultDecodeSelector)

	t.Run("good_metrics_high_score", func(t *testing.T) {
		metrics := PodMetrics{
			NumRunning:    1.0,
			GenTokPerS:    500.0,
			GPUCacheUsage: 30.0,
		}
		currentTBT := 0.05
		predictedTBT := 0.06
		score := selector.calculateScore(metrics, currentTBT, predictedTBT)

		// TBT score: 1/(1+0.06) ≈ 0.943
		// Throughput score: 500/1000 = 0.5
		// Utilization score: 1 - 0.3 = 0.7
		// Load score: 1/(1+1) = 0.5
		// Final: 0.943*0.4 + 0.5*0.3 + 0.7*0.2 + 0.5*0.1 ≈ 0.767
		assert.Greater(t, score, 0.7, "Good metrics should yield high score")
	})

	t.Run("poor_metrics_low_score", func(t *testing.T) {
		metrics := PodMetrics{
			NumRunning:    10.0,
			GenTokPerS:    50.0,
			GPUCacheUsage: 95.0,
		}
		currentTBT := 0.5
		predictedTBT := 0.8
		score := selector.calculateScore(metrics, currentTBT, predictedTBT)

		// Poor metrics should yield low score
		assert.Less(t, score, 0.3, "Poor metrics should yield low score")
	})

	t.Run("compare_relative_scores", func(t *testing.T) {
		metrics1 := PodMetrics{
			NumRunning:    1.0,
			GenTokPerS:    600.0,
			GPUCacheUsage: 40.0,
		}
		score1 := selector.calculateScore(metrics1, 0.05, 0.06)

		metrics2 := PodMetrics{
			NumRunning:    5.0,
			GenTokPerS:    200.0,
			GPUCacheUsage: 80.0,
		}
		score2 := selector.calculateScore(metrics2, 0.15, 0.20)

		assert.Greater(t, score1, score2, "Better metrics should yield higher score")
	})
}

// ============================================================================
// TestSelectDecodePod - 5 test cases
// ============================================================================

func TestSelectDecodePod(t *testing.T) {
	config := createDecodeTestConfig()
	selector := NewDecodeSelector(config).(*defaultDecodeSelector)

	t.Run("all_meet_slo", func(t *testing.T) {
		pods := []PodRef{
			{Name: "pod-1", Namespace: "default", IPPort: "10.0.0.1:8080", ModelName: "llama3-70b"},
			{Name: "pod-2", Namespace: "default", IPPort: "10.0.0.2:8080", ModelName: "llama3-70b"},
		}

		p95_1 := 0.05
		p95_2 := 0.08
		metricsMap := map[string]PodMetrics{
			"default/pod-1": {P95TPOTSec: &p95_1, NumRunning: 1.0, GenTokPerS: 400.0, GPUCacheUsage: 40.0},
			"default/pod-2": {P95TPOTSec: &p95_2, NumRunning: 2.0, GenTokPerS: 300.0, GPUCacheUsage: 60.0},
		}

		selectedPod, tbt, err := selector.SelectDecodePod(pods, metricsMap, 200*time.Millisecond, 500)
		require.NoError(t, err)
		assert.NotNil(t, selectedPod)
		assert.Equal(t, "pod-1", selectedPod.Name, "Should select pod with better metrics")
		assert.Greater(t, tbt, 0.0)
	})

	t.Run("some_violate_slo", func(t *testing.T) {
		pods := []PodRef{
			{Name: "pod-1", Namespace: "default", IPPort: "10.0.0.1:8080", ModelName: "llama3-70b"},
			{Name: "pod-2", Namespace: "default", IPPort: "10.0.0.2:8080", ModelName: "llama3-70b"},
		}

		p95_1 := 0.25 // Violates 200ms SLO with safety margin
		p95_2 := 0.08
		metricsMap := map[string]PodMetrics{
			"default/pod-1": {P95TPOTSec: &p95_1, NumRunning: 8.0, GenTokPerS: 100.0, GPUCacheUsage: 95.0},
			"default/pod-2": {P95TPOTSec: &p95_2, NumRunning: 2.0, GenTokPerS: 300.0, GPUCacheUsage: 60.0},
		}

		selectedPod, _, err := selector.SelectDecodePod(pods, metricsMap, 200*time.Millisecond, 500)
		require.NoError(t, err)
		assert.Equal(t, "pod-2", selectedPod.Name, "Should select pod meeting SLO")
	})

	t.Run("all_violate_fallback_relaxed", func(t *testing.T) {
		pods := []PodRef{
			{Name: "pod-1", Namespace: "default", IPPort: "10.0.0.1:8080", ModelName: "llama3-70b"},
		}

		p95 := 0.22 // Violates 200ms (180ms with safety), but meets 300ms relaxed
		metricsMap := map[string]PodMetrics{
			"default/pod-1": {P95TPOTSec: &p95, NumRunning: 3.0, GenTokPerS: 150.0, GPUCacheUsage: 70.0},
		}

		selectedPod, _, err := selector.SelectDecodePod(pods, metricsMap, 200*time.Millisecond, 500)
		require.NoError(t, err, "Should use relaxed SLO fallback")
		assert.Equal(t, "pod-1", selectedPod.Name)
	})

	t.Run("all_violate_even_relaxed", func(t *testing.T) {
		pods := []PodRef{
			{Name: "pod-1", Namespace: "default", IPPort: "10.0.0.1:8080", ModelName: "llama3-70b"},
		}

		p95 := 0.35 // Violates even relaxed SLO (300ms)
		metricsMap := map[string]PodMetrics{
			"default/pod-1": {P95TPOTSec: &p95, NumRunning: 10.0, GenTokPerS: 50.0, GPUCacheUsage: 98.0},
		}

		_, _, err := selector.SelectDecodePod(pods, metricsMap, 200*time.Millisecond, 500)
		assert.ErrorIs(t, err, ErrTBTSLOViolation, "Should reject when even relaxed SLO violated")
	})

	t.Run("empty_decode_pods", func(t *testing.T) {
		pods := []PodRef{}
		metricsMap := map[string]PodMetrics{}

		_, _, err := selector.SelectDecodePod(pods, metricsMap, 200*time.Millisecond, 500)
		assert.ErrorIs(t, err, ErrNoDecodeCandidate, "Should return error for empty pod list")
	})
}

// ============================================================================
// TestSLOChecker - 7 test cases
// ============================================================================

func TestSLOChecker(t *testing.T) {
	checker := NewSLOChecker()

	t.Run("ttft_meets_slo", func(t *testing.T) {
		err := checker.CheckTTFTSLO(25.0, 30*time.Second)
		assert.NoError(t, err, "25s < 30s should pass")
	})

	t.Run("ttft_violates_slo", func(t *testing.T) {
		err := checker.CheckTTFTSLO(35.0, 30*time.Second)
		assert.Error(t, err, "35s > 30s should fail")
	})

	t.Run("ttft_no_slo_specified", func(t *testing.T) {
		err := checker.CheckTTFTSLO(100.0, 0)
		assert.NoError(t, err, "No SLO specified should always pass")
	})

	t.Run("tbt_meets_slo", func(t *testing.T) {
		err := checker.CheckTBTSLO(0.15, 200*time.Millisecond)
		assert.NoError(t, err, "150ms < 200ms should pass")
	})

	t.Run("tbt_violates_slo", func(t *testing.T) {
		err := checker.CheckTBTSLO(0.25, 200*time.Millisecond)
		assert.Error(t, err, "250ms > 200ms should fail")
	})

	t.Run("tbt_no_slo_specified", func(t *testing.T) {
		err := checker.CheckTBTSLO(1.0, 0)
		assert.NoError(t, err, "No SLO specified should always pass")
	})

	t.Run("should_reject_ttft_violation", func(t *testing.T) {
		shouldReject := checker.ShouldReject(35.0, 0.15, 30*time.Second, 200*time.Millisecond)
		assert.True(t, shouldReject, "TTFT violation should cause rejection")
	})

	t.Run("should_reject_tbt_violation", func(t *testing.T) {
		shouldReject := checker.ShouldReject(25.0, 0.25, 30*time.Second, 200*time.Millisecond)
		assert.True(t, shouldReject, "TBT violation should cause rejection")
	})

	t.Run("should_not_reject_both_meet", func(t *testing.T) {
		shouldReject := checker.ShouldReject(25.0, 0.15, 30*time.Second, 200*time.Millisecond)
		assert.False(t, shouldReject, "Both meeting SLO should not reject")
	})
}

// ============================================================================
// TestEstimateOutputTokens - 4 test cases
// ============================================================================

func TestEstimateOutputTokens(t *testing.T) {
	t.Run("minimum_bound", func(t *testing.T) {
		result := estimateOutputTokens(5)
		assert.Equal(t, 10, result, "Should apply minimum of 10 tokens")
	})

	t.Run("normal_range", func(t *testing.T) {
		result := estimateOutputTokens(500)
		assert.Equal(t, 500, result, "Should use 1x input length")
	})

	t.Run("maximum_bound", func(t *testing.T) {
		result := estimateOutputTokens(3000)
		assert.Equal(t, 2048, result, "Should apply maximum of 2048 tokens")
	})

	t.Run("exact_boundary", func(t *testing.T) {
		result := estimateOutputTokens(1000)
		assert.Equal(t, 1000, result, "Should handle exact values correctly")
	})
}

// ============================================================================
// Benchmark Tests
// ============================================================================

func BenchmarkSelectDecodePod(b *testing.B) {
	config := createDecodeTestConfig()
	selector := NewDecodeSelector(config).(*defaultDecodeSelector)

	pods := make([]PodRef, 10)
	metricsMap := make(map[string]PodMetrics)
	for i := 0; i < 10; i++ {
		ipPort := fmt.Sprintf("10.0.0.%d:8080", i+1)
		pods[i] = PodRef{
			Name:      fmt.Sprintf("pod-%d", i+1),
			Namespace: "default",
			IPPort:    ipPort,
			ModelName: "llama3-70b",
		}
		p95 := float64(0.1 + float64(i)*0.01)
		metricsMap[ipPort] = PodMetrics{
			P95TPOTSec:    &p95,
			NumRunning:    float64(i % 5),
			GenTokPerS:    300.0 - float64(i)*10,
			GPUCacheUsage: 50.0 + float64(i)*3,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = selector.SelectDecodePod(pods, metricsMap, 200*time.Millisecond, 500)
	}
}

func BenchmarkPredictTBT(b *testing.B) {
	config := createDecodeTestConfig()
	selector := NewDecodeSelector(config).(*defaultDecodeSelector)

	p95 := 0.1
	metrics := PodMetrics{
		P95TPOTSec:    &p95,
		NumRunning:    3.0,
		GenTokPerS:    300.0,
		GPUCacheUsage: 70.0,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = selector.predictTBTWithNewRequest(metrics, 500)
	}
}
