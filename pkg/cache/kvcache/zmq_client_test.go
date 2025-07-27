// Copyright 2025 The AIBrix Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kvcache

import (
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"testing"
	"time"

	zmq "github.com/pebbe/zmq4"
	msgpack "github.com/shamaton/msgpack/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockEventHandler implements EventHandler for testing
type MockEventHandler struct {
	mu           sync.Mutex
	events       []KVEvent
	handleErrors map[int]error // Map of event index to error
	handleDelay  time.Duration
}

func NewMockEventHandler() *MockEventHandler {
	return &MockEventHandler{
		events:       []KVEvent{},
		handleErrors: make(map[int]error),
	}
}

func (m *MockEventHandler) HandleEvent(event KVEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.handleDelay > 0 {
		time.Sleep(m.handleDelay)
	}

	eventIndex := len(m.events)
	if err, ok := m.handleErrors[eventIndex]; ok {
		return err
	}

	m.events = append(m.events, event)
	return nil
}

func (m *MockEventHandler) GetEvents() []KVEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	events := make([]KVEvent, len(m.events))
	copy(events, m.events)
	return events
}

func (m *MockEventHandler) SetHandleError(eventIndex int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handleErrors[eventIndex] = err
}

func TestZMQClientConfig(t *testing.T) {
	config := DefaultZMQClientConfig("test-pod", "10.0.0.1", "test-model")

	assert.Equal(t, "test-pod", config.PodKey)
	assert.Equal(t, "10.0.0.1", config.PodIP)
	assert.Equal(t, "test-model", config.ModelName)
	assert.Equal(t, DefaultPubPort, config.PubPort)
	assert.Equal(t, DefaultRouterPort, config.RouterPort)
	assert.Equal(t, DefaultPollTimeout, config.PollTimeout)
	assert.Equal(t, DefaultReplayTimeout, config.ReplayTimeout)
	assert.Equal(t, DefaultReconnectInterval, config.ReconnectDelay)
}

func TestNewZMQClient(t *testing.T) {
	config := DefaultZMQClientConfig("test-pod", "10.0.0.1", "test-model")
	handler := NewMockEventHandler()

	client := NewZMQClient(config, handler)

	assert.NotNil(t, client)
	assert.Equal(t, config, client.config)
	assert.Equal(t, handler, client.eventHandler)
	assert.Equal(t, int64(-1), client.lastSeq)
	assert.False(t, client.connected)
	assert.NotNil(t, client.ctx)
	assert.NotNil(t, client.cancel)
	assert.NotNil(t, client.metrics)
}

func TestZMQClientLifecycle(t *testing.T) {
	config := DefaultZMQClientConfig("test-pod", "10.0.0.1", "test-model")
	handler := NewMockEventHandler()

	client := NewZMQClient(config, handler)

	// Test initial state
	assert.False(t, client.IsConnected())
	assert.Equal(t, int64(-1), client.GetLastSequence())

	// Test Stop without Start
	client.Stop()

	// Verify clean shutdown
	select {
	case <-client.ctx.Done():
		// Context should be cancelled
	default:
		t.Fatal("Context should be cancelled after Stop")
	}
}

func TestZMQClientReconnectDelay(t *testing.T) {
	config := DefaultZMQClientConfig("test-pod", "10.0.0.1", "test-model")
	config.ReconnectDelay = 100 * time.Millisecond
	handler := NewMockEventHandler()

	client := NewZMQClient(config, handler)

	// Test exponential backoff
	assert.Equal(t, config.ReconnectDelay, client.reconnectDelay)

	// Simulate failed reconnection
	client.mu.Lock()
	client.reconnectDelay = time.Duration(float64(client.reconnectDelay) * ReconnectBackoffFactor)
	client.mu.Unlock()

	assert.Equal(t, 200*time.Millisecond, client.reconnectDelay)

	// Test max reconnect interval
	client.mu.Lock()
	client.reconnectDelay = MaxReconnectInterval * 2
	if client.reconnectDelay > MaxReconnectInterval {
		client.reconnectDelay = MaxReconnectInterval
	}
	client.mu.Unlock()

	assert.Equal(t, MaxReconnectInterval, client.reconnectDelay)
}

