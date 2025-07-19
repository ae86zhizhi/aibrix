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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPClient_Post(t *testing.T) {
	tests := []struct {
		name           string
		serverHandler  http.HandlerFunc
		config         HTTPClientConfig
		request        map[string]string
		expectedResult []byte
		expectedError  bool
	}{
		{
			name: "successful request",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"result": "success"}`))
			},
			config: HTTPClientConfig{
				Timeout:    5 * time.Second,
				MaxRetries: 3,
			},
			request:        map[string]string{"input": "test"},
			expectedResult: []byte(`{"result": "success"}`),
			expectedError:  false,
		},
		{
			name: "server error with retry",
			serverHandler: func() http.HandlerFunc {
				var count int32
				return func(w http.ResponseWriter, r *http.Request) {
					n := atomic.AddInt32(&count, 1)
					if n < 3 {
						w.WriteHeader(http.StatusInternalServerError)
						w.Write([]byte("server error"))
					} else {
						w.WriteHeader(http.StatusOK)
						w.Write([]byte(`{"result": "success after retry"}`))
					}
				}
			}(),
			config: HTTPClientConfig{
				Timeout:    5 * time.Second,
				MaxRetries: 3,
			},
			request:        map[string]string{"input": "test"},
			expectedResult: []byte(`{"result": "success after retry"}`),
			expectedError:  false,
		},
		{
			name: "client error no retry",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte("bad request"))
			},
			config: HTTPClientConfig{
				Timeout:    5 * time.Second,
				MaxRetries: 3,
			},
			request:        map[string]string{"input": "test"},
			expectedResult: nil,
			expectedError:  true,
		},
		{
			name: "timeout error",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(100 * time.Millisecond)
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"result": "too late"}`))
			},
			config: HTTPClientConfig{
				Timeout:    50 * time.Millisecond,
				MaxRetries: 0,
			},
			request:        map[string]string{"input": "test"},
			expectedResult: nil,
			expectedError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.serverHandler)
			defer server.Close()

			client := NewHTTPClient(server.URL, tt.config)
			defer client.Close()

			result, err := client.Post(context.Background(), "/test", tt.request)

			if tt.expectedError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if string(result) != string(tt.expectedResult) {
					t.Errorf("expected result %s, got %s", tt.expectedResult, result)
				}
			}
		})
	}
}

func TestHTTPClient_Get(t *testing.T) {
	tests := []struct {
		name           string
		serverHandler  http.HandlerFunc
		config         HTTPClientConfig
		expectedResult []byte
		expectedError  bool
	}{
		{
			name: "successful GET request",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Errorf("expected GET method, got %s", r.Method)
				}
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"status": "healthy"}`))
			},
			config: HTTPClientConfig{
				Timeout: 5 * time.Second,
			},
			expectedResult: []byte(`{"status": "healthy"}`),
			expectedError:  false,
		},
		{
			name: "GET request with error",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
				w.Write([]byte("service unavailable"))
			},
			config: HTTPClientConfig{
				Timeout: 5 * time.Second,
			},
			expectedResult: nil,
			expectedError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.serverHandler)
			defer server.Close()

			client := NewHTTPClient(server.URL, tt.config)
			defer client.Close()

			result, err := client.Get(context.Background(), "/health")

			if tt.expectedError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if string(result) != string(tt.expectedResult) {
					t.Errorf("expected result %s, got %s", tt.expectedResult, result)
				}
			}
		})
	}
}

func TestHTTPClient_ConcurrencyControl(t *testing.T) {
	maxConcurrency := 5
	activeRequests := int32(0)
	maxActiveRequests := int32(0)

	// Create a server that tracks concurrent requests
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Increment active requests
		current := atomic.AddInt32(&activeRequests, 1)
		
		// Update max if necessary
		for {
			max := atomic.LoadInt32(&maxActiveRequests)
			if current <= max || atomic.CompareAndSwapInt32(&maxActiveRequests, max, current) {
				break
			}
		}

		// Simulate some work
		time.Sleep(50 * time.Millisecond)

		// Decrement active requests
		atomic.AddInt32(&activeRequests, -1)

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result": "success"}`))
	}))
	defer server.Close()

	config := HTTPClientConfig{
		Timeout:        5 * time.Second,
		MaxRetries:     0,
		MaxConcurrency: maxConcurrency,
	}

	client := NewHTTPClient(server.URL, config)
	defer client.Close()

	// Launch multiple concurrent requests
	numRequests := 20
	var wg sync.WaitGroup
	wg.Add(numRequests)

	for i := 0; i < numRequests; i++ {
		go func(id int) {
			defer wg.Done()
			request := map[string]int{"id": id}
			_, err := client.Post(context.Background(), "/test", request)
			if err != nil {
				t.Errorf("request %d failed: %v", id, err)
			}
		}(i)
	}

	wg.Wait()

	// Check that we never exceeded max concurrency
	if maxActiveRequests > int32(maxConcurrency) {
		t.Errorf("exceeded max concurrency: max active requests was %d, expected <= %d", 
			maxActiveRequests, maxConcurrency)
	}
}

