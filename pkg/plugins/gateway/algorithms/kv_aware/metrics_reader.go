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

// getMeanPrefillTime fetches or calculates mean prefill time
func (r *cacheMetricsReader) getMeanPrefillTime(podRef PodRef) (float64, error) {
	// Try direct metric first
	if val, err := r.getMetricValue(podRef, metrics.RequestPrefillTimeSeconds); err == nil {
		if histVal, err := r.cache.GetMetricValueByPod(podRef.Name, podRef.Namespace, metrics.RequestPrefillTimeSeconds); err == nil {
			if hist := histVal.GetHistogramValue(); hist != nil {
				return hist.GetMean(), nil
			}
		}
		return val, nil
	}

	// Fallback: Calculate from inference - decode
	inferenceTime, err1 := r.getMetricValue(podRef, metrics.RequestInferenceTimeSeconds)
	decodeTime, err2 := r.getMetricValue(podRef, metrics.RequestDecodeTimeSeconds)

	if err1 == nil && err2 == nil && inferenceTime > decodeTime {
		return inferenceTime - decodeTime, nil
	}

	return 0, fmt.Errorf("unable to determine prefill time")
}

// getP95QueueTime fetches or estimates P95 queue time
func (r *cacheMetricsReader) getP95QueueTime(podRef PodRef) (float64, error) {
	// Try to get queue time histogram
	if histVal, err := r.cache.GetMetricValueByPod(podRef.Name, podRef.Namespace, metrics.RequestQueueTimeSeconds); err == nil {
		if hist := histVal.GetHistogramValue(); hist != nil {
			if p95, err := hist.GetPercentile(95); err == nil {
				return p95, nil
			}
		}
	}

	// Simple approximation: 1.5x average queue time
	queueTime, err := r.getMetricValue(podRef, metrics.RequestQueueTimeSeconds)
	if err != nil {
		return 0, err
	}

	return queueTime * 1.5, nil
}

// fetchAggregatedMetrics fetches aggregated metrics from Prometheus
func (r *cacheMetricsReader) fetchAggregatedMetrics(pm *PodMetrics, podRef PodRef) error {
	// Only fetch if Prometheus enabled
	if !r.config.PromEnabled {
		return nil
	}

	// Fetch P95 TPOT (5m window)
	if val, err := r.getMetricValue(podRef, metrics.P95TPOT5mPod); err == nil {
		pm.P95TPOTSec = &val
	} else {
		klog.V(5).Infof("Failed to fetch P95TPOT5mPod for %s: %v", podRef.Name, err)
	}

	// Fetch Average TPOT (5m window)
	if val, err := r.getMetricValue(podRef, metrics.AvgTPOT5mPod); err == nil {
		pm.AvgTPOTSec = &val
	} else {
		klog.V(5).Infof("Failed to fetch AvgTPOT5mPod for %s: %v", podRef.Name, err)
	}

	// Fetch mean prefill time
	if val, err := r.getMeanPrefillTime(podRef); err == nil {
		pm.MeanPrefillSec = &val
	} else {
		klog.V(5).Infof("Failed to fetch mean prefill time for %s: %v", podRef.Name, err)
	}

	// Fetch P95 queue time
	if val, err := r.getP95QueueTime(podRef); err == nil {
		pm.P95QueueSec = &val
	} else {
		klog.V(5).Infof("Failed to fetch P95 queue time for %s: %v", podRef.Name, err)
	}

	return nil
}

// applyFallbackValues applies default values for missing metrics
func (r *cacheMetricsReader) applyFallbackValues(pm *PodMetrics) {
	// Throughput defaults
	if pm.PromptTokPerS == 0 {
		pm.PromptTokPerS = 1000 // 1k tokens/sec
		klog.V(5).Info("Using default prompt throughput: 1000 tok/s")
	}

	if pm.GenTokPerS == 0 {
		pm.GenTokPerS = 100 // 100 tokens/sec
		klog.V(5).Info("Using default generation throughput: 100 tok/s")
	}

	// Aggregated metric defaults (nullable pointers)
	if pm.MeanPrefillSec == nil {
		defaultPrefill := 1.0 // 1 second
		pm.MeanPrefillSec = &defaultPrefill
		klog.V(5).Info("Using default mean prefill time: 1.0s")
	}

	if pm.P95QueueSec == nil {
		defaultQueue := 0.5 // 0.5 second
		pm.P95QueueSec = &defaultQueue
		klog.V(5).Info("Using default P95 queue time: 0.5s")
	}

	if pm.P95TPOTSec == nil {
		defaultTPOT := 0.2 // 200ms
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

	// Get aggregated metrics if Prometheus enabled
	if r.config.PromEnabled {
		if err := r.fetchAggregatedMetrics(&podMetrics, podRef); err != nil {
			klog.V(4).Infof("Failed to fetch aggregated metrics for %s: %v",
				podRef.Name, err)
		}
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
