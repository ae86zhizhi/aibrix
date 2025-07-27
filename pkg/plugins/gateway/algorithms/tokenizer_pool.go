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

package routingalgorithms

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/vllm-project/aibrix/pkg/cache"
	"github.com/vllm-project/aibrix/pkg/utils/tokenizer"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

const (
	// vllmEngine is the constant for vLLM inference engine
	vllmEngine = "vllm"
)

// TokenizerPoolConfig represents configuration for the TokenizerPool
type TokenizerPoolConfig struct {
	EnableVLLMRemote     bool                // Feature flag
	EndpointTemplate     string              // "http://%s:8000"
	HealthCheckPeriod    time.Duration       // Default: 30s
	TokenizerTTL         time.Duration       // Default: 5m
	MaxTokenizersPerPool int                 // Default: 100
	FallbackTokenizer    tokenizer.Tokenizer // Fallback when remote fails
	ModelServiceMap      map[string]string   // Model -> Service endpoint mapping
	Timeout              time.Duration       // Request timeout
}

// tokenizerEntry represents a cached tokenizer with metadata
type tokenizerEntry struct {
	tokenizer    tokenizer.Tokenizer
	endpoint     string
	lastUsed     time.Time
	lastHealthy  time.Time
	healthStatus bool
}

// TokenizerPool manages model-specific tokenizers with caching and health checking
type TokenizerPool struct {
	mu         sync.RWMutex
	tokenizers map[string]*tokenizerEntry // model -> tokenizer mapping
	config     TokenizerPoolConfig
	cache      cache.Cache // for pod discovery
	metrics    *TokenizerPoolMetrics
	stopCh     chan struct{}
}

// TokenizerPoolMetrics contains Prometheus metrics for the pool
type TokenizerPoolMetrics struct {
	activeTokenizers           prometheus.Gauge
	tokenizerCreationSuccesses prometheus.Counter
	tokenizerCreationFailures  prometheus.Counter
	unhealthyTokenizers        prometheus.Counter
	tokenizerRequests          *prometheus.CounterVec
	tokenizerLatency           *prometheus.HistogramVec
}

// NewTokenizerPool creates a new TokenizerPool instance
func NewTokenizerPool(config TokenizerPoolConfig, cache cache.Cache) *TokenizerPool {
	metrics := &TokenizerPoolMetrics{
		activeTokenizers: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "aibrix_tokenizer_pool_active_tokenizers",
			Help: "Number of active tokenizers in the pool",
		}),
		tokenizerCreationSuccesses: promauto.NewCounter(prometheus.CounterOpts{
			Name: "aibrix_tokenizer_pool_creation_successes_total",
			Help: "Total number of successful tokenizer creations",
		}),
		tokenizerCreationFailures: promauto.NewCounter(prometheus.CounterOpts{
			Name: "aibrix_tokenizer_pool_creation_failures_total",
			Help: "Total number of failed tokenizer creations",
		}),
		unhealthyTokenizers: promauto.NewCounter(prometheus.CounterOpts{
			Name: "aibrix_tokenizer_pool_unhealthy_tokenizers_total",
			Help: "Total number of times tokenizers were marked unhealthy",
		}),
		tokenizerRequests: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "aibrix_tokenizer_pool_requests_total",
			Help: "Total number of tokenizer requests by model",
		}, []string{"model"}),
		tokenizerLatency: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "aibrix_tokenizer_pool_latency_seconds",
			Help:    "Tokenizer request latency in seconds",
			Buckets: prometheus.DefBuckets,
		}, []string{"model"}),
	}

	pool := &TokenizerPool{
		tokenizers: make(map[string]*tokenizerEntry),
		config:     config,
		cache:      cache,
		metrics:    metrics,
		stopCh:     make(chan struct{}),
	}

	// Start health checker if enabled
	if config.EnableVLLMRemote && config.HealthCheckPeriod > 0 {
		pool.startHealthChecker()
	}

	return pool
}

