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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test helper: create test config
func createTestConfig() *KVAwareConfig {
	return &KVAwareConfig{
		Enabled:                 true,
		EnableKVTransfer:        false,     // MVP: no actual transfer
		TransferBandwidthBps:    100e9 / 8, // 100 Gbps = 12.5 GB/s
		KVCopyThresholdBlk:      4,
		QueueFallbackMultiplier: 0.8,
		EMAAlpha:                0.7,
		TTFTSLO:                 30 * time.Second,
		TBTSLO:                  200 * time.Millisecond,
		Models: []ModelKVSpec{
			{
				ModelName:       "llama3-70b",
				PerTokenKVBytes: 327680, // 320 KB per token
				BlockSizeTokens: 16,
			},
		},
	}
}

// Test helper: create test estimator
func createTestEstimator() *defaultTTFTEstimator {
	return &defaultTTFTEstimator{
		config: createTestConfig(),
	}
}

// ============================================================================
// TestEstimateTransferTime - 7 test cases
// ============================================================================

func TestEstimateTransferTime(t *testing.T) {
	estimator := createTestEstimator()

	t.Run("no transfer when local equals best", func(t *testing.T) {
		result := estimator.estimateTransferTime(10, 10, 327680, 16)
		assert.Equal(t, 0.0, result, "Should return 0 when local >= best")
	})

	t.Run("no transfer when local exceeds best", func(t *testing.T) {
		result := estimator.estimateTransferTime(10, 15, 327680, 16)
		assert.Equal(t, 0.0, result, "Should return 0 when local > best")
	})

	t.Run("no transfer when KV transfer disabled", func(t *testing.T) {
		estimator.config.EnableKVTransfer = false
		result := estimator.estimateTransferTime(20, 10, 327680, 16)
		assert.Equal(t, 0.0, result, "Should return 0 when EnableKVTransfer=false")
	})

	t.Run("no transfer when difference below threshold", func(t *testing.T) {
		estimator.config.EnableKVTransfer = true
		estimator.config.KVCopyThresholdBlk = 5
		// Difference = 14 - 10 = 4 blocks, which is <= threshold of 5
		result := estimator.estimateTransferTime(14, 10, 327680, 16)
		assert.Equal(t, 0.0, result, "Should return 0 when block difference <= threshold")
	})

	t.Run("valid transfer estimation", func(t *testing.T) {
		estimator.config.EnableKVTransfer = true
		estimator.config.KVCopyThresholdBlk = 4
		estimator.config.TransferBandwidthBps = 100e9 / 8 // 12.5 GB/s

		// Transfer: 20 - 10 = 10 blocks = 160 tokens = 160 * 327680 = 52,428,800 bytes
		// Time = 52,428,800 / 12,500,000,000 ≈ 0.00419 seconds
		result := estimator.estimateTransferTime(20, 10, 327680, 16)

		assert.Greater(t, result, 0.0, "Should return positive transfer time")
		assert.Less(t, result, 1.0, "Transfer time should be < 1 second for 10 blocks")
		assert.InDelta(t, 0.00419, result, 0.001, "Transfer time calculation")
	})

	t.Run("large transfer", func(t *testing.T) {
		estimator.config.EnableKVTransfer = true
		estimator.config.KVCopyThresholdBlk = 4
		estimator.config.TransferBandwidthBps = 100e9 / 8 // 12.5 GB/s

		// Transfer: 100 blocks = 1600 tokens = 1600 * 327680 = 524,288,000 bytes ≈ 500 MB
		// Time = 524,288,000 / 12,500,000,000 ≈ 0.0419 seconds
		result := estimator.estimateTransferTime(100, 0, 327680, 16)

		assert.Greater(t, result, 0.0, "Should return positive transfer time")
		assert.InDelta(t, 0.0419, result, 0.01, "Transfer time for 100 blocks")
	})

	t.Run("zero bandwidth", func(t *testing.T) {
		estimator.config.EnableKVTransfer = true
		estimator.config.KVCopyThresholdBlk = 4
		estimator.config.TransferBandwidthBps = 0

		result := estimator.estimateTransferTime(20, 10, 327680, 16)
		assert.Equal(t, 0.0, result, "Should return 0 when bandwidth is 0")
	})
}

