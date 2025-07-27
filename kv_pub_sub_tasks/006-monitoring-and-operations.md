# Task 006: Monitoring and Operations

## Overview
Implement comprehensive monitoring, alerting, and operational tools for the KV cache event synchronization system to ensure production readiness and maintainability.

## Background
The KV event sync system is critical for routing performance. We need visibility into its operation, early warning of issues, and tools for troubleshooting.

## Requirements

### Functional Requirements
1. Prometheus metrics for all components
2. Grafana dashboards for visualization
3. Alerts for critical issues
4. Logging with appropriate levels
5. Debugging and troubleshooting tools
6. Health checks and readiness probes

### Non-Functional Requirements
1. Minimal performance overhead (<1%)
2. Metrics retention for 30 days
3. Sub-minute alert latency
4. Structured logging (JSON)
5. Distributed tracing support

## Metrics Design

### ZMQ Client Metrics
```go
// pkg/cache/kvcache/metrics.go
package kvcache

import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

var (
    // Configuration metrics
    kvSyncConfigurationValid = promauto.NewGauge(
        prometheus.GaugeOpts{
            Name: "aibrix_kv_sync_configuration_valid",
            Help: "Whether KV sync configuration is valid (1=valid, 0=invalid)",
        },
    )
    
    // Connection metrics
    zmqConnectionsTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "aibrix_kv_zmq_connections_total",
            Help: "Total number of ZMQ connection attempts",
        },
        []string{"pod", "model", "status"},
    )
    
    zmqConnectionsActive = promauto.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "aibrix_kv_zmq_connections_active",
            Help: "Number of active ZMQ connections",
        },
        []string{"model"},
    )
    
    // Event metrics
    kvEventsReceived = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "aibrix_kv_events_received_total",
            Help: "Total number of KV events received",
        },
        []string{"pod", "model", "event_type"},
    )
    
    kvEventsProcessed = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "aibrix_kv_events_processed_total",
            Help: "Total number of KV events processed successfully",
        },
        []string{"pod", "model", "event_type"},
    )
    
    kvEventsDropped = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "aibrix_kv_events_dropped_total",
            Help: "Total number of KV events dropped due to errors",
        },
        []string{"pod", "model", "event_type", "reason"},
    )
    
    kvEventProcessingDuration = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "aibrix_kv_event_processing_duration_seconds",
            Help:    "Time taken to process KV events",
            Buckets: prometheus.ExponentialBuckets(0.0001, 2, 10), // 0.1ms to 100ms
        },
        []string{"model", "event_type"},
    )
    
    // Sequence tracking
    kvEventSequenceGap = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "aibrix_kv_event_sequence_gap",
            Help:    "Gap in event sequence numbers (missed events)",
            Buckets: prometheus.ExponentialBuckets(1, 2, 10), // 1 to 1024
        },
        []string{"pod", "model"},
    )
    
    // Replay metrics
    kvReplayRequests = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "aibrix_kv_replay_requests_total",
            Help: "Total number of replay requests",
        },
        []string{"pod", "model", "status"},
    )
)

// RecordConnectionAttempt records a connection attempt
func RecordConnectionAttempt(pod, model, status string) {
    zmqConnectionsTotal.WithLabelValues(pod, model, status).Inc()
}

// RecordActiveConnection updates active connection gauge
func RecordActiveConnection(model string, delta float64) {
    zmqConnectionsActive.WithLabelValues(model).Add(delta)
}

// RecordEventReceived records event reception
func RecordEventReceived(pod, model string, eventType EventType) {
    kvEventsReceived.WithLabelValues(pod, model, string(eventType)).Inc()
}

// RecordEventProcessed records successful event processing
func RecordEventProcessed(pod, model string, eventType EventType, duration float64) {
    kvEventsProcessed.WithLabelValues(pod, model, string(eventType)).Inc()
    kvEventProcessingDuration.WithLabelValues(model, string(eventType)).Observe(duration)
}

// RecordEventDropped records dropped events
func RecordEventDropped(pod, model string, eventType EventType, reason string) {
    kvEventsDropped.WithLabelValues(pod, model, string(eventType), reason).Inc()
}

// RecordSequenceGap records missed events
func RecordSequenceGap(pod, model string, gap int64) {
    if gap > 0 {
        kvEventSequenceGap.WithLabelValues(pod, model).Observe(float64(gap))
    }
}
```

