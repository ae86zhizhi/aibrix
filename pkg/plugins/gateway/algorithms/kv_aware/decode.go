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
	"time"

	"k8s.io/klog/v2"
)

// DecodeSelector selects decode pods based on TBT constraints
type DecodeSelector interface {
	// SelectDecodePod selects the best decode pod meeting TBT SLO
	SelectDecodePod(
		decodePods []PodRef,
		metricsMap map[string]PodMetrics,
		tbtslo time.Duration,
		estimatedOutputTokens int,
	) (*PodRef, float64, error)
}

// defaultDecodeSelector implements DecodeSelector
type defaultDecodeSelector struct {
	config *KVAwareConfig
}

// NewDecodeSelector creates a new decode selector
func NewDecodeSelector(config *KVAwareConfig) DecodeSelector {
	return &defaultDecodeSelector{
		config: config,
	}
}

// SelectDecodePod selects the best decode pod based on TBT constraints
func (s *defaultDecodeSelector) SelectDecodePod(
	decodePods []PodRef,
	metricsMap map[string]PodMetrics,
	tbtslo time.Duration,
	estimatedOutputTokens int,
) (*PodRef, float64, error) {
	if len(decodePods) == 0 {
		return nil, 0, ErrNoDecodeCandidate
	}

	klog.V(4).Infof("Selecting decode pod from %d candidates with TBT SLO %.0fms",
		len(decodePods), tbtslo.Seconds()*1000)

	// Evaluate all decode pods
	candidates := s.evaluateDecodePods(decodePods, metricsMap, estimatedOutputTokens)

	if len(candidates) == 0 {
		klog.Warningf("No decode pods available after evaluation")
		return nil, 0, ErrNoDecodeCandidate
	}

	// Log evaluation results
	s.logEvaluationResults(candidates, tbtslo)

	// Filter candidates meeting SLO with safety margin (90%)
	safetyMargin := 0.9
	eligibleCandidates := s.filterBySLO(candidates, tbtslo, safetyMargin)

	if len(eligibleCandidates) == 0 {
		// Try relaxed SLO as fallback (1.5× original SLO)
		relaxedSLO := time.Duration(float64(tbtslo) * 1.5)
		klog.V(3).Infof("No pods meet TBT SLO %.0fms (with 90%% safety margin), "+
			"trying relaxed SLO %.0fms",
			tbtslo.Seconds()*1000, relaxedSLO.Seconds()*1000)

		// Use 100% of relaxed SLO (no safety margin for fallback)
		eligibleCandidates = s.filterBySLO(candidates, relaxedSLO, 1.0)

		if len(eligibleCandidates) == 0 {
			klog.Warningf("No decode pods meet even relaxed TBT SLO (%.0fms)",
				relaxedSLO.Seconds()*1000)
			return nil, 0, ErrTBTSLOViolation
		}

		klog.V(3).Infof("Using relaxed SLO fallback: %d pods eligible",
			len(eligibleCandidates))
	}

	// Select best candidate by score
	best := s.selectBestCandidate(eligibleCandidates)
	if best == nil {
		klog.Error("selectBestCandidate returned nil despite having candidates")
		return nil, 0, ErrNoDecodeCandidate
	}

	klog.V(3).Infof("Selected decode pod %s with predicted TBT %.0fms (SLO: %.0fms, score: %.3f)",
		best.Pod.IPPort, best.PredictedTBT*1000, tbtslo.Seconds()*1000, best.Score)

	return &best.Pod, best.PredictedTBT, nil
}

// getCurrentTBT gets the current Time Between Tokens for a pod
func (s *defaultDecodeSelector) getCurrentTBT(metrics PodMetrics) float64 {
	// Priority 1: Use P95 TPOT from 5-minute window
	if metrics.P95TPOTSec != nil && *metrics.P95TPOTSec > 0 {
		klog.V(5).Infof("Using P95 TPOT: %.3fs", *metrics.P95TPOTSec)
		return *metrics.P95TPOTSec
	}

	// Priority 2: Use average TPOT
	if metrics.AvgTPOTSec != nil && *metrics.AvgTPOTSec > 0 {
		klog.V(5).Infof("Using Avg TPOT: %.3fs", *metrics.AvgTPOTSec)
		return *metrics.AvgTPOTSec
	}

	// Priority 3: Calculate from generation throughput
	if metrics.GenTokPerS > 0 {
		tbt := 1.0 / metrics.GenTokPerS
		klog.V(5).Infof("Calculated TBT from throughput: %.3fs (%.0f tok/s)",
			tbt, metrics.GenTokPerS)
		return tbt
	}

	// Fallback: Conservative default (200ms)
	klog.V(5).Info("Using fallback TBT: 0.2s (200ms)")
	return 0.2
}

