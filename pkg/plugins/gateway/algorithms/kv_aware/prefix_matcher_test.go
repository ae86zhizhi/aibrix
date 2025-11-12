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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/vllm-project/aibrix/pkg/types"
)

// Mock indexer for testing
type mockIndexer struct {
	mock.Mock
}

func (m *mockIndexer) MatchPrefix(modelName string, loraID int64, tokens []byte, readyPods map[string]struct{}) (map[string]int, []uint64) {
	args := m.Called(modelName, loraID, tokens, readyPods)
	return args.Get(0).(map[string]int), args.Get(1).([]uint64)
}

// Mock tokenizer for testing
type mockTokenizer struct {
	mock.Mock
}

func (m *mockTokenizer) Tokenize(prompt string, modelName string) ([]int, error) {
	args := m.Called(prompt, modelName)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]int), args.Error(1)
}

// TestTokensToBytes tests the token to bytes conversion
func TestTokensToBytes(t *testing.T) {
	tests := []struct {
		name     string
		tokens   []int
		expected []byte
	}{
		{
			name:     "Empty tokens",
			tokens:   []int{},
			expected: []byte{},
		},
		{
			name:   "Single token",
			tokens: []int{123},
			expected: func() []byte {
				b := make([]byte, 4)
				binary.LittleEndian.PutUint32(b, 123)
				return b
			}(),
		},
		{
			name:   "Multiple tokens",
			tokens: []int{1, 2, 3, 4},
			expected: func() []byte {
				b := make([]byte, 16)
				binary.LittleEndian.PutUint32(b[0:], 1)
				binary.LittleEndian.PutUint32(b[4:], 2)
				binary.LittleEndian.PutUint32(b[8:], 3)
				binary.LittleEndian.PutUint32(b[12:], 4)
				return b
			}(),
		},
		{
			name:   "Large token values",
			tokens: []int{65535, 1000000},
			expected: func() []byte {
				b := make([]byte, 8)
				binary.LittleEndian.PutUint32(b[0:], 65535)
				binary.LittleEndian.PutUint32(b[4:], 1000000)
				return b
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tokensToBytes(tt.tokens)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestCreateReadyPodsMap tests the ready pods map creation
func TestCreateReadyPodsMap(t *testing.T) {
	tests := []struct {
		name      string
		readyPods []PodRef
		expected  map[string]struct{}
	}{
		{
			name:      "Empty pods",
			readyPods: []PodRef{},
			expected:  map[string]struct{}{},
		},
		{
			name: "Single pod",
			readyPods: []PodRef{
				{Namespace: "default", Name: "pod1"},
			},
			expected: map[string]struct{}{
				"default/pod1": {},
			},
		},
		{
			name: "Multiple pods",
			readyPods: []PodRef{
				{Namespace: "default", Name: "pod1"},
				{Namespace: "default", Name: "pod2"},
				{Namespace: "test", Name: "pod3"},
			},
			expected: map[string]struct{}{
				"default/pod1": {},
				"default/pod2": {},
				"test/pod3":    {},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := createReadyPodsMap(tt.readyPods)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestConvertPercentagesToBlocks tests percentage to block conversion
func TestConvertPercentagesToBlocks(t *testing.T) {
	tests := []struct {
		name           string
		podPercentages map[string]int
		totalBlocks    int
		expected       map[string]int
	}{
		{
			name:           "Empty pods",
			podPercentages: map[string]int{},
			totalBlocks:    100,
			expected:       map[string]int{},
		},
		{
			name: "100% match",
			podPercentages: map[string]int{
				"pod1": 100,
			},
			totalBlocks: 10,
			expected: map[string]int{
				"pod1": 10,
			},
		},
		{
			name: "50% match",
			podPercentages: map[string]int{
				"pod1": 50,
			},
			totalBlocks: 20,
			expected: map[string]int{
				"pod1": 10,
			},
		},
		{
			name: "Multiple pods with different percentages",
			podPercentages: map[string]int{
				"pod1": 75,
				"pod2": 50,
				"pod3": 25,
			},
			totalBlocks: 100,
			expected: map[string]int{
				"pod1": 75,
				"pod2": 50,
				"pod3": 25,
			},
		},
		{
			name: "Rounding down (integer division)",
			podPercentages: map[string]int{
				"pod1": 33,
			},
			totalBlocks: 10,
			expected: map[string]int{
				"pod1": 3, // 33 * 10 / 100 = 3.3 → 3
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertPercentagesToBlocks(tt.podPercentages, tt.totalBlocks)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestFindBestMatch tests finding the best matching pod
func TestFindBestMatch(t *testing.T) {
	tests := []struct {
		name            string
		podPrefixBlocks map[string]int
		expectedPod     string
		expectedBlocks  int
	}{
		{
			name:            "Empty pods",
			podPrefixBlocks: map[string]int{},
			expectedPod:     "",
			expectedBlocks:  0,
		},
		{
			name: "Single pod",
			podPrefixBlocks: map[string]int{
				"pod1": 5,
			},
			expectedPod:    "pod1",
			expectedBlocks: 5,
		},
		{
			name: "Multiple pods - clear winner",
			podPrefixBlocks: map[string]int{
				"pod1": 10,
				"pod2": 5,
				"pod3": 3,
			},
			expectedPod:    "pod1",
			expectedBlocks: 10,
		},
		{
			name: "Zero blocks",
			podPrefixBlocks: map[string]int{
				"pod1": 0,
				"pod2": 0,
			},
			expectedPod:    "",
			expectedBlocks: 0,
		},
		{
			name: "Mixed zeros and non-zeros",
			podPrefixBlocks: map[string]int{
				"pod1": 0,
				"pod2": 7,
				"pod3": 0,
			},
			expectedPod:    "pod2",
			expectedBlocks: 7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod, blocks := findBestMatch(tt.podPrefixBlocks)
			assert.Equal(t, tt.expectedPod, pod)
			assert.Equal(t, tt.expectedBlocks, blocks)
		})
	}
}

// TestComputePrefixMatch tests the main prefix matching function
func TestComputePrefixMatch(t *testing.T) {
	tests := []struct {
		name               string
		modelName          string
		loraID             int64
		tokens             []int
		readyPods          []PodRef
		blockSize          int
		mockPercentages    map[string]int
		mockHashes         []uint64
		expectedBestPod    string
		expectedBestBlocks int
		expectError        bool
	}{
		{
			name:      "Successful match with single pod",
			modelName: "llama3-70b",
			loraID:    -1,
			tokens:    []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
			readyPods: []PodRef{
				{Namespace: "default", Name: "pod1"},
			},
			blockSize: 16,
			mockPercentages: map[string]int{
				"default/pod1": 100,
			},
			mockHashes:         []uint64{123456789},
			expectedBestPod:    "default/pod1",
			expectedBestBlocks: 1,
			expectError:        false,
		},
		{
			name:      "Multiple pods with different matches",
			modelName: "llama3-70b",
			loraID:    -1,
			tokens:    make([]int, 64), // 64 tokens = 4 blocks
			readyPods: []PodRef{
				{Namespace: "default", Name: "pod1"},
				{Namespace: "default", Name: "pod2"},
				{Namespace: "default", Name: "pod3"},
			},
			blockSize: 16,
			mockPercentages: map[string]int{
				"default/pod1": 75,
				"default/pod2": 50,
				"default/pod3": 25,
			},
			mockHashes:         []uint64{111, 222, 333, 444},
			expectedBestPod:    "default/pod1",
			expectedBestBlocks: 3,
			expectError:        false,
		},
		{
			name:      "No matches (0% for all pods)",
			modelName: "llama3-70b",
			loraID:    -1,
			tokens:    make([]int, 32),
			readyPods: []PodRef{
				{Namespace: "default", Name: "pod1"},
			},
			blockSize: 16,
			mockPercentages: map[string]int{
				"default/pod1": 0,
			},
			mockHashes:         []uint64{999},
			expectedBestPod:    "",
			expectedBestBlocks: 0,
			expectError:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock indexer
			mockIdx := &mockIndexer{}
			mockTok := &mockTokenizer{}

			// Create prefix matcher
			matcher := NewPrefixMatcher(mockIdx, mockTok)

			// Convert tokens to bytes for mock expectation
			expectedBytes := tokensToBytes(tt.tokens)
			expectedReadyMap := createReadyPodsMap(tt.readyPods)

			// Set up mock expectations
			mockIdx.On("MatchPrefix", tt.modelName, tt.loraID, expectedBytes, expectedReadyMap).
				Return(tt.mockPercentages, tt.mockHashes)

			// Call the function
			result, err := matcher.ComputePrefixMatch(tt.modelName, tt.loraID, tt.tokens, tt.readyPods, tt.blockSize)

			// Verify results
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedBestPod, result.BestPod)
				assert.Equal(t, tt.expectedBestBlocks, result.BestBlocks)
				assert.Equal(t, tt.mockHashes, result.PrefixHashes)
				assert.Equal(t, len(tt.mockPercentages), len(result.PodPrefixBlocks))
			}

			mockIdx.AssertExpectations(t)
		})
	}
}

// TestComputePrefixMatch_ErrorCases tests error handling
func TestComputePrefixMatch_ErrorCases(t *testing.T) {
	tests := []struct {
		name      string
		modelName string
		loraID    int64
		tokens    []int
		readyPods []PodRef
		blockSize int
		errorMsg  string
	}{
		{
			name:      "Empty tokens",
			modelName: "llama3-70b",
			loraID:    -1,
			tokens:    []int{},
			readyPods: []PodRef{{Namespace: "default", Name: "pod1"}},
			blockSize: 16,
			errorMsg:  "empty token sequence",
		},
		{
			name:      "No ready pods",
			modelName: "llama3-70b",
			loraID:    -1,
			tokens:    []int{1, 2, 3},
			readyPods: []PodRef{},
			blockSize: 16,
			errorMsg:  "no ready pods provided",
		},
		{
			name:      "Invalid block size (zero)",
			modelName: "llama3-70b",
			loraID:    -1,
			tokens:    []int{1, 2, 3},
			readyPods: []PodRef{{Namespace: "default", Name: "pod1"}},
			blockSize: 0,
			errorMsg:  "invalid block size: 0",
		},
		{
			name:      "Invalid block size (negative)",
			modelName: "llama3-70b",
			loraID:    -1,
			tokens:    []int{1, 2, 3},
			readyPods: []PodRef{{Namespace: "default", Name: "pod1"}},
			blockSize: -5,
			errorMsg:  "invalid block size: -5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockIdx := &mockIndexer{}
			mockTok := &mockTokenizer{}
			matcher := NewPrefixMatcher(mockIdx, mockTok)

			_, err := matcher.ComputePrefixMatch(tt.modelName, tt.loraID, tt.tokens, tt.readyPods, tt.blockSize)

			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.errorMsg)
		})
	}
}

// TestExtractLoraID tests LoRA ID extraction from headers
func TestExtractLoraID(t *testing.T) {
	tests := []struct {
		name       string
		headers    map[string]string
		expectedID int64
	}{
		{
			name:       "No headers",
			headers:    nil,
			expectedID: -1,
		},
		{
			name:       "Empty headers",
			headers:    map[string]string{},
			expectedID: -1,
		},
		{
			name: "X-LoRA-ID header present",
			headers: map[string]string{
				"X-LoRA-ID": "12345",
			},
			expectedID: 12345,
		},
		{
			name: "X-Lora-Adapter-Id header present",
			headers: map[string]string{
				"X-Lora-Adapter-Id": "67890",
			},
			expectedID: 67890,
		},
		{
			name: "Both headers present (X-LoRA-ID takes precedence)",
			headers: map[string]string{
				"X-LoRA-ID":         "111",
				"X-Lora-Adapter-Id": "222",
			},
			expectedID: 111,
		},
		{
			name: "Invalid LoRA ID format",
			headers: map[string]string{
				"X-LoRA-ID": "invalid",
			},
			expectedID: -1,
		},
		{
			name: "Empty LoRA ID value",
			headers: map[string]string{
				"X-LoRA-ID": "",
			},
			expectedID: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a real routing context and set headers
			ctx := &types.RoutingContext{
				ReqHeaders: tt.headers,
			}

			// Create router with minimal config
			router := &kvAwareRouter{
				config: &KVAwareConfig{},
			}

			// Extract LoRA ID
			result := router.extractLoraID(ctx)
			assert.Equal(t, tt.expectedID, result)
		})
	}
}

// TestTokenizer tests the tokenizer implementation
func TestTokenizer(t *testing.T) {
	tests := []struct {
		name        string
		prompt      string
		modelName   string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "Empty prompt",
			prompt:      "",
			modelName:   "llama3-70b",
			expectError: true,
			errorMsg:    "empty prompt",
		},
		{
			name:        "Empty model name",
			prompt:      "Hello world",
			modelName:   "",
			expectError: true,
			errorMsg:    "empty model name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokenizer := NewTokenizer()
			_, err := tokenizer.Tokenize(tt.prompt, tt.modelName)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				// Note: We can't test successful tokenization without a real tokenizer
				// This would require integration testing with utils.TokenizeInputText
				assert.NoError(t, err)
			}
		})
	}
}

// Benchmark tests

func BenchmarkTokensToBytes(b *testing.B) {
	tokens := make([]int, 1000)
	for i := range tokens {
		tokens[i] = i
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tokensToBytes(tokens)
	}
}

func BenchmarkConvertPercentagesToBlocks(b *testing.B) {
	percentages := make(map[string]int)
	for i := 0; i < 100; i++ {
		percentages[fmt.Sprintf("pod%d", i)] = i
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = convertPercentagesToBlocks(percentages, 1000)
	}
}

func BenchmarkFindBestMatch(b *testing.B) {
	blocks := make(map[string]int)
	for i := 0; i < 100; i++ {
		blocks[fmt.Sprintf("pod%d", i)] = i
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = findBestMatch(blocks)
	}
}