### Sync Indexer Metrics
```go
// pkg/utils/syncprefixcacheindexer/metrics.go
package syncprefixcacheindexer

import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

var (
    // Capacity metrics
    syncIndexerContexts = promauto.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "aibrix_sync_indexer_contexts",
            Help: "Number of active contexts in sync indexer",
        },
        []string{"model"},
    )
    
    syncIndexerPrefixes = promauto.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "aibrix_sync_indexer_prefixes",
            Help: "Number of prefixes stored per context",
        },
        []string{"model", "lora_id"},
    )
    
    syncIndexerMappings = promauto.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "aibrix_sync_indexer_mappings",
            Help: "Number of engine to gateway hash mappings",
        },
        []string{"model", "lora_id"},
    )
    
    // Operation metrics
    syncIndexerOperations = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "aibrix_sync_indexer_operations_total",
            Help: "Total number of indexer operations",
        },
        []string{"model", "operation", "status"},
    )
    
    syncIndexerOperationDuration = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "aibrix_sync_indexer_operation_duration_seconds",
            Help:    "Duration of indexer operations",
            Buckets: prometheus.ExponentialBuckets(0.00001, 2, 10), // 10us to 10ms
        },
        []string{"operation"},
    )
    
    // Eviction metrics
    syncIndexerEvictions = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "aibrix_sync_indexer_evictions_total",
            Help: "Total number of evictions",
        },
        []string{"reason"},
    )
    
    // Match metrics
    syncIndexerMatches = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "aibrix_sync_indexer_match_percentage",
            Help:    "Percentage of prefix match",
            Buckets: prometheus.LinearBuckets(0, 10, 11), // 0%, 10%, ..., 100%
        },
        []string{"model"},
    )
)
```

### Gateway Routing Metrics (Updated)
```go
// pkg/plugins/gateway/algorithms/metrics.go
package routingalgorithms

var (
    // Routing decision metrics
    prefixCacheRoutingLatency = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "aibrix_prefix_cache_routing_latency_seconds",
            Help:    "Latency of prefix cache routing decisions",
            Buckets: prometheus.ExponentialBuckets(0.00001, 2, 10), // 10us to 10ms
        },
        []string{"model"},
    )
    
    prefixCacheHitRate = promauto.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "aibrix_prefix_cache_hit_rate",
            Help: "Prefix cache hit rate (rolling average)",
        },
        []string{"model"},
    )
    
    // Sync status
    prefixCacheSyncStatus = promauto.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "aibrix_prefix_cache_sync_status",
            Help: "Status of KV event sync (1=active, 0=inactive)",
        },
        []string{"model"},
    )
)
```

## Grafana Dashboards

### KV Event Sync Overview Dashboard
```json
{
  "dashboard": {
    "title": "AIBrix KV Event Sync Overview",
    "panels": [
      {
        "title": "Active ZMQ Connections",
        "targets": [
          {
            "expr": "sum(aibrix_kv_zmq_connections_active) by (model)"
          }
        ],
        "type": "graph"
      },
      {
        "title": "Event Processing Rate",
        "targets": [
          {
            "expr": "rate(aibrix_kv_events_processed_total[5m])"
          }
        ],
        "type": "graph"
      },
      {
        "title": "Event Processing Latency (p99)",
        "targets": [
          {
            "expr": "histogram_quantile(0.99, rate(aibrix_kv_event_processing_duration_seconds_bucket[5m]))"
          }
        ],
        "type": "graph"
      },
      {
        "title": "Dropped Events",
        "targets": [
          {
            "expr": "rate(aibrix_kv_events_dropped_total[5m])"
          }
        ],
        "type": "graph"
      },
      {
        "title": "Sequence Gaps (Missed Events)",
        "targets": [
          {
            "expr": "histogram_quantile(0.95, rate(aibrix_kv_event_sequence_gap_bucket[5m]))"
          }
        ],
        "type": "graph"
      },
      {
        "title": "Sync Indexer Capacity",
        "targets": [
          {
            "expr": "sum(aibrix_sync_indexer_contexts)"
          },
          {
            "expr": "sum(aibrix_sync_indexer_prefixes)"
          }
        ],
        "type": "graph"
      },
      {
        "title": "Prefix Cache Hit Rate",
        "targets": [
          {
            "expr": "aibrix_prefix_cache_hit_rate"
          }
        ],
        "type": "graph"
      },
      {
        "title": "Routing Decision Latency",
        "targets": [
          {
            "expr": "histogram_quantile(0.99, rate(aibrix_prefix_cache_routing_latency_seconds_bucket[5m]))"
          }
        ],
        "type": "graph"
      }
    ]
  }
}
```

