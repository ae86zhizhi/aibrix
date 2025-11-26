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
	"math"
	"sync"

	"k8s.io/klog/v2"
)

// TTFTEstimator estimates Time To First Token components
type TTFTEstimator interface {
	// EstimateTTFT calculates all TTFT components for a single pod
	EstimateTTFT(
		pod PodRef,
		prefixMatch *PrefixMatch,
		metrics PodMetrics,
		promptTokens []int,
	) (TTFTComponents, error)

	// EstimatePrefillPods evaluates all prefill pods concurrently
	EstimatePrefillPods(
		pods []PodRef,
		prefixMatch *PrefixMatch,
		metricsMap map[string]PodMetrics,
		promptTokens []int,
	) []PrefillEval
}

// defaultTTFTEstimator implements TTFTEstimator
type defaultTTFTEstimator struct {
	config *KVAwareConfig
}

// NewTTFTEstimator creates a new TTFT estimator
func NewTTFTEstimator(config *KVAwareConfig) TTFTEstimator {
	return &defaultTTFTEstimator{
		config: config,
	}
}

// estimateTransferTime calculates KV cache transfer time (MVP: estimation only)
func (e *defaultTTFTEstimator) estimateTransferTime(
	bestBlocks int,
	localBlocks int,
	perTokenBytes int64,
	blockSize int,
) float64 {
	// No transfer needed if local is best or better
	if localBlocks >= bestBlocks {
		return 0
	}

	// No transfer in MVP (estimation only)
	if !e.config.EnableKVTransfer {
		return 0
	}

	// Check if transfer is worthwhile
	blockDifference := bestBlocks - localBlocks
	if blockDifference <= e.config.KVCopyThresholdBlk {
		return 0 // Not worth transferring
	}

	// Calculate bytes to transfer
	missingTokens := blockDifference * blockSize
	bytesToTransfer := float64(missingTokens) * float64(perTokenBytes)

	// Estimate transfer time with configured bandwidth
	if e.config.TransferBandwidthBps > 0 {
		transferTime := bytesToTransfer / e.config.TransferBandwidthBps

		klog.V(5).Infof("Transfer estimation: %d blocks (%d tokens, %.2f MB) at %.2f Gbps = %.3fs",
			blockDifference, missingTokens, bytesToTransfer/1e6,
			e.config.TransferBandwidthBps*8/1e9, transferTime)

		return transferTime
	}

	return 0
}

// clamp restricts value to [min, max] range
func clamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// estimateQueueTime calculates expected queue waiting time
// Phase 008: Refactored with correct priority order and algorithms
// Priority: Direct histogram P95 > Little's Law with true λ > Heuristic fallback
func (e *defaultTTFTEstimator) estimateQueueTime(metrics PodMetrics) float64 {
	// ===== Method A: Direct from queue histogram (PREFERRED) =====
	// P95 queue time directly from request_queue_time_seconds histogram
	if metrics.P95QueueSec != nil && *metrics.P95QueueSec > 0 {
		p95Queue := *metrics.P95QueueSec

		// Optional: Scale based on current vs average queue length
		// If current queue is longer than average, estimate proportionally longer wait
		if metrics.AvgNumWaiting != nil && *metrics.AvgNumWaiting > 0 && metrics.NumWaiting > 0 {
			ratio := metrics.NumWaiting / *metrics.AvgNumWaiting
			if ratio > 1.0 {
				// Sub-linear scaling: queue 2x longer doesn't mean 2x wait
				// Using β = 0.7 for conservative scaling
				scale := math.Pow(ratio, 0.7)
				p95Queue *= scale
				klog.V(5).Infof("Queue time (Method A scaled): P95=%.3fs, ratio=%.2f, scale=%.2f, result=%.3fs",
					*metrics.P95QueueSec, ratio, scale, p95Queue)
			}
		} else {
			klog.V(5).Infof("Queue time (Method A direct): P95=%.3fs", p95Queue)
		}

		return p95Queue
	}

	// ===== Method B: Little's Law with true λ =====
	// L = λ × W => W = L / λ
	// where λ = rate(request_success_total) is true throughput
	if metrics.LambdaReqPerS != nil && *metrics.LambdaReqPerS > 0 && metrics.NumWaiting > 0 {
		lambda := *metrics.LambdaReqPerS
		W_mean := metrics.NumWaiting / lambda // Little's Law: W = L / λ

		// Apply P95/Mean ratio if available, otherwise use default 1.5
		factor := 1.5 // Default P95/Mean ratio assumption for queue times
		if metrics.P95QueueSec != nil && metrics.MeanQueueSec != nil && *metrics.MeanQueueSec > 0 {
			factor = *metrics.P95QueueSec / *metrics.MeanQueueSec
		}

		queueTime := W_mean * factor
		klog.V(5).Infof("Queue time (Method B Little's Law): L=%.0f, λ=%.2f req/s, W_mean=%.3fs, factor=%.2f, result=%.3fs",
			metrics.NumWaiting, lambda, W_mean, factor, queueTime)

		return queueTime
	}

	// ===== Method C: Heuristic fallback (bound to historical data) =====
	// Instead of hardcoded 0.5s/request, use MeanPrefillSec as proxy
	if metrics.NumWaiting > 0 {
		// Use MeanPrefillSec as proxy for "time per request" if available
		// Clamped to reasonable range [0.05s, 2.0s] to prevent extreme values
		basePerReq := 0.5 // Default 500ms per request
		if metrics.MeanPrefillSec != nil && *metrics.MeanPrefillSec > 0 {
			basePerReq = clamp(*metrics.MeanPrefillSec, 0.05, 2.0)
		}

		queueTime := metrics.NumWaiting * basePerReq
		klog.V(5).Infof("Queue time (Method C fallback): %.0f waiting * %.3fs/req = %.3fs",
			metrics.NumWaiting, basePerReq, queueTime)

		return queueTime
	}

	// No queue
	klog.V(5).Info("Queue time: 0 (no waiting requests)")
	return 0
}

