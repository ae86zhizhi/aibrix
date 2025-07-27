# Task 003: Routing Algorithm Integration with Sync Hash Indexer

## Overview
Integrate the appropriate prefix cache indexer with the routing algorithm based on configuration. When KV sync is disabled, use the original `prefixcacheindexer.PrefixHashTable` for full backward compatibility. When KV sync is enabled, use the new `syncprefixcacheindexer.SyncPrefixHashTable` that receives real-time updates from vLLM.

## Background
The current prefix cache router uses a local prefix hash table (`prefixcacheindexer.PrefixHashTable`). When KV sync is enabled, we need to switch to using the sync hash indexer (`syncprefixcacheindexer.SyncPrefixHashTable`) which is populated by real-time KV events from vLLM pods. When disabled, the system must continue using the original indexer for backward compatibility.

## Requirements

### Functional Requirements
1. Use the cache's sync hash indexer (always available)
2. Maintain backward compatibility with existing routing interface
3. Support both event-driven and manual prefix addition (based on configuration)
4. Ensure identical behavior when KV sync is disabled
5. Provide metrics for routing decisions

### Non-Functional Requirements
1. No performance degradation in routing decisions
2. Thread-safe access to shared indexer
3. Minimal memory overhead
4. Observable routing behavior

## Technical Specification

### Architecture Changes

```
Current Flow:
Request → Tokenizer → Local Hash Table → Route Decision

New Flow (KV Sync Disabled):
Request → Tokenizer → Sync Hash Indexer (local mode) → Route Decision

New Flow (KV Sync Enabled):
Request → Tokenizer → Sync Hash Indexer → Route Decision
                           ↑
                      KV Events from vLLM
```

The sync hash indexer replaces the local hash table but maintains identical behavior when KV sync is disabled.

### File Modifications

```
pkg/plugins/gateway/algorithms/
├── prefix_cache.go          // Modify to use sync indexer
├── prefix_cache_test.go     // Update tests
└── router.go               // Update interface if needed
```

### Core Implementation Changes

