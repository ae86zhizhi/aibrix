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
	"sort"
	"strings"
	"time"

	"github.com/vllm-project/aibrix/pkg/cache"
	"github.com/vllm-project/aibrix/pkg/metrics"
	routingalgorithms "github.com/vllm-project/aibrix/pkg/plugins/gateway/algorithms"
	"github.com/vllm-project/aibrix/pkg/types"
	"github.com/vllm-project/aibrix/pkg/utils/syncprefixcacheindexer"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// RoutingStatistics defines the interface for routing statistics tracking
// Implementation in statistics.go
type RoutingStatistics interface {
	IncrementTotal()
	IncrementSuccess()
	IncrementFallback(reason string)
	IncrementRejection(reason string)
	IncrementError(reason string)
	RecordLatency(duration time.Duration)
	RecordCacheHit(hitRate float64)
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
	stats          RoutingStatistics // Phase 007: Statistics tracking (noop for now)
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

	// Create statistics tracker (Phase 007)
	stats := NewRoutingStatistics()

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
		stats:          stats,
		fallbackAlgo:   routingalgorithms.RouterLeastRequest,
	}

	klog.V(2).Infof("KV-aware router initialized with config: TTFT SLO=%v, TBT SLO=%v, Bandwidth=%v Gbps",
		config.TTFTSLO, config.TBTSLO, config.TransferBandwidthBps/1e9)
	klog.V(2).Info("KV-aware router initialized with metrics reader and cache (TTL: 5s)")
	klog.V(2).Info("KV-aware router initialized with prefix matcher and tokenizer (Phase 004)")
	klog.V(2).Info("KV-aware router initialized with TTFT estimator (Phase 005)")
	klog.V(2).Info("KV-aware router initialized with decode selector and SLO checker (Phase 006)")
	klog.V(2).Info("KV-aware router initialized with statistics tracking (Phase 007)")

	return router, nil
}

