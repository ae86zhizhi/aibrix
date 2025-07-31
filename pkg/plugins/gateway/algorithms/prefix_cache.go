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
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/vllm-project/aibrix/pkg/cache"
	"github.com/vllm-project/aibrix/pkg/constants"
	"github.com/vllm-project/aibrix/pkg/metrics"
	"github.com/vllm-project/aibrix/pkg/types"
	"github.com/vllm-project/aibrix/pkg/utils"
	"github.com/vllm-project/aibrix/pkg/utils/prefixcacheindexer"
	syncindexer "github.com/vllm-project/aibrix/pkg/utils/syncprefixcacheindexer"
	"github.com/vllm-project/aibrix/pkg/utils/tokenizer"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

const (
	defaultTokenizerType                      = "character"
	defaultPodRunningRequestImbalanceAbsCount = 8
	defaultStandardDeviationFactor            = 1
)

var (
	RouterPrefixCache                  types.RoutingAlgorithm = "prefix-cache"
	tokenizerType                                             = utils.LoadEnv("AIBRIX_PREFIX_CACHE_TOKENIZER_TYPE", "character")
	podRunningRequestImbalanceAbsCount int                    = utils.LoadEnvInt("AIBRIX_PREFIX_CACHE_POD_RUNNING_REQUEST_IMBALANCE_ABS_COUNT", defaultPodRunningRequestImbalanceAbsCount)
	standardDeviationFactor            int                    = utils.LoadEnvInt("AIBRIX_PREFIX_CACHE_STANDARD_DEVIATION_FACTOR", defaultStandardDeviationFactor)
)

// PrefixCacheMetrics holds all prefix cache metrics
type PrefixCacheMetrics struct {
	prefixCacheRoutingDecisions *prometheus.CounterVec
	prefixCacheIndexerStatus    *prometheus.GaugeVec
	prefixCacheRoutingLatency   *prometheus.HistogramVec
}

// Global metrics instance
var (
	prefixCacheMetrics     *PrefixCacheMetrics
	prefixCacheMetricsOnce sync.Once
	prefixCacheMetricsMu   sync.RWMutex
)

// createPrefixCacheMetrics creates all prefix cache metrics (but doesn't register them)
func createPrefixCacheMetrics() *PrefixCacheMetrics {
	return &PrefixCacheMetrics{
		prefixCacheRoutingDecisions: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "aibrix_prefix_cache_routing_decisions_total",
				Help: "Total number of routing decisions by match percentage",
			},
			[]string{"model", "match_percent_bucket", "using_kv_sync"},
		),
		prefixCacheIndexerStatus: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "aibrix_prefix_cache_indexer_status",
				Help: "Status of prefix cache indexer (1=available, 0=unavailable)",
			},
			[]string{"model", "indexer_type"},
		),
		prefixCacheRoutingLatency: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "aibrix_prefix_cache_routing_latency_seconds",
				Help:    "Latency of prefix cache routing decisions",
				Buckets: prometheus.ExponentialBuckets(0.00001, 2, 15), // 10μs to ~160ms
			},
			[]string{"model", "using_kv_sync"},
		),
	}
}

// registerPrefixCacheMetrics registers all metrics with Prometheus
func (m *PrefixCacheMetrics) register() error {
	collectors := []prometheus.Collector{
		m.prefixCacheRoutingDecisions,
		m.prefixCacheIndexerStatus,
		m.prefixCacheRoutingLatency,
	}

	for _, collector := range collectors {
		if err := prometheus.Register(collector); err != nil {
			// If already registered, it's ok (might happen in tests)
			if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
				return fmt.Errorf("failed to register metric: %w", err)
			}
		}
	}

	return nil
}

// initializePrefixCacheMetrics initializes prefix cache metrics if enabled
func initializePrefixCacheMetrics() error {
	// Check if metrics are enabled
	metricsEnabled := utils.LoadEnv(constants.EnvPrefixCacheMetricsEnabled, "false") == "true"
	if !metricsEnabled {
		return nil
	}

	var err error
	prefixCacheMetricsOnce.Do(func() {
		prefixCacheMetricsMu.Lock()
		defer prefixCacheMetricsMu.Unlock()

		metrics := createPrefixCacheMetrics()
		if registerErr := metrics.register(); registerErr != nil {
			err = registerErr
			klog.Errorf("Failed to register prefix cache metrics: %v", registerErr)
			return
		}
		prefixCacheMetrics = metrics
		klog.Info("Prefix cache metrics registered successfully")
	})
	return err
}