// ============================================================================
// TestEstimateQueueTime - 6 test cases
// ============================================================================

func TestEstimateQueueTime(t *testing.T) {
	estimator := createTestEstimator()

	t.Run("no queue when no waiting requests", func(t *testing.T) {
		metrics := PodMetrics{
			NumWaiting: 0,
		}
		result := estimator.estimateQueueTime(metrics)
		assert.Equal(t, 0.0, result, "Should return 0 when no waiting requests")
	})

	// Phase 008: P95 from histogram is now PREFERRED over service rate
	t.Run("Method A - P95 from histogram", func(t *testing.T) {
		p95Queue := 2.5
		metrics := PodMetrics{
			NumWaiting:  10,
			P95QueueSec: &p95Queue,
		}

		// Should use P95 directly (Method A)
		result := estimator.estimateQueueTime(metrics)
		assert.Equal(t, 2.5, result, "Should use P95 directly")
	})

	t.Run("Method A - P95 with scaling", func(t *testing.T) {
		p95Queue := 2.0
		avgWaiting := 5.0
		metrics := PodMetrics{
			NumWaiting:    10, // Current queue 2x average
			P95QueueSec:   &p95Queue,
			AvgNumWaiting: &avgWaiting,
		}

		// ratio = 10/5 = 2, scale = 2^0.7 ≈ 1.62
		// result = 2.0 * 1.62 ≈ 3.25
		result := estimator.estimateQueueTime(metrics)
		assert.InDelta(t, 3.25, result, 0.1, "Should scale P95 by queue ratio")
	})

	// Phase 008: P95 is now preferred, so this test validates P95 takes precedence
	t.Run("P95 preferred over Lambda", func(t *testing.T) {
		p95Queue := 10.0
		lambda := 2.0
		metrics := PodMetrics{
			NumWaiting:    5,
			P95QueueSec:   &p95Queue,
			LambdaReqPerS: &lambda,
		}

		// P95 is preferred (Method A), should return 10.0
		result := estimator.estimateQueueTime(metrics)
		assert.Equal(t, 10.0, result, "Should use P95 over Lambda")
	})

	t.Run("Method B - Little's Law with Lambda", func(t *testing.T) {
		lambda := 2.0 // 2 req/s
		metrics := PodMetrics{
			NumWaiting:    10,
			LambdaReqPerS: &lambda,
			// No P95QueueSec, falls to Method B
		}

		// W_mean = L/λ = 10/2 = 5, factor = 1.5 (default)
		// result = 5 * 1.5 = 7.5
		result := estimator.estimateQueueTime(metrics)
		assert.InDelta(t, 7.5, result, 0.1, "Should use Little's Law with Lambda")
	})

	t.Run("Method C - fallback heuristic", func(t *testing.T) {
		metrics := PodMetrics{
			NumWaiting: 8,
			// No P95QueueSec, no LambdaReqPerS
		}

		// Fallback: 8 * 0.5 = 4s (default basePerReq)
		result := estimator.estimateQueueTime(metrics)
		assert.Equal(t, 4.0, result, "Should use fallback heuristic")
	})

	t.Run("Method C - fallback with MeanPrefillSec", func(t *testing.T) {
		meanPrefill := 0.8
		metrics := PodMetrics{
			NumWaiting:     4,
			MeanPrefillSec: &meanPrefill,
			// No P95QueueSec, no LambdaReqPerS
		}

		// Uses MeanPrefillSec as basePerReq: 4 * 0.8 = 3.2s
		result := estimator.estimateQueueTime(metrics)
		assert.Equal(t, 3.2, result, "Should use MeanPrefillSec as base")
	})

	t.Run("zero Lambda falls back", func(t *testing.T) {
		lambda := 0.0
		p95Queue := 3.0
		metrics := PodMetrics{
			NumWaiting:    5,
			LambdaReqPerS: &lambda,
			P95QueueSec:   &p95Queue,
		}

		// P95 is available, should use it (Method A)
		result := estimator.estimateQueueTime(metrics)
		assert.Equal(t, 3.0, result, "Should use P95 when Lambda is 0")
	})
}