```go
// pkg/plugins/gateway/algorithms/prefix_cache.go

package routingalgorithms

import (
    "math"
    "math/rand"
    "sort"

    "github.com/vllm-project/aibrix/pkg/cache"
    "github.com/vllm-project/aibrix/pkg/types"
    "github.com/vllm-project/aibrix/pkg/utils"
    "github.com/vllm-project/aibrix/pkg/utils/prefixcacheindexer"      // Original indexer
    syncindexer "github.com/vllm-project/aibrix/pkg/utils/syncprefixcacheindexer"  // KV sync indexer
    "github.com/vllm-project/aibrix/pkg/utils/tokenizer"
    v1 "k8s.io/api/core/v1"
    "k8s.io/klog/v2"
)

type prefixCacheRouter struct {
    cache              cache.Cache
    tokenizer          tokenizer.Tokenizer
    
    // Indexer selection based on configuration
    prefixCacheIndexer *prefixcacheindexer.PrefixHashTable  // Original indexer
    syncIndexer        *syncindexer.SyncPrefixHashTable     // Optional KV sync indexer
    useKVSync          bool  // Flag to determine which indexer to use
    
    // Configuration for fallback behavior
    fallbackEnabled    bool
    remoteTokenizer    tokenizer.Tokenizer // For consistent tokenization
}

func NewPrefixCacheRouter() (types.Router, error) {
    // Tokenizer selection logic with dependency awareness
    var tokenizerObj tokenizer.Tokenizer
    var remoteTokenizerObj tokenizer.Tokenizer
    
    // Check configuration dependencies
    useRemoteTokenizer := utils.LoadEnvBool("AIBRIX_USE_REMOTE_TOKENIZER", false)
    kvSyncEnabled := utils.LoadEnvBool("AIBRIX_KV_EVENT_SYNC_ENABLED", false)
    
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
            Engine:   utils.LoadEnv("AIBRIX_REMOTE_TOKENIZER_ENGINE", "vllm"),
            Endpoint: utils.LoadEnv("AIBRIX_REMOTE_TOKENIZER_ENDPOINT", ""),
            Model:    utils.LoadEnv("AIBRIX_REMOTE_TOKENIZER_MODEL", ""),
            Timeout:  30 * time.Second,
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

    // Create the appropriate indexer based on configuration
    router := prefixCacheRouter{
        cache:           c,
        tokenizer:       tokenizerObj,
        remoteTokenizer: remoteTokenizerObj,
        fallbackEnabled: utils.LoadEnvBool("AIBRIX_PREFIX_CACHE_FALLBACK_ENABLED", true),
        useKVSync:       kvSyncEnabled && useRemoteTokenizer,
    }
    
    if router.useKVSync {
        // Use sync indexer from cache when KV sync is enabled
        router.syncIndexer = c.GetSyncPrefixIndexer()
        if router.syncIndexer == nil {
            klog.Warning("Sync indexer not available, falling back to local indexer")
            router.useKVSync = false
            router.prefixCacheIndexer = prefixcacheindexer.NewPrefixHashTable()
        }
    } else {
        // Use original local indexer for backward compatibility
        router.prefixCacheIndexer = prefixcacheindexer.NewPrefixHashTable()
    }

    return router, nil
}

func (p prefixCacheRouter) Route(ctx *types.RoutingContext, readyPodList types.PodList) (string, error) {
    var prefixHashes []uint64
    var matchedPods map[string]int
    var targetPod *v1.Pod

    // Get model information from context
    modelName := ctx.Model
    if modelName == "" {
        // Extract from first ready pod if not in context
        if len(readyPodList) > 0 {
            modelName = readyPodList[0].Labels["model.aibrix.ai/name"]
        }
    }
    
    // Get LoRA ID if present
    loraID := int64(-1)
    if ctx.LoraAdapter != "" {
        // Parse LoRA ID from adapter name or context
        // This is simplified - actual implementation may vary
    }

    // Tokenize input
    tokens, err := p.tokenizer.TokenizeInputText(ctx.Message)
    if err != nil {
        return "", err
    }

    // Route based on indexer type
    if p.useKVSync {
        // Use sync indexer with KV event support
        if p.syncIndexer == nil {
            klog.Error("Sync indexer not available")
            return p.fallbackRoute(readyPodList)
        }
        
        // Convert ready pods to map for efficient lookup
        readyPodsMap := make(map[string]struct{})
        for _, pod := range readyPodList {
            podKey := utils.GetPodKey(pod.Namespace, pod.Name)
            readyPodsMap[podKey] = struct{}{}
        }

        // Match prefixes using sync indexer
        matchedPods, prefixHashes = p.syncIndexer.MatchPrefix(modelName, loraID, tokens, readyPodsMap)

        // If using remote tokenizer, also add prefix to indexer
        // This ensures new prompts are indexed even before vLLM processes them
        if p.remoteTokenizer != nil && len(prefixHashes) > 0 {
            // Add prefix for the selected pod after routing decision
            defer func() {
                if targetPod != nil {
                    podKey := utils.GetPodKey(targetPod.Namespace, targetPod.Name)
                    p.syncIndexer.AddPrefix(modelName, loraID, podKey, prefixHashes)
                }
            }()
        }
    } else {
        // Use original local indexer for backward compatibility
        prefixHashes = p.prefixCacheIndexer.GetPrefixHashes(tokens)
        matchedPods = p.prefixCacheIndexer.MatchPrefix(prefixHashes, func(podList utils.PodArray) utils.PodArray {
            // Filter to only ready pods
            filtered := make(utils.PodArray, 0)
            for _, podName := range podList {
                for _, readyPod := range readyPodList {
                    if utils.GetPodKey(readyPod.Namespace, readyPod.Name) == podName {
                        filtered = append(filtered, podName)
                        break
                    }
                }
            }
            return filtered
        })
    }

    klog.V(4).InfoS("prefix cache matching completed",
        "model", modelName,
        "lora_id", loraID,
        "matched_pods", len(matchedPods),
        "prefix_hashes", len(prefixHashes),
        "ready_pods", len(readyPodList))

    // Rest of the routing logic remains the same...
    
    if len(matchedPods) == 0 {
        klog.V(4).Info("no prefix match, using least loaded pod")
        targetPod = p.selectLeastLoadedPod(readyPodList)
    } else {
        // Find pods with maximum prefix match
        maxMatch := 0
        for _, match := range matchedPods {
            if match > maxMatch {
                maxMatch = match
            }
        }

        // Get all pods with max match percentage
        var maxMatchPods []string
        for pod, match := range matchedPods {
            if match == maxMatch {
                maxMatchPods = append(maxMatchPods, pod)
            }
        }

        klog.V(4).InfoS("found pods with max prefix match",
            "max_match_percentage", maxMatch,
            "num_pods", len(maxMatchPods))

        // Select from max match pods based on load
        targetPod = p.selectFromMatchedPods(readyPodList, maxMatchPods, maxMatch)
    }

    if targetPod == nil {
        return "", ErrNoPodAvailable
    }

    selectedPodKey := utils.GetPodKey(targetPod.Namespace, targetPod.Name)
    
    // Update indexer after routing decision (for local indexer)
    if !p.useKVSync && len(prefixHashes) > 0 {
        // Add prefix to local indexer
        p.prefixCacheIndexer.AddPrefix(prefixHashes, utils.PodArray{selectedPodKey})
    }
    
    // Log routing decision with metrics
    if klog.V(3).Enabled() {
        matchPercent := 0
        if p.useKVSync {
            if match, exists := matchedPods[selectedPodKey]; exists {
                matchPercent = match
            }
        } else {
            // For local indexer, calculate match percentage differently
            if len(matchedPods) > 0 && matchedPods[selectedPodKey] > 0 {
                matchPercent = matchedPods[selectedPodKey]
            }
        }
        
        klog.InfoS("prefix cache routing decision",
            "selected_pod", selectedPodKey,
            "prefix_match_percent", matchPercent,
            "running_requests", p.getRunningRequests(targetPod),
            "model", modelName,
            "using_kv_sync", p.useKVSync)
    }

    return utils.GetModelPodEndpoint(targetPod), nil
}

// fallbackRoute provides fallback routing when indexer is unavailable
func (p prefixCacheRouter) fallbackRoute(readyPodList types.PodList) (string, error) {
    if !p.fallbackEnabled || len(readyPodList) == 0 {
        return "", ErrNoPodAvailable
    }
    
    // Use simple random selection as fallback
    selected := readyPodList[rand.Intn(len(readyPodList))]
    return utils.GetModelPodEndpoint(selected), nil
}

// selectLeastLoadedPod selects the pod with least running requests
func (p prefixCacheRouter) selectLeastLoadedPod(readyPodList types.PodList) *v1.Pod {
    if len(readyPodList) == 0 {
        return nil
    }

    // Sort by running requests
    sort.Slice(readyPodList, func(i, j int) bool {
        reqI := p.getRunningRequests(readyPodList[i])
        reqJ := p.getRunningRequests(readyPodList[j])
        return reqI < reqJ
    })

    return readyPodList[0]
}

// selectFromMatchedPods selects a pod from matched pods based on load
func (p prefixCacheRouter) selectFromMatchedPods(
    readyPodList types.PodList,
    maxMatchPods []string,
    maxMatchPercent int,
) *v1.Pod {
    // Filter ready pods that are in maxMatchPods
    var candidatePods []*v1.Pod
    for _, pod := range readyPodList {
        podKey := utils.GetPodKey(pod.Namespace, pod.Name)
        for _, matchPod := range maxMatchPods {
            if podKey == matchPod {
                candidatePods = append(candidatePods, pod)
                break
            }
        }
    }

    if len(candidatePods) == 0 {
        return nil
    }

    // For high prefix match (>50%), use least loaded
    if maxMatchPercent > 50 {
        return p.selectLeastLoadedPod(candidatePods)
    }

    // For lower match, use load balancing with some randomness
    return p.selectWithLoadBalancing(candidatePods)
}

// getRunningRequests gets the number of running requests for a pod
func (p prefixCacheRouter) getRunningRequests(pod *v1.Pod) int {
    podKey := utils.GetPodKey(pod.Namespace, pod.Name)
    if podData, exists := p.cache.GetPod(podKey); exists {
        return int(podData.Metrics.RunningRequests.GetValue())
    }
    return 0
}

// selectWithLoadBalancing selects a pod considering load distribution
func (p prefixCacheRouter) selectWithLoadBalancing(pods []*v1.Pod) *v1.Pod {
    if len(pods) == 0 {
        return nil
    }

    // Calculate average load
    totalRequests := 0
    for _, pod := range pods {
        totalRequests += p.getRunningRequests(pod)
    }
    avgRequests := float64(totalRequests) / float64(len(pods))

    // Find pods below average load
    var underloadedPods []*v1.Pod
    for _, pod := range pods {
        if float64(p.getRunningRequests(pod)) <= avgRequests*1.1 { // 10% tolerance
            underloadedPods = append(underloadedPods, pod)
        }
    }

    if len(underloadedPods) > 0 {
        // Random selection from underloaded pods
        return underloadedPods[rand.Intn(len(underloadedPods))]
    }

    // Fallback to least loaded
    return p.selectLeastLoadedPod(pods)
}
```

