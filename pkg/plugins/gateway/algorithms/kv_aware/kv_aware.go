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
	config        *KVAwareConfig
	cache         cache.Cache
	prefixIndexer *syncprefixcacheindexer.SyncPrefixHashTable
	fallbackAlgo  types.RoutingAlgorithm
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

	// Create prefix indexer instance for Phase 004 (not used in Phase 002)
	// In Phase 004, this will be used for prefix matching
	indexer := syncprefixcacheindexer.NewSyncPrefixHashTable()

	router := &kvAwareRouter{
		config:        config,
		cache:         c,
		prefixIndexer: indexer,
		fallbackAlgo:  routingalgorithms.RouterLeastRequest,
	}

	klog.V(2).Infof("KV-aware router initialized with config: TTFT SLO=%v, TBT SLO=%v, Bandwidth=%v Gbps",
		config.TTFTSLO, config.TBTSLO, config.TransferBandwidthBps/1e9)

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