// getPrefixCacheMetrics returns the global metrics instance if available
func getPrefixCacheMetrics() *PrefixCacheMetrics {
	prefixCacheMetricsMu.RLock()
	defer prefixCacheMetricsMu.RUnlock()
	return prefixCacheMetrics
}

func init() {
	Register(RouterPrefixCache, NewPrefixCacheRouter)
}

// kvSyncPrefixCacheRouter handles routing when KV sync is enabled
type kvSyncPrefixCacheRouter struct {
	cache           cache.Cache
	remoteTokenizer tokenizer.Tokenizer
	syncIndexer     *syncindexer.SyncPrefixHashTable
	metricsEnabled  bool
}

type prefixCacheRouter struct {
	cache              cache.Cache
	tokenizer          tokenizer.Tokenizer
	prefixCacheIndexer *prefixcacheindexer.PrefixHashTable

	// KV sync router - only created when needed
	kvSyncRouter *kvSyncPrefixCacheRouter
}

func NewPrefixCacheRouter() (types.Router, error) {
	// Initialize prefix cache metrics if enabled
	if err := initializePrefixCacheMetrics(); err != nil {
		klog.Errorf("Failed to initialize prefix cache metrics: %v", err)
		// Continue without metrics rather than failing
	}

	// Tokenizer selection logic with dependency awareness
	var tokenizerObj tokenizer.Tokenizer
	var remoteTokenizerObj tokenizer.Tokenizer

	// Check configuration dependencies
	remoteTokenizerValue := utils.LoadEnv("AIBRIX_USE_REMOTE_TOKENIZER", "false")
	useRemoteTokenizer, _ := strconv.ParseBool(remoteTokenizerValue)
	kvSyncValue := utils.LoadEnv("AIBRIX_KV_EVENT_SYNC_ENABLED", "false")
	kvSyncEnabled, _ := strconv.ParseBool(kvSyncValue)

	// Log configuration state
	klog.InfoS("prefix cache router configuration",
		"remote_tokenizer_requested", useRemoteTokenizer,
		"kv_sync_requested", kvSyncEnabled)

	// Enforce dependency: KV sync requires remote tokenizer
	if kvSyncEnabled && !useRemoteTokenizer {
		klog.Warning("KV event sync requires remote tokenizer. " +
			"Remote tokenizer will be automatically enabled.")
		useRemoteTokenizer = true
	}

	// Configure remote tokenizer if needed
	if useRemoteTokenizer {
		// Create remote tokenizer for vLLM consistency
		remoteConfig := tokenizer.RemoteTokenizerConfig{
			Engine:     utils.LoadEnv("AIBRIX_REMOTE_TOKENIZER_ENGINE", "vllm"),
			Endpoint:   utils.LoadEnv("AIBRIX_REMOTE_TOKENIZER_ENDPOINT", ""),
			Model:      utils.LoadEnv("AIBRIX_REMOTE_TOKENIZER_MODEL", ""),
			Timeout:    30 * time.Second,
			MaxRetries: 3,
		}

		if remoteConfig.Endpoint != "" {
			remote, err := tokenizer.NewRemoteTokenizer(remoteConfig)
			if err != nil {
				if kvSyncEnabled {
					// Remote tokenizer is required for KV sync
					return nil, fmt.Errorf("failed to create remote tokenizer (required for KV sync): %w", err)
				}
				klog.Warningf("Failed to create remote tokenizer: %v, falling back to local", err)
			} else {
				remoteTokenizerObj = remote
				tokenizerObj = remote // Use remote as primary
				klog.Info("Remote tokenizer initialized successfully")
			}
		} else if kvSyncEnabled {
			return nil, fmt.Errorf("AIBRIX_REMOTE_TOKENIZER_ENDPOINT not configured (required for KV sync)")
		}
	}

	// Fallback to local tokenizer if remote not available
	if tokenizerObj == nil {
		if tokenizerType == "tiktoken" {
			tokenizerObj = tokenizer.NewTiktokenTokenizer()
		} else {
			tokenizerObj = tokenizer.NewCharacterTokenizer()
		}
	}

	c, err := cache.Get()
	if err != nil {
		klog.Error("fail to get cache store in prefix cache router")
		return nil, err
	}

	klog.InfoS("prefix_cache_configurations",
		"tokenizer_type", tokenizerType,
		"remote_tokenizer_enabled", remoteTokenizerObj != nil,
		"kv_sync_enabled", kvSyncEnabled,
		"pod_running_request_imbalance_abs_count", podRunningRequestImbalanceAbsCount,
		"matched_pods_running_requests_standard_deviation_factor", standardDeviationFactor)

	// Create main router with local indexer
	router := prefixCacheRouter{
		cache:              c,
		tokenizer:          tokenizerObj,
		prefixCacheIndexer: prefixcacheindexer.NewPrefixHashTable(),
	}

	// Only create KV sync router if enabled
	if kvSyncEnabled && useRemoteTokenizer {
		kvSyncRouter := &kvSyncPrefixCacheRouter{
			cache:           c,
			remoteTokenizer: remoteTokenizerObj,
			syncIndexer:     syncindexer.NewSyncPrefixHashTable(),
			metricsEnabled:  true,
		}

		router.kvSyncRouter = kvSyncRouter

		// Set initial metrics only if KV sync is enabled
		if metrics := getPrefixCacheMetrics(); metrics != nil {
			metrics.prefixCacheIndexerStatus.WithLabelValues("", "sync").Set(1)
			metrics.prefixCacheIndexerStatus.WithLabelValues("", "local").Set(0)
		}
	} else {
		// Set local indexer metrics only if metrics are enabled
		if metrics := getPrefixCacheMetrics(); metrics != nil {
			metrics.prefixCacheIndexerStatus.WithLabelValues("", "local").Set(1)
			metrics.prefixCacheIndexerStatus.WithLabelValues("", "sync").Set(0)
		}
	}

	return router, nil
}

