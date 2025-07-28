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

package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vllm-project/aibrix/pkg/utils/syncprefixcacheindexer"
)

// BenchmarkMetrics holds performance metrics
type BenchmarkMetrics struct {
	EventProcessingLatency   time.Duration `json:"event_processing_latency"`
	RoutingDecisionLatency   time.Duration `json:"routing_decision_latency"`
	EventsPerSecond         float64       `json:"events_per_second"`
	MemoryUsageMB           float64       `json:"memory_usage_mb"`
	CPUUsagePercent         float64       `json:"cpu_usage_percent"`
	NetworkBandwidthMBps    float64       `json:"network_bandwidth_mbps"`
	ConcurrentConnections   int           `json:"concurrent_connections"`
	TotalEventsProcessed    int64         `json:"total_events_processed"`
	ErrorRate               float64       `json:"error_rate"`
}

// BenchmarkResult represents a single benchmark run
type BenchmarkResult struct {
	Name      string            `json:"name"`
	Timestamp time.Time         `json:"timestamp"`
	Duration  time.Duration     `json:"duration"`
	Metrics   BenchmarkMetrics  `json:"metrics"`
}

// BenchmarkKVEventProcessingLatency measures event processing latency
func BenchmarkKVEventProcessingLatency(b *testing.B) {
	// Create sync indexer
	indexer := syncprefixcacheindexer.NewSyncPrefixHashTable()
	defer indexer.Close()

	// Prepare test events
	events := make([]syncprefixcacheindexer.BlockStored, 1000)
	for i := 0; i < 1000; i++ {
		tokens := generateTokenSequence(100)
		events[i] = syncprefixcacheindexer.BlockStored{
			BlockHashes: []int64{int64(i * 1000)},
			Tokens:      [][]byte{convertTokensToBytes(tokens)},
			ModelName:   "test-model",
			LoraID:      -1,
			SourcePod:   "10.0.0.1",
		}
	}

	b.ResetTimer()

	// Measure latency
	latencies := make([]time.Duration, 0, b.N)
	
	for i := 0; i < b.N; i++ {
		event := events[i%len(events)]
		
		start := time.Now()
		
		// Process event through indexer
		err := indexer.ProcessBlockStored(event)
		if err != nil {
			b.Fatalf("Failed to process block stored: %v", err)
		}
		
		latency := time.Since(start)
		latencies = append(latencies, latency)
	}

	// Calculate metrics
	avgLatency := calculateAverageLatency(latencies)
	p99Latency := calculatePercentileLatency(latencies, 99)
	
	b.ReportMetric(float64(avgLatency.Nanoseconds()), "avg_latency_ns")
	b.ReportMetric(float64(p99Latency.Nanoseconds()), "p99_latency_ns")
}

// BenchmarkKVEventThroughput measures maximum event throughput
func BenchmarkKVEventThroughput(b *testing.B) {
	// Create sync indexer
	indexer := syncprefixcacheindexer.NewSyncPrefixHashTable()
	defer indexer.Close()

	// Prepare events
	numEvents := 10000
	events := make([]syncprefixcacheindexer.BlockStored, numEvents)
	for i := 0; i < numEvents; i++ {
		tokens := generateTokenSequence(50 + i%100) // Variable length
		events[i] = syncprefixcacheindexer.BlockStored{
			BlockHashes: []int64{int64(i * 1000)},
			Tokens:      [][]byte{convertTokensToBytes(tokens)},
			ModelName:   "test-model",
			LoraID:      -1,
			SourcePod:   "10.0.0.1",
		}
	}

	b.ResetTimer()

	// Measure throughput
	start := time.Now()
	processed := int64(0)
	errors := int64(0)

	// Process events concurrently
	var wg sync.WaitGroup
	numWorkers := runtime.NumCPU()
	eventsPerWorker := b.N / numWorkers

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			
			for i := 0; i < eventsPerWorker; i++ {
				event := events[(workerID*eventsPerWorker+i)%numEvents]
				// Create a copy with unique block hash to avoid conflicts
				eventCopy := event
				eventCopy.BlockHashes = []int64{int64(workerID*1000000 + i)}
				
				err := indexer.ProcessBlockStored(eventCopy)
				if err != nil {
					atomic.AddInt64(&errors, 1)
				} else {
					atomic.AddInt64(&processed, 1)
				}
			}
		}(w)
	}

	wg.Wait()
	duration := time.Since(start)

	// Calculate metrics
	eventsPerSecond := float64(processed) / duration.Seconds()
	errorRate := float64(errors) / float64(processed+errors)

	b.ReportMetric(eventsPerSecond, "events_per_second")
	b.ReportMetric(errorRate*100, "error_rate_percent")
}