// ============================================================================
// TestEstimatePrefillTime - 7 test cases
// ============================================================================

func TestEstimatePrefillTime(t *testing.T) {
	estimator := createTestEstimator()

	t.Run("all tokens cached", func(t *testing.T) {
		metrics := PodMetrics{
			PromptTokPerS: 1000,
		}

		// 512 tokens total, 32 blocks * 16 = 512 tokens cached
		result := estimator.estimatePrefillTime(512, 32, 16, metrics)
		assert.Equal(t, 0.0, result, "Should return 0 when all tokens cached")
	})

	t.Run("throughput-based estimation", func(t *testing.T) {
		metrics := PodMetrics{
			PromptTokPerS: 2000, // 2000 tokens/s
		}

		// 1000 tokens total, 500 tokens cached (31.25 blocks, but we use 31)
		// Uncached: 1000 - 31*16 = 1000 - 496 = 504 tokens
		// Time = 504 / 2000 = 0.252s
		result := estimator.estimatePrefillTime(1000, 31, 16, metrics)

		assert.InDelta(t, 0.252, result, 0.01, "Throughput-based estimation")
	})

	// Phase 008: EMA now fuses per-token time, not per-request time
	t.Run("throughput with EMA per-token calibration", func(t *testing.T) {
		meanPrefillPerTok := 0.0008 // 0.8ms per token from historical data
		metrics := PodMetrics{
			PromptTokPerS:     2000, // 2000 tok/s -> instant = 0.0005 s/tok
			MeanPrefillPerTok: &meanPrefillPerTok,
		}

		// Uncached: 512 tokens
		// instantPerTok = 1/2000 = 0.0005 s/tok
		// EMA: 0.7 * 0.0005 + 0.3 * 0.0008 = 0.00035 + 0.00024 = 0.00059 s/tok
		// result = 512 * 0.00059 = 0.30208
		result := estimator.estimatePrefillTime(512, 0, 16, metrics)

		expected := 512 * (0.7*0.0005 + 0.3*0.0008)
		assert.InDelta(t, expected, result, 0.01, "EMA per-token calibration")
	})

	t.Run("throughput only (no per-token history)", func(t *testing.T) {
		// When MeanPrefillPerTok is not available, just use throughput
		metrics := PodMetrics{
			PromptTokPerS: 2000, // 2000 tok/s -> 0.0005 s/tok
		}

		// Uncached: 512 tokens
		// result = 512 * 0.0005 = 0.256s
		result := estimator.estimatePrefillTime(512, 0, 16, metrics)

		assert.InDelta(t, 0.256, result, 0.01, "Throughput only estimation")
	})

	t.Run("historical scaling", func(t *testing.T) {
		meanPrefill := 2.0
		metrics := PodMetrics{
			PromptTokPerS:  0, // No throughput data
			MeanPrefillSec: &meanPrefill,
		}

		// 1024 uncached tokens
		// Scale factor = 1024 / 512 = 2.0
		// Scaled = 2.0 * 2.0 = 4.0s
		result := estimator.estimatePrefillTime(1024, 0, 16, metrics)

		assert.Equal(t, 4.0, result, "Historical scaling estimation")
	})

	t.Run("fallback constant per token", func(t *testing.T) {
		metrics := PodMetrics{
			PromptTokPerS: 0,
			// No MeanPrefillSec
		}

		// 800 uncached tokens
		// Fallback = 800 * 0.001 = 0.8s
		result := estimator.estimatePrefillTime(800, 0, 16, metrics)

		assert.Equal(t, 0.8, result, "Fallback constant per token")
	})

	t.Run("partial cache hit", func(t *testing.T) {
		metrics := PodMetrics{
			PromptTokPerS: 5000,
		}

		// 2000 tokens total, 50 blocks cached = 800 tokens
		// Uncached = 2000 - 800 = 1200 tokens
		// Time = 1200 / 5000 = 0.24s
		result := estimator.estimatePrefillTime(2000, 50, 16, metrics)

		assert.InDelta(t, 0.24, result, 0.01, "Partial cache hit")
	})

	t.Run("negative uncached tokens", func(t *testing.T) {
		metrics := PodMetrics{
			PromptTokPerS: 1000,
		}

		// Edge case: more cached than total (should be 0)
		// 500 tokens total, 100 blocks cached = 1600 tokens
		// Uncached = 500 - 1600 = -1100, clamped to 0
		result := estimator.estimatePrefillTime(500, 100, 16, metrics)

		assert.Equal(t, 0.0, result, "Should handle negative uncached tokens")
	})
}

