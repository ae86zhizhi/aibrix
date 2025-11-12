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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Helper functions to create test pods

func createPodWithRole(name, namespace, role, ip string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				RoleLabelKey: role,
			},
		},
		Status: v1.PodStatus{
			PodIP: ip,
		},
	}
}

func createPodWithoutRole(name, namespace, ip string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{},
		},
		Status: v1.PodStatus{
			PodIP: ip,
		},
	}
}

func createPodWithCustomPort(name, namespace, role, ip, port string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				RoleLabelKey: role,
			},
			Annotations: map[string]string{
				MetricPortAnnotation: port,
			},
		},
		Status: v1.PodStatus{
			PodIP: ip,
		},
	}
}

func createPodWithModel(name, namespace, role, ip, model string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				RoleLabelKey:  role,
				ModelLabelKey: model,
			},
		},
		Status: v1.PodStatus{
			PodIP: ip,
		},
	}
}

// TestSeparatePrefillDecodePods tests the P/D pod separation logic
func TestSeparatePrefillDecodePods(t *testing.T) {
	router := &kvAwareRouter{}

	t.Run("valid separation", func(t *testing.T) {
		pods := []*v1.Pod{
			createPodWithRole("prefill-1", "default", RolePrefill, "10.0.0.1"),
			createPodWithRole("prefill-2", "default", RolePrefill, "10.0.0.2"),
			createPodWithRole("decode-1", "default", RoleDecode, "10.0.0.3"),
			createPodWithRole("decode-2", "default", RoleDecode, "10.0.0.4"),
		}

		prefillPods, decodePods, err := router.separatePrefillDecodePods(pods)

		require.NoError(t, err)
		assert.Len(t, prefillPods, 2)
		assert.Len(t, decodePods, 2)
	})

	t.Run("no role labels", func(t *testing.T) {
		pods := []*v1.Pod{
			createPodWithoutRole("pod-1", "default", "10.0.0.1"),
			createPodWithoutRole("pod-2", "default", "10.0.0.2"),
		}

		prefillPods, decodePods, err := router.separatePrefillDecodePods(pods)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no pods with valid P/D role labels")
		assert.Nil(t, prefillPods)
		assert.Nil(t, decodePods)
	})

	t.Run("mixed pods", func(t *testing.T) {
		pods := []*v1.Pod{
			createPodWithRole("prefill-1", "default", RolePrefill, "10.0.0.1"),
			createPodWithoutRole("pod-2", "default", "10.0.0.2"),
			createPodWithRole("decode-1", "default", RoleDecode, "10.0.0.3"),
		}

		prefillPods, decodePods, err := router.separatePrefillDecodePods(pods)

		require.NoError(t, err)
		assert.Len(t, prefillPods, 1)
		assert.Len(t, decodePods, 1)
	})

	t.Run("unknown role value", func(t *testing.T) {
		pods := []*v1.Pod{
			createPodWithRole("prefill-1", "default", RolePrefill, "10.0.0.1"),
			createPodWithRole("unknown-1", "default", "unknown-role", "10.0.0.2"),
			createPodWithRole("decode-1", "default", RoleDecode, "10.0.0.3"),
		}

		prefillPods, decodePods, err := router.separatePrefillDecodePods(pods)

		require.NoError(t, err)
		assert.Len(t, prefillPods, 1)
		assert.Len(t, decodePods, 1)
	})

	t.Run("only prefill pods", func(t *testing.T) {
		pods := []*v1.Pod{
			createPodWithRole("prefill-1", "default", RolePrefill, "10.0.0.1"),
			createPodWithRole("prefill-2", "default", RolePrefill, "10.0.0.2"),
		}

		prefillPods, decodePods, err := router.separatePrefillDecodePods(pods)

		require.NoError(t, err)
		assert.Len(t, prefillPods, 2)
		assert.Len(t, decodePods, 0)
	})

	t.Run("only decode pods", func(t *testing.T) {
		pods := []*v1.Pod{
			createPodWithRole("decode-1", "default", RoleDecode, "10.0.0.1"),
			createPodWithRole("decode-2", "default", RoleDecode, "10.0.0.2"),
		}

		prefillPods, decodePods, err := router.separatePrefillDecodePods(pods)

		require.NoError(t, err)
		assert.Len(t, prefillPods, 0)
		assert.Len(t, decodePods, 2)
	})

	t.Run("empty pod list", func(t *testing.T) {
		pods := []*v1.Pod{}

		prefillPods, decodePods, err := router.separatePrefillDecodePods(pods)

		require.Error(t, err)
		assert.Nil(t, prefillPods)
		assert.Nil(t, decodePods)
	})

	t.Run("nil labels", func(t *testing.T) {
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-1",
				Namespace: "default",
				Labels:    nil,
			},
			Status: v1.PodStatus{
				PodIP: "10.0.0.1",
			},
		}
		pods := []*v1.Pod{pod}

		prefillPods, decodePods, err := router.separatePrefillDecodePods(pods)

		require.Error(t, err)
		assert.Nil(t, prefillPods)
		assert.Nil(t, decodePods)
	})
}