// Route implements the complete KV-aware routing algorithm (Phase 007)
func (r *kvAwareRouter) Route(ctx *types.RoutingContext, readyPodList types.PodList) (string, error) {
	startTime := time.Now()
	requestID := ctx.RequestID

	// Track routing attempt
	r.stats.IncrementTotal()

	defer func() {
		duration := time.Since(startTime)
		r.stats.RecordLatency(duration)
		klog.V(4).Infof("KV-aware routing took %v for request %s", duration, requestID)
	}()

	// Step 1: Get all ready pods
	allPods := readyPodList.All()
	if len(allPods) == 0 {
		r.stats.IncrementError("no_pods")
		return "", fmt.Errorf("no ready pods available")
	}

	// Step 2: Separate prefill and decode pods
	prefillPods, decodePods, err := r.separatePrefillDecodePods(allPods)
	if err != nil {
		klog.Errorf("Failed to separate P/D pods: %v, falling back", err)
		r.stats.IncrementFallback("pd_separation_failed")
		return r.fallback(ctx, readyPodList)
	}

	// Check if we have both types of pods
	if len(prefillPods) == 0 || len(decodePods) == 0 {
		klog.Warningf("Missing P/D pods (prefill=%d, decode=%d), falling back",
			len(prefillPods), len(decodePods))
		r.stats.IncrementFallback("missing_pd_pods")
		return r.fallback(ctx, readyPodList)
	}

	// Step 3: Convert to PodRefs
	prefillRefs := r.convertPodsToPodRefs(prefillPods, RolePrefill)
	decodeRefs := r.convertPodsToPodRefs(decodePods, RoleDecode)

	// Step 4: Tokenize prompt
	promptTokens, err := r.getPromptTokens(ctx)
	if err != nil {
		klog.Errorf("Failed to tokenize prompt: %v, falling back", err)
		r.stats.IncrementFallback("tokenization_failed")
		return r.fallback(ctx, readyPodList)
	}

	klog.V(4).Infof("Request %s: %d tokens, %d prefill pods, %d decode pods",
		requestID, len(promptTokens), len(prefillRefs), len(decodeRefs))

	// Step 5: Compute prefix matches
	prefixMatch, err := r.computePrefixMatchesForRefs(ctx, prefillRefs, promptTokens)
	if err != nil {
		klog.Errorf("Failed to compute prefix matches: %v, continuing without cache", err)
		// Create empty prefix match as fallback
		prefixMatch = &PrefixMatch{
			PodPrefixBlocks: make(map[string]int),
			BestBlocks:      0,
		}
	}

	// Log prefix match results
	r.logPrefixMatchResults(requestID, prefixMatch, len(promptTokens))

	// Step 6: Get metrics for all pods
	allRefs := append(prefillRefs, decodeRefs...)
	metricsMap := r.metricsReader.BatchGetPodMetrics(allRefs)

	// Step 7: Evaluate all prefill pods
	prefillEvals := r.ttftEstimator.EstimatePrefillPods(
		prefillRefs, prefixMatch, metricsMap, promptTokens,
	)

	// Step 8: Select best prefill pod
	bestPrefill := r.selectBestPrefill(prefillEvals)
	if bestPrefill == nil {
		klog.Error("No suitable prefill pod found")
		r.stats.IncrementError("no_prefill_candidate")
		return "", ErrNoPrefillCandidate
	}

	// Step 9: Check TTFT SLO
	if r.config.TTFTSLO > 0 {
		if err := r.sloChecker.CheckTTFTSLO(bestPrefill.TTFT, r.config.TTFTSLO); err != nil {
			klog.Warningf("TTFT SLO violation for request %s: %v", requestID, err)
			// Try relaxed SLO
			relaxedSLO := RelaxSLO(r.config.TTFTSLO, 1.5)
			if err := r.sloChecker.CheckTTFTSLO(bestPrefill.TTFT, relaxedSLO); err != nil {
				r.stats.IncrementRejection("ttft_slo_violation")
				return "", NewRejectionError(429, "Cannot meet TTFT SLO", "ttft_slo_violation", 5*time.Second)
			}
			klog.V(3).Infof("Request %s accepted with relaxed TTFT SLO", requestID)
		}
	}

	// Step 10: Estimate output tokens for decode selection
	outputTokens := estimateOutputTokens(len(promptTokens))

	// Step 11: Select decode pod
	decodePod, predictedTBT, err := r.decodeSelector.SelectDecodePod(
		decodeRefs, metricsMap, r.config.TBTSLO, outputTokens,
	)
	if err != nil {
		klog.Errorf("Failed to select decode pod: %v", err)
		r.stats.IncrementError("no_decode_candidate")
		return "", err
	}

	// Step 12: Final SLO check
	if r.sloChecker.ShouldReject(bestPrefill.TTFT, predictedTBT,
		r.config.TTFTSLO, r.config.TBTSLO) {
		klog.Warningf("Request %s rejected due to SLO violation", requestID)
		r.stats.IncrementRejection("combined_slo_violation")
		return "", NewRejectionError(429, "Cannot meet SLO", "cannot_meet_slo", 5*time.Second)
	}

	// Step 13: Create routing decision
	decision := &RoutingDecision{
		RequestID:     requestID,
		Timestamp:     time.Now(),
		PrefillPod:    &bestPrefill.Pod,
		DecodePod:     decodePod,
		EstimatedTTFT: bestPrefill.TTFT,
		PredictedTBT:  predictedTBT,
		PrefillEvals:  prefillEvals,
		DecisionType:  "kv_aware",
		PrefixBlocks:  bestPrefill.LocalPrefixBlk,
		TotalBlocks:   (len(promptTokens) + r.config.Models[0].BlockSizeTokens - 1) / r.config.Models[0].BlockSizeTokens,
	}

	// Log routing decision
	r.logRoutingDecision(decision)

	// Track successful routing
	r.stats.IncrementSuccess()
	r.stats.RecordCacheHit(float64(decision.PrefixBlocks) / float64(decision.TotalBlocks))

	// Set target pod and return address
	// For MVP, route to prefill pod (in future, could route to both)
	targetPod := r.getPodFromRef(bestPrefill.Pod)
	ctx.SetTargetPod(targetPod)

	targetAddr := r.getPodAddressFromPod(targetPod)

	klog.V(2).Infof("Routed request %s to prefill pod %s (TTFT: %.2fs, TBT: %.3fs)",
		requestID, bestPrefill.Pod.Name, bestPrefill.TTFT, predictedTBT)

	return targetAddr, nil
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

// getPromptTokens extracts and tokenizes the prompt from routing context
func (r *kvAwareRouter) getPromptTokens(ctx *types.RoutingContext) ([]int, error) {
	return ctx.PromptTokens()
}

// computePrefixMatchesForRefs adapts the existing computePrefixMatches to work with PodRefs
func (r *kvAwareRouter) computePrefixMatchesForRefs(
	ctx *types.RoutingContext,
	readyPods []PodRef,
	tokens []int,
) (*PrefixMatch, error) {
	// Extract LoRA ID from request headers
	loraID := r.extractLoraID(ctx)

	// Get model name from context
	modelName := ctx.Model
	if modelName == "" && len(r.config.Models) > 0 {
		modelName = r.config.Models[0].ModelName
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
		return nil, fmt.Errorf("prefix match computation failed: %w", err)
	}

	return &prefixMatch, nil
}

// selectBestPrefill selects the prefill pod with the lowest TTFT
func (r *kvAwareRouter) selectBestPrefill(evals []PrefillEval) *PrefillEval {
	if len(evals) == 0 {
		return nil
	}

	// Sort by TTFT (ascending)
	sort.Slice(evals, func(i, j int) bool {
		return evals[i].TTFT < evals[j].TTFT
	})

	best := &evals[0]

	// Log top candidates
	if klog.V(4).Enabled() {
		klog.V(4).Info("Top prefill candidates:")
		for i := 0; i < len(evals) && i < 3; i++ {
			klog.V(4).Infof("  [%d] %s: TTFT=%.2fs (local=%d blocks)",
				i+1, evals[i].Pod.Name, evals[i].TTFT, evals[i].LocalPrefixBlk)
		}
	}

	return best
}

// logPrefixMatchResults logs prefix matching results
func (r *kvAwareRouter) logPrefixMatchResults(requestID string, match *PrefixMatch, totalTokens int) {
	if !klog.V(3).Enabled() {
		return
	}

	blockSize := r.config.Models[0].BlockSizeTokens
	totalBlocks := (totalTokens + blockSize - 1) / blockSize

	klog.V(3).Infof("Prefix match for request %s:", requestID)
	klog.V(3).Infof("  Total: %d tokens (%d blocks)", totalTokens, totalBlocks)
	klog.V(3).Infof("  Best: %s with %d blocks", match.BestPod, match.BestBlocks)

	if klog.V(4).Enabled() {
		for pod, blocks := range match.PodPrefixBlocks {
			hitRate := float64(blocks) / float64(totalBlocks) * 100
			klog.V(4).Infof("    %s: %d blocks (%.1f%%)", pod, blocks, hitRate)
		}
	}
}

// logRoutingDecision logs detailed routing decision
func (r *kvAwareRouter) logRoutingDecision(decision *RoutingDecision) {
	if !klog.V(2).Enabled() {
		return
	}

	klog.V(2).Infof("=== Routing Decision for %s ===", decision.RequestID)
	klog.V(2).Infof("  Prefill: %s (TTFT: %.2fs)",
		decision.PrefillPod.Name, decision.EstimatedTTFT)
	klog.V(2).Infof("  Decode: %s (TBT: %.3fs)",
		decision.DecodePod.Name, decision.PredictedTBT)
	klog.V(2).Infof("  Cache hit: %d/%d blocks (%.1f%%)",
		decision.PrefixBlocks, decision.TotalBlocks,
		float64(decision.PrefixBlocks)/float64(decision.TotalBlocks)*100)
	klog.V(2).Info("================================")
}

// getPodFromRef converts PodRef to Pod object
func (r *kvAwareRouter) getPodFromRef(ref PodRef) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ref.Name,
			Namespace: ref.Namespace,
		},
		Status: v1.PodStatus{
			PodIP: extractIPFromIPPort(ref.IPPort),
		},
	}
}

// extractIPFromIPPort extracts IP from "IP:port" format
func extractIPFromIPPort(ipPort string) string {
	if idx := strings.LastIndex(ipPort, ":"); idx > 0 {
		return ipPort[:idx]
	}
	return ipPort
}

// getPodAddressFromPod returns the address to route to (renamed from existing getPodAddress)
func (r *kvAwareRouter) getPodAddressFromPod(pod *v1.Pod) string {
	if pod.Status.PodIP == "" {
		return ""
	}
	// Default to port 8000 (vLLM default)
	return fmt.Sprintf("%s:8000", pod.Status.PodIP)
}

// RelaxSLO relaxes SLO by a multiplier
func RelaxSLO(slo time.Duration, multiplier float64) time.Duration {
	return time.Duration(float64(slo) * multiplier)
}
