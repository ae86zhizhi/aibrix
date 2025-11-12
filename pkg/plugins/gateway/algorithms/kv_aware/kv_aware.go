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
	"time"

	"github.com/vllm-project/aibrix/pkg/cache"
	"github.com/vllm-project/aibrix/pkg/metrics"
	routingalgorithms "github.com/vllm-project/aibrix/pkg/plugins/gateway/algorithms"
	"github.com/vllm-project/aibrix/pkg/types"
	"github.com/vllm-project/aibrix/pkg/utils/syncprefixcacheindexer"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

// Constants for KV-aware routing
const (
	// RouterKVAware is the routing algorithm name
	RouterKVAware types.RoutingAlgorithm = "kv-aware"

	// Pod label keys and values
	RoleLabelKey = "aibrix.ai/role"
	RolePrefill  = "prefill"
	RoleDecode   = "decode"

	// Annotations
	MetricPortAnnotation = "model.aibrix.ai/metric-port"
	DefaultMetricPort    = "8080"

	// Model label
	ModelLabelKey = "model.aibrix.ai/model-name"
)

// init registers the KV-aware router with the routing algorithm manager
func init() {
	routingalgorithms.Register(RouterKVAware, NewKVAwareRouter)
}

// kvAwareRouter implements the KV-aware routing algorithm
type kvAwareRouter struct {
	config         *KVAwareConfig
	cache          cache.Cache
	prefixIndexer  *syncprefixcacheindexer.SyncPrefixHashTable
	prefixMatcher  PrefixMatcher
	tokenizer      Tokenizer
	metricsReader  MetricsReader
	metricsCache   *metricsCache
	ttftEstimator  TTFTEstimator  // Phase 005: TTFT estimation
	decodeSelector DecodeSelector // Phase 006: Decode selection
	sloChecker     SLOChecker     // Phase 006: SLO checking
	fallbackAlgo   types.RoutingAlgorithm
}

// NewKVAwareRouter creates a new KV-aware router instance
func NewKVAwareRouter() (types.Router, error) {
	// Load configuration
	config, err := LoadConfig()
	if err != nil {
		klog.Warningf("Failed to load KV-aware config, using defaults: %v", err)
		config = &KVAwareConfig{Enabled: false}
	}

	// If disabled, return fallback router
	if !config.Enabled {
		klog.V(2).Info("KV-aware routing is disabled, using least-request fallback")
		return routingalgorithms.NewLeastRequestRouter()
	}

	// Get cache instance
	c, err := cache.Get()
	if err != nil {
		klog.Errorf("Failed to get cache instance for KV-aware router: %v", err)
		return nil, fmt.Errorf("failed to get cache: %w", err)
	}

	// Create prefix indexer instance (Phase 004)
	indexer := syncprefixcacheindexer.NewSyncPrefixHashTable()

	// Create tokenizer (Phase 004)
	tokenizer := NewTokenizer()

	// Create prefix matcher (Phase 004)
	prefixMatcher := NewPrefixMatcher(indexer, tokenizer)

	// Create metrics reader (Phase 003)
	metricsReader := NewMetricsReader(c, config)

	// Create metrics cache with 5 second TTL (Phase 003)
	metricsCache := newMetricsCache(5 * time.Second)
	metricsCache.startCleanupLoop(30 * time.Second)

	// Create TTFT estimator (Phase 005)
	ttftEstimator := NewTTFTEstimator(config)

	// Create decode selector (Phase 006)
	decodeSelector := NewDecodeSelector(config)

	// Create SLO checker (Phase 006)
	sloChecker := NewSLOChecker()

	router := &kvAwareRouter{
		config:         config,
		cache:          c,
		prefixIndexer:  indexer,
		prefixMatcher:  prefixMatcher,
		tokenizer:      tokenizer,
		metricsReader:  metricsReader,
		metricsCache:   metricsCache,
		ttftEstimator:  ttftEstimator,
		decodeSelector: decodeSelector,
		sloChecker:     sloChecker,
		fallbackAlgo:   routingalgorithms.RouterLeastRequest,
	}

	klog.V(2).Infof("KV-aware router initialized with config: TTFT SLO=%v, TBT SLO=%v, Bandwidth=%v Gbps",
		config.TTFTSLO, config.TBTSLO, config.TransferBandwidthBps/1e9)
	klog.V(2).Info("KV-aware router initialized with metrics reader and cache (TTL: 5s)")
	klog.V(2).Info("KV-aware router initialized with prefix matcher and tokenizer (Phase 004)")
	klog.V(2).Info("KV-aware router initialized with TTFT estimator (Phase 005)")
	klog.V(2).Info("KV-aware router initialized with decode selector and SLO checker (Phase 006)")

	return router, nil
}