// TestGetPodAddress tests pod address generation
func TestGetPodAddress(t *testing.T) {
	router := &kvAwareRouter{}

	t.Run("default port", func(t *testing.T) {
		pod := createPodWithRole("pod-1", "default", RolePrefill, "10.0.0.1")
		address := router.getPodAddress(pod)
		assert.Equal(t, "10.0.0.1:8080", address)
	})

	t.Run("custom port annotation", func(t *testing.T) {
		pod := createPodWithCustomPort("pod-1", "default", RolePrefill, "10.0.0.1", "9090")
		address := router.getPodAddress(pod)
		assert.Equal(t, "10.0.0.1:9090", address)
	})

	t.Run("nil annotations", func(t *testing.T) {
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "pod-1",
				Namespace:   "default",
				Annotations: nil,
			},
			Status: v1.PodStatus{
				PodIP: "10.0.0.2",
			},
		}
		address := router.getPodAddress(pod)
		assert.Equal(t, "10.0.0.2:8080", address)
	})

	t.Run("IPv6 address", func(t *testing.T) {
		pod := createPodWithRole("pod-1", "default", RolePrefill, "2001:db8::1")
		address := router.getPodAddress(pod)
		assert.Equal(t, "2001:db8::1:8080", address)
	})
}

// TestConvertPodsToPodRefs tests pod conversion
func TestConvertPodsToPodRefs(t *testing.T) {
	router := &kvAwareRouter{}

	t.Run("with model labels", func(t *testing.T) {
		pods := []*v1.Pod{
			createPodWithModel("pod-1", "default", RolePrefill, "10.0.0.1", "llama3-70b"),
			createPodWithModel("pod-2", "default", RolePrefill, "10.0.0.2", "llama3-8b"),
		}

		refs := router.convertPodsToPodRefs(pods, RolePrefill)

		require.Len(t, refs, 2)
		assert.Equal(t, "pod-1", refs[0].Name)
		assert.Equal(t, "default", refs[0].Namespace)
		assert.Equal(t, "10.0.0.1:8080", refs[0].IPPort)
		assert.Equal(t, RolePrefill, refs[0].Role)
		assert.Equal(t, "llama3-70b", refs[0].ModelName)

		assert.Equal(t, "pod-2", refs[1].Name)
		assert.Equal(t, "llama3-8b", refs[1].ModelName)
	})

	t.Run("without model labels", func(t *testing.T) {
		pods := []*v1.Pod{
			createPodWithRole("pod-1", "default", RoleDecode, "10.0.0.1"),
		}

		refs := router.convertPodsToPodRefs(pods, RoleDecode)

		require.Len(t, refs, 1)
		assert.Equal(t, "pod-1", refs[0].Name)
		assert.Equal(t, RoleDecode, refs[0].Role)
		assert.Equal(t, "", refs[0].ModelName)
	})

	t.Run("empty pod list", func(t *testing.T) {
		pods := []*v1.Pod{}
		refs := router.convertPodsToPodRefs(pods, RolePrefill)
		assert.Empty(t, refs)
	})

	t.Run("custom port", func(t *testing.T) {
		pods := []*v1.Pod{
			createPodWithCustomPort("pod-1", "default", RolePrefill, "10.0.0.1", "9090"),
		}

		refs := router.convertPodsToPodRefs(pods, RolePrefill)

		require.Len(t, refs, 1)
		assert.Equal(t, "10.0.0.1:9090", refs[0].IPPort)
	})

	t.Run("pod ref key", func(t *testing.T) {
		pods := []*v1.Pod{
			createPodWithRole("pod-1", "test-namespace", RolePrefill, "10.0.0.1"),
		}

		refs := router.convertPodsToPodRefs(pods, RolePrefill)

		require.Len(t, refs, 1)
		assert.Equal(t, "test-namespace/pod-1", refs[0].Key())
	})
}

// TestSubscribedMetrics tests the metrics subscription
func TestSubscribedMetrics(t *testing.T) {
	router := &kvAwareRouter{}
	metrics := router.SubscribedMetrics()

	// Verify we have the expected number of metrics
	assert.GreaterOrEqual(t, len(metrics), 12, "Should subscribe to at least 12 metrics")

	// Check for key metrics
	expectedMetrics := []string{
		"num_requests_waiting",
		"num_requests_running",
		"gpu_cache_usage_perc",
		"cpu_cache_usage_perc",
		"avg_prompt_throughput_toks_per_s",
		"avg_generation_throughput_toks_per_s",
		"time_to_first_token_seconds",
		"time_per_output_token_seconds",
		"request_queue_time_seconds",
		"request_prefill_time_seconds",
		"p95_tpot_5m_pod",
		"avg_tpot_pod_5m",
	}

	for _, expected := range expectedMetrics {
		assert.Contains(t, metrics, expected, "Should contain metric: %s", expected)
	}

	// Check for no duplicates
	uniqueMetrics := make(map[string]bool)
	for _, metric := range metrics {
		assert.False(t, uniqueMetrics[metric], "Duplicate metric found: %s", metric)
		uniqueMetrics[metric] = true
	}
}