// ============================================================================
// TestEstimateTTFT - 5 test cases
// ============================================================================

func TestEstimateTTFT(t *testing.T) {
	estimator := createTestEstimator()

	// Test pod
	pod := PodRef{
		Name:      "test-pod",
		Namespace: "default",
		IPPort:    "10.0.0.1:8080",
		ModelName: "llama3-70b",
	}

	t.Run("basic TTFT estimation", func(t *testing.T) {
		prefixMatch := &PrefixMatch{
			BestBlocks: 10,
			PodPrefixBlocks: map[string]int{
				pod.Key(): 10,
			},
		}

		meanPrefill := 1.0
		metrics := PodMetrics{
			NumWaiting:     5,
			PromptTokPerS:  2000,
			MeanPrefillSec: &meanPrefill,
		}

		promptTokens := make([]int, 512)

		components, err := estimator.EstimateTTFT(pod, prefixMatch, metrics, promptTokens)

		require.NoError(t, err)
		assert.Equal(t, 0.0, components.TransferTime, "No transfer when local == best")
		assert.Greater(t, components.QueueTime, 0.0, "Should have queue time")
		assert.Greater(t, components.PrefillTime, 0.0, "Should have prefill time")
		assert.Equal(t, components.TotalTTFT,
			components.TransferTime+components.QueueTime+components.PrefillTime,
			"Total should be sum of components")
	})

	t.Run("TTFT with potential transfer", func(t *testing.T) {
		estimator.config.EnableKVTransfer = true
		estimator.config.KVCopyThresholdBlk = 4

		prefixMatch := &PrefixMatch{
			BestBlocks: 20,
			PodPrefixBlocks: map[string]int{
				pod.Key(): 10,
			},
		}

		metrics := PodMetrics{
			NumWaiting:    0,
			PromptTokPerS: 2000,
		}

		promptTokens := make([]int, 512)

		components, err := estimator.EstimateTTFT(pod, prefixMatch, metrics, promptTokens)

		require.NoError(t, err)
		assert.Greater(t, components.TransferTime, 0.0, "Should estimate transfer time")
		assert.Equal(t, 0.0, components.QueueTime, "No queue time")
		// Prefill should use effectiveBlocks = bestBlocks after transfer
	})

	t.Run("TTFT bounds enforcement", func(t *testing.T) {
		// Create metrics that would produce very large values
		prefixMatch := &PrefixMatch{
			BestBlocks: 0,
			PodPrefixBlocks: map[string]int{
				pod.Key(): 0,
			},
		}

		meanPrefill := 100.0 // Very slow prefill
		metrics := PodMetrics{
			NumWaiting:     1000, // Huge queue
			PromptTokPerS:  1,    // Very slow
			MeanPrefillSec: &meanPrefill,
		}

		promptTokens := make([]int, 10000) // Large prompt

		components, err := estimator.EstimateTTFT(pod, prefixMatch, metrics, promptTokens)

		require.NoError(t, err)
		// Bounds: transfer [0,10], queue [0,30], prefill [0,60]
		assert.LessOrEqual(t, components.TransferTime, 10.0, "Transfer time capped at 10s")
		assert.LessOrEqual(t, components.QueueTime, 30.0, "Queue time capped at 30s")
		assert.LessOrEqual(t, components.PrefillTime, 60.0, "Prefill time capped at 60s")
	})

	t.Run("model config lookup", func(t *testing.T) {
		prefixMatch := &PrefixMatch{
			BestBlocks: 0,
			PodPrefixBlocks: map[string]int{
				pod.Key(): 0,
			},
		}

		metrics := PodMetrics{
			PromptTokPerS: 1000,
		}

		promptTokens := make([]int, 100)

		// Should find "llama3-70b" in config
		components, err := estimator.EstimateTTFT(pod, prefixMatch, metrics, promptTokens)

		require.NoError(t, err)
		assert.Greater(t, components.TotalTTFT, 0.0, "Should compute TTFT with model config")
	})

	t.Run("unknown model fallback", func(t *testing.T) {
		unknownPod := PodRef{
			Name:      "unknown-model-pod",
			Namespace: "default",
			IPPort:    "10.0.0.2:8080",
			ModelName: "unknown-model-xyz",
		}

		prefixMatch := &PrefixMatch{
			BestBlocks: 0,
			PodPrefixBlocks: map[string]int{
				unknownPod.Key(): 0,
			},
		}

		metrics := PodMetrics{
			PromptTokPerS: 1000,
		}

		promptTokens := make([]int, 100)

		// Should use default config for unknown model
		components, err := estimator.EstimateTTFT(unknownPod, prefixMatch, metrics, promptTokens)

		require.NoError(t, err)
		assert.Greater(t, components.TotalTTFT, 0.0, "Should compute TTFT with default config")
	})
}