func (p prefixCacheRouter) Route(ctx *types.RoutingContext, readyPodList types.PodList) (string, error) {
	if p.kvSyncRouter != nil {
		return p.kvSyncRouter.Route(ctx, readyPodList)
	}
	// Original implementation unchanged
	return p.routeOriginal(ctx, readyPodList)
}

// routeOriginal preserves the exact original implementation for backward compatibility
func (p prefixCacheRouter) routeOriginal(ctx *types.RoutingContext, readyPodList types.PodList) (string, error) {
	var prefixHashes []uint64
	var matchedPods map[string]int
	var targetPod *v1.Pod

	tokens, err := p.tokenizer.TokenizeInputText(ctx.Message)
	if err != nil {
		return "", err
	}

	readyPods := readyPodList.All()
	readyPodsMap := map[string]struct{}{}
	for _, pod := range readyPods {
		readyPodsMap[pod.Name] = struct{}{}
	}

	var isLoadImbalanced bool
	targetPod, isLoadImbalanced = getTargetPodOnLoadImbalance(p.cache, readyPods)
	if isLoadImbalanced {
		prefixHashes = p.prefixCacheIndexer.GetPrefixHashes(tokens)
		klog.InfoS("prefix_cache_load_imbalanced",
			"request_id", ctx.RequestID,
			"target_pod", targetPod.Name,
			"target_pod_ip", targetPod.Status.PodIP,
			"pod_request_count", getRequestCounts(p.cache, readyPods))
	} else {
		matchedPods, prefixHashes = p.prefixCacheIndexer.MatchPrefix(tokens, ctx.Model, readyPodsMap)
		klog.InfoS("prefix_hashes", "request_id", ctx.RequestID, "prefix_hashes", prefixHashes)

		if len(matchedPods) > 0 {
			targetPod = getTargetPodFromMatchedPods(p.cache, readyPods, matchedPods)
			if targetPod != nil {
				klog.InfoS("prefix_cache_matched_pods",
					"request_id", ctx.RequestID,
					"target_pod", targetPod.Name,
					"target_pod_ip", targetPod.Status.PodIP,
					"matched_pods", matchedPods,
					"pod_request_count", getRequestCounts(p.cache, readyPods))
			} else {
				klog.InfoS("prefix_cache_skip_matched_pods",
					"request_id", ctx.RequestID,
					"matched_pods", matchedPods,
					"pod_request_count", getRequestCounts(p.cache, readyPods))
			}
		}
	}

	// no pod with prefix match, as a fallback select pod with least request count
	if len(matchedPods) == 0 || targetPod == nil {
		targetPod = selectTargetPodWithLeastRequestCount(p.cache, readyPods)
		klog.InfoS("prefix_cache_fallback_least_request_count",
			"request_id", ctx.RequestID,
			"target_pod", targetPod.Name,
			"target_pod_ip", targetPod.Status.PodIP,
			"matched_pods", matchedPods,
			"pod_request_count", getRequestCounts(p.cache, readyPods))
	}

	if len(prefixHashes) > 0 {
		p.prefixCacheIndexer.AddPrefix(prefixHashes, ctx.Model, targetPod.Name)
	}

	ctx.SetTargetPod(targetPod)
	return ctx.TargetAddress(), nil
}

