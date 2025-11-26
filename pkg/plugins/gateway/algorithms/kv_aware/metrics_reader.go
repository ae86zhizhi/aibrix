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
	"sync"
	"time"

	"github.com/vllm-project/aibrix/pkg/cache"
	"github.com/vllm-project/aibrix/pkg/metrics"
	"k8s.io/klog/v2"
)

// MetricsReader defines the interface for fetching pod metrics
type MetricsReader interface {
	// GetPodMetrics fetches metrics for a single pod
	GetPodMetrics(podRef PodRef) (PodMetrics, error)

	// BatchGetPodMetrics fetches metrics for multiple pods concurrently
	BatchGetPodMetrics(pods []PodRef) map[string]PodMetrics
}

// cacheMetricsReader implements MetricsReader using the AIBrix cache
type cacheMetricsReader struct {
	cache  cache.Cache
	config *KVAwareConfig
}

// NewMetricsReader creates a new MetricsReader instance
func NewMetricsReader(cache cache.Cache, config *KVAwareConfig) MetricsReader {
	return &cacheMetricsReader{
		cache:  cache,
		config: config,
	}
}

// getMetricValue is a helper function to fetch a single metric value
func (r *cacheMetricsReader) getMetricValue(podRef PodRef, metricName string) (float64, error) {
	value, err := r.cache.GetMetricValueByPod(
		podRef.Name,
		podRef.Namespace,
		metricName,
	)
	if err != nil {
		return 0, err
	}

	// Extract float64 from MetricValue interface
	return value.GetSimpleValue(), nil
}

// fetchGaugeMetrics fetches real-time gauge metrics
func (r *cacheMetricsReader) fetchGaugeMetrics(pm *PodMetrics, podRef PodRef) error {
	// Fetch num_requests_waiting
	if val, err := r.getMetricValue(podRef, metrics.NumRequestsWaiting); err == nil {
		pm.NumWaiting = val
	} else {
		klog.V(5).Infof("Failed to fetch NumRequestsWaiting for %s: %v", podRef.Name, err)
	}

	// Fetch num_requests_running
	if val, err := r.getMetricValue(podRef, metrics.NumRequestsRunning); err == nil {
		pm.NumRunning = val
	} else {
		klog.V(5).Infof("Failed to fetch NumRequestsRunning for %s: %v", podRef.Name, err)
	}

	// Fetch GPU cache usage
	if val, err := r.getMetricValue(podRef, metrics.GPUCacheUsagePerc); err == nil {
		pm.GPUCacheUsage = val
	} else {
		klog.V(5).Infof("Failed to fetch GPUCacheUsagePerc for %s: %v", podRef.Name, err)
	}

	// Fetch CPU cache usage (optional)
	if val, err := r.getMetricValue(podRef, metrics.CPUCacheUsagePerc); err == nil {
		pm.CPUCacheUsage = val
	} else {
		klog.V(6).Infof("Failed to fetch CPUCacheUsagePerc for %s: %v", podRef.Name, err)
	}

	return nil
}

// fetchThroughputMetrics fetches throughput metrics
func (r *cacheMetricsReader) fetchThroughputMetrics(pm *PodMetrics, podRef PodRef) error {
	// Fetch prompt throughput
	if val, err := r.getMetricValue(podRef, metrics.AvgPromptThroughputToksPerS); err == nil {
		pm.PromptTokPerS = val
	} else {
		klog.V(5).Infof("Failed to fetch AvgPromptThroughputToksPerS for %s: %v", podRef.Name, err)
	}

	// Fetch generation throughput
	if val, err := r.getMetricValue(podRef, metrics.AvgGenerationThroughputToksPerS); err == nil {
		pm.GenTokPerS = val
	} else {
		klog.V(5).Infof("Failed to fetch AvgGenerationThroughputToksPerS for %s: %v", podRef.Name, err)
	}

	return nil
}

