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

// Package constants defines common constants used throughout the AIBrix platform.
package constants

// Model-related Kubernetes label constants.
// These labels are used to identify and manage Pods running LLM inference services.
const (
	// ModelLabelName defines the Kubernetes label key for the unique name of an AIBrix model.
	// This label is used by controllers and selectors to identify model-specific resources.
	// Example values: "llama2-7b", "deepseek-r1", "qwen-14b"
	ModelLabelName = "model.aibrix.ai/name"

	// ModelLabelEngine defines the Kubernetes label key for the inference engine type.
	// This label is used for engine-specific optimizations and monitoring.
	// Supported values: "vllm", "sglang", "tgi"
	ModelLabelEngine = "model.aibrix.ai/engine"

	// ModelLabelMetricPort defines the Kubernetes label key for the Prometheus metrics exposure port.
	// This label is used by the metrics collection system to scrape inference service performance data.
	// Example values: "8000", "9090"
	ModelLabelMetricPort = "model.aibrix.ai/metric-port"

	// ModelLabelPort defines the Kubernetes label key for the model inference API port.
	// This label is used by clients and the gateway to connect to the inference service.
	// Example values: "8000", "8080"
	ModelLabelPort = "model.aibrix.ai/port"

	// ModelLabelSGLangBootstrapPort defines the Kubernetes label key for the SGLang bootstrap port.
	// This label is specific to SGLang engine deployments and is used for bootstrap operations.
	// Example values: "30000", "30001"
	ModelLabelSGLangBootstrapPort = "model.aibrix.ai/sglang-bootstrap-port"
)

// GetModelName safely retrieves the model name from a label map.
// It returns the model name and a boolean indicating whether the label was present.
// Returns ("", false) if the map is nil or the label is not present.
func GetModelName(m map[string]string) (string, bool) {
	if m == nil {
		return "", false
	}
	val, ok := m[ModelLabelName]
	return val, ok
}

// GetInferenceEngine safely retrieves the inference engine from a label map.
// It returns the engine type and a boolean indicating whether the label was present.
// Returns ("", false) if the map is nil or the label is not present.
func GetInferenceEngine(m map[string]string) (string, bool) {
	if m == nil {
		return "", false
	}
	val, ok := m[ModelLabelEngine]
	return val, ok
}

// GetMetricPort safely retrieves the metric port from a label map.
// It returns the port number as a string and a boolean indicating whether the label was present.
// Returns ("", false) if the map is nil or the label is not present.
func GetMetricPort(m map[string]string) (string, bool) {
	if m == nil {
		return "", false
	}
	val, ok := m[ModelLabelMetricPort]
	return val, ok
}

// GetServicePort safely retrieves the service port from a label map.
// It returns the port number as a string and a boolean indicating whether the label was present.
// Returns ("", false) if the map is nil or the label is not present.
func GetServicePort(m map[string]string) (string, bool) {
	if m == nil {
		return "", false
	}
	val, ok := m[ModelLabelPort]
	return val, ok
}

// GetSGLangBootstrapPort safely retrieves the SGLang bootstrap port from a label map.
// It returns the port number as a string and a boolean indicating whether the label was present.
// Returns ("", false) if the map is nil or the label is not present.
func GetSGLangBootstrapPort(m map[string]string) (string, bool) {
	if m == nil {
		return "", false
	}
	val, ok := m[ModelLabelSGLangBootstrapPort]
	return val, ok
}