// Route handles KV sync routing with clean implementation
func (k *kvSyncPrefixCacheRouter) Route(ctx *types.RoutingContext, readyPodList types.PodList) (string, error) {
	// Start timing for latency metric if metrics are enabled
	var startTime time.Time
	if k.metricsEnabled {
		startTime = time.Now()
		defer func() {
			if metrics := getPrefixCacheMetrics(); metrics != nil {
				metrics.prefixCacheRoutingLatency.WithLabelValues(ctx.Model, "true").Observe(time.Since(startTime).Seconds())
			}
		}()
	}

	var prefixHashes []uint64
	var matchedPods map[string]int
	var targetPod *v1.Pod

	// Get model information from context
	modelName := ctx.Model
	allPods := readyPodList.All()
	if modelName == "" && len(allPods) > 0 {
		modelName = allPods[0].Labels["model.aibrix.ai/name"]
	}

	loraID := int64(-1) // TODO: Extract from context when available

	// Tokenize input using remote tokenizer
	tokens, err := k.remoteTokenizer.TokenizeInputText(ctx.Message)
	if err != nil {
		return "", err
	}

	readyPods := readyPodList.All()

	// Check for load imbalance first
	var isLoadImbalanced bool
	targetPod, isLoadImbalanced = getTargetPodOnLoadImbalance(k.cache, readyPods)

	if isLoadImbalanced {
		// Handle load imbalance case
		prefixHashes = k.syncIndexer.GetPrefixHashes(tokens)

		klog.InfoS("prefix_cache_load_imbalanced",
			"request_id", ctx.RequestID,
			"target_pod", targetPod.Name,
			"target_pod_ip", targetPod.Status.PodIP,
			"pod_request_count", getRequestCounts(k.cache, readyPods))
	} else {
		// Normal routing with prefix matching
		// Build pod key map for sync indexer
		readyPodsMap := map[string]struct{}{}
		for _, pod := range readyPods {
			podKey := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
			readyPodsMap[podKey] = struct{}{}
		}

		// Match prefixes using sync indexer
		if k.syncIndexer == nil {
			// Return error if sync indexer is not available
			return "", fmt.Errorf("sync indexer not available for KV sync routing")
		}
		matchedPods, prefixHashes = k.syncIndexer.MatchPrefix(modelName, loraID, tokens, readyPodsMap)

		klog.V(4).InfoS("prefix cache matching completed",
			"model", modelName,
			"lora_id", loraID,
			"matched_pods", len(matchedPods),
			"prefix_hashes", len(prefixHashes),
			"ready_pods", readyPodList.Len())

		if len(matchedPods) > 0 {
			targetPod = getTargetPodFromMatchedPodsWithKeys(k.cache, readyPods, matchedPods)
			if targetPod != nil {
				klog.InfoS("prefix_cache_matched_pods",
					"request_id", ctx.RequestID,
					"target_pod", targetPod.Name,
					"target_pod_ip", targetPod.Status.PodIP,
					"matched_pods", matchedPods,
					"pod_request_count", getRequestCounts(k.cache, readyPods))
			} else {
				klog.InfoS("prefix_cache_skip_matched_pods",
					"request_id", ctx.RequestID,
					"matched_pods", matchedPods,
					"pod_request_count", getRequestCounts(k.cache, readyPods))
			}
		}
	}

	// Fallback to least request count selection
	if len(matchedPods) == 0 || targetPod == nil {
		targetPod = selectPodWithLeastRequestCount(k.cache, readyPods)
		if targetPod != nil {
			klog.InfoS("prefix_cache_fallback_least_request_count",
				"request_id", ctx.RequestID,
				"target_pod", targetPod.Name,
				"target_pod_ip", targetPod.Status.PodIP,
				"matched_pods", matchedPods,
				"pod_request_count", getRequestCounts(k.cache, readyPods))
		}
	}

	// Handle case where no pods are available
	if targetPod == nil {
		return "", fmt.Errorf("no ready pods available for routing")
	}

	selectedPodKey := fmt.Sprintf("%s/%s", targetPod.Namespace, targetPod.Name)

	// Add prefix to sync indexer if we have prefixes
	if len(prefixHashes) > 0 {
		_ = k.syncIndexer.AddPrefix(modelName, loraID, selectedPodKey, prefixHashes)
	}

	// Record routing decision metric if metrics are enabled
	if k.metricsEnabled {
		matchPercent := 0
		if len(matchedPods) > 0 {
			if percent, exists := matchedPods[selectedPodKey]; exists {
				matchPercent = percent
			}
		}
		recordRoutingDecision(modelName, matchPercent, true)
	}

	ctx.SetTargetPod(targetPod)
	return ctx.TargetAddress(), nil
}

