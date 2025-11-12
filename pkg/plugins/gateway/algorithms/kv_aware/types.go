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
	"time"
)

// ModelKVSpec defines KV cache parameters for a specific model
type ModelKVSpec struct {
	ModelName       string `yaml:"model_name" json:"model_name"`
	PerTokenKVBytes int64  `yaml:"per_token_kv_bytes" json:"per_token_kv_bytes"`
	BlockSizeTokens int    `yaml:"block_size_tokens" json:"block_size_tokens"`
}

// KVAwareConfig is the main configuration for KV-aware routing
type KVAwareConfig struct {
	// Feature flags
	Enabled          bool `yaml:"enabled" json:"enabled"`
	EnableKVTransfer bool `yaml:"enable_kv_transfer" json:"enable_kv_transfer"`

	// Network and transfer settings
	TransferBandwidthBps float64 `yaml:"transfer_bandwidth_bps" json:"transfer_bandwidth_bps"`
	KVCopyThresholdBlk   int     `yaml:"kv_copy_threshold_blocks" json:"kv_copy_threshold_blocks"`

	// SLO constraints
	TTFTSLO time.Duration `yaml:"ttft_slo" json:"ttft_slo"`
	TBTSLO  time.Duration `yaml:"tbt_slo" json:"tbt_slo"`

	// Metrics configuration
	PromEnabled       bool          `yaml:"prom_enabled" json:"prom_enabled"`
	MetricsWindowSize time.Duration `yaml:"metrics_window_size" json:"metrics_window_size"`

	// Model specifications
	Models []ModelKVSpec `yaml:"models" json:"models"`

	// Estimation parameters
	EMAAlpha                float64 `yaml:"ema_alpha" json:"ema_alpha"`
	QueueFallbackMultiplier float64 `yaml:"queue_fallback_multiplier" json:"queue_fallback_multiplier"`
}

// PodRef represents a reference to a pod
type PodRef struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	IPPort    string `json:"ip_port"` // "<PodIP>:<metricPort>"
	Role      string `json:"role"`    // "prefill" or "decode"
	ModelName string `json:"model_name"`
}

// Key returns a unique identifier for the pod
func (p PodRef) Key() string {
	return p.Namespace + "/" + p.Name
}

// PodMetrics contains metrics snapshot for a pod
type PodMetrics struct {
	// Real-time gauges
	NumWaiting    float64 `json:"num_waiting"`
	NumRunning    float64 `json:"num_running"`
	GPUCacheUsage float64 `json:"gpu_cache_usage"`
	CPUCacheUsage float64 `json:"cpu_cache_usage"`

	// Throughput metrics
	PromptTokPerS float64 `json:"prompt_tok_per_s"`
	GenTokPerS    float64 `json:"gen_tok_per_s"`

	// Aggregated metrics (5m window, nullable)
	MeanPrefillSec *float64 `json:"mean_prefill_sec,omitempty"`
	P95QueueSec    *float64 `json:"p95_queue_sec,omitempty"`
	P95TPOTSec     *float64 `json:"p95_tpot_sec,omitempty"`
	AvgTPOTSec     *float64 `json:"avg_tpot_sec,omitempty"`

	// Metadata
	LastUpdated      time.Time `json:"last_updated"`
	MetricsAvailable bool      `json:"metrics_available"`
}

// PrefixMatch contains prefix matching results
type PrefixMatch struct {
	PodPrefixBlocks map[string]int `json:"pod_prefix_blocks"` // pod_key -> continuous blocks
	PrefixHashes    []uint64       `json:"prefix_hashes"`     // hash sequence for this request
	BestPod         string         `json:"best_pod"`          // pod with longest match
	BestBlocks      int            `json:"best_blocks"`       // longest match in blocks
}

// PrefillEval contains evaluation results for a prefill pod
type PrefillEval struct {
	Pod              PodRef  `json:"pod"`
	LocalPrefixBlk   int     `json:"local_prefix_blocks"`
	UseBestPrefixBlk int     `json:"use_best_prefix_blocks"`
	TTransfer        float64 `json:"t_transfer"`
	TQueue           float64 `json:"t_queue"`
	TPrefill         float64 `json:"t_prefill"`
	TTFT             float64 `json:"ttft"`
	Explanation      string  `json:"explanation,omitempty"`
}

// DecodeCandidate represents a decode pod candidate
type DecodeCandidate struct {
	Pod          PodRef  `json:"pod"`
	CurrentTBT   float64 `json:"current_tbt"`
	PredictedTBT float64 `json:"predicted_tbt"`
	Score        float64 `json:"score"`
}

// RoutingDecision represents the final routing decision
type RoutingDecision struct {
	RequestID       string         `json:"request_id"`
	Timestamp       time.Time      `json:"timestamp"`
	PrefillPod      *PodRef        `json:"prefill_pod,omitempty"`
	DecodePod       *PodRef        `json:"decode_pod,omitempty"`
	EstimatedTTFT   float64        `json:"estimated_ttft"`
	PredictedTBT    float64        `json:"predicted_tbt"`
	PrefillEvals    []PrefillEval  `json:"prefill_evals"`
	DecisionType    string         `json:"decision_type"` // "kv_aware", "fallback", "rejected"
	PrefixBlocks    int            `json:"prefix_blocks"` // Number of cached prefix blocks
	TotalBlocks     int            `json:"total_blocks"`  // Total prompt blocks
	RejectionReason string         `json:"rejection_reason,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

// TTFTComponents breaks down TTFT estimation
type TTFTComponents struct {
	TransferTime float64 `json:"transfer_time"`
	QueueTime    float64 `json:"queue_time"`
	PrefillTime  float64 `json:"prefill_time"`
	TotalTTFT    float64 `json:"total_ttft"`
}