// predictTBTWithNewRequest predicts TBT after adding a new request
func (s *defaultDecodeSelector) predictTBTWithNewRequest(
	metrics PodMetrics,
	estimatedOutputTokens int,
) float64 {
	currentTBT := s.getCurrentTBT(metrics)
	currentBatchSize := metrics.NumRunning

	// Simple linear model: TBT increases with batch size
	// Based on empirical observations from paper
	batchSizeEffect := 1.0 + (0.1 * currentBatchSize)

	// Account for KV cache pressure
	cacheEffect := 1.0
	if metrics.GPUCacheUsage > 90 {
		cacheEffect = 1.5 // 50% degradation when cache > 90%
		klog.V(5).Infof("GPU cache usage %.1f%% > 90%%, applying 50%% TBT penalty",
			metrics.GPUCacheUsage)
	} else if metrics.GPUCacheUsage > 80 {
		cacheEffect = 1.2 // 20% degradation when cache > 80%
		klog.V(5).Infof("GPU cache usage %.1f%% > 80%%, applying 20%% TBT penalty",
			metrics.GPUCacheUsage)
	}

	// Account for long generation sequences
	lengthEffect := 1.0
	if estimatedOutputTokens > 2000 {
		lengthEffect = 1.2 // 20% degradation for very long sequences
		klog.V(5).Infof("Output tokens %d > 2000, applying 20%% penalty",
			estimatedOutputTokens)
	} else if estimatedOutputTokens > 1000 {
		lengthEffect = 1.1 // 10% degradation for long sequences
		klog.V(5).Infof("Output tokens %d > 1000, applying 10%% penalty",
			estimatedOutputTokens)
	}

	predictedTBT := currentTBT * batchSizeEffect * cacheEffect * lengthEffect

	// Apply reasonable bounds
	if predictedTBT < 0.01 { // Minimum 10ms
		predictedTBT = 0.01
	}
	if predictedTBT > 5.0 { // Maximum 5s (sanity check)
		klog.Warningf("Predicted TBT %.2fs exceeds 5s limit, capping at 5s", predictedTBT)
		predictedTBT = 5.0
	}

	klog.V(5).Infof("TBT prediction: current=%.3fs, batch_effect=%.2f, cache_effect=%.2f, "+
		"length_effect=%.2f, predicted=%.3fs",
		currentTBT, batchSizeEffect, cacheEffect, lengthEffect, predictedTBT)

	return predictedTBT
}

// calculateScore calculates selection score for a decode pod
func (s *defaultDecodeSelector) calculateScore(
	metrics PodMetrics,
	currentTBT, predictedTBT float64,
) float64 {
	// Component 1: TBT Score (40% weight) - lower TBT is better
	// Normalize: 1.0 / (1.0 + predictedTBT)
	// Range: ~0.17 (for 5s TBT) to ~0.99 (for 0.01s TBT)
	tbtScore := 1.0 / (1.0 + predictedTBT)

	// Component 2: Throughput Score (30% weight) - higher throughput is better
	// Normalize: min(GenTokPerS / 1000, 1.0)
	// Range: 0 to 1.0 (capped at 1000 tok/s)
	throughputScore := metrics.GenTokPerS / 1000.0
	if throughputScore > 1.0 {
		throughputScore = 1.0
	}

	// Component 3: Utilization Score (20% weight) - lower cache usage is better
	// Normalize: 1.0 - (GPUCacheUsage / 100)
	// Range: 0 (100% usage) to 1.0 (0% usage)
	utilizationScore := 1.0 - (metrics.GPUCacheUsage / 100.0)
	if utilizationScore < 0 {
		utilizationScore = 0
	}

	// Component 4: Load Score (10% weight) - fewer running requests is better
	// Normalize: 1.0 / (1.0 + NumRunning)
	// Range: ~0.09 (for 10 requests) to 1.0 (for 0 requests)
	loadScore := 1.0 / (1.0 + metrics.NumRunning)

	// Weighted combination: 40% TBT + 30% Throughput + 20% Utilization + 10% Load
	finalScore := (tbtScore * 0.4) +
		(throughputScore * 0.3) +
		(utilizationScore * 0.2) +
		(loadScore * 0.1)

	klog.V(5).Infof("Score calculation: tbt=%.3f (40%%), throughput=%.3f (30%%), "+
		"utilization=%.3f (20%%), load=%.3f (10%%), final=%.3f",
		tbtScore, throughputScore, utilizationScore, loadScore, finalScore)

	return finalScore
}