// getTargetPodFromMatchedPodsWithKeys is similar to getTargetPodFromMatchedPods but uses pod keys
func getTargetPodFromMatchedPodsWithKeys(cache cache.Cache, readyPods []*v1.Pod, matchedPods map[string]int) *v1.Pod {
	var targetPodKey string
	requestCount := []float64{}

	// Build pod key to pod mapping
	podKeyToPod := make(map[string]*v1.Pod)
	for _, pod := range readyPods {
		podKey := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
		podKeyToPod[podKey] = pod
	}

	podRequestCount := getRequestCountsWithKeys(cache, readyPods)
	for _, cnt := range podRequestCount {
		requestCount = append(requestCount, float64(cnt))
	}
	meanRequestCount := mean(requestCount)
	stdDevRequestCount := standardDeviation(requestCount)

	podkeys := []string{}
	for podkey := range matchedPods {
		podkeys = append(podkeys, podkey)
	}
	rand.Shuffle(len(podkeys), func(i, j int) {
		podkeys[i], podkeys[j] = podkeys[j], podkeys[i]
	})

	// sort pods with decreasing %perfixmatch AND for same %prefixmatch sort by increasing request count
	sort.SliceStable(podkeys, func(i, j int) bool {
		if matchedPods[podkeys[i]] == matchedPods[podkeys[j]] {
			return podRequestCount[podkeys[i]] < podRequestCount[podkeys[j]]
		}
		return matchedPods[podkeys[i]] > matchedPods[podkeys[j]]
	})

	// select targetpod with highest %prefixmatch and request_count within stddev
	for _, podkey := range podkeys {
		reqCnt := float64(podRequestCount[podkey])
		if reqCnt <= meanRequestCount+float64(standardDeviationFactor)*stdDevRequestCount {
			targetPodKey = podkey
			break
		}
	}

	return podKeyToPod[targetPodKey]
}