// TestMockZMQPublisher tests with a mock ZMQ publisher
func TestMockZMQPublisher(t *testing.T) {
	// Skip if ZMQ is not available
	ctx, err := zmq.NewContext()
	if err != nil {
		t.Skip("ZMQ not available:", err)
	}
	defer ctx.Term()

	// Create mock publisher
	publisher, err := zmq.NewSocket(zmq.PUB)
	require.NoError(t, err)
	defer publisher.Close()

	err = publisher.Bind("tcp://127.0.0.1:5557")
	require.NoError(t, err)

	// Allow time for binding
	time.Sleep(100 * time.Millisecond)

	// Create client
	config := DefaultZMQClientConfig("test-pod", "127.0.0.1", "test-model")
	config.PollTimeout = 50 * time.Millisecond
	handler := NewMockEventHandler()
	client := NewZMQClient(config, handler)

	// Connect should work
	err = client.Connect()
	assert.NoError(t, err)
	assert.True(t, client.IsConnected())

	// Prepare test event
	now := time.Now().UTC().Truncate(time.Second)
	testEvent := &BlockStoredEvent{
		Type:        EventTypeBlockStored,
		Timestamp:   now,
		BlockHashes: []int64{123, 456},
		TokenIDs:    [][]int32{{1, 2}, {3, 4}},
		ModelName:   "test-model",
	}

	// Create event batch
	batch := map[string]interface{}{
		"events": []interface{}{
			map[string]interface{}{
				"type":         string(testEvent.Type),
				"timestamp":    testEvent.Timestamp.Unix(),
				"block_hashes": []interface{}{int64(123), int64(456)},
				"token_ids":    []interface{}{[]interface{}{int32(1), int32(2)}, []interface{}{int32(3), int32(4)}},
				"model_name":   testEvent.ModelName,
			},
		},
	}

	payload, err := msgpack.Marshal(batch)
	require.NoError(t, err)

	// Publish message
	seq := make([]byte, 8)
	binary.BigEndian.PutUint64(seq, 1)

	_, err = publisher.SendBytes([]byte("test-topic"), zmq.SNDMORE)
	require.NoError(t, err)
	_, err = publisher.SendBytes(seq, zmq.SNDMORE)
	require.NoError(t, err)
	_, err = publisher.SendBytes(payload, 0)
	require.NoError(t, err)

	// Start client in background
	ctx, cancel := context.WithCancel(context.Background())
	client.ctx = ctx
	client.cancel = cancel

	go func() {
		err := client.consumeEvents()
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Logf("consumeEvents error: %v", err)
		}
	}()

	// Wait for event to be processed
	time.Sleep(200 * time.Millisecond)

	// Stop client
	cancel()
	time.Sleep(100 * time.Millisecond)

	// Check received events
	events := handler.GetEvents()
	assert.Len(t, events, 1)

	if len(events) > 0 {
		receivedEvent, ok := events[0].(*BlockStoredEvent)
		assert.True(t, ok)
		assert.Equal(t, testEvent.Type, receivedEvent.Type)
		assert.Equal(t, testEvent.BlockHashes, receivedEvent.BlockHashes)
		assert.Equal(t, "test-pod", receivedEvent.PodName)
	}
}

func TestMetricsTracking(t *testing.T) {
	config := DefaultZMQClientConfig("test-metrics-pod", "10.0.0.1", "test-model")
	handler := NewMockEventHandler()

	client := NewZMQClient(config, handler)

	// Test connection metrics
	client.mu.Lock()
	client.connected = true
	client.mu.Unlock()
	client.metrics.IncrementConnectionCount()

	// Test disconnection metrics
	client.markDisconnected()
	assert.False(t, client.IsConnected())

	// Test event metrics
	client.metrics.IncrementEventCount(string(EventTypeBlockStored))
	client.metrics.RecordEventProcessingLatency(1 * time.Millisecond)

	// Test error metrics
	client.metrics.IncrementErrorCount("test_error")

	// Test missed events
	client.metrics.IncrementMissedEvents(5)

	// Cleanup metrics
	client.metrics.Delete()
}

func TestEventHandlerErrors(t *testing.T) {
	handler := NewMockEventHandler()
	handler.SetHandleError(0, errors.New("test error"))

	event := &BlockStoredEvent{
		Type:      EventTypeBlockStored,
		Timestamp: time.Now(),
	}

	err := handler.HandleEvent(event)
	assert.Error(t, err)
	assert.Equal(t, "test error", err.Error())

	// Second event should succeed
	err = handler.HandleEvent(event)
	assert.NoError(t, err)

	events := handler.GetEvents()
	assert.Len(t, events, 1)
}