// GetTokenizer returns a tokenizer for the specified model
func (p *TokenizerPool) GetTokenizer(model string, pods []v1.Pod) tokenizer.Tokenizer {
	// Metrics
	p.metrics.tokenizerRequests.WithLabelValues(model).Inc()
	startTime := time.Now()
	defer func() {
		p.metrics.tokenizerLatency.WithLabelValues(model).Observe(time.Since(startTime).Seconds())
	}()

	// If remote tokenizer is disabled, return fallback immediately
	if !p.config.EnableVLLMRemote {
		return p.config.FallbackTokenizer
	}

	// Fast path: check existing tokenizer
	p.mu.RLock()
	if entry, exists := p.tokenizers[model]; exists && entry.healthStatus {
		entry.lastUsed = time.Now()
		p.mu.RUnlock()
		return entry.tokenizer
	}
	p.mu.RUnlock()

	// Slow path: create new tokenizer
	return p.createOrUpdateTokenizer(model, pods)
}

// createOrUpdateTokenizer creates or updates a tokenizer for the model
func (p *TokenizerPool) createOrUpdateTokenizer(model string, pods []v1.Pod) tokenizer.Tokenizer {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check after acquiring write lock
	if entry, exists := p.tokenizers[model]; exists && entry.healthStatus {
		entry.lastUsed = time.Now()
		return entry.tokenizer
	}

	// Check pool size limit
	if len(p.tokenizers) >= p.config.MaxTokenizersPerPool {
		klog.Warningf("TokenizerPool reached max size %d, using fallback tokenizer", p.config.MaxTokenizersPerPool)
		return p.config.FallbackTokenizer
	}

	// Find endpoint for model
	endpoint := p.findVLLMEndpointForModel(model, pods)
	if endpoint == "" {
		klog.V(4).Infof("No vLLM endpoint found for model %s, using fallback tokenizer", model)
		p.metrics.tokenizerCreationFailures.Inc()
		return p.config.FallbackTokenizer
	}

	// Create remote tokenizer
	config := tokenizer.RemoteTokenizerConfig{
		Engine:             vllmEngine,
		Endpoint:           endpoint,
		Model:              model,
		Timeout:            p.config.Timeout,
		MaxRetries:         3,
		AddSpecialTokens:   true,
		ReturnTokenStrings: false,
	}

	tok, err := tokenizer.NewRemoteTokenizer(config)
	if err != nil {
		klog.Warningf("Failed to create vLLM tokenizer for model %s: %v", model, err)
		p.metrics.tokenizerCreationFailures.Inc()
		return p.config.FallbackTokenizer
	}

	// Verify health before adding
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if remoteTok, ok := tok.(interface{ IsHealthy(context.Context) bool }); ok {
		if !remoteTok.IsHealthy(ctx) {
			klog.Warningf("Created tokenizer for model %s is not healthy", model)
			p.metrics.tokenizerCreationFailures.Inc()
			return p.config.FallbackTokenizer
		}
	}

	// Add to pool
	p.tokenizers[model] = &tokenizerEntry{
		tokenizer:    tok,
		endpoint:     endpoint,
		lastUsed:     time.Now(),
		lastHealthy:  time.Now(),
		healthStatus: true,
	}

	p.metrics.activeTokenizers.Set(float64(len(p.tokenizers)))
	p.metrics.tokenizerCreationSuccesses.Inc()
	klog.V(3).Infof("Created vLLM tokenizer for model %s at endpoint %s", model, endpoint)

	return tok
}

// findVLLMEndpointForModel finds the vLLM endpoint for a specific model
func (p *TokenizerPool) findVLLMEndpointForModel(model string, pods []v1.Pod) string {
	// Priority order for endpoint discovery:
	// 1. Service endpoint (if configured)
	if endpoint, exists := p.config.ModelServiceMap[model]; exists {
		return endpoint
	}

	// 2. Direct pod endpoint
	for i := range pods {
		pod := &pods[i]
		if !isPodReady(pod) {
			continue
		}

		// Check model match
		podModel := getModelFromPod(pod)
		if podModel != model {
			continue
		}

		// Check if it's a vLLM pod
		if !isVLLMPod(pod) {
			continue
		}

		return fmt.Sprintf(p.config.EndpointTemplate, pod.Status.PodIP)
	}

	return ""
}