### Metrics and Observability

```go
// Add metrics for routing decisions
var (
    prefixCacheRoutingDecisions = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "aibrix_prefix_cache_routing_decisions_total",
            Help: "Total number of routing decisions by match percentage",
        },
        []string{"model", "match_percent_bucket"},
    )
    
    prefixCacheIndexerStatus = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "aibrix_prefix_cache_indexer_status",
            Help: "Status of prefix cache indexer (1=available, 0=unavailable)",
        },
        []string{"model"},
    )
)

// Register metrics in init()
func init() {
    prometheus.MustRegister(prefixCacheRoutingDecisions)
    prometheus.MustRegister(prefixCacheIndexerStatus)
}

// Update metrics in routing logic
func recordRoutingDecision(model string, matchPercent int) {
    bucket := "0"
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
    
    prefixCacheRoutingDecisions.WithLabelValues(model, bucket).Inc()
}
```

### Configuration Updates

```yaml
# Gateway plugin deployment
env:
  # Feature control - order matters due to dependencies
  # Remote tokenizer is required for KV sync
  - name: AIBRIX_USE_REMOTE_TOKENIZER
    value: "true"  # Must be true if using KV sync
  - name: AIBRIX_KV_EVENT_SYNC_ENABLED
    value: "true"  # Requires remote tokenizer
  
  # Remote tokenizer configuration
  - name: AIBRIX_PREFIX_CACHE_TOKENIZER_TYPE
    value: "remote"  # Must be "remote" when using KV sync
  - name: AIBRIX_REMOTE_TOKENIZER_ENGINE
    value: "vllm"
  - name: AIBRIX_REMOTE_TOKENIZER_ENDPOINT
    value: "http://vllm-service:8000"
  - name: AIBRIX_REMOTE_TOKENIZER_TIMEOUT
    value: "30s"
  - name: AIBRIX_REMOTE_TOKENIZER_MAX_RETRIES
    value: "3"
  
  # Fallback behavior
  - name: AIBRIX_PREFIX_CACHE_FALLBACK_ENABLED
    value: "true"
  
  # Local tokenizer configuration (used only if remote disabled)
  - name: AIBRIX_LOCAL_TOKENIZER_TYPE
    value: "character"  # Options: "character", "tiktoken"
```