// BenchmarkKVEventMemoryUsage measures memory consumption
func BenchmarkKVEventMemoryUsage(b *testing.B) {
	// Get initial memory stats
	var initialMem runtime.MemStats
	runtime.ReadMemStats(&initialMem)

	// Create sync indexer with different sizes
	sizes := []struct {
		name     string
		maxSize  int
		numItems int
	}{
		{"small", 10 * 1024 * 1024, 1000},   // 10MB, 1K items
		{"medium", 100 * 1024 * 1024, 10000}, // 100MB, 10K items
		{"large", 1024 * 1024 * 1024, 100000}, // 1GB, 100K items
	}

	for _, size := range sizes {
		b.Run(size.name, func(b *testing.B) {
			// Force GC before test
			runtime.GC()
			runtime.GC()

			indexer := syncprefixcacheindexer.NewSyncPrefixHashTable()
			defer indexer.Close()

			// Fill indexer
			for i := 0; i < size.numItems; i++ {
				tokens := generateTokenSequence(100 + i%200)
				event := syncprefixcacheindexer.BlockStored{
					BlockHashes: []int64{int64(i * 1000)},
					Tokens:      [][]byte{convertTokensToBytes(tokens)},
					ModelName:   "test-model",
					LoraID:      -1,
					SourcePod:   "10.0.0.1",
				}
				
				err := indexer.ProcessBlockStored(event)
				if err != nil {
					b.Logf("Failed to process block %d: %v", i, err)
				}
			}

			// Measure memory after filling
			var afterMem runtime.MemStats
			runtime.ReadMemStats(&afterMem)

			memUsedMB := float64(afterMem.Alloc-initialMem.Alloc) / 1024 / 1024
			b.ReportMetric(memUsedMB, "memory_used_mb")
			b.ReportMetric(float64(size.numItems)/memUsedMB, "items_per_mb")
		})
	}
}

// BenchmarkKVEventConcurrency measures performance under concurrent load
func BenchmarkKVEventConcurrency(b *testing.B) {
	concurrencyLevels := []int{1, 10, 50, 100, 500}

	for _, concurrency := range concurrencyLevels {
		b.Run(fmt.Sprintf("concurrency_%d", concurrency), func(b *testing.B) {
			indexer := syncprefixcacheindexer.NewSyncPrefixHashTable()
			defer indexer.Close()

			// Prepare events
			events := make([]syncprefixcacheindexer.BlockStored, 1000)
			for i := 0; i < 1000; i++ {
				tokens := generateTokenSequence(100)
				events[i] = syncprefixcacheindexer.BlockStored{
					BlockHashes: []int64{int64(i * 1000)},
					Tokens:      [][]byte{convertTokensToBytes(tokens)},
					ModelName:   "test-model",
					LoraID:      -1,
					SourcePod:   "10.0.0.1",
				}
			}

			b.ResetTimer()

			// Run concurrent operations
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			var wg sync.WaitGroup
			processed := int64(0)
			errors := int64(0)

			// Start workers
			for w := 0; w < concurrency; w++ {
				wg.Add(1)
				go func(workerID int) {
					defer wg.Done()

					for i := 0; i < b.N/concurrency; i++ {
						select {
						case <-ctx.Done():
							return
						default:
							event := events[(workerID*1000+i)%len(events)]
							// Create a copy with unique block hash to avoid conflicts
							eventCopy := event
							eventCopy.BlockHashes = []int64{int64(workerID*1000000 + i)}
							eventCopy.SourcePod = fmt.Sprintf("10.0.0.%d", workerID%255)
							
							err := indexer.ProcessBlockStored(eventCopy)
							
							if err != nil {
								atomic.AddInt64(&errors, 1)
							} else {
								atomic.AddInt64(&processed, 1)
							}
						}
					}
				}(w)
			}

			wg.Wait()

			b.ReportMetric(float64(processed), "total_processed")
			b.ReportMetric(float64(errors), "total_errors")
			b.ReportMetric(float64(concurrency), "concurrency_level")
		})
	}
}

