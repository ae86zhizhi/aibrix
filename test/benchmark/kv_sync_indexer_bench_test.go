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
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vllm-project/aibrix/pkg/utils/syncprefixcacheindexer"
)

// BenchmarkSyncIndexerAddRequest measures the performance of adding requests
func BenchmarkSyncIndexerAddRequest(b *testing.B) {
	indexer := syncprefixcacheindexer.NewSyncPrefixHashTable()
	defer indexer.Close()

	// Prepare token sequences
	tokens := make([][]byte, 1000)
	for i := 0; i < 1000; i++ {
		tokens[i] = generateByteTokens(100)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		tokenSeq := tokens[i%len(tokens)]
		podIP := fmt.Sprintf("10.0.0.%d", (i%250)+1)

		event := syncprefixcacheindexer.BlockStored{
			BlockHashes: []int64{int64(i)},
			Tokens:      [][]byte{tokenSeq},
			ModelName:   "test-model",
			LoraID:      -1,
			SourcePod:   podIP,
		}
		err := indexer.ProcessBlockStored(event)
		if err != nil {
			b.Fatalf("Failed to process block stored: %v", err)
		}
	}
}

// BenchmarkSyncIndexerLookup measures the performance of token lookup
func BenchmarkSyncIndexerLookup(b *testing.B) {
	indexer := syncprefixcacheindexer.NewSyncPrefixHashTable()
	defer indexer.Close()

	// Pre-populate indexer
	for i := 0; i < 10000; i++ {
		tokens := generateByteTokens(50 + i%100)
		podIP := fmt.Sprintf("10.0.0.%d", (i%250)+1)
		event := syncprefixcacheindexer.BlockStored{
			BlockHashes: []int64{int64(i)},
			Tokens:      [][]byte{tokens},
			ModelName:   "test-model",
			LoraID:      -1,
			SourcePod:   podIP,
		}
		indexer.ProcessBlockStored(event)
	}

	// Prepare lookup sequences
	lookupTokens := make([][]byte, 100)
	for i := 0; i < 100; i++ {
		lookupTokens[i] = generateByteTokens(25 + i%50)
	}

	// Create ready pods map for lookup
	readyPods := make(map[string]struct{})
	for i := 0; i < 250; i++ {
		readyPods[fmt.Sprintf("10.0.0.%d", i+1)] = struct{}{}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		tokens := lookupTokens[i%len(lookupTokens)]
		_, _ = indexer.MatchPrefix("test-model", -1, tokens, readyPods)
	}
}

// BenchmarkSyncIndexerConcurrent measures concurrent performance
func BenchmarkSyncIndexerConcurrent(b *testing.B) {
	concurrencyLevels := []int{1, 10, 50, 100}

	for _, concurrency := range concurrencyLevels {
		b.Run(fmt.Sprintf("concurrency_%d", concurrency), func(b *testing.B) {
			indexer := syncprefixcacheindexer.NewSyncPrefixHashTable()
			defer indexer.Close()

			// Prepare token sequences
			tokenSequences := make([][]byte, 1000)
			for i := 0; i < 1000; i++ {
				tokenSequences[i] = generateByteTokens(100)
			}

			b.ResetTimer()

			var wg sync.WaitGroup
			processed := int64(0)
			errors := int64(0)

			// Start workers
			for w := 0; w < concurrency; w++ {
				wg.Add(1)
				go func(workerID int) {
					defer wg.Done()

					for i := 0; i < b.N/concurrency; i++ {
						tokens := tokenSequences[(workerID*100+i)%len(tokenSequences)]
						podIP := fmt.Sprintf("10.0.0.%d", (workerID%250)+1)

						event := syncprefixcacheindexer.BlockStored{
							BlockHashes: []int64{int64(workerID*1000000 + i)},
							Tokens:      [][]byte{tokens},
							ModelName:   "test-model",
							LoraID:      -1,
							SourcePod:   podIP,
						}
						err := indexer.ProcessBlockStored(event)
						if err != nil {
							atomic.AddInt64(&errors, 1)
						} else {
							atomic.AddInt64(&processed, 1)
						}
					}
				}(w)
			}

			wg.Wait()

			b.ReportMetric(float64(processed), "total_processed")
			b.ReportMetric(float64(errors), "total_errors")
		})
	}
}

// BenchmarkSyncIndexerMemoryUsage measures memory consumption
func BenchmarkSyncIndexerMemoryUsage(b *testing.B) {
	sizes := []struct {
		name     string
		maxSize  int
		numItems int
	}{
		{"small", 10 * 1024 * 1024, 1000},     // 10MB, 1K items
		{"medium", 100 * 1024 * 1024, 10000},  // 100MB, 10K items
		{"large", 1024 * 1024 * 1024, 100000}, // 1GB, 100K items
	}

	for _, size := range sizes {
		b.Run(size.name, func(b *testing.B) {
			// Force GC before test
			runtime.GC()
			runtime.GC()

			var initialMem runtime.MemStats
			runtime.ReadMemStats(&initialMem)

			indexer := syncprefixcacheindexer.NewSyncPrefixHashTable()
			defer indexer.Close()

			// Fill indexer
			for i := 0; i < size.numItems; i++ {
				tokens := generateByteTokens(100 + i%200)
				podIP := fmt.Sprintf("10.0.0.%d", (i%250)+1)

				event := syncprefixcacheindexer.BlockStored{
					BlockHashes: []int64{int64(i)},
					Tokens:      [][]byte{tokens},
					ModelName:   "test-model",
					LoraID:      -1,
					SourcePod:   podIP,
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

// BenchmarkSyncIndexerLargePrefix tests performance with large token sequences
func BenchmarkSyncIndexerLargePrefix(b *testing.B) {
	prefixSizes := []int{100, 1000, 5000, 10000}

	for _, size := range prefixSizes {
		b.Run(fmt.Sprintf("prefix_size_%d", size), func(b *testing.B) {
			indexer := syncprefixcacheindexer.NewSyncPrefixHashTable()
			defer indexer.Close()

			// Generate large token sequence
			tokens := generateByteTokens(size)

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				podIP := fmt.Sprintf("10.0.0.%d", (i%250)+1)

				event := syncprefixcacheindexer.BlockStored{
					BlockHashes: []int64{int64(i)},
					Tokens:      [][]byte{tokens},
					ModelName:   "test-model",
					LoraID:      -1,
					SourcePod:   podIP,
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

// Helper functions

// generateByteTokens generates a byte sequence representing tokens
func generateByteTokens(numTokens int) []byte {
	// Each token is 4 bytes (int32)
	bytes := make([]byte, numTokens*4)
	for i := 0; i < numTokens; i++ {
		token := int32(1000 + i)
		bytes[i*4] = byte(token >> 24)
		bytes[i*4+1] = byte(token >> 16)
		bytes[i*4+2] = byte(token >> 8)
		bytes[i*4+3] = byte(token)
	}
	return bytes
}