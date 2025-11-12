package kvaware

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestLoadConfig(t *testing.T) {
	// Test default configuration
	t.Run("default_config", func(t *testing.T) {
		config, err := LoadConfig()
		assert.NoError(t, err)
		assert.False(t, config.Enabled)
		assert.False(t, config.EnableKVTransfer)
		assert.Equal(t, 1.25e10, config.TransferBandwidthBps)
	})

	// Test environment variable loading
	t.Run("env_config", func(t *testing.T) {
		t.Setenv("AIBRIX_KV_AWARE_ENABLED", "true")
		t.Setenv("AIBRIX_KV_TRANSFER_BW_GBPS", "200")

		config, err := LoadConfig()
		assert.NoError(t, err)
		assert.True(t, config.Enabled)
		assert.Equal(t, 2.5e10, config.TransferBandwidthBps) // 200 Gbps
	})

	// Test all environment variables
	t.Run("all_env_vars", func(t *testing.T) {
		t.Setenv("AIBRIX_KV_AWARE_ENABLED", "true")
		t.Setenv("AIBRIX_KV_TRANSFER_ENABLED", "true")
		t.Setenv("AIBRIX_KV_TRANSFER_BW_GBPS", "200")
		t.Setenv("AIBRIX_KV_COPY_THRESHOLD_BLOCKS", "4")
		t.Setenv("AIBRIX_TTFT_SLO_SECONDS", "60")
		t.Setenv("AIBRIX_TBT_SLO_MS", "100")

		config, err := LoadConfig()
		assert.NoError(t, err)
		assert.True(t, config.Enabled)
		// Note: EnableKVTransfer will be set to false by ValidateConfig (MVP constraint)
		assert.Equal(t, 2.5e10, config.TransferBandwidthBps) // 200 Gbps
		assert.Equal(t, 4, config.KVCopyThresholdBlk)
		assert.Equal(t, 60*time.Second, config.TTFTSLO)
		assert.Equal(t, 100*time.Millisecond, config.TBTSLO)
	})
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      KVAwareConfig
		shouldError bool
		errorMsg    string
	}{
		{
			name: "valid_config",
			config: KVAwareConfig{
				TransferBandwidthBps: 1.25e10,
				TTFTSLO:              30 * time.Second,
				TBTSLO:               200 * time.Millisecond,
				EMAAlpha:             0.7,
				Models: []ModelKVSpec{
					{
						ModelName:       "test",
						PerTokenKVBytes: 1000,
						BlockSizeTokens: 16,
					},
				},
			},
			shouldError: false,
		},
		{
			name: "invalid_bandwidth",
			config: KVAwareConfig{
				TransferBandwidthBps: -1,
				TTFTSLO:              30 * time.Second,
				TBTSLO:               200 * time.Millisecond,
			},
			shouldError: true,
			errorMsg:    "must be positive",
		},
		{
			name: "low_bandwidth_warning",
			config: KVAwareConfig{
				TransferBandwidthBps: 1e9 / 8, // 1 Gbps (below recommended 100 Gbps)
				TTFTSLO:              30 * time.Second,
				TBTSLO:               200 * time.Millisecond,
				EMAAlpha:             0.7,
				Models: []ModelKVSpec{
					{
						ModelName:       "test",
						PerTokenKVBytes: 1000,
						BlockSizeTokens: 16,
					},
				},
			},
			shouldError: false, // Warning, not error
		},
		{
			name: "block_size_mismatch",
			config: KVAwareConfig{
				TransferBandwidthBps: 1.25e10,
				TTFTSLO:              30 * time.Second,
				TBTSLO:               200 * time.Millisecond,
				EMAAlpha:             0.7,
				Models: []ModelKVSpec{
					{
						ModelName:       "test",
						PerTokenKVBytes: 1000,
						BlockSizeTokens: 32, // Mismatch with indexer (16)
					},
				},
			},
			shouldError: true,
			errorMsg:    "block size",
		},
		{
			name: "block_size_out_of_range",
			config: KVAwareConfig{
				TransferBandwidthBps: 1.25e10,
				TTFTSLO:              30 * time.Second,
				TBTSLO:               200 * time.Millisecond,
				EMAAlpha:             0.7,
				Models: []ModelKVSpec{
					{
						ModelName:       "test",
						PerTokenKVBytes: 1000,
						BlockSizeTokens: 8, // Outside recommended range [16, 512]
					},
				},
			},
			shouldError: true, // Due to mismatch with indexer
		},
		{
			name: "invalid_per_token_kv_bytes",
			config: KVAwareConfig{
				TransferBandwidthBps: 1.25e10,
				TTFTSLO:              30 * time.Second,
				TBTSLO:               200 * time.Millisecond,
				EMAAlpha:             0.7,
				Models: []ModelKVSpec{
					{
						ModelName:       "test",
						PerTokenKVBytes: -100, // Invalid
						BlockSizeTokens: 16,
					},
				},
			},
			shouldError: true,
			errorMsg:    "invalid per_token_kv_bytes",
		},
		{
			name: "invalid_ema",
			config: KVAwareConfig{
				TransferBandwidthBps: 1.25e10,
				TTFTSLO:              30 * time.Second,
				TBTSLO:               200 * time.Millisecond,
				EMAAlpha:             1.5, // Out of range
			},
			shouldError: true,
			errorMsg:    "must be in [0, 1]",
		},
		{
			name: "invalid_ttft_slo",
			config: KVAwareConfig{
				TransferBandwidthBps: 1.25e10,
				TTFTSLO:              -1 * time.Second, // Invalid
				TBTSLO:               200 * time.Millisecond,
				EMAAlpha:             0.7,
			},
			shouldError: true,
			errorMsg:    "ttft_slo must be positive",
		},
		{
			name: "invalid_tbt_slo",
			config: KVAwareConfig{
				TransferBandwidthBps: 1.25e10,
				TTFTSLO:              30 * time.Second,
				TBTSLO:               -1 * time.Millisecond, // Invalid
				EMAAlpha:             0.7,
			},
			shouldError: true,
			errorMsg:    "tbt_slo must be positive",
		},
		{
			name: "unrealistic_ttft_slo",
			config: KVAwareConfig{
				TransferBandwidthBps: 1.25e10,
				TTFTSLO:              50 * time.Millisecond, // Unrealistically low
				TBTSLO:               200 * time.Millisecond,
				EMAAlpha:             0.7,
				Models: []ModelKVSpec{
					{
						ModelName:       "test",
						PerTokenKVBytes: 1000,
						BlockSizeTokens: 16,
					},
				},
			},
			shouldError: false, // Warning, not error
		},
		{
			name: "unrealistic_tbt_slo",
			config: KVAwareConfig{
				TransferBandwidthBps: 1.25e10,
				TTFTSLO:              30 * time.Second,
				TBTSLO:               5 * time.Millisecond, // Unrealistically low
				EMAAlpha:             0.7,
				Models: []ModelKVSpec{
					{
						ModelName:       "test",
						PerTokenKVBytes: 1000,
						BlockSizeTokens: 16,
					},
				},
			},
			shouldError: false, // Warning, not error
		},
		{
			name: "kv_transfer_enabled_warning",
			config: KVAwareConfig{
				TransferBandwidthBps: 1.25e10,
				TTFTSLO:              30 * time.Second,
				TBTSLO:               200 * time.Millisecond,
				EMAAlpha:             0.7,
				EnableKVTransfer:     true, // Should be disabled by validator
				Models: []ModelKVSpec{
					{
						ModelName:       "test",
						PerTokenKVBytes: 1000,
						BlockSizeTokens: 16,
					},
				},
			},
			shouldError: false, // Warning, gets disabled
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set required env for block size
			t.Setenv("AIBRIX_PREFIX_CACHE_BLOCK_SIZE", "16")

			err := ValidateConfig(&tt.config)
			if tt.shouldError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGetModelConfig(t *testing.T) {
	config := &KVAwareConfig{
		Models: []ModelKVSpec{
			{
				ModelName:       "llama3-70b",
				PerTokenKVBytes: 327680,
				BlockSizeTokens: 16,
			},
			{
				ModelName:       "default",
				PerTokenKVBytes: 32768,
				BlockSizeTokens: 16,
			},
		},
	}

	// Test existing model
	modelConfig, err := config.GetModelConfig("llama3-70b")
	assert.NoError(t, err)
	assert.Equal(t, int64(327680), modelConfig.PerTokenKVBytes)

	// Test unknown model (should use default)
	modelConfig, err = config.GetModelConfig("unknown-model")
	assert.NoError(t, err)
	assert.Equal(t, "unknown-model", modelConfig.ModelName)
	assert.Equal(t, int64(32768), modelConfig.PerTokenKVBytes)

	// Test unknown model with no default
	configNoDefault := &KVAwareConfig{
		Models: []ModelKVSpec{
			{
				ModelName:       "llama3-70b",
				PerTokenKVBytes: 327680,
				BlockSizeTokens: 16,
			},
		},
	}
	_, err = configNoDefault.GetModelConfig("unknown-model")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no configuration found")
}

func TestPodRefKey(t *testing.T) {
	pod := PodRef{
		Namespace: "default",
		Name:      "pod-1",
	}
	assert.Equal(t, "default/pod-1", pod.Key())
}