// getModelFromPod extracts model information from pod
func getModelFromPod(pod *v1.Pod) string {
	// 1. Check labels (highest priority)
	if model, ok := pod.Labels["aibrix.ai/model"]; ok {
		return model
	}

	// 2. Check annotations
	if model, ok := pod.Annotations["aibrix.ai/model"]; ok {
		return model
	}

	// 3. Check environment variables
	for _, container := range pod.Spec.Containers {
		for _, env := range container.Env {
			if env.Name == "MODEL_NAME" || env.Name == "MODEL" {
				return env.Value
			}
		}
	}

	return ""
}

// isVLLMPod checks if a pod is running vLLM engine
func isVLLMPod(pod *v1.Pod) bool {
	// Check labels
	if engine, ok := pod.Labels["aibrix.ai/inference-engine"]; ok && engine == vllmEngine {
		return true
	}

	// Check annotations
	if engine, ok := pod.Annotations["aibrix.ai/inference-engine"]; ok && engine == vllmEngine {
		return true
	}

	// Check environment variables
	for _, container := range pod.Spec.Containers {
		for _, env := range container.Env {
			if env.Name == "INFERENCE_ENGINE" && env.Value == vllmEngine {
				return true
			}
		}
	}

	// Default assumption based on port (vLLM typically runs on 8000)
	for _, container := range pod.Spec.Containers {
		for _, port := range container.Ports {
			if port.ContainerPort == 8000 {
				return true
			}
		}
	}

	return false
}

// isPodReady checks if a pod is ready to serve requests
func isPodReady(pod *v1.Pod) bool {
	if pod.Status.Phase != v1.PodRunning {
		return false
	}

	for _, condition := range pod.Status.Conditions {
		if condition.Type == v1.PodReady && condition.Status == v1.ConditionTrue {
			return true
		}
	}

	return false
}

// startHealthChecker starts the background health checking routine
func (p *TokenizerPool) startHealthChecker() {
	ticker := time.NewTicker(p.config.HealthCheckPeriod)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				p.performHealthCheck()
				p.cleanupStaleTokenizers()
			case <-p.stopCh:
				return
			}
		}
	}()
}

// performHealthCheck checks health of all tokenizers
func (p *TokenizerPool) performHealthCheck() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for model, entry := range p.tokenizers {
		if remoteTok, ok := entry.tokenizer.(interface{ IsHealthy(context.Context) bool }); ok {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			healthy := remoteTok.IsHealthy(ctx)
			cancel()

			oldStatus := entry.healthStatus
			entry.healthStatus = healthy
			if healthy {
				entry.lastHealthy = time.Now()
			} else if oldStatus {
				// Only log and count when transitioning from healthy to unhealthy
				klog.Warningf("Tokenizer for model %s is now unhealthy", model)
				p.metrics.unhealthyTokenizers.Inc()
			}
		}
	}
}

// cleanupStaleTokenizers removes unused tokenizers
func (p *TokenizerPool) cleanupStaleTokenizers() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	for model, entry := range p.tokenizers {
		// Remove if unused for TTL duration
		if now.Sub(entry.lastUsed) > p.config.TokenizerTTL {
			if closer, ok := entry.tokenizer.(interface{ Close() error }); ok {
				if err := closer.Close(); err != nil {
					klog.Errorf("Error closing tokenizer for model %s: %v", model, err)
				}
			}
			delete(p.tokenizers, model)
			klog.V(4).Infof("Removed stale tokenizer for model %s", model)
		}
	}

	p.metrics.activeTokenizers.Set(float64(len(p.tokenizers)))
}

// Close gracefully shuts down the TokenizerPool
func (p *TokenizerPool) Close() error {
	// Stop health checker
	close(p.stopCh)

	// Close all tokenizers
	p.mu.Lock()
	defer p.mu.Unlock()

	for model, entry := range p.tokenizers {
		if closer, ok := entry.tokenizer.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				klog.Errorf("Error closing tokenizer for model %s: %v", model, err)
			}
		}
	}
	p.tokenizers = make(map[string]*tokenizerEntry)
	p.metrics.activeTokenizers.Set(0)

	return nil
}
