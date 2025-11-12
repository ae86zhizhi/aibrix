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

	"github.com/vllm-project/aibrix/pkg/utils"
	"k8s.io/klog/v2"
)

// DefaultKVAwareConfig provides default configuration values
var DefaultKVAwareConfig = KVAwareConfig{
	Enabled:                 false,
	EnableKVTransfer:        false,         // MVP: no actual transfer
	TransferBandwidthBps:    100 * 1e9 / 8, // 100 Gbps = 12.5 GB/s
	KVCopyThresholdBlk:      2,
	TTFTSLO:                 30 * time.Second,
	TBTSLO:                  200 * time.Millisecond,
	PromEnabled:             true,
	MetricsWindowSize:       5 * time.Minute,
	EMAAlpha:                0.7,
	QueueFallbackMultiplier: 0.5,
	Models: []ModelKVSpec{
		{
			ModelName:       "default",
			PerTokenKVBytes: 32768, // 32KB default
			BlockSizeTokens: 16,
		},
	},
}

// LoadConfig loads KV-aware configuration from environment and config files
func LoadConfig() (*KVAwareConfig, error) {
	config := DefaultKVAwareConfig

	// Load from environment variables
	if enabled := utils.LoadEnvBool("AIBRIX_KV_AWARE_ENABLED", false); enabled {
		config.Enabled = enabled
	}

	if enableTransfer := utils.LoadEnvBool("AIBRIX_KV_TRANSFER_ENABLED", false); enableTransfer {
		config.EnableKVTransfer = enableTransfer
	}

	if bwGbps := utils.LoadEnvFloat("AIBRIX_KV_TRANSFER_BW_GBPS", 100); bwGbps > 0 {
		config.TransferBandwidthBps = bwGbps * 1e9 / 8 // Convert Gbps to Bps
	}

	if threshold := utils.LoadEnvInt("AIBRIX_KV_COPY_THRESHOLD_BLOCKS", 2); threshold > 0 {
		config.KVCopyThresholdBlk = threshold
	}

	if ttftSec := utils.LoadEnvInt("AIBRIX_TTFT_SLO_SECONDS", 30); ttftSec > 0 {
		config.TTFTSLO = time.Duration(ttftSec) * time.Second
	}

	if tbtMs := utils.LoadEnvInt("AIBRIX_TBT_SLO_MS", 200); tbtMs > 0 {
		config.TBTSLO = time.Duration(tbtMs) * time.Millisecond
	}

	// Validate configuration
	if err := ValidateConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid KV-aware config: %w", err)
	}

	return &config, nil
}

// ValidateConfig validates the KV-aware configuration
func ValidateConfig(config *KVAwareConfig) error {
	// Check required fields
	if config.TransferBandwidthBps <= 0 {
		return fmt.Errorf("transfer_bandwidth_bps must be positive, got %f",
			config.TransferBandwidthBps)
	}

	// Validate bandwidth against paper recommendations
	minBandwidthBps := 100 * 1e9 / 8 // 100 Gbps
	if config.TransferBandwidthBps < minBandwidthBps {
		klog.Warningf("Bandwidth %.2f Gbps below recommended 100 Gbps",
			config.TransferBandwidthBps*8/1e9)
	}

	// Validate block sizes
	indexerBlockSize := utils.LoadEnvInt("AIBRIX_PREFIX_CACHE_BLOCK_SIZE", 16)
	for _, model := range config.Models {
		if model.BlockSizeTokens != indexerBlockSize {
			return fmt.Errorf("model %s block size %d != indexer block size %d",
				model.ModelName, model.BlockSizeTokens, indexerBlockSize)
		}

		// Validate block size range per paper
		if model.BlockSizeTokens < 16 || model.BlockSizeTokens > 512 {
			klog.Warningf("Block size %d outside recommended range [16, 512]",
				model.BlockSizeTokens)
		}

		// Validate per-token KV bytes
		if model.PerTokenKVBytes <= 0 {
			return fmt.Errorf("model %s has invalid per_token_kv_bytes: %d",
				model.ModelName, model.PerTokenKVBytes)
		}
	}

	// Validate EMA coefficient
	if config.EMAAlpha < 0 || config.EMAAlpha > 1 {
		return fmt.Errorf("ema_alpha must be in [0, 1], got %f", config.EMAAlpha)
	}

	// Validate SLOs
	if config.TTFTSLO <= 0 {
		return fmt.Errorf("ttft_slo must be positive, got %v", config.TTFTSLO)
	}

	if config.TBTSLO <= 0 {
		return fmt.Errorf("tbt_slo must be positive, got %v", config.TBTSLO)
	}

	// Warn about unrealistic SLOs
	if config.TTFTSLO < 100*time.Millisecond {
		klog.Warningf("Unrealistic TTFT SLO: %v", config.TTFTSLO)
	}

	if config.TBTSLO < 10*time.Millisecond {
		klog.Warningf("Unrealistic TBT SLO: %v", config.TBTSLO)
	}

	// MVP constraint: KV transfer must be disabled
	if config.EnableKVTransfer {
		klog.Warningf("KV transfer is enabled but MVP only supports estimation")
		config.EnableKVTransfer = false
	}

	return nil
}

// GetModelConfig returns configuration for a specific model
func (c *KVAwareConfig) GetModelConfig(modelName string) (*ModelKVSpec, error) {
	for _, model := range c.Models {
		if model.ModelName == modelName {
			return &model, nil
		}
	}

	// Return default if model not found
	for _, model := range c.Models {
		if model.ModelName == "default" {
			klog.V(4).Infof("Using default config for model %s", modelName)
			defaultCopy := model
			defaultCopy.ModelName = modelName
			return &defaultCopy, nil
		}
	}

	return nil, fmt.Errorf("no configuration found for model %s", modelName)
}