// estimatePrefillTime calculates expected prefill computation time
// Phase 008: Refactored to use per-token time for accurate scaling
// Uses EMA fusion of real-time throughput and historical per-token time
func (e *defaultTTFTEstimator) estimatePrefillTime(
	totalTokens int,
	cachedBlocks int,
	blockSize int,
	metrics PodMetrics,
) float64 {
	// Calculate uncached tokens
	cachedTokens := cachedBlocks * blockSize
	uncachedTokens := totalTokens - cachedTokens
	if uncachedTokens <= 0 {
		klog.V(5).Info("Prefill time: 0 (all tokens cached)")
		return 0
	}

	// ===== Method 1: Real-time throughput + per-token EMA (PREFERRED) =====
	// Use current throughput and optionally fuse with historical per-token time
	if metrics.PromptTokPerS > 0 {
		instantPerTok := 1.0 / metrics.PromptTokPerS // Current per-token time

		// Apply EMA with historical per-token time if available
		perTok := instantPerTok
		if metrics.MeanPrefillPerTok != nil && *metrics.MeanPrefillPerTok > 0 {
			// EMA fusion: α * instant + (1-α) * historical
			// This provides smoothing between current and historical measurements
			perTok = e.config.EMAAlpha*instantPerTok +
				(1-e.config.EMAAlpha)*(*metrics.MeanPrefillPerTok)

			klog.V(5).Infof("Prefill time (Method 1 EMA): instant=%.6fs/tok, hist=%.6fs/tok, fused=%.6fs/tok",
				instantPerTok, *metrics.MeanPrefillPerTok, perTok)
		}

		prefillTime := float64(uncachedTokens) * perTok
		klog.V(5).Infof("Prefill time (Method 1): %d tokens * %.6fs/tok = %.3fs",
			uncachedTokens, perTok, prefillTime)

		return prefillTime
	}

	// ===== Method 2: Historical per-token time only =====
	// Use MeanPrefillPerTok from PromQL if available
	if metrics.MeanPrefillPerTok != nil && *metrics.MeanPrefillPerTok > 0 {
		prefillTime := float64(uncachedTokens) * (*metrics.MeanPrefillPerTok)
		klog.V(5).Infof("Prefill time (Method 2): %d tokens * %.6fs/tok = %.3fs",
			uncachedTokens, *metrics.MeanPrefillPerTok, prefillTime)

		return prefillTime
	}

	// ===== Method 3: Derive per-token from MeanPrefillSec =====
	// If we have average prefill time but not per-token, estimate it
	if metrics.MeanPrefillSec != nil && *metrics.MeanPrefillSec > 0 {
		// Estimate average tokens per request from configuration or use default
		avgToksPerReq := 512.0 // Default assumption

		derivedPerTok := *metrics.MeanPrefillSec / avgToksPerReq
		prefillTime := float64(uncachedTokens) * derivedPerTok

		klog.V(5).Infof("Prefill time (Method 3 derived): mean=%.3fs, avgToks=%.0f, perTok=%.6fs, result=%.3fs",
			*metrics.MeanPrefillSec, avgToksPerReq, derivedPerTok, prefillTime)

		return prefillTime
	}

	// ===== Method 4: Fallback constant (last resort) =====
	// Use 1ms/token as conservative fallback
	fallbackPerTok := 0.001 // 1ms per token

	prefillTime := float64(uncachedTokens) * fallbackPerTok
	klog.V(5).Infof("Prefill time (Method 4 fallback): %d tokens * %.6fs/tok = %.3fs",
		uncachedTokens, fallbackPerTok, prefillTime)

	return prefillTime
}

