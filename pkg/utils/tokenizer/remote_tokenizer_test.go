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

package tokenizer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestNewRemoteTokenizer(t *testing.T) {
	tests := []struct {
		name          string
		config        RemoteTokenizerConfig
		expectedError bool
		errorContains string
	}{
		{
			name: "valid vllm config",
			config: RemoteTokenizerConfig{
				Engine:   "vllm",
				Endpoint: "http://localhost:8000",
				Timeout:  5 * time.Second,
			},
			expectedError: false,
		},
		{
			name: "missing engine",
			config: RemoteTokenizerConfig{
				Endpoint: "http://localhost:8000",
			},
			expectedError: true,
			errorContains: "engine is required",
		},
		{
			name: "missing endpoint",
			config: RemoteTokenizerConfig{
				Engine: "vllm",
			},
			expectedError: true,
			errorContains: "endpoint is required",
		},
		{
			name: "invalid endpoint URL",
			config: RemoteTokenizerConfig{
				Engine:   "vllm",
				Endpoint: "not-a-url",
			},
			expectedError: true,
			errorContains: "invalid endpoint URL",
		},
		{
			name: "unsupported engine",
			config: RemoteTokenizerConfig{
				Engine:   "unknown-engine",
				Endpoint: "http://localhost:8000",
			},
			expectedError: true,
			errorContains: "unsupported engine",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokenizer, err := NewRemoteTokenizer(tt.config)
			
			if tt.expectedError {
				if err == nil {
					t.Error("expected error but got none")
				} else if tt.errorContains != "" && !contains(err.Error(), tt.errorContains) {
					t.Errorf("expected error containing %q, got %q", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if tokenizer == nil {
					t.Error("expected tokenizer but got nil")
				}
			}
		})
	}
}

func TestRemoteTokenizer_TokenizeInputText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check request path
		if r.URL.Path != "/tokenize" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		// Parse request
		var req VLLMTokenizeCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Return mock response
		resp := VLLMTokenizeResponse{
			Count:       5,
			MaxModelLen: 4096,
			Tokens:      []int{101, 102, 103, 104, 105},
		}
		
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config := RemoteTokenizerConfig{
		Engine:     "vllm",
		Endpoint:   server.URL,
		Timeout:    5 * time.Second,
		MaxRetries: 2,
	}

	tokenizer, err := NewRemoteTokenizer(config)
	if err != nil {
		t.Fatalf("failed to create tokenizer: %v", err)
	}

	// Test tokenization
	result, err := tokenizer.TokenizeInputText("test input")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify result
	expectedTokens := []int{101, 102, 103, 104, 105}
	expectedBytes := intToByteArray(expectedTokens)
	if !reflect.DeepEqual(result, expectedBytes) {
		t.Errorf("expected tokens %v as bytes, got %v", expectedTokens, result)
	}
}

func TestRemoteTokenizer_TokenizeWithOptions(t *testing.T) {
	tests := []struct {
		name           string
		input          TokenizeInput
		serverResponse VLLMTokenizeResponse
		expectedTokens []int
		expectedError  bool
	}{
		{
			name: "completion input",
			input: TokenizeInput{
				Type:             CompletionInput,
				Text:             "Hello world",
				AddSpecialTokens: true,
			},
			serverResponse: VLLMTokenizeResponse{
				Count:  3,
				Tokens: []int{1, 2, 3},
			},
			expectedTokens: []int{1, 2, 3},
			expectedError:  false,
		},
		{
			name: "chat input",
			input: TokenizeInput{
				Type: ChatInput,
				Messages: []ChatMessage{
					{Role: "user", Content: "Hello"},
					{Role: "assistant", Content: "Hi there!"},
				},
				AddSpecialTokens: true,
			},
			serverResponse: VLLMTokenizeResponse{
				Count:  5,
				Tokens: []int{10, 20, 30, 40, 50},
			},
			expectedTokens: []int{10, 20, 30, 40, 50},
			expectedError:  false,
		},
		{
			name: "with token strings",
			input: TokenizeInput{
				Type:               CompletionInput,
				Text:               "test",
				ReturnTokenStrings: true,
			},
			serverResponse: VLLMTokenizeResponse{
				Count:       2,
				Tokens:      []int{100, 200},
				TokenStrs:   []string{"te", "st"},
			},
			expectedTokens: []int{100, 200},
			expectedError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Determine expected path based on input type
				expectedPath := "/tokenize"
				if tt.input.Type == ChatInput {
					expectedPath = "/tokenize"
				}

				if r.URL.Path != expectedPath {
					t.Errorf("unexpected path: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}

				// Return mock response
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(tt.serverResponse)
			}))
			defer server.Close()

			config := RemoteTokenizerConfig{
				Engine:   "vllm",
				Endpoint: server.URL,
				Timeout:  5 * time.Second,
			}

			tokenizer, err := NewRemoteTokenizer(config)
			if err != nil {
				t.Fatalf("failed to create tokenizer: %v", err)
			}

			// Test tokenization
			result, err := tokenizer.TokenizeWithOptions(context.Background(), tt.input)
			
			if tt.expectedError {
				if err == nil {
					t.Error("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				
				// Check tokens
				if !reflect.DeepEqual(result.Tokens, tt.expectedTokens) {
					t.Errorf("expected tokens %v, got %v", tt.expectedTokens, result.Tokens)
				}
				
				// Check token strings if requested
				if tt.input.ReturnTokenStrings && tt.serverResponse.TokenStrs != nil {
					if !reflect.DeepEqual(result.TokenStrings, tt.serverResponse.TokenStrs) {
						t.Errorf("expected token strings %v, got %v", 
							tt.serverResponse.TokenStrs, result.TokenStrings)
					}
				}
			}
		})
	}
}

