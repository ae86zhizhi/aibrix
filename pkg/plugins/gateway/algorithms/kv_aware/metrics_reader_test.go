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
	"github.com/stretchr/testify/mock"
	"github.com/vllm-project/aibrix/pkg/cache"
	"github.com/vllm-project/aibrix/pkg/metrics"
	"github.com/vllm-project/aibrix/pkg/types"
	v1 "k8s.io/api/core/v1"
)

// mockCacheForMetrics is a mock implementation of cache.Cache for testing
type mockCacheForMetrics struct {
	mock.Mock
}

func (m *mockCacheForMetrics) GetMetricValueByPod(podName, podNamespace, metricName string) (metrics.MetricValue, error) {
	args := m.Called(podName, podNamespace, metricName)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(metrics.MetricValue), args.Error(1)
}

func (m *mockCacheForMetrics) GetMetricValueByPodModel(podName, podNamespace, modelName string, metricName string) (metrics.MetricValue, error) {
	args := m.Called(podName, podNamespace, modelName, metricName)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(metrics.MetricValue), args.Error(1)
}

func (m *mockCacheForMetrics) AddSubscriber(subscriber metrics.MetricSubscriber) {
	m.Called(subscriber)
}

func (m *mockCacheForMetrics) GetPod(podName, podNamespace string) (*v1.Pod, error) {
	args := m.Called(podName, podNamespace)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*v1.Pod), args.Error(1)
}

func (m *mockCacheForMetrics) ListPodsByModel(modelName string) (types.PodList, error) {
	args := m.Called(modelName)
	return args.Get(0).(types.PodList), args.Error(1)
}

func (m *mockCacheForMetrics) HasModel(modelName string) bool {
	args := m.Called(modelName)
	return args.Bool(0)
}

func (m *mockCacheForMetrics) ListModels() []string {
	args := m.Called()
	return args.Get(0).([]string)
}

func (m *mockCacheForMetrics) ListModelsByPod(podName, podNamespace string) ([]string, error) {
	args := m.Called(podName, podNamespace)
	return args.Get(0).([]string), args.Error(1)
}

func (m *mockCacheForMetrics) AddRequestCount(ctx *types.RoutingContext, requestID string, modelName string) int64 {
	args := m.Called(ctx, requestID, modelName)
	return args.Get(0).(int64)
}

func (m *mockCacheForMetrics) DoneRequestCount(ctx *types.RoutingContext, requestID string, modelName string, traceTerm int64) {
	m.Called(ctx, requestID, modelName, traceTerm)
}

func (m *mockCacheForMetrics) DoneRequestTrace(ctx *types.RoutingContext, requestID string, modelName string, inputTokens, outputTokens, traceTerm int64) {
	m.Called(ctx, requestID, modelName, inputTokens, outputTokens, traceTerm)
}

func (m *mockCacheForMetrics) GetModelProfileByPod(pod *v1.Pod, modelName string) (*cache.ModelGPUProfile, error) {
	args := m.Called(pod, modelName)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*cache.ModelGPUProfile), args.Error(1)
}

func (m *mockCacheForMetrics) GetModelProfileByDeploymentName(deploymentName string, modelName string) (*cache.ModelGPUProfile, error) {
	args := m.Called(deploymentName, modelName)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*cache.ModelGPUProfile), args.Error(1)
}

func (m *mockCacheForMetrics) GetOutputPredictor(modelName string) (types.OutputPredictor, error) {
	args := m.Called(modelName)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(types.OutputPredictor), args.Error(1)
}

func (m *mockCacheForMetrics) GetRouter(ctx *types.RoutingContext) (types.Router, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(types.Router), args.Error(1)
}