// BenchmarkKVEventLargePrefix tests performance with very long token sequences
func BenchmarkKVEventLargePrefix(b *testing.B) {
	prefixSizes := []int{100, 1000, 5000, 10000, 50000}

	for _, size := range prefixSizes {
		b.Run(fmt.Sprintf("prefix_size_%d", size), func(b *testing.B) {
			indexer := syncprefixcacheindexer.NewSyncPrefixHashTable()
			defer indexer.Close()

			// Generate large token sequence
			tokens := generateTokenSequence(size)
			byteTokens := convertTokensToBytes(tokens)

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				event := syncprefixcacheindexer.BlockStored{
					BlockHashes: []int64{int64(i * 1000)},
					Tokens:      [][]byte{byteTokens},
					ModelName:   "test-model",
					LoraID:      -1,
					SourcePod:   "10.0.0.1",
				}
				
				start := time.Now()
				err := indexer.ProcessBlockStored(event)
				latency := time.Since(start)
				
				if err != nil {
					b.Errorf("Failed to add request: %v", err)
				}
				
				b.ReportMetric(float64(latency.Microseconds()), "latency_us")
			}

			b.ReportMetric(float64(size), "token_count")
		})
	}
}

// BenchmarkKVEventBurstLoad simulates burst load scenarios
func BenchmarkKVEventBurstLoad(b *testing.B) {
	indexer := syncprefixcacheindexer.NewSyncPrefixHashTable()
	defer indexer.Close()

	// Burst parameters
	burstSize := 10000
	burstDuration := 5 * time.Second

	// Prepare burst events
	events := make([]syncprefixcacheindexer.BlockStored, burstSize)
	for i := 0; i < burstSize; i++ {
		tokens := generateTokenSequence(50 + i%150)
		events[i] = syncprefixcacheindexer.BlockStored{
			BlockHashes: []int64{int64(i * 1000)},
			Tokens:      [][]byte{convertTokensToBytes(tokens)},
			ModelName:   "test-model",
			LoraID:      -1,
			SourcePod:   "10.0.0.1",
		}
	}

	b.ResetTimer()

	// Simulate burst
	start := time.Now()
	processed := 0
	errors := 0

	deadline := start.Add(burstDuration)
	
	for time.Now().Before(deadline) && processed < b.N {
		event := events[processed%burstSize]
		// Create a copy with unique block hash to avoid conflicts
		eventCopy := event
		eventCopy.BlockHashes = []int64{int64(processed * 1000)}
		
		err := indexer.ProcessBlockStored(eventCopy)
		
		if err != nil {
			errors++
		}
		processed++
	}

	duration := time.Since(start)
	eventsPerSecond := float64(processed) / duration.Seconds()

	b.ReportMetric(eventsPerSecond, "burst_events_per_second")
	b.ReportMetric(float64(errors)/float64(processed)*100, "burst_error_rate_percent")
}

// BenchmarkKVEventRoutingDecision measures routing decision update latency
func BenchmarkKVEventRoutingDecision(b *testing.B) {
	indexer := syncprefixcacheindexer.NewSyncPrefixHashTable()
	defer indexer.Close()

	// Pre-fill indexer with data
	for i := 0; i < 10000; i++ {
		tokens := generateTokenSequence(100)
		event := syncprefixcacheindexer.BlockStored{
			BlockHashes: []int64{int64(i * 1000)},
			Tokens:      [][]byte{convertTokensToBytes(tokens)},
			ModelName:   "test-model",
			LoraID:      -1,
			SourcePod:   fmt.Sprintf("10.0.0.%d", i%255),
		}
		indexer.ProcessBlockStored(event)
	}

	// Test sequences for routing decisions
	testSequences := make([][]int32, 100)
	for i := 0; i < 100; i++ {
		testSequences[i] = generateTokenSequence(50 + i%100)
	}

	b.ResetTimer()

	latencies := make([]time.Duration, 0, b.N)

	// Create a map of ready pods for MatchPrefix
	readyPods := make(map[string]struct{})
	for i := 0; i < 255; i++ {
		readyPods[fmt.Sprintf("10.0.0.%d", i)] = struct{}{}
	}

	for i := 0; i < b.N; i++ {
		tokens := testSequences[i%len(testSequences)]
		byteTokens := convertTokensToBytes(tokens)
		
		start := time.Now()
		
		// Simulate routing decision
		matches, _ := indexer.MatchPrefix("test-model", -1, byteTokens, readyPods)
		if len(matches) > 0 {
			// In real scenario, this would affect routing
			_ = matches
		}
		
		latency := time.Since(start)
		latencies = append(latencies, latency)
	}

	avgLatency := calculateAverageLatency(latencies)
	p99Latency := calculatePercentileLatency(latencies, 99)

	b.ReportMetric(float64(avgLatency.Microseconds()), "avg_routing_latency_us")
	b.ReportMetric(float64(p99Latency.Microseconds()), "p99_routing_latency_us")
}