// EstimateTTFT calculates all TTFT components for a single pod
func (e *defaultTTFTEstimator) EstimateTTFT(
	pod PodRef,
	prefixMatch *PrefixMatch,
	metrics PodMetrics,
	promptTokens []int,
) (TTFTComponents, error) {
	// Get model configuration
	modelConfig := e.getModelConfig(pod.ModelName)

	totalTokens := len(promptTokens)
	blockSize := modelConfig.BlockSizeTokens

	// Get local and best prefix matches
	localBlocks := prefixMatch.PodPrefixBlocks[pod.Key()]
	bestBlocks := prefixMatch.BestBlocks

	// Component 1: Transfer time estimation
	tTransfer := e.estimateTransferTime(
		bestBlocks,
		localBlocks,
		modelConfig.PerTokenKVBytes,
		blockSize,
	)

	// Determine effective blocks after potential transfer
	effectiveBlocks := localBlocks
	if e.config.EnableKVTransfer && (bestBlocks-localBlocks) > e.config.KVCopyThresholdBlk {
		effectiveBlocks = bestBlocks // Assume we have best blocks after transfer
	}

	// Component 2: Queue time estimation
	tQueue := e.estimateQueueTime(metrics)

	// Component 3: Prefill time estimation
	tPrefill := e.estimatePrefillTime(
		totalTokens,
		effectiveBlocks,
		blockSize,
		metrics,
	)

	// Apply bounds and sanity checks
	components := TTFTComponents{
		TransferTime: e.boundValue(tTransfer, 0, 10), // Max 10s transfer
		QueueTime:    e.boundValue(tQueue, 0, 30),    // Max 30s queue
		PrefillTime:  e.boundValue(tPrefill, 0, 60),  // Max 60s prefill
	}

	components.TotalTTFT = components.TransferTime +
		components.QueueTime +
		components.PrefillTime

	klog.V(4).Infof("TTFT estimation for pod %s: transfer=%.2fs, queue=%.2fs, prefill=%.2fs, total=%.2fs",
		pod.Name, components.TransferTime, components.QueueTime,
		components.PrefillTime, components.TotalTTFT)

	return components, nil
}

// EstimatePrefillPods evaluates all prefill pods concurrently
func (e *defaultTTFTEstimator) EstimatePrefillPods(
	pods []PodRef,
	prefixMatch *PrefixMatch,
	metricsMap map[string]PodMetrics,
	promptTokens []int,
) []PrefillEval {
	evals := make([]PrefillEval, len(pods))
	var wg sync.WaitGroup

	for i, pod := range pods {
		wg.Add(1)
		go func(idx int, p PodRef) {
			defer wg.Done()

			metrics := metricsMap[p.Key()]
			components, err := e.EstimateTTFT(p, prefixMatch, metrics, promptTokens)

			if err != nil {
				klog.Warningf("Failed to estimate TTFT for pod %s: %v", p.Name, err)
				components = TTFTComponents{
					TotalTTFT: math.MaxFloat64, // Worst case
				}
			}

			localBlocks := prefixMatch.PodPrefixBlocks[p.Key()]
			useBlocks := localBlocks

			// Check if we would use best blocks after transfer
			if e.config.EnableKVTransfer && components.TransferTime > 0 {
				useBlocks = prefixMatch.BestBlocks
			}

			evals[idx] = PrefillEval{
				Pod:              p,
				LocalPrefixBlk:   localBlocks,
				UseBestPrefixBlk: useBlocks,
				TTransfer:        components.TransferTime,
				TQueue:           components.QueueTime,
				TPrefill:         components.PrefillTime,
				TTFT:             components.TotalTTFT,
				Explanation: fmt.Sprintf(
					"local=%d blocks, best=%d blocks, transfer=%.2fs, queue=%.2fs, prefill=%.2fs",
					localBlocks, prefixMatch.BestBlocks,
					components.TransferTime, components.QueueTime, components.PrefillTime,
				),
			}
		}(i, pod)
	}

	wg.Wait()

	// Log evaluation results
	if klog.V(4).Enabled() {
		e.logEvaluationResults(evals)
	}

	return evals
}

// boundValue ensures a value is within specified bounds
func (e *defaultTTFTEstimator) boundValue(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		klog.V(4).Infof("Capping value %.2f to max %.2f", value, max)
		return max
	}
	return value
}

// logEvaluationResults logs detailed evaluation results for debugging
func (e *defaultTTFTEstimator) logEvaluationResults(evals []PrefillEval) {
	klog.V(4).Info("Prefill pod evaluation results:")

	for i, eval := range evals {
		klog.V(4).Infof("  [%d] Pod %s: TTFT=%.2fs (%s)",
			i, eval.Pod.Name, eval.TTFT, eval.Explanation)
	}

	// Find and log best option
	bestIdx := 0
	for i, eval := range evals {
		if eval.TTFT < evals[bestIdx].TTFT {
			bestIdx = i
		}
	}

	if len(evals) > 0 {
		klog.V(4).Infof("  Best option: Pod %s with TTFT=%.2fs",
			evals[bestIdx].Pod.Name, evals[bestIdx].TTFT)
	}
}

// getModelConfig retrieves model configuration with fallback
func (e *defaultTTFTEstimator) getModelConfig(modelName string) *ModelKVSpec {
	// Try to find model in config
	for i := range e.config.Models {
		if e.config.Models[i].ModelName == modelName {
			return &e.config.Models[i]
		}
	}

	// Use default if model not found
	klog.V(4).Infof("Using default model config for %s", modelName)
	return &ModelKVSpec{
		ModelName:       modelName,
		PerTokenKVBytes: 32768, // 32KB default
		BlockSizeTokens: e.config.Models[0].BlockSizeTokens,
	}
}