## Alerting Rules

```yaml
# config/prometheus/kv-sync-alerts.yaml
groups:
  - name: kv_sync_alerts
    interval: 30s
    rules:
      # Configuration alerts
      - alert: KVSyncConfigurationError
        expr: aibrix_kv_sync_configuration_invalid == 1
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "KV sync configuration error"
          description: "KV sync requires remote tokenizer to be enabled. Please set AIBRIX_USE_REMOTE_TOKENIZER=true"
      
      # Connection alerts
      - alert: KVSyncConnectionFailure
        expr: rate(aibrix_kv_zmq_connections_total{status="failed"}[5m]) > 0.1
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "KV sync connection failures detected"
          description: "Pod {{ $labels.pod }} failing to connect to vLLM ({{ $value }} failures/sec)"
      
      - alert: KVSyncNoActiveConnections
        expr: aibrix_kv_zmq_connections_active == 0
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "No active KV sync connections for model"
          description: "Model {{ $labels.model }} has no active KV sync connections"
      
      # Event processing alerts
      - alert: KVEventDropRate
        expr: rate(aibrix_kv_events_dropped_total[5m]) > 10
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "High KV event drop rate"
          description: "Dropping {{ $value }} events/sec for {{ $labels.model }}"
      
      - alert: KVEventProcessingLatency
        expr: histogram_quantile(0.99, rate(aibrix_kv_event_processing_duration_seconds_bucket[5m])) > 0.01
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High KV event processing latency"
          description: "P99 latency {{ $value }}s for {{ $labels.model }}"
      
      # Sequence gap alerts
      - alert: KVEventSequenceGaps
        expr: histogram_quantile(0.95, rate(aibrix_kv_event_sequence_gap_bucket[5m])) > 10
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "Large sequence gaps in KV events"
          description: "Missing {{ $value }} events for {{ $labels.pod }}"
      
      # Capacity alerts
      - alert: SyncIndexerNearCapacity
        expr: aibrix_sync_indexer_contexts > 900
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Sync indexer approaching context limit"
          description: "{{ $value }} contexts active (limit: 1000)"
      
      - alert: SyncIndexerEvictionRate
        expr: rate(aibrix_sync_indexer_evictions_total[5m]) > 1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High sync indexer eviction rate"
          description: "Evicting {{ $value }} contexts/sec due to {{ $labels.reason }}"
      
      # Routing alerts
      - alert: PrefixCacheLowHitRate
        expr: aibrix_prefix_cache_hit_rate < 0.3
        for: 10m
        labels:
          severity: info
        annotations:
          summary: "Low prefix cache hit rate"
          description: "Hit rate {{ $value }} for {{ $labels.model }}"
```

## Logging Standards

