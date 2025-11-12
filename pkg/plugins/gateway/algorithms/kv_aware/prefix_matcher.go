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
	"encoding/binary"
	"fmt"

	"k8s.io/klog/v2"
)

// PrefixIndexer defines the interface for the underlying prefix indexer
type PrefixIndexer interface {
	// MatchPrefix matches the input token prefix if already cached
	// returns map[podname]%prefixmatch along with all prefix hashes
	MatchPrefix(modelName string, loraID int64, tokens []byte, readyPods map[string]struct{}) (map[string]int, []uint64)
}

// PrefixMatcher defines the interface for prefix matching operations
type PrefixMatcher interface {
	// ComputePrefixMatch computes prefix matching for given tokens and pods
	ComputePrefixMatch(
		modelName string,
		loraID int64,
		tokens []int,
		readyPods []PodRef,
		blockSize int,
	) (PrefixMatch, error)
}

// indexerPrefixMatcher implements PrefixMatcher using PrefixIndexer
type indexerPrefixMatcher struct {
	indexer   PrefixIndexer
	tokenizer Tokenizer
}

// NewPrefixMatcher creates a new PrefixMatcher instance
func NewPrefixMatcher(indexer PrefixIndexer, tokenizer Tokenizer) PrefixMatcher {
	return &indexerPrefixMatcher{
		indexer:   indexer,
		tokenizer: tokenizer,
	}
}

// ComputePrefixMatch is the main orchestration function
func (m *indexerPrefixMatcher) ComputePrefixMatch(
	modelName string,
	loraID int64,
	tokens []int,
	readyPods []PodRef,
	blockSize int,
) (PrefixMatch, error) {
	// Validate inputs
	if len(tokens) == 0 {
		return PrefixMatch{}, fmt.Errorf("empty token sequence")
	}
	if len(readyPods) == 0 {
		return PrefixMatch{}, fmt.Errorf("no ready pods provided")
	}
	if blockSize <= 0 {
		return PrefixMatch{}, fmt.Errorf("invalid block size: %d", blockSize)
	}

	klog.V(4).Infof("Computing prefix match: model=%s, lora=%d, tokens=%d, pods=%d, blockSize=%d",
		modelName, loraID, len(tokens), len(readyPods), blockSize)

	// Step 1: Convert tokens to bytes (4 bytes per token, little-endian)
	tokenBytes := tokensToBytes(tokens)
	klog.V(5).Infof("Converted %d tokens to %d bytes", len(tokens), len(tokenBytes))

	// Step 2: Create ready pods map for indexer
	readyPodsMap := createReadyPodsMap(readyPods)
	klog.V(5).Infof("Created ready pods map with %d pods", len(readyPodsMap))

	// Step 3: Query indexer for prefix matches (returns percentages and hashes)
	podPercentages, prefixHashes := m.indexer.MatchPrefix(modelName, loraID, tokenBytes, readyPodsMap)
	klog.V(4).Infof("Indexer returned %d pod matches with %d prefix hashes",
		len(podPercentages), len(prefixHashes))

	// Step 4: Convert percentages to block counts
	totalBlocks := len(tokens) / blockSize
	podPrefixBlocks := convertPercentagesToBlocks(podPercentages, totalBlocks)

	// Step 5: Find best match
	bestPod, bestBlocks := findBestMatch(podPrefixBlocks)

	// Step 6: Log results (at V4 for visibility)
	logMatchResults(modelName, loraID, totalBlocks, podPrefixBlocks, bestPod, bestBlocks)

	// Step 7: Construct result
	result := PrefixMatch{
		PodPrefixBlocks: podPrefixBlocks,
		PrefixHashes:    prefixHashes,
		BestPod:         bestPod,
		BestBlocks:      bestBlocks,
	}

	return result, nil
}

// tokensToBytes converts token IDs to byte representation (4 bytes per token, little-endian)
func tokensToBytes(tokens []int) []byte {
	// Each token is encoded as 4 bytes (uint32), little-endian
	bytes := make([]byte, len(tokens)*4)
	for i, token := range tokens {
		binary.LittleEndian.PutUint32(bytes[i*4:], uint32(token))
	}
	return bytes
}

// createReadyPodsMap converts PodRef slice to map[podKey]struct{} for indexer
func createReadyPodsMap(readyPods []PodRef) map[string]struct{} {
	readyPodsMap := make(map[string]struct{}, len(readyPods))
	for _, pod := range readyPods {
		readyPodsMap[pod.Key()] = struct{}{}
	}
	return readyPodsMap
}

// convertPercentagesToBlocks converts percentage matches to block counts
func convertPercentagesToBlocks(podPercentages map[string]int, totalBlocks int) map[string]int {
	podPrefixBlocks := make(map[string]int, len(podPercentages))
	for podKey, percentage := range podPercentages {
		// percentage is in [0, 100]
		// blocks = (percentage * totalBlocks) / 100
		blocks := (percentage * totalBlocks) / 100
		podPrefixBlocks[podKey] = blocks

		klog.V(5).Infof("Pod %s: %d%% match = %d blocks (total: %d)",
			podKey, percentage, blocks, totalBlocks)
	}
	return podPrefixBlocks
}

// findBestMatch finds the pod with the most prefix blocks
func findBestMatch(podPrefixBlocks map[string]int) (string, int) {
	bestPod := ""
	bestBlocks := 0

	for podKey, blocks := range podPrefixBlocks {
		if blocks > bestBlocks {
			bestPod = podKey
			bestBlocks = blocks
		}
	}

	return bestPod, bestBlocks
}

// logMatchResults logs the prefix matching results
func logMatchResults(modelName string, loraID int64, totalBlocks int, podPrefixBlocks map[string]int, bestPod string, bestBlocks int) {
	if len(podPrefixBlocks) == 0 {
		klog.V(4).Infof("Prefix match result: model=%s lora=%d totalBlocks=%d NO MATCHES",
			modelName, loraID, totalBlocks)
		return
	}

	klog.V(4).Infof("Prefix match result: model=%s lora=%d totalBlocks=%d matches=%d best=%s (%d blocks)",
		modelName, loraID, totalBlocks, len(podPrefixBlocks), bestPod, bestBlocks)

	// Log individual pod matches at V5 verbosity
	for podKey, blocks := range podPrefixBlocks {
		percentage := 0
		if totalBlocks > 0 {
			percentage = (blocks * 100) / totalBlocks
		}
		klog.V(5).Infof("  Pod %s: %d blocks (%d%%)", podKey, blocks, percentage)
	}
}