// TestGetPodMetrics_AllAvailable tests fetching metrics when all are available
func TestGetPodMetrics_AllAvailable(t *testing.T) {
	mockCache := new(mockCacheForMetrics)
	config := &KVAwareConfig{
		PromEnabled: true,
	}
	reader := NewMetricsReader(mockCache, config)

	podRef := PodRef{
		Name:      "test-pod",
		Namespace: "default",
		IPPort:    "10.0.0.1:8080",
		Role:      "prefill",
	}

	// Mock gauge metrics
	mockCache.On("GetMetricValueByPod", "test-pod", "default", metrics.NumRequestsWaiting).
		Return(&metrics.SimpleMetricValue{Value: 5.0}, nil)
	mockCache.On("GetMetricValueByPod", "test-pod", "default", metrics.NumRequestsRunning).
		Return(&metrics.SimpleMetricValue{Value: 10.0}, nil)
	mockCache.On("GetMetricValueByPod", "test-pod", "default", metrics.GPUCacheUsagePerc).
		Return(&metrics.SimpleMetricValue{Value: 75.5}, nil)
	mockCache.On("GetMetricValueByPod", "test-pod", "default", metrics.CPUCacheUsagePerc).
		Return(&metrics.SimpleMetricValue{Value: 50.0}, nil)

	// Mock throughput metrics
	mockCache.On("GetMetricValueByPod", "test-pod", "default", metrics.AvgPromptThroughputToksPerS).
		Return(&metrics.SimpleMetricValue{Value: 2000.0}, nil)
	mockCache.On("GetMetricValueByPod", "test-pod", "default", metrics.AvgGenerationThroughputToksPerS).
		Return(&metrics.SimpleMetricValue{Value: 150.0}, nil)

	// Mock aggregated metrics
	mockCache.On("GetMetricValueByPod", "test-pod", "default", metrics.P95TPOT5mPod).
		Return(&metrics.SimpleMetricValue{Value: 0.15}, nil)
	mockCache.On("GetMetricValueByPod", "test-pod", "default", metrics.AvgTPOT5mPod).
		Return(&metrics.SimpleMetricValue{Value: 0.10}, nil)
	mockCache.On("GetMetricValueByPod", "test-pod", "default", metrics.RequestPrefillTimeSeconds).
		Return(&metrics.HistogramMetricValue{Sum: 10.0, Count: 10.0}, nil)
	mockCache.On("GetMetricValueByPod", "test-pod", "default", metrics.RequestQueueTimeSeconds).
		Return(&metrics.HistogramMetricValue{Sum: 5.0, Count: 10.0, Buckets: map[string]float64{"0.5": 9.0, "1.0": 10.0}}, nil)

	result, err := reader.GetPodMetrics(podRef)

	assert.NoError(t, err)
	assert.True(t, result.MetricsAvailable)
	assert.Equal(t, 5.0, result.NumWaiting)
	assert.Equal(t, 10.0, result.NumRunning)
	assert.Equal(t, 75.5, result.GPUCacheUsage)
	assert.Equal(t, 50.0, result.CPUCacheUsage)
	assert.Equal(t, 2000.0, result.PromptTokPerS)
	assert.Equal(t, 150.0, result.GenTokPerS)
	assert.NotNil(t, result.P95TPOTSec)
	assert.Equal(t, 0.15, *result.P95TPOTSec)
	assert.NotNil(t, result.MeanPrefillSec)
	assert.Equal(t, 1.0, *result.MeanPrefillSec)

	mockCache.AssertExpectations(t)
}

// TestGetPodMetrics_SomeMissing tests fetching metrics when some are missing
func TestGetPodMetrics_SomeMissing(t *testing.T) {
	mockCache := new(mockCacheForMetrics)
	config := &KVAwareConfig{
		PromEnabled: false, // Prometheus disabled
	}
	reader := NewMetricsReader(mockCache, config)

	podRef := PodRef{
		Name:      "test-pod",
		Namespace: "default",
	}

	// Mock only some gauge metrics
	mockCache.On("GetMetricValueByPod", "test-pod", "default", metrics.NumRequestsWaiting).
		Return(&metrics.SimpleMetricValue{Value: 3.0}, nil)
	mockCache.On("GetMetricValueByPod", "test-pod", "default", metrics.NumRequestsRunning).
		Return(nil, fmt.Errorf("metric not found"))
	mockCache.On("GetMetricValueByPod", "test-pod", "default", metrics.GPUCacheUsagePerc).
		Return(&metrics.SimpleMetricValue{Value: 60.0}, nil)
	mockCache.On("GetMetricValueByPod", "test-pod", "default", metrics.CPUCacheUsagePerc).
		Return(nil, fmt.Errorf("metric not found"))

	// Mock throughput metrics as missing
	mockCache.On("GetMetricValueByPod", "test-pod", "default", metrics.AvgPromptThroughputToksPerS).
		Return(nil, fmt.Errorf("metric not found"))
	mockCache.On("GetMetricValueByPod", "test-pod", "default", metrics.AvgGenerationThroughputToksPerS).
		Return(nil, fmt.Errorf("metric not found"))

	result, err := reader.GetPodMetrics(podRef)

	assert.NoError(t, err)
	assert.True(t, result.MetricsAvailable)
	assert.Equal(t, 3.0, result.NumWaiting)
	assert.Equal(t, 0.0, result.NumRunning) // Missing, defaults to 0
	assert.Equal(t, 60.0, result.GPUCacheUsage)
	assert.Equal(t, 1000.0, result.PromptTokPerS) // Fallback value
	assert.Equal(t, 100.0, result.GenTokPerS)     // Fallback value
	assert.NotNil(t, result.MeanPrefillSec)
	assert.Equal(t, 1.0, *result.MeanPrefillSec) // Fallback value

	mockCache.AssertExpectations(t)
}

