/*
Copyright 2025 The Aibrix Team.

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

package constants_test

import (
	"testing"

	"github.com/vllm-project/aibrix/pkg/constants"
)

// TestModelLabelValues verifies that the model label constants have the correct values
func TestModelLabelValues(t *testing.T) {
	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{
			name:     "ModelLabelName has correct value",
			got:      constants.ModelLabelName,
			expected: "model.aibrix.ai/name",
		},
		{
			name:     "ModelLabelEngine has correct value",
			got:      constants.ModelLabelEngine,
			expected: "model.aibrix.ai/engine",
		},
		{
			name:     "ModelLabelMetricPort has correct value",
			got:      constants.ModelLabelMetricPort,
			expected: "model.aibrix.ai/metric-port",
		},
		{
			name:     "ModelLabelPort has correct value",
			got:      constants.ModelLabelPort,
			expected: "model.aibrix.ai/port",
		},
		{
			name:     "ModelLabelSGLangBootstrapPort has correct value",
			got:      constants.ModelLabelSGLangBootstrapPort,
			expected: "model.aibrix.ai/sglang-bootstrap-port",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("got %q, want %q", tt.got, tt.expected)
			}
		})
	}
}

// TestGetModelName tests the GetModelName helper function
func TestGetModelName(t *testing.T) {
	tests := []struct {
		name     string
		labels   map[string]string
		wantName string
		wantOk   bool
	}{
		{
			name: "label present",
			labels: map[string]string{
				constants.ModelLabelName: "llama2-7b",
			},
			wantName: "llama2-7b",
			wantOk:   true,
		},
		{
			name: "label not present",
			labels: map[string]string{
				"other-label": "value",
			},
			wantName: "",
			wantOk:   false,
		},
		{
			name:     "nil map",
			labels:   nil,
			wantName: "",
			wantOk:   false,
		},
		{
			name:     "empty map",
			labels:   map[string]string{},
			wantName: "",
			wantOk:   false,
		},
		{
			name: "label present but empty value",
			labels: map[string]string{
				constants.ModelLabelName: "",
			},
			wantName: "",
			wantOk:   true, // The key exists, so ok is true
		},
		{
			name: "map with multiple keys including model name",
			labels: map[string]string{
				"other-key":              "value",
				constants.ModelLabelName: "deepseek-r1",
				"another-key":            "another-value",
			},
			wantName: "deepseek-r1",
			wantOk:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotOk := constants.GetModelName(tt.labels)
			if gotName != tt.wantName {
				t.Errorf("GetModelName() gotName = %v, want %v", gotName, tt.wantName)
			}
			if gotOk != tt.wantOk {
				t.Errorf("GetModelName() gotOk = %v, want %v", gotOk, tt.wantOk)
			}
		})
	}
}

// TestGetInferenceEngine tests the GetInferenceEngine helper function
func TestGetInferenceEngine(t *testing.T) {
	tests := []struct {
		name       string
		labels     map[string]string
		wantEngine string
		wantOk     bool
	}{
		{
			name: "vllm engine",
			labels: map[string]string{
				constants.ModelLabelEngine: "vllm",
			},
			wantEngine: "vllm",
			wantOk:     true,
		},
		{
			name: "sglang engine",
			labels: map[string]string{
				constants.ModelLabelEngine: "sglang",
			},
			wantEngine: "sglang",
			wantOk:     true,
		},
		{
			name: "tgi engine",
			labels: map[string]string{
				constants.ModelLabelEngine: "tgi",
			},
			wantEngine: "tgi",
			wantOk:     true,
		},
		{
			name:       "empty map",
			labels:     map[string]string{},
			wantEngine: "",
			wantOk:     false,
		},
		{
			name:       "nil map does not panic",
			labels:     nil,
			wantEngine: "",
			wantOk:     false,
		},
		{
			name: "label present but empty value",
			labels: map[string]string{
				constants.ModelLabelEngine: "",
			},
			wantEngine: "",
			wantOk:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotEngine, gotOk := constants.GetInferenceEngine(tt.labels)
			if gotEngine != tt.wantEngine {
				t.Errorf("GetInferenceEngine() gotEngine = %v, want %v", gotEngine, tt.wantEngine)
			}
			if gotOk != tt.wantOk {
				t.Errorf("GetInferenceEngine() gotOk = %v, want %v", gotOk, tt.wantOk)
			}
		})
	}
}

// TestGetMetricPort tests the GetMetricPort helper function
func TestGetMetricPort(t *testing.T) {
	tests := []struct {
		name     string
		labels   map[string]string
		wantPort string
		wantOk   bool
	}{
		{
			name: "metric port 8000",
			labels: map[string]string{
				constants.ModelLabelMetricPort: "8000",
			},
			wantPort: "8000",
			wantOk:   true,
		},
		{
			name: "metric port 9090",
			labels: map[string]string{
				constants.ModelLabelMetricPort: "9090",
			},
			wantPort: "9090",
			wantOk:   true,
		},
		{
			name:     "empty map",
			labels:   map[string]string{},
			wantPort: "",
			wantOk:   false,
		},
		{
			name:     "nil map does not panic",
			labels:   nil,
			wantPort: "",
			wantOk:   false,
		},
		{
			name: "label present but empty value",
			labels: map[string]string{
				constants.ModelLabelMetricPort: "",
			},
			wantPort: "",
			wantOk:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPort, gotOk := constants.GetMetricPort(tt.labels)
			if gotPort != tt.wantPort {
				t.Errorf("GetMetricPort() gotPort = %v, want %v", gotPort, tt.wantPort)
			}
			if gotOk != tt.wantOk {
				t.Errorf("GetMetricPort() gotOk = %v, want %v", gotOk, tt.wantOk)
			}
		})
	}
}

// TestGetServicePort tests the GetServicePort helper function
func TestGetServicePort(t *testing.T) {
	tests := []struct {
		name     string
		labels   map[string]string
		wantPort string
		wantOk   bool
	}{
		{
			name: "service port 8000",
			labels: map[string]string{
				constants.ModelLabelPort: "8000",
			},
			wantPort: "8000",
			wantOk:   true,
		},
		{
			name: "service port 8080",
			labels: map[string]string{
				constants.ModelLabelPort: "8080",
			},
			wantPort: "8080",
			wantOk:   true,
		},
		{
			name:     "empty map",
			labels:   map[string]string{},
			wantPort: "",
			wantOk:   false,
		},
		{
			name:     "nil map does not panic",
			labels:   nil,
			wantPort: "",
			wantOk:   false,
		},
		{
			name: "label present but empty value",
			labels: map[string]string{
				constants.ModelLabelPort: "",
			},
			wantPort: "",
			wantOk:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPort, gotOk := constants.GetServicePort(tt.labels)
			if gotPort != tt.wantPort {
				t.Errorf("GetServicePort() gotPort = %v, want %v", gotPort, tt.wantPort)
			}
			if gotOk != tt.wantOk {
				t.Errorf("GetServicePort() gotOk = %v, want %v", gotOk, tt.wantOk)
			}
		})
	}
}

// TestGetSGLangBootstrapPort tests the GetSGLangBootstrapPort helper function
func TestGetSGLangBootstrapPort(t *testing.T) {
	tests := []struct {
		name     string
		labels   map[string]string
		wantPort string
		wantOk   bool
	}{
		{
			name: "bootstrap port 30000",
			labels: map[string]string{
				constants.ModelLabelSGLangBootstrapPort: "30000",
			},
			wantPort: "30000",
			wantOk:   true,
		},
		{
			name: "bootstrap port 30001",
			labels: map[string]string{
				constants.ModelLabelSGLangBootstrapPort: "30001",
			},
			wantPort: "30001",
			wantOk:   true,
		},
		{
			name:     "empty map",
			labels:   map[string]string{},
			wantPort: "",
			wantOk:   false,
		},
		{
			name:     "nil map does not panic",
			labels:   nil,
			wantPort: "",
			wantOk:   false,
		},
		{
			name: "label present but empty value",
			labels: map[string]string{
				constants.ModelLabelSGLangBootstrapPort: "",
			},
			wantPort: "",
			wantOk:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPort, gotOk := constants.GetSGLangBootstrapPort(tt.labels)
			if gotPort != tt.wantPort {
				t.Errorf("GetSGLangBootstrapPort() gotPort = %v, want %v", gotPort, tt.wantPort)
			}
			if gotOk != tt.wantOk {
				t.Errorf("GetSGLangBootstrapPort() gotOk = %v, want %v", gotOk, tt.wantOk)
			}
		})
	}
}

// TestEdgeCases tests all helper functions for nil safety
func TestEdgeCases(t *testing.T) {
	// Test that all helper functions handle nil without panicking
	t.Run("nil safety", func(t *testing.T) {
		// These calls should not panic
		_, _ = constants.GetModelName(nil)
		_, _ = constants.GetInferenceEngine(nil)
		_, _ = constants.GetMetricPort(nil)
		_, _ = constants.GetServicePort(nil)
		_, _ = constants.GetSGLangBootstrapPort(nil)
	})

	// Test with map containing empty strings
	t.Run("empty string values", func(t *testing.T) {
		m := map[string]string{
			constants.ModelLabelName:                "",
			constants.ModelLabelEngine:              "",
			constants.ModelLabelPort:                "",
			constants.ModelLabelMetricPort:          "",
			constants.ModelLabelSGLangBootstrapPort: "",
		}

		if got, ok := constants.GetModelName(m); got != "" || !ok {
			t.Errorf("GetModelName with empty value = (%q, %v), want (\"\", true)", got, ok)
		}
		if got, ok := constants.GetInferenceEngine(m); got != "" || !ok {
			t.Errorf("GetInferenceEngine with empty value = (%q, %v), want (\"\", true)", got, ok)
		}
		if got, ok := constants.GetServicePort(m); got != "" || !ok {
			t.Errorf("GetServicePort with empty value = (%q, %v), want (\"\", true)", got, ok)
		}
		if got, ok := constants.GetMetricPort(m); got != "" || !ok {
			t.Errorf("GetMetricPort with empty value = (%q, %v), want (\"\", true)", got, ok)
		}
		if got, ok := constants.GetSGLangBootstrapPort(m); got != "" || !ok {
			t.Errorf("GetSGLangBootstrapPort with empty value = (%q, %v), want (\"\", true)", got, ok)
		}
	})
}

// TestIntegration tests the constants in a more integrated scenario
func TestIntegration(t *testing.T) {
	// Simulate a typical pod labels map
	podLabels := map[string]string{
		constants.ModelLabelName:                "qwen-14b",
		constants.ModelLabelEngine:              "sglang",
		constants.ModelLabelPort:                "8080",
		constants.ModelLabelMetricPort:          "9090",
		constants.ModelLabelSGLangBootstrapPort: "30000",
		"app":                                   "inference-service",
		"version":                               "v1",
	}

	// Verify all getters work correctly
	if name, ok := constants.GetModelName(podLabels); !ok || name != "qwen-14b" {
		t.Errorf("GetModelName = (%q, %v), want (%q, true)", name, ok, "qwen-14b")
	}
	if engine, ok := constants.GetInferenceEngine(podLabels); !ok || engine != "sglang" {
		t.Errorf("GetInferenceEngine = (%q, %v), want (%q, true)", engine, ok, "sglang")
	}
	if port, ok := constants.GetServicePort(podLabels); !ok || port != "8080" {
		t.Errorf("GetServicePort = (%q, %v), want (%q, true)", port, ok, "8080")
	}
	if metricPort, ok := constants.GetMetricPort(podLabels); !ok || metricPort != "9090" {
		t.Errorf("GetMetricPort = (%q, %v), want (%q, true)", metricPort, ok, "9090")
	}
	if bootstrapPort, ok := constants.GetSGLangBootstrapPort(podLabels); !ok || bootstrapPort != "30000" {
		t.Errorf("GetSGLangBootstrapPort = (%q, %v), want (%q, true)", bootstrapPort, ok, "30000")
	}

	// Test the pattern for checking if a model label exists
	if modelName, hasModel := constants.GetModelName(podLabels); hasModel && modelName != "" {
		// This replaces the old HasModelLabels function
		t.Logf("Pod has model label: %s", modelName)
	} else {
		t.Error("Pod should have model label")
	}
}