// ============================================================================
// TestEstimatePrefillPods - 4 test cases
// ============================================================================

func TestEstimatePrefillPods(t *testing.T) {
	estimator := createTestEstimator()

	t.Run("single pod evaluation", func(t *testing.T) {
		pods := []PodRef{
			{Name: "pod-1", Namespace: "default", IPPort: "10.0.0.1:8080", ModelName: "llama3-70b"},
		}

		prefixMatch := &PrefixMatch{
			BestBlocks: 10,
			PodPrefixBlocks: map[string]int{
				pods[0].Key(): 10,
			},
		}

		meanPrefill := 1.0
		metricsMap := map[string]PodMetrics{
			pods[0].Key(): {
				NumWaiting:     2,
				PromptTokPerS:  2000,
				MeanPrefillSec: &meanPrefill,
			},
		}

		promptTokens := make([]int, 512)

		evals := estimator.EstimatePrefillPods(pods, prefixMatch, metricsMap, promptTokens)

		require.Len(t, evals, 1)
		assert.Equal(t, pods[0].Name, evals[0].Pod.Name)
		assert.Greater(t, evals[0].TTFT, 0.0, "Should have positive TTFT")
		assert.NotEmpty(t, evals[0].Explanation, "Should have explanation")
	})

	t.Run("multiple pods concurrent evaluation", func(t *testing.T) {
		pods := []PodRef{
			{Name: "pod-1", Namespace: "default", IPPort: "10.0.0.1:8080", ModelName: "llama3-70b"},
			{Name: "pod-2", Namespace: "default", IPPort: "10.0.0.2:8080", ModelName: "llama3-70b"},
			{Name: "pod-3", Namespace: "default", IPPort: "10.0.0.3:8080", ModelName: "llama3-70b"},
		}

		prefixMatch := &PrefixMatch{
			BestBlocks: 20,
			PodPrefixBlocks: map[string]int{
				pods[0].Key(): 20, // Best match
				pods[1].Key(): 15, // Good match
				pods[2].Key(): 5,  // Poor match
			},
		}

		meanPrefill := 1.0
		metricsMap := map[string]PodMetrics{
			pods[0].Key(): {NumWaiting: 0, PromptTokPerS: 2000, MeanPrefillSec: &meanPrefill},
			pods[1].Key(): {NumWaiting: 2, PromptTokPerS: 2000, MeanPrefillSec: &meanPrefill},
			pods[2].Key(): {NumWaiting: 5, PromptTokPerS: 2000, MeanPrefillSec: &meanPrefill},
		}

		promptTokens := make([]int, 512)

		evals := estimator.EstimatePrefillPods(pods, prefixMatch, metricsMap, promptTokens)

		require.Len(t, evals, 3)

		// Pod 1 should have lowest TTFT (best cache, no queue)
		assert.Less(t, evals[0].TTFT, evals[1].TTFT, "pod-1 should be faster than pod-2")
		assert.Less(t, evals[1].TTFT, evals[2].TTFT, "pod-2 should be faster than pod-3")

		// Verify all evals have required fields
		for i, eval := range evals {
			assert.Equal(t, pods[i].Name, eval.Pod.Name)
			assert.GreaterOrEqual(t, eval.TTFT, 0.0, "TTFT should be non-negative")
			assert.NotEmpty(t, eval.Explanation, "Should have explanation")
		}
	})

	t.Run("missing metrics handling", func(t *testing.T) {
		pods := []PodRef{
			{Name: "pod-1", Namespace: "default", IPPort: "10.0.0.1:8080", ModelName: "llama3-70b"},
		}

		prefixMatch := &PrefixMatch{
			BestBlocks: 10,
			PodPrefixBlocks: map[string]int{
				pods[0].Key(): 10,
			},
		}

		// Empty metrics map
		metricsMap := map[string]PodMetrics{}

		promptTokens := make([]int, 512)

		evals := estimator.EstimatePrefillPods(pods, prefixMatch, metricsMap, promptTokens)

		require.Len(t, evals, 1)
		// Should still compute TTFT with fallback methods
		assert.Greater(t, evals[0].TTFT, 0.0, "Should compute TTFT with missing metrics")
	})

	t.Run("transfer enabled evaluation", func(t *testing.T) {
		estimator.config.EnableKVTransfer = true
		estimator.config.KVCopyThresholdBlk = 4

		pods := []PodRef{
			{Name: "pod-1", Namespace: "default", IPPort: "10.0.0.1:8080", ModelName: "llama3-70b"},
			{Name: "pod-2", Namespace: "default", IPPort: "10.0.0.2:8080", ModelName: "llama3-70b"},
		}

		prefixMatch := &PrefixMatch{
			BestBlocks: 20,
			PodPrefixBlocks: map[string]int{
				pods[0].Key(): 20, // No transfer needed
				pods[1].Key(): 10, // Transfer 10 blocks
			},
		}

		meanPrefill := 1.0
		metricsMap := map[string]PodMetrics{
			pods[0].Key(): {NumWaiting: 0, PromptTokPerS: 2000, MeanPrefillSec: &meanPrefill},
			pods[1].Key(): {NumWaiting: 0, PromptTokPerS: 2000, MeanPrefillSec: &meanPrefill},
		}

		promptTokens := make([]int, 512)

		evals := estimator.EstimatePrefillPods(pods, prefixMatch, metricsMap, promptTokens)

		require.Len(t, evals, 2)

		// Pod 1 should have no transfer time
		assert.Equal(t, 0.0, evals[0].TTransfer, "pod-1 should have no transfer")
		assert.Equal(t, 20, evals[0].UseBestPrefixBlk, "pod-1 uses its own 20 blocks")

		// Pod 2 should have transfer time and use best blocks
		assert.Greater(t, evals[1].TTransfer, 0.0, "pod-2 should have transfer time")
		assert.Equal(t, 20, evals[1].UseBestPrefixBlk, "pod-2 should use best 20 blocks after transfer")
	})
}