### Structured Logging Implementation
```go
// pkg/cache/kvcache/logging.go
package kvcache

import (
    "k8s.io/klog/v2"
)

// LogEvent logs KV event with structured fields
func LogEvent(level klog.Level, event KVEvent, fields ...interface{}) {
    if !klog.V(level).Enabled() {
        return
    }
    
    baseFields := []interface{}{
        "event_type", event.GetType(),
        "timestamp", event.GetTimestamp(),
    }
    
    switch e := event.(type) {
    case *BlockStoredEvent:
        baseFields = append(baseFields,
            "block_count", len(e.BlockHashes),
            "model", e.ModelName,
            "pod", e.PodName,
        )
    case *BlockRemovedEvent:
        baseFields = append(baseFields,
            "block_count", len(e.BlockHashes),
            "model", e.ModelName,
            "pod", e.PodName,
        )
    }
    
    allFields := append(baseFields, fields...)
    klog.InfoS("KV event", allFields...)
}

// Log levels:
// V(1) - High-level operations (connections, disconnections)
// V(2) - Event summaries
// V(3) - Individual events
// V(4) - Detailed event data
// V(5) - Debug information
```

## Health Checks

### KV Sync Health Endpoint
```go
// pkg/cache/health.go
package cache

import (
    "encoding/json"
    "net/http"
    "time"
)

type KVSyncHealth struct {
    Status      string            `json:"status"`
    Models      map[string]ModelHealth `json:"models"`
    LastUpdated time.Time         `json:"last_updated"`
}

type ModelHealth struct {
    ActiveConnections int       `json:"active_connections"`
    EventRate         float64   `json:"event_rate"`
    LastEvent         time.Time `json:"last_event"`
    Errors            int       `json:"errors"`
}

// HandleKVSyncHealth handles health check requests
func (s *Store) HandleKVSyncHealth(w http.ResponseWriter, r *http.Request) {
    health := s.getKVSyncHealth()
    
    // Determine overall status
    status := "healthy"
    for _, model := range health.Models {
        if model.ActiveConnections == 0 {
            status = "degraded"
        }
        if time.Since(model.LastEvent) > 5*time.Minute {
            status = "unhealthy"
        }
    }
    health.Status = status
    
    // Set appropriate HTTP status
    httpStatus := http.StatusOK
    if status == "unhealthy" {
        httpStatus = http.StatusServiceUnavailable
    }
    
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(httpStatus)
    json.NewEncoder(w).Encode(health)
}
```

## Debugging Tools

### KV Event Inspector
```bash
#!/bin/bash
# tools/kv-event-inspector.sh

POD=$1
if [ -z "$POD" ]; then
    echo "Usage: $0 <pod-name>"
    exit 1
fi

echo "Inspecting KV events for pod: $POD"

# Get pod IP
POD_IP=$(kubectl get pod $POD -o jsonpath='{.status.podIP}')
echo "Pod IP: $POD_IP"

# Create inspector pod
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: kv-event-inspector
  namespace: default
spec:
  containers:
  - name: inspector
    image: aibrix/debug-tools:latest
    command: ["sleep", "3600"]
EOF

# Wait for pod
kubectl wait --for=condition=ready pod/kv-event-inspector

# Run ZMQ subscriber
kubectl exec -it kv-event-inspector -- python3 - <<'PYTHON'
import zmq
import msgpack
import sys
from datetime import datetime

pod_ip = "$POD_IP"
context = zmq.Context()
socket = context.socket(zmq.SUB)
socket.connect(f"tcp://{pod_ip}:5557")
socket.subscribe(b"")

print(f"Connected to {pod_ip}:5557, listening for events...")

while True:
    try:
        parts = socket.recv_multipart()
        if len(parts) >= 3:
            topic, seq, payload = parts[0], parts[1], parts[2]
            
            # Decode sequence
            seq_num = int.from_bytes(seq, 'big')
            
            # Decode payload
            data = msgpack.unpackb(payload, raw=False)
            
            print(f"\n[{datetime.now()}] Seq: {seq_num}")
            print(f"Events: {len(data.get('events', []))}")
            
            for event in data.get('events', []):
                print(f"  Type: {event.get('type')}")
                if event.get('type') == 'BLOCK_STORED':
                    print(f"  Blocks: {len(event.get('block_hashes', []))}")
                    print(f"  First block: {event.get('block_hashes', [None])[0]}")
                
    except KeyboardInterrupt:
        break
    except Exception as e:
        print(f"Error: {e}")

socket.close()
context.term()
PYTHON

# Cleanup
kubectl delete pod kv-event-inspector
```