// evaluateDecodePods evaluates all decode pods
func (s *defaultDecodeSelector) evaluateDecodePods(
	decodePods []PodRef,
	metricsMap map[string]PodMetrics,
	estimatedOutputTokens int,
) []DecodeCandidate {
	candidates := make([]DecodeCandidate, 0, len(decodePods))

	for _, pod := range decodePods {
		metrics, ok := metricsMap[pod.IPPort]
		if !ok {
			klog.V(4).Infof("No metrics found for decode pod %s, skipping", pod.IPPort)
			continue
		}

		// Get current TBT
		currentTBT := s.getCurrentTBT(metrics)

		// Predict TBT with new request
		predictedTBT := s.predictTBTWithNewRequest(metrics, estimatedOutputTokens)

		// Calculate selection score
		score := s.calculateScore(metrics, currentTBT, predictedTBT)

		candidates = append(candidates, DecodeCandidate{
			Pod:          pod,
			CurrentTBT:   currentTBT,
			PredictedTBT: predictedTBT,
			Score:        score,
		})
	}

	klog.V(4).Infof("Evaluated %d decode pods, found %d candidates",
		len(decodePods), len(candidates))

	return candidates
}

// filterBySLO filters candidates by TBT SLO
func (s *defaultDecodeSelector) filterBySLO(
	candidates []DecodeCandidate,
	tbtslo time.Duration,
	safetyMargin float64,
) []DecodeCandidate {
	if len(candidates) == 0 {
		return candidates
	}

	// Calculate effective SLO threshold (apply safety margin)
	effectiveSLO := tbtslo.Seconds() * safetyMargin

	filtered := make([]DecodeCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.PredictedTBT <= effectiveSLO {
			filtered = append(filtered, candidate)
		} else {
			klog.V(5).Infof("Pod %s filtered out: predicted TBT %.0fms > effective SLO %.0fms",
				candidate.Pod.IPPort,
				candidate.PredictedTBT*1000,
				effectiveSLO*1000)
		}
	}

	klog.V(4).Infof("Filtered candidates by TBT SLO (%.0fms with %.0f%% safety margin): %d/%d passed",
		tbtslo.Seconds()*1000, safetyMargin*100, len(filtered), len(candidates))

	return filtered
}

// selectBestCandidate selects the best candidate by score
func (s *defaultDecodeSelector) selectBestCandidate(
	candidates []DecodeCandidate,
) *DecodeCandidate {
	if len(candidates) == 0 {
		return nil
	}

	// Find candidate with highest score
	bestIdx := 0
	bestScore := candidates[0].Score

	for i := 1; i < len(candidates); i++ {
		candidate := candidates[i]

		// Select if score is higher
		if candidate.Score > bestScore {
			bestIdx = i
			bestScore = candidate.Score
		} else if candidate.Score == bestScore {
			// Tie-breaking: prefer lower predicted TBT
			if candidate.PredictedTBT < candidates[bestIdx].PredictedTBT {
				bestIdx = i
			}
		}
	}

	klog.V(4).Infof("Selected best decode pod: %s (score=%.3f, predicted TBT=%.0fms)",
		candidates[bestIdx].Pod.IPPort,
		candidates[bestIdx].Score,
		candidates[bestIdx].PredictedTBT*1000)

	return &candidates[bestIdx]
}

// logEvaluationResults logs the evaluation results for debugging
func (s *defaultDecodeSelector) logEvaluationResults(
	candidates []DecodeCandidate,
	tbtslo time.Duration,
) {
	if !klog.V(4).Enabled() {
		return
	}

	klog.V(4).Infof("=== Decode Pod Evaluation Results (TBT SLO: %.0fms) ===",
		tbtslo.Seconds()*1000)
	for i, c := range candidates {
		klog.V(4).Infof("  [%d] Pod: %s, Current TBT: %.0fms, Predicted TBT: %.0fms, Score: %.3f",
			i+1,
			c.Pod.IPPort,
			c.CurrentTBT*1000,
			c.PredictedTBT*1000,
			c.Score,
		)
	}
}
