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
)

// Error definitions for KV-aware routing
var (
	// ErrNoDecodeCandidate indicates no suitable decode pod was found
	ErrNoDecodeCandidate = fmt.Errorf("no decode candidate available")

	// ErrNoPrefillCandidate indicates no suitable prefill pod was found
	ErrNoPrefillCandidate = fmt.Errorf("no prefill candidate available")

	// ErrTTFTSLOViolation indicates all candidates would violate TTFT SLO
	ErrTTFTSLOViolation = fmt.Errorf("all candidates violate TTFT SLO")

	// ErrTBTSLOViolation indicates the decode pod would violate TBT SLO
	ErrTBTSLOViolation = fmt.Errorf("decode candidate violates TBT SLO")

	// ErrPrefixIndexerUnavailable indicates the prefix indexer is not available
	ErrPrefixIndexerUnavailable = fmt.Errorf("prefix indexer unavailable")

	// ErrMetricsUnavailable indicates pod metrics are not available
	ErrMetricsUnavailable = fmt.Errorf("metrics unavailable")

	// ErrNoPrefillPods indicates no prefill pods are available
	ErrNoPrefillPods = fmt.Errorf("no prefill pods available")

	// ErrNoDecodePods indicates no decode pods are available
	ErrNoDecodePods = fmt.Errorf("no decode pods available")
)

// RejectionError represents a routing rejection with a specific reason
// This can be used to return 429 or 503 errors to the client
type RejectionError struct {
	// Code is the HTTP status code to return (e.g., 429, 503)
	Code int

	// Message is the error message
	Message string

	// Reason provides additional context for the rejection
	Reason string

	// RetryAfter suggests when the client should retry
	RetryAfter time.Duration
}

// Error implements the error interface
func (e *RejectionError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("%s: %s", e.Message, e.Reason)
	}
	return e.Message
}

// NewRejectionError creates a new RejectionError
func NewRejectionError(code int, message, reason string, retryAfter time.Duration) *RejectionError {
	return &RejectionError{
		Code:       code,
		Message:    message,
		Reason:     reason,
		RetryAfter: retryAfter,
	}
}