// Route selects a target pod based on KV-aware routing logic
func (r *kvAwareRouter) Route(ctx *types.RoutingContext, readyPodList types.PodList) (string, error) {
	// Get all ready pods
	allPods := readyPodList.All()
	if len(allPods) == 0 {
		return "", fmt.Errorf("no ready pods available")
	}

	// Separate prefill and decode pods by role label
	prefillPods, decodePods, err := r.separatePrefillDecodePods(allPods)
	if err != nil {
		klog.V(3).Infof("Failed to separate P/D pods: %v, using fallback", err)
		return r.fallback(ctx, readyPodList)
	}

	// Check if we have both types of pods
	if len(prefillPods) == 0 {
		klog.V(3).Info("No prefill pods available, using fallback")
		return r.fallback(ctx, readyPodList)
	}
	if len(decodePods) == 0 {
		klog.V(3).Info("No decode pods available, using fallback")
		return r.fallback(ctx, readyPodList)
	}

	klog.V(4).Infof("Found %d prefill pods and %d decode pods", len(prefillPods), len(decodePods))

	// PLACEHOLDER: Phase 002 - Just select first prefill pod
	// Full implementation will be done in Phase 005 (TTFT Estimation)
	selectedPod := prefillPods[0]
	podAddress := r.getPodAddress(selectedPod)

	klog.V(3).Infof("Selected prefill pod: %s/%s (placeholder selection)",
		selectedPod.Namespace, selectedPod.Name)

	// Set target pod in context
	ctx.SetTargetPod(selectedPod)

	return podAddress, nil
}

// SubscribedMetrics returns the list of metrics this router needs
func (r *kvAwareRouter) SubscribedMetrics() []string {
	return []string{
		// Real-time gauges (for current state)
		metrics.NumRequestsWaiting,
		metrics.NumRequestsRunning,
		metrics.GPUCacheUsagePerc,
		metrics.CPUCacheUsagePerc,

		// Throughput metrics
		metrics.AvgPromptThroughputToksPerS,
		metrics.AvgGenerationThroughputToksPerS,

		// Latency metrics (for SLO checking)
		metrics.TimeToFirstTokenSeconds,
		metrics.TimePerOutputTokenSeconds,

		// Aggregated metrics (5m window for Phase 003)
		metrics.RequestQueueTimeSeconds,
		metrics.RequestPrefillTimeSeconds,
		metrics.P95TPOT5mPod,
		metrics.AvgTPOT5mPod,
	}
}

// separatePrefillDecodePods separates pods by their role label
func (r *kvAwareRouter) separatePrefillDecodePods(allPods []*v1.Pod) ([]*v1.Pod, []*v1.Pod, error) {
	var prefillPods []*v1.Pod
	var decodePods []*v1.Pod

	for _, pod := range allPods {
		if pod.Labels == nil {
			continue
		}

		role, exists := pod.Labels[RoleLabelKey]
		if !exists {
			klog.V(5).Infof("Pod %s/%s has no role label, skipping", pod.Namespace, pod.Name)
			continue
		}

		switch role {
		case RolePrefill:
			prefillPods = append(prefillPods, pod)
		case RoleDecode:
			decodePods = append(decodePods, pod)
		default:
			klog.V(4).Infof("Pod %s/%s has unknown role: %s, skipping", pod.Namespace, pod.Name, role)
		}
	}

	if len(prefillPods) == 0 && len(decodePods) == 0 {
		return nil, nil, fmt.Errorf("no pods with valid P/D role labels")
	}

	return prefillPods, decodePods, nil
}