// ============================================================================
// TestBoundValue - Helper function test
// ============================================================================

func TestBoundValue(t *testing.T) {
	estimator := createTestEstimator()

	t.Run("value within bounds", func(t *testing.T) {
		result := estimator.boundValue(5.0, 0.0, 10.0)
		assert.Equal(t, 5.0, result)
	})

	t.Run("value below min", func(t *testing.T) {
		result := estimator.boundValue(-5.0, 0.0, 10.0)
		assert.Equal(t, 0.0, result)
	})

	t.Run("value above max", func(t *testing.T) {
		result := estimator.boundValue(15.0, 0.0, 10.0)
		assert.Equal(t, 10.0, result)
	})

	t.Run("value equals min", func(t *testing.T) {
		result := estimator.boundValue(0.0, 0.0, 10.0)
		assert.Equal(t, 0.0, result)
	})

	t.Run("value equals max", func(t *testing.T) {
		result := estimator.boundValue(10.0, 0.0, 10.0)
		assert.Equal(t, 10.0, result)
	})
}

// ============================================================================
// TestEstimatorGetModelConfig - Helper function test
// ============================================================================

func TestEstimatorGetModelConfig(t *testing.T) {
	estimator := createTestEstimator()

	t.Run("find existing model", func(t *testing.T) {
		config := estimator.getModelConfig("llama3-70b")
		require.NotNil(t, config)
		assert.Equal(t, "llama3-70b", config.ModelName)
		assert.Equal(t, int64(327680), config.PerTokenKVBytes)
		assert.Equal(t, 16, config.BlockSizeTokens)
	})

	t.Run("use default for unknown model", func(t *testing.T) {
		config := estimator.getModelConfig("unknown-model")
		require.NotNil(t, config)
		assert.Equal(t, "unknown-model", config.ModelName)
		assert.Equal(t, int64(32768), config.PerTokenKVBytes) // Default 32KB
		assert.Equal(t, 16, config.BlockSizeTokens)
	})
}