// SaveBenchmarkResults saves benchmark results to a file
func SaveBenchmarkResults(results []BenchmarkResult, filename string) error {
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal results: %w", err)
	}

	return ioutil.WriteFile(filename, data, 0644)
}

// LoadBaselineMetrics loads baseline metrics from a file
func LoadBaselineMetrics(filename string) (map[string]BenchmarkMetrics, error) {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return nil, nil // No baseline exists
	}

	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read baseline: %w", err)
	}

	var results []BenchmarkResult
	if err := json.Unmarshal(data, &results); err != nil {
		return nil, fmt.Errorf("failed to unmarshal baseline: %w", err)
	}

	baseline := make(map[string]BenchmarkMetrics)
	for _, result := range results {
		baseline[result.Name] = result.Metrics
	}

	return baseline, nil
}

// CompareWithBaseline compares current results with baseline
func CompareWithBaseline(current BenchmarkMetrics, baseline BenchmarkMetrics) (bool, string) {
	const threshold = 0.1 // 10% regression threshold

	var regressions []string

	// Check latency regression
	if current.EventProcessingLatency > time.Duration(float64(baseline.EventProcessingLatency)*(1+threshold)) {
		regressions = append(regressions, fmt.Sprintf(
			"Event processing latency regressed by %.1f%% (baseline: %v, current: %v)",
			(float64(current.EventProcessingLatency)/float64(baseline.EventProcessingLatency)-1)*100,
			baseline.EventProcessingLatency,
			current.EventProcessingLatency,
		))
	}

	// Check throughput regression
	if current.EventsPerSecond < baseline.EventsPerSecond*(1-threshold) {
		regressions = append(regressions, fmt.Sprintf(
			"Throughput regressed by %.1f%% (baseline: %.0f eps, current: %.0f eps)",
			(1-current.EventsPerSecond/baseline.EventsPerSecond)*100,
			baseline.EventsPerSecond,
			current.EventsPerSecond,
		))
	}

	// Check memory regression
	if current.MemoryUsageMB > baseline.MemoryUsageMB*(1+threshold) {
		regressions = append(regressions, fmt.Sprintf(
			"Memory usage increased by %.1f%% (baseline: %.1f MB, current: %.1f MB)",
			(current.MemoryUsageMB/baseline.MemoryUsageMB-1)*100,
			baseline.MemoryUsageMB,
			current.MemoryUsageMB,
		))
	}

	if len(regressions) > 0 {
		return false, fmt.Sprintf("Performance regressions detected:\n%s", 
			joinStrings(regressions, "\n"))
	}

	return true, "No performance regressions detected"
}

// Helper functions

// convertTokensToBytes converts int32 tokens to byte representation
func convertTokensToBytes(tokens []int32) []byte {
	bytes := make([]byte, len(tokens)*4)
	for i, token := range tokens {
		bytes[i*4] = byte(token >> 24)
		bytes[i*4+1] = byte(token >> 16)
		bytes[i*4+2] = byte(token >> 8)
		bytes[i*4+3] = byte(token)
	}
	return bytes
}

func generateTokenSequence(length int) []int32 {
	tokens := make([]int32, length)
	for i := 0; i < length; i++ {
		tokens[i] = int32(1000 + i)
	}
	return tokens
}

func calculateAverageLatency(latencies []time.Duration) time.Duration {
	if len(latencies) == 0 {
		return 0
	}

	sum := time.Duration(0)
	for _, l := range latencies {
		sum += l
	}
	return sum / time.Duration(len(latencies))
}

func calculatePercentileLatency(latencies []time.Duration, percentile int) time.Duration {
	if len(latencies) == 0 {
		return 0
	}

	// Simple percentile calculation (not perfectly accurate but good enough)
	index := len(latencies) * percentile / 100
	if index >= len(latencies) {
		index = len(latencies) - 1
	}
	
	return latencies[index]
}

func joinStrings(strs []string, sep string) string {
	result := ""
	for i, s := range strs {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}