### Sync Indexer Dump Tool
```go
// cmd/debug/sync-indexer-dump.go
package main

import (
    "encoding/json"
    "fmt"
    "os"
    
    "github.com/vllm-project/aibrix/pkg/cache"
)

func main() {
    if len(os.Args) < 2 {
        fmt.Println("Usage: sync-indexer-dump <model-name>")
        os.Exit(1)
    }
    
    modelName := os.Args[1]
    
    // Get cache instance
    c, err := cache.Get()
    if err != nil {
        fmt.Printf("Error getting cache: %v\n", err)
        os.Exit(1)
    }
    
    indexer := c.GetSyncPrefixIndexer()
    if indexer == nil {
        fmt.Println("Sync indexer not available")
        os.Exit(1)
    }
    
    // Dump model data
    stats := indexer.GetModelStats(modelName)
    
    data, _ := json.MarshalIndent(stats, "", "  ")
    fmt.Println(string(data))
}
```

## Operational Runbooks

### Runbook: High Event Drop Rate
```markdown
# Runbook: High KV Event Drop Rate

## Alert
`KVEventDropRate` firing

## Impact
- Reduced prefix cache accuracy
- Suboptimal routing decisions
- Potential performance degradation

## Diagnosis
1. Check gateway logs:
   ```bash
   kubectl logs -n aibrix-system deployment/aibrix-gateway-plugins | grep "dropped"
   ```

2. Check metrics:
   ```bash
   curl http://gateway-metrics:8080/metrics | grep dropped
   ```

3. Identify drop reasons:
   ```promql
   topk(5, rate(aibrix_kv_events_dropped_total[5m])) by (reason)
   ```

## Common Causes and Fixes

### 1. Processing Timeout
- **Symptom**: `reason="timeout"`
- **Fix**: Increase processing workers or timeout
  ```bash
  kubectl set env deployment/aibrix-gateway-plugins \
    AIBRIX_KV_EVENT_WORKERS=10
  ```

### 2. Memory Pressure
- **Symptom**: `reason="memory_limit"`
- **Fix**: Increase memory limits or adjust eviction
  ```yaml
  resources:
    limits:
      memory: 2Gi
  ```

### 3. Network Issues
- **Symptom**: `reason="network_error"`
- **Fix**: Check network policies and connectivity
  ```bash
  kubectl exec -it deployment/aibrix-gateway-plugins -- \
    nc -zv <pod-ip> 5557
  ```

## Escalation
If issue persists after 30 minutes, escalate to:
- On-call SRE
- Platform team
```

## Performance Monitoring

### Benchmark Suite
```go
// test/perf/kv_sync_perf_test.go
package perf

import (
    "testing"
    "time"
)

func BenchmarkKVSyncEndToEnd(b *testing.B) {
    // Setup test environment
    env := setupPerfTestEnv(b)
    defer env.Cleanup()
    
    b.ResetTimer()
    
    for i := 0; i < b.N; i++ {
        // Simulate vLLM event
        event := generateTestEvent(i)
        env.PublishEvent(event)
        
        // Make routing request
        start := time.Now()
        endpoint := env.RouteRequest("test prompt")
        routingTime := time.Since(start)
        
        // Verify prefix cache was used
        if !env.VerifyPrefixCacheHit(endpoint) {
            b.Error("Expected prefix cache hit")
        }
        
        b.ReportMetric(float64(routingTime.Microseconds()), "routing_us")
    }
}
```

## Success Criteria

1. All metrics exposed and collected
2. Dashboards provide clear visibility
3. Alerts fire appropriately
4. Runbooks cover common scenarios
5. Performance overhead <1%

## Dependencies

- Prometheus and Grafana deployed
- Tasks 001-005 completed
- Access to metric storage

## Timeline

1. **Metrics Implementation** (Day 1)
   - Add metrics to all components
   - Test metric collection

2. **Dashboards** (Day 2)
   - Create Grafana dashboards
   - Test visualizations

3. **Alerting** (Day 3)
   - Configure alert rules
   - Test alert firing

4. **Tools and Runbooks** (Day 4-5)
   - Implement debugging tools
   - Write operational runbooks
   - Performance monitoring setup