// ============================================================================
// TestNewTTFTEstimator - Constructor test
// ============================================================================

func TestNewTTFTEstimator(t *testing.T) {
	config := createTestConfig()
	estimator := NewTTFTEstimator(config)

	require.NotNil(t, estimator)

	// Type assertion to check implementation
	impl, ok := estimator.(*defaultTTFTEstimator)
	require.True(t, ok, "Should return *defaultTTFTEstimator")
	assert.Equal(t, config, impl.config, "Should store config reference")
}

// ============================================================================
// Benchmark tests
// ============================================================================

func BenchmarkEstimateTTFT(b *testing.B) {
	estimator := createTestEstimator()

	pod := PodRef{
		Name:      "bench-pod",
		Namespace: "default",
		IPPort:    "10.0.0.1:8080",
		ModelName: "llama3-70b",
	}

	prefixMatch := &PrefixMatch{
		BestBlocks: 20,
		PodPrefixBlocks: map[string]int{
			pod.Key(): 15,
		},
	}

	meanPrefill := 1.0
	metrics := PodMetrics{
		NumWaiting:     5,
		PromptTokPerS:  2000,
		MeanPrefillSec: &meanPrefill,
	}

	promptTokens := make([]int, 512)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = estimator.EstimateTTFT(pod, prefixMatch, metrics, promptTokens)
	}
}

func BenchmarkEstimatePrefillPods(b *testing.B) {
	estimator := createTestEstimator()

	pods := make([]PodRef, 10)
	podPrefixBlocks := make(map[string]int)
	metricsMap := make(map[string]PodMetrics)
	meanPrefill := 1.0

	for i := 0; i < 10; i++ {
		pods[i] = PodRef{
			Name:      "pod-" + string(rune(i)),
			Namespace: "default",
			IPPort:    "10.0.0." + string(rune(i)) + ":8080",
			ModelName: "llama3-70b",
		}
		podPrefixBlocks[pods[i].Key()] = 10 + i
		metricsMap[pods[i].Key()] = PodMetrics{
			NumWaiting:     float64(i),
			PromptTokPerS:  2000,
			MeanPrefillSec: &meanPrefill,
		}
	}

	prefixMatch := &PrefixMatch{
		BestBlocks:      20,
		PodPrefixBlocks: podPrefixBlocks,
	}

	promptTokens := make([]int, 512)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = estimator.EstimatePrefillPods(pods, prefixMatch, metricsMap, promptTokens)
	}
}