func getTargetPodFromMatchedPods(cache cache.Cache, readyPods []*v1.Pod, matchedPods map[string]int) *v1.Pod {
	var targetPodName string
	requestCount := []float64{}

	podRequestCount := getRequestCounts(cache, readyPods)
	for _, cnt := range podRequestCount {
		requestCount = append(requestCount, float64(cnt))
	}
	meanRequestCount := mean(requestCount)
	stdDevRequestCount := standardDeviation(requestCount)

	podnames := []string{}
	for podname := range matchedPods {
		podnames = append(podnames, podname)
	}
	rand.Shuffle(len(podnames), func(i, j int) {
		podnames[i], podnames[j] = podnames[j], podnames[i]
	})

	// sort pods with decreasing %perfixmatch AND for same %prefixmatch sort by increasing request count
	sort.SliceStable(podnames, func(i, j int) bool {
		if matchedPods[podnames[i]] == matchedPods[podnames[j]] {
			return podRequestCount[podnames[i]] < podRequestCount[podnames[j]]
		}
		return matchedPods[podnames[i]] > matchedPods[podnames[j]]
	})

	// select targetpod with highest %prefixmatch and request_count within stddev
	for _, podname := range podnames {
		reqCnt := float64(podRequestCount[podname])
		if reqCnt <= meanRequestCount+float64(standardDeviationFactor)*stdDevRequestCount {
			targetPodName = podname
			break
		}
	}
	targetPod, _ := utils.FilterPodByName(targetPodName, readyPods)
	return targetPod
}

// getTargetPodOnLoadImbalance evaluates if the load is imbalanced based on the abs difference between
// pods with min and max outstanding request counts
func getTargetPodOnLoadImbalance(cache cache.Cache, readyPods []*v1.Pod) (*v1.Pod, bool) {
	var imbalance bool
	var targetPod *v1.Pod
	targetPods := []string{}
	minValue := math.MaxInt32
	maxValue := math.MinInt32

	podRequestCount := getRequestCounts(cache, readyPods)
	for _, value := range podRequestCount {
		if value <= minValue {
			minValue = value
		}
		if value > maxValue {
			maxValue = value
		}
	}
	for podname, value := range podRequestCount {
		if minValue == value {
			targetPods = append(targetPods, podname)
		}
	}

	if maxValue-minValue > podRunningRequestImbalanceAbsCount {
		targetPod, _ = utils.FilterPodByName(targetPods[rand.Intn(len(targetPods))], readyPods)
		imbalance = true
	}

	return targetPod, imbalance
}

// getRequestCountsWithKeys returns running request count for each pod using pod keys
func getRequestCountsWithKeys(cache cache.Cache, readyPods []*v1.Pod) map[string]int {
	podRequestCount := map[string]int{}
	for _, pod := range readyPods {
		podKey := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
		runningReq, err := cache.GetMetricValueByPod(pod.Name, pod.Namespace, metrics.RealtimeNumRequestsRunning)
		if err != nil {
			runningReq = &metrics.SimpleMetricValue{Value: 0}
		}
		podRequestCount[podKey] = int(runningReq.GetSimpleValue())
	}
	return podRequestCount
}

func selectPodWithLeastRequestCount(cache cache.Cache, readyPods []*v1.Pod) *v1.Pod {
	var targetPod *v1.Pod
	targetPods := []string{}

	minCount := math.MaxInt32
	podRequestCount := getRequestCounts(cache, readyPods)
	klog.V(4).InfoS("selectPodWithLeastRequestCount", "podRequestCount", podRequestCount)
	for _, totalReq := range podRequestCount {
		if totalReq <= minCount {
			minCount = totalReq
		}
	}
	for podname, totalReq := range podRequestCount {
		if totalReq == minCount {
			targetPods = append(targetPods, podname)
		}
	}
	if len(targetPods) > 0 {
		targetPod, _ = utils.FilterPodByName(targetPods[rand.Intn(len(targetPods))], readyPods)
	}
	return targetPod
}

// recordRoutingDecision records metrics for routing decisions
func recordRoutingDecision(model string, matchPercent int, usingKVSync bool) {
	metrics := getPrefixCacheMetrics()
	if metrics == nil {
		return
	}

	var bucket string
	switch {
	case matchPercent == 0:
		bucket = "0"
	case matchPercent <= 25:
		bucket = "1-25"
	case matchPercent <= 50:
		bucket = "26-50"
	case matchPercent <= 75:
		bucket = "51-75"
	case matchPercent <= 99:
		bucket = "76-99"
	default:
		bucket = "100"
	}

	metrics.prefixCacheRoutingDecisions.WithLabelValues(model, bucket, strconv.FormatBool(usingKVSync)).Inc()
}
