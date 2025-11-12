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

	"k8s.io/klog/v2"
)

// SLOChecker checks if SLO constraints can be met
type SLOChecker interface {
	// CheckTTFTSLO checks if TTFT meets SLO
	CheckTTFTSLO(estimatedTTFT float64, ttftslo time.Duration) error

	// CheckTBTSLO checks if TBT meets SLO
	CheckTBTSLO(predictedTBT float64, tbtslo time.Duration) error

	// ShouldReject determines if request should be rejected (429)
	ShouldReject(ttft float64, tbt float64, ttftslo, tbtslo time.Duration) bool
}

// defaultSLOChecker implements SLOChecker
type defaultSLOChecker struct{}

// NewSLOChecker creates a new SLO checker
func NewSLOChecker() SLOChecker {
	return &defaultSLOChecker{}
}

// CheckTTFTSLO checks if TTFT meets SLO
func (c *defaultSLOChecker) CheckTTFTSLO(estimatedTTFT float64, ttftslo time.Duration) error {
	if ttftslo <= 0 {
		return nil // No SLO specified
	}

	if estimatedTTFT > ttftslo.Seconds() {
		klog.V(3).Infof("TTFT SLO violation: estimated %.2fs > SLO %.2fs",
			estimatedTTFT, ttftslo.Seconds())
		return fmt.Errorf("TTFT %.2fs exceeds SLO %.2fs",
			estimatedTTFT, ttftslo.Seconds())
	}

	return nil
}

// CheckTBTSLO checks if TBT meets SLO
func (c *defaultSLOChecker) CheckTBTSLO(predictedTBT float64, tbtslo time.Duration) error {
	if tbtslo <= 0 {
		return nil // No SLO specified
	}

	if predictedTBT > tbtslo.Seconds() {
		klog.V(3).Infof("TBT SLO violation: predicted %.3fs > SLO %.2fs",
			predictedTBT, tbtslo.Seconds())
		return fmt.Errorf("TBT %.3fs exceeds SLO %.2fs",
			predictedTBT, tbtslo.Seconds())
	}

	return nil
}

// ShouldReject determines if request should be rejected (429)
func (c *defaultSLOChecker) ShouldReject(
	ttft float64,
	tbt float64,
	ttftslo, tbtslo time.Duration,
) bool {
	// Check both SLOs
	ttftViolation := ttftslo > 0 && ttft > ttftslo.Seconds()
	tbtViolation := tbtslo > 0 && tbt > tbtslo.Seconds()

	if ttftViolation || tbtViolation {
		klog.V(2).Infof("SLO violation detected - TTFT: %.2fs/%.2fs, TBT: %.3fs/%.2fs",
			ttft, ttftslo.Seconds(), tbt, tbtslo.Seconds())
		return true
	}

	return false
}