// TestApplyFallbackValues tests fallback value application
func TestApplyFallbackValues(t *testing.T) {
	mockCache := new(mockCacheForMetrics)
	config := &KVAwareConfig{}
	reader := &cacheMetricsReader{
		cache:  mockCache,
		config: config,
	}

	pm := &PodMetrics{
		PromptTokPerS: 0,
		GenTokPerS:    0,
	}

	reader.applyFallbackValues(pm)

	assert.Equal(t, 1000.0, pm.PromptTokPerS)
	assert.Equal(t, 100.0, pm.GenTokPerS)
	assert.NotNil(t, pm.MeanPrefillSec)
	assert.Equal(t, 1.0, *pm.MeanPrefillSec)
	assert.NotNil(t, pm.P95QueueSec)
	assert.Equal(t, 0.5, *pm.P95QueueSec)
	assert.NotNil(t, pm.P95TPOTSec)
	assert.Equal(t, 0.2, *pm.P95TPOTSec)
}

// TestBatchGetPodMetrics tests concurrent batch fetching
func TestBatchGetPodMetrics(t *testing.T) {
	mockCache := new(mockCacheForMetrics)
	config := &KVAwareConfig{
		PromEnabled: false,
	}
	reader := NewMetricsReader(mockCache, config)

	pods := []PodRef{
		{Name: "pod-1", Namespace: "default"},
		{Name: "pod-2", Namespace: "default"},
		{Name: "pod-3", Namespace: "default"},
	}

	// Mock metrics for all pods
	for _, pod := range pods {
		mockCache.On("GetMetricValueByPod", pod.Name, "default", metrics.NumRequestsWaiting).
			Return(&metrics.SimpleMetricValue{Value: 1.0}, nil)
		mockCache.On("GetMetricValueByPod", pod.Name, "default", metrics.NumRequestsRunning).
			Return(&metrics.SimpleMetricValue{Value: 2.0}, nil)
		mockCache.On("GetMetricValueByPod", pod.Name, "default", metrics.GPUCacheUsagePerc).
			Return(&metrics.SimpleMetricValue{Value: 70.0}, nil)
		mockCache.On("GetMetricValueByPod", pod.Name, "default", metrics.CPUCacheUsagePerc).
			Return(nil, fmt.Errorf("not found"))
		mockCache.On("GetMetricValueByPod", pod.Name, "default", metrics.AvgPromptThroughputToksPerS).
			Return(&metrics.SimpleMetricValue{Value: 1500.0}, nil)
		mockCache.On("GetMetricValueByPod", pod.Name, "default", metrics.AvgGenerationThroughputToksPerS).
			Return(&metrics.SimpleMetricValue{Value: 120.0}, nil)
	}

	results := reader.BatchGetPodMetrics(pods)

	assert.Equal(t, 3, len(results))
	for _, pod := range pods {
		metrics, exists := results[pod.Key()]
		assert.True(t, exists, "Metrics should exist for %s", pod.Name)
		assert.True(t, metrics.MetricsAvailable)
		assert.Equal(t, 1.0, metrics.NumWaiting)
		assert.Equal(t, 2.0, metrics.NumRunning)
		assert.Equal(t, 1500.0, metrics.PromptTokPerS)
	}

	mockCache.AssertExpectations(t)
}

// TestMetricsCache_GetPut tests cache get and put operations
func TestMetricsCache_GetPut(t *testing.T) {
	cache := newMetricsCache(5 * time.Second)

	podMetrics := PodMetrics{
		NumWaiting:       5.0,
		NumRunning:       10.0,
		MetricsAvailable: true,
		LastUpdated:      time.Now(),
	}

	key := "default/test-pod"

	// Test cache miss
	_, ok := cache.get(key)
	assert.False(t, ok)

	// Test cache put and hit
	cache.put(key, podMetrics)
	cached, ok := cache.get(key)
	assert.True(t, ok)
	assert.Equal(t, 5.0, cached.NumWaiting)
	assert.Equal(t, 10.0, cached.NumRunning)
}