// getPodAddress returns the pod's IP:port address for metrics
func (r *kvAwareRouter) getPodAddress(pod *v1.Pod) string {
	port := DefaultMetricPort
	if pod.Annotations != nil {
		if customPort, exists := pod.Annotations[MetricPortAnnotation]; exists {
			port = customPort
		}
	}
	return fmt.Sprintf("%s:%s", pod.Status.PodIP, port)
}

// convertPodsToPodRefs converts Kubernetes pods to PodRef structures
func (r *kvAwareRouter) convertPodsToPodRefs(pods []*v1.Pod, role string) []PodRef {
	refs := make([]PodRef, 0, len(pods))
	for _, pod := range pods {
		modelName := ""
		if pod.Labels != nil {
			modelName = pod.Labels[ModelLabelKey]
		}

		refs = append(refs, PodRef{
			Namespace: pod.Namespace,
			Name:      pod.Name,
			IPPort:    r.getPodAddress(pod),
			Role:      role,
			ModelName: modelName,
		})
	}
	return refs
}

// getPodMetricsWithCache retrieves pod metrics with local caching
func (r *kvAwareRouter) getPodMetricsWithCache(podRef PodRef) (PodMetrics, error) {
	// Check cache first
	if cached, ok := r.metricsCache.get(podRef.Key()); ok {
		klog.V(5).Infof("Using cached metrics for %s", podRef.Name)
		return cached, nil
	}

	// Fetch fresh metrics
	metrics, err := r.metricsReader.GetPodMetrics(podRef)
	if err != nil {
		return metrics, err
	}

	// Cache the results
	r.metricsCache.put(podRef.Key(), metrics)

	return metrics, nil
}

// computePrefixMatches uses the prefix matcher to compute prefix matches for ready pods
func (r *kvAwareRouter) computePrefixMatches(
	ctx *types.RoutingContext,
	prompt string,
	modelName string,
	readyPods []PodRef,
) (PrefixMatch, error) {
	// Extract LoRA ID from request headers
	loraID := r.extractLoraID(ctx)

	// Tokenize the prompt
	tokens, err := r.tokenizer.Tokenize(prompt, modelName)
	if err != nil {
		klog.Errorf("Failed to tokenize prompt for model %s: %v", modelName, err)
		return PrefixMatch{}, fmt.Errorf("tokenization failed: %w", err)
	}

	// Get block size from model spec
	blockSize := r.config.Models[0].BlockSizeTokens // Default from first model
	for _, modelSpec := range r.config.Models {
		if modelSpec.ModelName == modelName {
			blockSize = modelSpec.BlockSizeTokens
			break
		}
	}

	// Compute prefix matches
	prefixMatch, err := r.prefixMatcher.ComputePrefixMatch(
		modelName,
		loraID,
		tokens,
		readyPods,
		blockSize,
	)
	if err != nil {
		klog.Errorf("Failed to compute prefix match: %v", err)
		return PrefixMatch{}, fmt.Errorf("prefix match computation failed: %w", err)
	}

	klog.V(4).Infof("Computed prefix match: best=%s blocks=%d total_matches=%d",
		prefixMatch.BestPod, prefixMatch.BestBlocks, len(prefixMatch.PodPrefixBlocks))

	return prefixMatch, nil
}

// extractLoraID extracts LoRA ID from request headers
// Returns -1 if no LoRA adapter is specified (representing no LoRA)
func (r *kvAwareRouter) extractLoraID(ctx *types.RoutingContext) int64 {
	// Check for LoRA ID in request headers
	// Common header names: X-LoRA-ID, X-Lora-Adapter-Id
	headers := ctx.ReqHeaders
	if headers != nil {
		// Try X-LoRA-ID header first
		if loraIDStr, exists := headers["X-LoRA-ID"]; exists && loraIDStr != "" {
			var loraID int64
			if _, err := fmt.Sscanf(loraIDStr, "%d", &loraID); err == nil {
				klog.V(5).Infof("Extracted LoRA ID from X-LoRA-ID header: %d", loraID)
				return loraID
			}
		}

		// Try X-Lora-Adapter-Id header
		if loraIDStr, exists := headers["X-Lora-Adapter-Id"]; exists && loraIDStr != "" {
			var loraID int64
			if _, err := fmt.Sscanf(loraIDStr, "%d", &loraID); err == nil {
				klog.V(5).Infof("Extracted LoRA ID from X-Lora-Adapter-Id header: %d", loraID)
				return loraID
			}
		}
	}

	// No LoRA adapter specified
	klog.V(5).Info("No LoRA ID found in headers, using -1 (no LoRA)")
	return -1
}