### Dependency Rules

The router enforces the following configuration dependencies:

1. **KV Sync → Remote Tokenizer**
   - If `AIBRIX_KV_EVENT_SYNC_ENABLED=true`, remote tokenizer is automatically enabled
   - This ensures tokenization consistency between gateway and vLLM

2. **Remote Tokenizer Configuration**
   - When enabled, requires valid endpoint configuration
   - Falls back to local tokenizer only if KV sync is disabled

3. **Graceful Degradation**
   - If remote tokenizer fails and KV sync is disabled: use local tokenizer
   - If remote tokenizer fails and KV sync is enabled: router initialization fails

## Testing Plan

### Unit Tests
1. Test routing with sync indexer
2. Test fallback behavior
3. Test metric recording
4. Test load balancing logic
5. Test with various match percentages

### Integration Tests
1. Test with mock sync indexer
2. Test with real KV events
3. Test indexer unavailability
4. Test performance under load

### Load Tests
1. Benchmark routing decision time
2. Test with 1000+ pods
3. Test with high request rate
4. Memory usage analysis

## Implementation Steps

1. **Refactor Router** (Day 1)
   - Remove local prefix hash table
   - Add sync indexer integration
   - Add remote tokenizer support

2. **Add Metrics** (Day 2)
   - Implement routing metrics
   - Add indexer status monitoring
   - Create Grafana dashboards

3. **Testing** (Day 3-4)
   - Update existing tests
   - Add new integration tests
   - Performance benchmarking

4. **Documentation** (Day 5)
   - Update routing documentation
   - Add configuration guide
   - Create troubleshooting guide

## Success Criteria

1. Routing decisions use real-time KV cache state
2. No performance regression (<1ms routing time)
3. Graceful fallback when indexer unavailable
4. Complete metrics coverage
5. All existing tests pass

## Dependencies

- Task 002: Cache system integration (provides sync indexer)
- Can start refactoring in parallel but need Task 002 for testing

## Risks and Mitigations

1. **Performance Impact**
   - Risk: Sync indexer lookup may be slower
   - Mitigation: Optimize indexer queries, add caching layer if needed

2. **Indexer Unavailability**
   - Risk: System fails if indexer not ready
   - Mitigation: Implement graceful fallback

3. **Tokenization Consistency**
   - Risk: Local vs remote tokenizer differences
   - Mitigation: Use remote tokenizer when available

4. **Configuration Complexity**
   - Risk: Too many configuration options
   - Mitigation: Sensible defaults, clear documentation