// fetchQueueMetricsFromHistogram extracts queue time metrics from histogram
// This is the PREFERRED source for queue time estimation
func (r *cacheMetricsReader) fetchQueueMetricsFromHistogram(pm *PodMetrics, podRef PodRef) {
	histVal, err := r.cache.GetMetricValueByPod(
		podRef.Name, podRef.Namespace,
		metrics.RequestQueueTimeSeconds,
	)
	if err != nil {
		klog.V(5).Infof("Queue histogram not available for %s: %v", podRef.Name, err)
		return
	}

	hist := histVal.GetHistogramValue()
	if hist == nil || hist.Count == 0 {
		klog.V(5).Infof("Queue histogram empty for %s", podRef.Name)
		return
	}

	// Mean queue time from histogram
	mean := hist.GetMean()
	pm.MeanQueueSec = &mean

	// P95 queue time from histogram
	if p95, err := hist.GetPercentile(95); err == nil {
		pm.P95QueueSec = &p95
		klog.V(5).Infof("Queue metrics for %s: mean=%.3fs, P95=%.3fs (samples=%.0f)",
			podRef.Name, mean, p95, hist.Count)
	}
}

// fetchPrefillMetricsFromHistogram extracts prefill time metrics from histogram
// This is the CORRECT source for pure PREFILL phase time (not e2e latency)
func (r *cacheMetricsReader) fetchPrefillMetricsFromHistogram(pm *PodMetrics, podRef PodRef) {
	histVal, err := r.cache.GetMetricValueByPod(
		podRef.Name, podRef.Namespace,
		metrics.RequestPrefillTimeSeconds,
	)
	if err != nil {
		klog.V(5).Infof("Prefill histogram not available for %s: %v", podRef.Name, err)
		return
	}

	hist := histVal.GetHistogramValue()
	if hist == nil || hist.Count == 0 {
		klog.V(5).Infof("Prefill histogram empty for %s", podRef.Name)
		return
	}

	// Mean prefill time (pure PREFILL phase)
	mean := hist.GetMean()
	pm.MeanPrefillSec = &mean
	klog.V(5).Infof("Prefill metrics for %s: mean=%.3fs (samples=%.0f)",
		podRef.Name, mean, hist.Count)
}

// fetchTPOTMetricsFromHistogram extracts TPOT metrics from histogram
func (r *cacheMetricsReader) fetchTPOTMetricsFromHistogram(pm *PodMetrics, podRef PodRef) {
	histVal, err := r.cache.GetMetricValueByPod(
		podRef.Name, podRef.Namespace,
		metrics.TimePerOutputTokenSeconds,
	)
	if err != nil {
		return
	}

	hist := histVal.GetHistogramValue()
	if hist == nil || hist.Count == 0 {
		return
	}

	mean := hist.GetMean()
	pm.AvgTPOTSec = &mean

	if p95, err := hist.GetPercentile(95); err == nil {
		pm.P95TPOTSec = &p95
	}
}

// fetchPromQLMetrics fetches PromQL-based aggregated metrics
// These metrics require Prometheus to be available
func (r *cacheMetricsReader) fetchPromQLMetrics(pm *PodMetrics, podRef PodRef) {
	// Lambda (true throughput rate for Little's Law)
	if val, err := r.getMetricValue(podRef, metrics.RequestThroughputRate1m); err == nil && val > 0 {
		pm.LambdaReqPerS = &val
		klog.V(5).Infof("Lambda for %s: %.2f req/s", podRef.Name, val)
	}

	// Average queue length baseline (for scaling estimation)
	if val, err := r.getMetricValue(podRef, metrics.AvgNumWaiting5m); err == nil {
		pm.AvgNumWaiting = &val
	}

	// Per-token prefill time
	if val, err := r.getMetricValue(podRef, metrics.MeanPrefillPerTok5m); err == nil && val > 0 {
		pm.MeanPrefillPerTok = &val
		klog.V(5).Infof("PrefillPerTok for %s: %.6f s/tok", podRef.Name, val)
	}
}

// fetchAggregatedMetrics fetches histogram and PromQL-aggregated metrics
// Phase 008: Refactored to prioritize direct histogram measurements
func (r *cacheMetricsReader) fetchAggregatedMetrics(pm *PodMetrics, podRef PodRef) error {
	// ===== 1. Queue Metrics from Histogram (PREFERRED) =====
	r.fetchQueueMetricsFromHistogram(pm, podRef)

	// ===== 2. Prefill Metrics from Histogram (CORRECT SOURCE) =====
	r.fetchPrefillMetricsFromHistogram(pm, podRef)

	// ===== 3. TPOT Metrics from Histogram =====
	r.fetchTPOTMetricsFromHistogram(pm, podRef)

	// ===== 4. PromQL Aggregated Metrics (if Prometheus available) =====
	if r.config.PromEnabled {
		r.fetchPromQLMetrics(pm, podRef)
	}

	return nil
}