// TestMetricsCache_Expiration tests cache expiration
func TestMetricsCache_Expiration(t *testing.T) {
	cache := newMetricsCache(100 * time.Millisecond)

	podMetrics := PodMetrics{
		NumWaiting:       5.0,
		MetricsAvailable: true,
	}

	key := "default/test-pod"
	cache.put(key, podMetrics)

	// Should be cached
	_, ok := cache.get(key)
	assert.True(t, ok)

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Should be expired
	_, ok = cache.get(key)
	assert.False(t, ok)
}

// TestMetricsCache_Cleanup tests cache cleanup
func TestMetricsCache_Cleanup(t *testing.T) {
	cache := newMetricsCache(50 * time.Millisecond)

	podMetrics := PodMetrics{
		NumWaiting:       5.0,
		MetricsAvailable: true,
	}

	// Add multiple entries
	cache.put("default/pod-1", podMetrics)
	cache.put("default/pod-2", podMetrics)
	cache.put("default/pod-3", podMetrics)

	assert.Equal(t, 3, cache.size())

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// Run cleanup
	cache.cleanup()

	assert.Equal(t, 0, cache.size())
}

// TestMetricsCache_Clear tests cache clear
func TestMetricsCache_Clear(t *testing.T) {
	cache := newMetricsCache(5 * time.Second)

	podMetrics := PodMetrics{
		NumWaiting:       5.0,
		MetricsAvailable: true,
	}

	cache.put("default/pod-1", podMetrics)
	cache.put("default/pod-2", podMetrics)

	assert.Equal(t, 2, cache.size())

	cache.clear()

	assert.Equal(t, 0, cache.size())
}

// TestGetMeanPrefillTime tests prefill time calculation
func TestGetMeanPrefillTime(t *testing.T) {
	mockCache := new(mockCacheForMetrics)
	config := &KVAwareConfig{}
	reader := &cacheMetricsReader{
		cache:  mockCache,
		config: config,
	}

	podRef := PodRef{
		Name:      "test-pod",
		Namespace: "default",
	}

	// Test with histogram
	// Note: getMeanPrefillTime calls GetMetricValueByPod twice - once in getMetricValue, once for histogram
	mockCache.On("GetMetricValueByPod", "test-pod", "default", metrics.RequestPrefillTimeSeconds).
		Return(&metrics.HistogramMetricValue{Sum: 20.0, Count: 10.0}, nil).Times(2)

	result, err := reader.getMeanPrefillTime(podRef)
	assert.NoError(t, err)
	assert.Equal(t, 2.0, result)

	// Test fallback calculation
	mockCache.On("GetMetricValueByPod", "test-pod", "default", metrics.RequestPrefillTimeSeconds).
		Return(nil, fmt.Errorf("not found")).Once()
	mockCache.On("GetMetricValueByPod", "test-pod", "default", metrics.RequestInferenceTimeSeconds).
		Return(&metrics.SimpleMetricValue{Value: 5.0}, nil).Once()
	mockCache.On("GetMetricValueByPod", "test-pod", "default", metrics.RequestDecodeTimeSeconds).
		Return(&metrics.SimpleMetricValue{Value: 2.0}, nil).Once()

	result, err = reader.getMeanPrefillTime(podRef)
	assert.NoError(t, err)
	assert.Equal(t, 3.0, result)

	mockCache.AssertExpectations(t)
}

// TestGetP95QueueTime tests P95 queue time estimation
func TestGetP95QueueTime(t *testing.T) {
	mockCache := new(mockCacheForMetrics)
	config := &KVAwareConfig{}
	reader := &cacheMetricsReader{
		cache:  mockCache,
		config: config,
	}

	podRef := PodRef{
		Name:      "test-pod",
		Namespace: "default",
	}

	// Test with histogram
	hist := &metrics.HistogramMetricValue{
		Sum:   10.0,
		Count: 10.0,
		Buckets: map[string]float64{
			"0.1": 3.0,
			"0.5": 7.0,
			"1.0": 9.5,
			"2.0": 10.0,
		},
	}
	mockCache.On("GetMetricValueByPod", "test-pod", "default", metrics.RequestQueueTimeSeconds).
		Return(hist, nil).Once()

	result, err := reader.getP95QueueTime(podRef)
	assert.NoError(t, err)
	assert.InDelta(t, 1.0, result, 0.1) // P95 should be around 1.0

	mockCache.AssertExpectations(t)
}