func TestHTTPClient_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a slow server
		time.Sleep(1 * time.Second)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result": "success"}`))
	}))
	defer server.Close()

	config := HTTPClientConfig{
		Timeout:        5 * time.Second,
		MaxRetries:     0,
		MaxConcurrency: 10,
	}

	client := NewHTTPClient(server.URL, config)
	defer client.Close()

	// Create a context that will be cancelled
	ctx, cancel := context.WithCancel(context.Background())
	
	// Cancel context after a short delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	// Try to make a request
	_, err := client.Post(ctx, "/test", map[string]string{"input": "test"})
	
	// Should get a context cancelled error
	if err == nil {
		t.Error("expected error due to context cancellation, got none")
	}
	// Check if the error contains context.Canceled (it might be wrapped)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error, got: %v", err)
	}
}

func TestHTTPClient_ExponentialBackoff(t *testing.T) {
	requestTimes := []time.Time{}
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestTimes = append(requestTimes, time.Now())
		mu.Unlock()

		// Always return server error to trigger retries
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer server.Close()

	config := HTTPClientConfig{
		Timeout:        5 * time.Second,
		MaxRetries:     3,
		MaxConcurrency: 10,
	}

	client := NewHTTPClient(server.URL, config)
	defer client.Close()

	_, _ = client.Post(context.Background(), "/test", map[string]string{"input": "test"})

	// Should have made 4 requests (initial + 3 retries)
	if len(requestTimes) != 4 {
		t.Errorf("expected 4 requests, got %d", len(requestTimes))
	}

	// Check exponential backoff timing
	// Expected delays: 0ms, 100ms, 200ms, 400ms
	expectedDelays := []time.Duration{0, 100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond}
	
	for i := 1; i < len(requestTimes); i++ {
		actualDelay := requestTimes[i].Sub(requestTimes[i-1])
		expectedDelay := expectedDelays[i]
		
		// Allow for some timing variance (50ms tolerance)
		tolerance := 50 * time.Millisecond
		if actualDelay < expectedDelay-tolerance || actualDelay > expectedDelay+tolerance {
			t.Errorf("retry %d: expected delay ~%v, got %v", i, expectedDelay, actualDelay)
		}
	}
}

func TestHTTPClient_RequestBodyIntegrity(t *testing.T) {
	expectedBody := map[string]string{"input": "test data"}
	
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check content type
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", ct)
		}

		// Read and verify request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read request body: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		var received map[string]string
		if err := json.Unmarshal(body, &received); err != nil {
			t.Errorf("failed to unmarshal request body: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if received["input"] != expectedBody["input"] {
			t.Errorf("expected input %s, got %s", expectedBody["input"], received["input"])
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result": "success"}`))
	}))
	defer server.Close()

	config := HTTPClientConfig{
		Timeout:        5 * time.Second,
		MaxRetries:     0,
		MaxConcurrency: 10,
	}

	client := NewHTTPClient(server.URL, config)
	defer client.Close()

	_, err := client.Post(context.Background(), "/test", expectedBody)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHTTPClient_ErrorTypes(t *testing.T) {
	tests := []struct {
		name          string
		serverHandler http.HandlerFunc
		expectedType  error
		checkErrorType func(error) bool
	}{
		{
			name: "HTTP 4xx error",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte("bad request"))
			},
			checkErrorType: func(err error) bool {
				httpErr, ok := err.(ErrHTTPRequest)
				return ok && httpErr.StatusCode == http.StatusBadRequest
			},
		},
		{
			name: "HTTP 5xx error",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("internal server error"))
			},
			checkErrorType: func(err error) bool {
				// After retries, should be wrapped in fmt.Errorf
				return err != nil && fmt.Sprintf("%v", err) != ""
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.serverHandler)
			defer server.Close()

			config := HTTPClientConfig{
				Timeout:        5 * time.Second,
				MaxRetries:     2,
				MaxConcurrency: 10,
			}

			client := NewHTTPClient(server.URL, config)
			defer client.Close()

			_, err := client.Post(context.Background(), "/test", map[string]string{"input": "test"})
			
			if err == nil {
				t.Error("expected error but got none")
			} else if !tt.checkErrorType(err) {
				t.Errorf("error type check failed for error: %v", err)
			}
		})
	}
}