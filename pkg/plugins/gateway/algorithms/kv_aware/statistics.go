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
	"sort"
	"sync"
	"time"
)

// routingStatistics tracks routing performance and decision quality
// This replaces the interface defined inline in kv_aware.go
type routingStatistics struct {
	mu sync.RWMutex

	// Counters
	totalRequests    int64
	successfulRoutes int64
	fallbackRoutes   int64
	rejectedRequests int64
	errors           int64

	// Detailed tracking
	fallbackReasons  map[string]int64
	rejectionReasons map[string]int64
	errorTypes       map[string]int64

	// Performance metrics (circular buffers, last 1000 entries)
	latencies      []time.Duration
	cacheHitRates  []float64
	ttftEstimates  []float64
	tbtPredictions []float64

	// Window for metrics
	windowStart time.Time
}

// StatsSnapshot represents a point-in-time snapshot of statistics
type StatsSnapshot struct {
	TotalRequests    int64
	SuccessfulRoutes int64
	FallbackRoutes   int64
	RejectedRequests int64
	Errors           int64

	SuccessRate   float64
	FallbackRate  float64
	RejectionRate float64
	ErrorRate     float64

	AvgLatency      time.Duration
	P95Latency      time.Duration
	AvgCacheHitRate float64

	WindowDuration time.Duration

	TopFallbackReasons  map[string]int64
	TopRejectionReasons map[string]int64
	TopErrorTypes       map[string]int64
}

// NewRoutingStatistics creates a new statistics tracker
func NewRoutingStatistics() RoutingStatistics {
	return &routingStatistics{
		fallbackReasons:  make(map[string]int64),
		rejectionReasons: make(map[string]int64),
		errorTypes:       make(map[string]int64),
		latencies:        make([]time.Duration, 0, 1000),
		cacheHitRates:    make([]float64, 0, 1000),
		ttftEstimates:    make([]float64, 0, 1000),
		tbtPredictions:   make([]float64, 0, 1000),
		windowStart:      time.Now(),
	}
}

// IncrementTotal increments total request counter
func (s *routingStatistics) IncrementTotal() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.totalRequests++
}

// IncrementSuccess increments successful routing counter
func (s *routingStatistics) IncrementSuccess() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.successfulRoutes++
}

// IncrementFallback increments fallback counter with reason
func (s *routingStatistics) IncrementFallback(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fallbackRoutes++
	s.fallbackReasons[reason]++
}

// IncrementRejection increments rejection counter with reason
func (s *routingStatistics) IncrementRejection(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rejectedRequests++
	s.rejectionReasons[reason]++
}

// IncrementError increments error counter with type
func (s *routingStatistics) IncrementError(errorType string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errors++
	s.errorTypes[errorType]++
}

// RecordLatency records routing decision latency
func (s *routingStatistics) RecordLatency(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.latencies = append(s.latencies, d)
	if len(s.latencies) > 1000 {
		s.latencies = s.latencies[1:]
	}
}

// RecordCacheHit records cache hit rate for a request
func (s *routingStatistics) RecordCacheHit(rate float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cacheHitRates = append(s.cacheHitRates, rate)
	if len(s.cacheHitRates) > 1000 {
		s.cacheHitRates = s.cacheHitRates[1:]
	}
}

// RecordTTFT records TTFT estimate
func (s *routingStatistics) RecordTTFT(ttft float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ttftEstimates = append(s.ttftEstimates, ttft)
	if len(s.ttftEstimates) > 1000 {
		s.ttftEstimates = s.ttftEstimates[1:]
	}
}

// RecordTBT records TBT prediction
func (s *routingStatistics) RecordTBT(tbt float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tbtPredictions = append(s.tbtPredictions, tbt)
	if len(s.tbtPredictions) > 1000 {
		s.tbtPredictions = s.tbtPredictions[1:]
	}
}

// GetSnapshot returns a point-in-time snapshot of statistics
func (s *routingStatistics) GetSnapshot() StatsSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snapshot := StatsSnapshot{
		TotalRequests:    s.totalRequests,
		SuccessfulRoutes: s.successfulRoutes,
		FallbackRoutes:   s.fallbackRoutes,
		RejectedRequests: s.rejectedRequests,
		Errors:           s.errors,
		WindowDuration:   time.Since(s.windowStart),
	}

	// Calculate rates
	if s.totalRequests > 0 {
		total := float64(s.totalRequests)
		snapshot.SuccessRate = float64(s.successfulRoutes) / total
		snapshot.FallbackRate = float64(s.fallbackRoutes) / total
		snapshot.RejectionRate = float64(s.rejectedRequests) / total
		snapshot.ErrorRate = float64(s.errors) / total
	}

	// Calculate latency metrics
	if len(s.latencies) > 0 {
		snapshot.AvgLatency = calculateAvgDuration(s.latencies)
		snapshot.P95Latency = calculateP95Duration(s.latencies)
	}

	// Calculate average cache hit rate
	if len(s.cacheHitRates) > 0 {
		snapshot.AvgCacheHitRate = calculateAvg(s.cacheHitRates)
	}

	// Copy top reasons (top 5)
	snapshot.TopFallbackReasons = copyTopN(s.fallbackReasons, 5)
	snapshot.TopRejectionReasons = copyTopN(s.rejectionReasons, 5)
	snapshot.TopErrorTypes = copyTopN(s.errorTypes, 5)

	return snapshot
}

// Reset resets all statistics
func (s *routingStatistics) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.totalRequests = 0
	s.successfulRoutes = 0
	s.fallbackRoutes = 0
	s.rejectedRequests = 0
	s.errors = 0

	s.fallbackReasons = make(map[string]int64)
	s.rejectionReasons = make(map[string]int64)
	s.errorTypes = make(map[string]int64)

	s.latencies = s.latencies[:0]
	s.cacheHitRates = s.cacheHitRates[:0]
	s.ttftEstimates = s.ttftEstimates[:0]
	s.tbtPredictions = s.tbtPredictions[:0]

	s.windowStart = time.Now()
}

// Helper functions

func calculateAvgDuration(durations []time.Duration) time.Duration {
	if len(durations) == 0 {
		return 0
	}

	var sum time.Duration
	for _, d := range durations {
		sum += d
	}
	return sum / time.Duration(len(durations))
}

func calculateP95Duration(durations []time.Duration) time.Duration {
	if len(durations) == 0 {
		return 0
	}

	// Create sorted copy
	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	idx := int(float64(len(sorted)) * 0.95)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}

	return sorted[idx]
}

func calculateAvg(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func copyTopN(m map[string]int64, n int) map[string]int64 {
	if len(m) == 0 {
		return nil
	}

	// Sort by value
	type kv struct {
		key   string
		value int64
	}

	pairs := make([]kv, 0, len(m))
	for k, v := range m {
		pairs = append(pairs, kv{k, v})
	}

	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].value > pairs[j].value
	})

	// Take top N
	result := make(map[string]int64)
	for i := 0; i < len(pairs) && i < n; i++ {
		result[pairs[i].key] = pairs[i].value
	}

	return result
}