// applyFallbackValues applies default values for missing metrics
// Phase 008: Removed hardcoded fallbacks for queue/prefill metrics
// The estimation algorithms now handle missing data gracefully
func (r *cacheMetricsReader) applyFallbackValues(pm *PodMetrics) {
	// Throughput defaults (keep these as they represent hardware capability)
	if pm.PromptTokPerS == 0 {
		pm.PromptTokPerS = 1000 // 1k tokens/sec - reasonable default
		klog.V(5).Info("Using default prompt throughput: 1000 tok/s")
	}

	if pm.GenTokPerS == 0 {
		pm.GenTokPerS = 100 // 100 tokens/sec - reasonable default
		klog.V(5).Info("Using default generation throughput: 100 tok/s")
	}

	// NOTE: DO NOT set hardcoded defaults for queue/prefill metrics.
	// The estimation algorithms (estimate.go) handle missing data with
	// multi-level fallback that binds to historical observations.
	// - MeanQueueSec, P95QueueSec: handled by estimateQueueTime
	// - MeanPrefillSec, MeanPrefillPerTok: handled by estimatePrefillTime
	// - LambdaReqPerS, AvgNumWaiting: handled by estimateQueueTime

	// TPOT fallback (for TBT SLO checking only)
	if pm.P95TPOTSec == nil {
		defaultTPOT := 0.2 // 200ms - conservative estimate
		pm.P95TPOTSec = &defaultTPOT
		klog.V(5).Info("Using default P95 TPOT: 0.2s")
	}
}

// GetPodMetrics fetches all metrics for a single pod
func (r *cacheMetricsReader) GetPodMetrics(podRef PodRef) (PodMetrics, error) {
	startTime := time.Now()
	defer func() {
		klog.V(5).Infof("GetPodMetrics for %s took %v",
			podRef.Name, time.Since(startTime))
	}()

	podMetrics := PodMetrics{
		LastUpdated:      time.Now(),
		MetricsAvailable: false,
	}

	// Get real-time gauge metrics
	if err := r.fetchGaugeMetrics(&podMetrics, podRef); err != nil {
		klog.V(4).Infof("Failed to fetch gauge metrics for %s: %v",
			podRef.Name, err)
	}

	// Get throughput metrics
	if err := r.fetchThroughputMetrics(&podMetrics, podRef); err != nil {
		klog.V(4).Infof("Failed to fetch throughput metrics for %s: %v",
			podRef.Name, err)
	}

	// Get aggregated metrics (histograms always available, PromQL requires Prometheus)
	// Phase 008: Always fetch histogram metrics, PromQL is conditional inside fetchAggregatedMetrics
	if err := r.fetchAggregatedMetrics(&podMetrics, podRef); err != nil {
		klog.V(4).Infof("Failed to fetch aggregated metrics for %s: %v",
			podRef.Name, err)
	}

	// Apply fallback values for missing metrics
	r.applyFallbackValues(&podMetrics)

	podMetrics.MetricsAvailable = true
	return podMetrics, nil
}

// BatchGetPodMetrics fetches metrics for multiple pods concurrently
func (r *cacheMetricsReader) BatchGetPodMetrics(pods []PodRef) map[string]PodMetrics {
	results := make(map[string]PodMetrics)
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Limit concurrent fetches to 10
	sem := make(chan struct{}, 10)

	for _, pod := range pods {
		wg.Add(1)
		go func(p PodRef) {
			defer wg.Done()
			sem <- struct{}{}        // Acquire
			defer func() { <-sem }() // Release

			metrics, err := r.GetPodMetrics(p)
			if err != nil {
				klog.Warningf("Failed to fetch metrics for %s: %v", p.Name, err)
				metrics = PodMetrics{
					MetricsAvailable: false,
					LastUpdated:      time.Now(),
				}
			}

			mu.Lock()
			results[p.Key()] = metrics
			mu.Unlock()
		}(pod)
	}

	wg.Wait()
	return results
}