func TestRemoteTokenizer_Detokenize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/detokenize" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		// Parse request
		var req VLLMDetokenizeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Return mock response based on input
		resp := VLLMDetokenizeResponse{
			Prompt: "Hello world",
		}
		
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config := RemoteTokenizerConfig{
		Engine:   "vllm",
		Endpoint: server.URL,
		Timeout:  5 * time.Second,
	}

	tokenizer, err := NewRemoteTokenizer(config)
	if err != nil {
		t.Fatalf("failed to create tokenizer: %v", err)
	}

	// Test detokenization
	tokens := []int{100, 200, 300}
	
	result, err := tokenizer.Detokenize(context.Background(), tokens)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	expectedText := "Hello world"
	if result != expectedText {
		t.Errorf("expected text %q, got %q", expectedText, result)
	}
}

func TestRemoteTokenizer_IsHealthy(t *testing.T) {
	tests := []struct {
		name           string
		serverHandler  http.HandlerFunc
		expectedHealth bool
	}{
		{
			name: "healthy service",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := VLLMTokenizeResponse{
					Count:  1,
					Tokens: []int{1},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			},
			expectedHealth: true,
		},
		{
			name: "unhealthy service",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			expectedHealth: false,
		},
		{
			name: "timeout",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(200 * time.Millisecond)
				w.WriteHeader(http.StatusOK)
			},
			expectedHealth: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.serverHandler)
			defer server.Close()

			config := RemoteTokenizerConfig{
				Engine:   "vllm",
				Endpoint: server.URL,
				Timeout:  100 * time.Millisecond,
			}

			tokenizer, err := NewRemoteTokenizer(config)
			if err != nil {
				t.Fatalf("failed to create tokenizer: %v", err)
			}

			ctx := context.Background()
			healthy := tokenizer.IsHealthy(ctx)
			
			if healthy != tt.expectedHealth {
				t.Errorf("expected health status %v, got %v", tt.expectedHealth, healthy)
			}
		})
	}
}

func TestRemoteTokenizer_ConcurrentRequests(t *testing.T) {
	requestCount := 0
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()

		// Simulate some processing time
		time.Sleep(10 * time.Millisecond)

		resp := VLLMTokenizeResponse{
			Count:  3,
			Tokens: []int{1, 2, 3},
		}
		
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config := RemoteTokenizerConfig{
		Engine:         "vllm",
		Endpoint:       server.URL,
		Timeout:        5 * time.Second,
		MaxConcurrency: 5, // Limit concurrent requests
	}

	tokenizer, err := NewRemoteTokenizer(config)
	if err != nil {
		t.Fatalf("failed to create tokenizer: %v", err)
	}

	// Launch concurrent tokenization requests
	numRequests := 20
	var wg sync.WaitGroup
	errors := make([]error, numRequests)

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := tokenizer.TokenizeInputText("test input")
			errors[idx] = err
		}(i)
	}

	wg.Wait()

	// Check for errors
	errorCount := 0
	for i, err := range errors {
		if err != nil {
			t.Errorf("request %d failed: %v", i, err)
			errorCount++
		}
	}

	if errorCount > 0 {
		t.Errorf("%d out of %d requests failed", errorCount, numRequests)
	}

	// Verify all requests were processed
	if requestCount != numRequests {
		t.Errorf("expected %d requests, got %d", numRequests, requestCount)
	}
}

func TestRemoteTokenizer_DifferentEngines(t *testing.T) {
	engines := []struct {
		name              string
		engine            string
		completionPath    string
		chatPath          string
		detokenizePath    string
	}{
		{
			name:           "vllm",
			engine:         "vllm",
			completionPath: "/tokenize",
			chatPath:       "/tokenize",
			detokenizePath: "/detokenize",
		},
	}

	for _, eng := range engines {
		t.Run(eng.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Check that the path matches expected for the engine
				validPath := false
				if r.URL.Path == eng.completionPath || 
				   r.URL.Path == eng.chatPath || 
				   r.URL.Path == eng.detokenizePath {
					validPath = true
				}

				if !validPath {
					t.Errorf("unexpected path %s for engine %s", r.URL.Path, eng.engine)
					w.WriteHeader(http.StatusNotFound)
					return
				}

				// Return appropriate response
				if r.URL.Path == eng.detokenizePath {
					resp := VLLMDetokenizeResponse{Prompt: "test"}
					json.NewEncoder(w).Encode(resp)
				} else {
					resp := VLLMTokenizeResponse{
						Count:  1,
						Tokens: []int{100},
					}
					json.NewEncoder(w).Encode(resp)
				}
			}))
			defer server.Close()

			config := RemoteTokenizerConfig{
				Engine:   eng.engine,
				Endpoint: server.URL,
				Timeout:  5 * time.Second,
			}

			tokenizer, err := NewRemoteTokenizer(config)
			if err != nil {
				t.Fatalf("failed to create tokenizer: %v", err)
			}

			// Test basic tokenization
			_, err = tokenizer.TokenizeInputText("test")
			if err != nil {
				t.Errorf("tokenization failed for engine %s: %v", eng.engine, err)
			}
		})
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr || 
		   len(s) > len(substr) && contains(s[1:], substr)
}