// fallback uses the fallback algorithm when KV-aware routing cannot proceed
func (r *kvAwareRouter) fallback(ctx *types.RoutingContext, readyPodList types.PodList) (string, error) {
	klog.V(3).Infof("KV-aware routing falling back to %s algorithm", r.fallbackAlgo)

	// Create fallback router
	fallbackRouter, err := routingalgorithms.NewLeastRequestRouter()
	if err != nil {
		return "", fmt.Errorf("failed to create fallback router: %w", err)
	}

	// Use fallback router
	return fallbackRouter.Route(ctx, readyPodList)
}

// evaluatePrefillCandidates evaluates all prefill pods and returns TTFT estimates (Phase 005)
func (r *kvAwareRouter) evaluatePrefillCandidates(
	ctx *types.RoutingContext,
	prefillPods []PodRef,
	prefixMatch PrefixMatch,
	promptTokens []int,
) ([]PrefillEval, error) {
	if len(prefillPods) == 0 {
		return nil, fmt.Errorf("no prefill pods available")
	}

	klog.V(4).Infof("Evaluating %d prefill pod candidates", len(prefillPods))

	// 1. Batch fetch metrics for all prefill pods
	metricsMap := r.metricsReader.BatchGetPodMetrics(prefillPods)

	// 2. Estimate TTFT for all pods concurrently
	evals := r.ttftEstimator.EstimatePrefillPods(
		prefillPods,
		&prefixMatch,
		metricsMap,
		promptTokens,
	)

	// 3. Filter out pods that exceed TTFT SLO (if configured)
	if r.config.TTFTSLO > 0 {
		filteredEvals := make([]PrefillEval, 0, len(evals))
		for _, eval := range evals {
			if eval.TTFT <= r.config.TTFTSLO.Seconds() {
				filteredEvals = append(filteredEvals, eval)
			} else {
				klog.V(4).Infof("Pod %s exceeds TTFT SLO: %.2fs > %.2fs",
					eval.Pod.Name, eval.TTFT, r.config.TTFTSLO.Seconds())
			}
		}
		evals = filteredEvals
	}

	if len(evals) == 0 {
		return nil, fmt.Errorf("no prefill pods meet TTFT SLO requirements")
	}

	return evals, nil
}

// selectBestPrefillPod selects the prefill pod with minimum TTFT (Phase 005)
func (r *kvAwareRouter) selectBestPrefillPod(
	evals []PrefillEval,
) (*PodRef, error) {
	if len(evals) == 0 {
		return nil, fmt.Errorf("no prefill pods meet TTFT SLO")
	}

	// Find pod with minimum TTFT
	bestIdx := 0
	for i := 1; i < len(evals); i++ {
		if evals[i].TTFT < evals[bestIdx].TTFT {
			bestIdx = i
		} else if evals[i].TTFT == evals[bestIdx].TTFT {
			// Tie-breaker: prefer pod with more cached blocks
			if evals[i].LocalPrefixBlk > evals[bestIdx].LocalPrefixBlk {
				bestIdx = i
			}
		}
	}

	klog.V(3).Infof("Selected prefill pod %s with TTFT=%.2fs (from %d candidates)",
		evals[bestIdx].Pod.Name, evals[bestIdx].TTFT, len(evals))

	return &evals[bestIdx].Pod, nil
}

// estimateOutputTokens estimates the number of output tokens for a request (Phase 006)
func estimateOutputTokens(promptLength int) int {
	// Simple heuristic: output is typically 0.5x to 2x input length
	// Use 1x as default estimate
	estimated := promptLength

	// Apply reasonable bounds
	if estimated < 10 {
		estimated = 10 // Minimum 10 tokens
	} else if estimated > 2048 {
		estimated = 2048 // Maximum 2048 tokens for estimation
	}

	return estimated
}
