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

// estimateQueueTime calculates expected queue waiting time
func (e *defaultTTFTEstimator) estimateQueueTime(metrics PodMetrics) float64 {
	// Method 1: Use service rate if we have historical data
	if metrics.MeanPrefillSec != nil && *metrics.MeanPrefillSec > 0 {
		serviceRate := 1.0 / *metrics.MeanPrefillSec
		if serviceRate > 0 && metrics.NumWaiting > 0 {
			// Little's Law approximation
			queueTime := metrics.NumWaiting / serviceRate

			// Apply dampening factor for stability
			queueTime *= e.config.QueueFallbackMultiplier

			klog.V(5).Infof("Queue time (service rate): %.0f waiting / %.2f svc_rate = %.2fs",
				metrics.NumWaiting, serviceRate, queueTime)

			return queueTime
		}
	}

	// Method 2: Use historical P95 queue time
	if metrics.P95QueueSec != nil && *metrics.P95QueueSec > 0 {
		klog.V(5).Infof("Queue time (P95): %.2fs", *metrics.P95QueueSec)
		return *metrics.P95QueueSec
	}

	// Method 3: Fallback heuristic based on queue length
	if metrics.NumWaiting > 0 {
		// Assume ~0.5s per queued request as rough estimate
		queueTime := metrics.NumWaiting * 0.5
		klog.V(5).Infof("Queue time (fallback): %.0f waiting * 0.5 = %.2fs",
			metrics.NumWaiting, queueTime)
		return queueTime
	}

	// No queue
	return 0
}

// estimatePrefillTime calculates expected prefill computation time
func (e *defaultTTFTEstimator) estimatePrefillTime(
	totalTokens int,
	cachedBlocks int,
	blockSize int,
	metrics PodMetrics,
) float64 {
	// Calculate uncached tokens
	cachedTokens := cachedBlocks * blockSize
	uncachedTokens := totalTokens - cachedTokens
	if uncachedTokens < 0 {
		uncachedTokens = 0
	}

	// No prefill needed if everything is cached
	if uncachedTokens == 0 {
		klog.V(5).Info("All tokens cached, no prefill needed")
		return 0
	}

	// Method 1: Throughput-based estimation
	if metrics.PromptTokPerS > 0 {
		baseEstimate := float64(uncachedTokens) / metrics.PromptTokPerS

		// Apply EMA calibration with historical data if available
		if metrics.MeanPrefillSec != nil && *metrics.MeanPrefillSec > 0 {
			// Weight: ema_alpha for model, (1-ema_alpha) for historical
			calibrated := e.config.EMAAlpha*baseEstimate +
				(1-e.config.EMAAlpha)*(*metrics.MeanPrefillSec)

			klog.V(5).Infof("Prefill time (calibrated): %d tokens / %.0f tok/s = %.2fs base, %.2fs calibrated",
				uncachedTokens, metrics.PromptTokPerS, baseEstimate, calibrated)

			return calibrated
		}

		klog.V(5).Infof("Prefill time (throughput): %d tokens / %.0f tok/s = %.2fs",
			uncachedTokens, metrics.PromptTokPerS, baseEstimate)

		return baseEstimate
	}

	// Method 2: Pure historical if no throughput data
	if metrics.MeanPrefillSec != nil && *metrics.MeanPrefillSec > 0 {
		// Scale by token ratio
		avgTokensPerRequest := 512 // Assumed average
		scaleFactor := float64(uncachedTokens) / float64(avgTokensPerRequest)
		scaled := *metrics.MeanPrefillSec * scaleFactor

		klog.V(5).Infof("Prefill time (historical): %.2fs * %.2f scale = %.2fs",
			*metrics.MeanPrefillSec, scaleFactor, scaled)

		return scaled
	}

	// Method 3: Fallback constant time per token
	// Assume 1ms per token as extreme fallback
	fallback := float64(uncachedTokens) * 0.001
	klog.V(5).Infof("Prefill time (fallback): %d tokens * 0.001 = %.2fs",
		uncachedTokens, fallback)

	return fallback
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
		TransferTime: e.boundValue(tTransfer, 0, 10),   // Max 10s transfer
		QueueTime:    e.boundValue(tQueue, 0, 30),      // Max 30s queue
		PrefillTime:  e.boundValue(tPrefill, 0, 60),    // Max 60s prefill
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