// TestErrorTypes tests the error definitions
func TestErrorTypes(t *testing.T) {
	t.Run("predefined errors", func(t *testing.T) {
		assert.NotNil(t, ErrNoDecodeCandidate)
		assert.NotNil(t, ErrNoPrefillCandidate)
		assert.NotNil(t, ErrTTFTSLOViolation)
		assert.NotNil(t, ErrTBTSLOViolation)
		assert.NotNil(t, ErrPrefixIndexerUnavailable)
		assert.NotNil(t, ErrMetricsUnavailable)
		assert.NotNil(t, ErrNoPrefillPods)
		assert.NotNil(t, ErrNoDecodePods)
	})

	t.Run("rejection error", func(t *testing.T) {
		err := NewRejectionError(429, "Too many requests", "TTFT SLO violation", 5000)

		assert.Equal(t, 429, err.Code)
		assert.Equal(t, "Too many requests", err.Message)
		assert.Equal(t, "TTFT SLO violation", err.Reason)
		assert.Contains(t, err.Error(), "Too many requests")
		assert.Contains(t, err.Error(), "TTFT SLO violation")
	})

	t.Run("rejection error without reason", func(t *testing.T) {
		err := NewRejectionError(503, "Service unavailable", "", 0)

		assert.Equal(t, "Service unavailable", err.Error())
	})
}

// TestConstants tests the constant definitions
func TestConstants(t *testing.T) {
	t.Run("router name", func(t *testing.T) {
		assert.Equal(t, "kv-aware", string(RouterKVAware))
	})

	t.Run("role labels", func(t *testing.T) {
		assert.Equal(t, "aibrix.ai/role", RoleLabelKey)
		assert.Equal(t, "prefill", RolePrefill)
		assert.Equal(t, "decode", RoleDecode)
	})

	t.Run("annotations", func(t *testing.T) {
		assert.Equal(t, "model.aibrix.ai/metric-port", MetricPortAnnotation)
		assert.Equal(t, "8080", DefaultMetricPort)
	})

	t.Run("model label", func(t *testing.T) {
		assert.Equal(t, "model.aibrix.ai/model-name", ModelLabelKey)
	})
}
// Tests for Statistics (Phase 007)

func TestStatistics_IncrementCounters(t *testing.T) {
	stats := NewRoutingStatistics()

	stats.IncrementTotal()
	stats.IncrementSuccess()
	stats.IncrementFallback("test_reason")
	stats.IncrementRejection("slo_violation")
	stats.IncrementError("no_pods")

	snapshot := stats.(*routingStatistics).GetSnapshot()
	assert.Equal(t, int64(1), snapshot.TotalRequests)
	assert.Equal(t, int64(1), snapshot.SuccessfulRoutes)
	assert.Equal(t, int64(1), snapshot.FallbackRoutes)
	assert.Equal(t, int64(1), snapshot.RejectedRequests)
	assert.Equal(t, int64(1), snapshot.Errors)
}

func TestStatistics_RecordMetrics(t *testing.T) {
	stats := NewRoutingStatistics()

	// Record some latencies
	stats.RecordLatency(100 * time.Microsecond)
	stats.RecordLatency(200 * time.Microsecond)
	stats.RecordLatency(150 * time.Microsecond)

	// Record cache hits
	stats.RecordCacheHit(0.5)
	stats.RecordCacheHit(0.7)
	stats.RecordCacheHit(0.6)

	snapshot := stats.(*routingStatistics).GetSnapshot()
	assert.Greater(t, snapshot.AvgLatency, time.Duration(0))
	assert.Greater(t, snapshot.P95Latency, time.Duration(0))
	assert.Greater(t, snapshot.AvgCacheHitRate, 0.0)
	assert.Less(t, snapshot.AvgCacheHitRate, 1.0)
}

func TestStatistics_Reset(t *testing.T) {
	stats := NewRoutingStatistics().(*routingStatistics)

	// Add some data
	stats.IncrementTotal()
	stats.IncrementSuccess()
	stats.RecordLatency(100 * time.Microsecond)

	// Reset
	stats.Reset()

	snapshot := stats.GetSnapshot()
	assert.Equal(t, int64(0), snapshot.TotalRequests)
	assert.Equal(t, int64(0), snapshot.SuccessfulRoutes)
}

func TestHelperMethods(t *testing.T) {
	t.Run("extractIPFromIPPort", func(t *testing.T) {
		assert.Equal(t, "10.0.1.1", extractIPFromIPPort("10.0.1.1:8080"))
		assert.Equal(t, "192.168.1.1", extractIPFromIPPort("192.168.1.1:9000"))
		assert.Equal(t, "invalid", extractIPFromIPPort("invalid"))
	})

	t.Run("RelaxSLO", func(t *testing.T) {
		slo := 1 * time.Second
		relaxed := RelaxSLO(slo, 1.5)
		assert.Equal(t, 1500*time.Millisecond, relaxed)
	})
}
