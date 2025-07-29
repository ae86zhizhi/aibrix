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
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metric labels
const (
	LabelPodKey    = "pod_key"
	LabelEventType = "event_type"
	LabelErrorType = "error_type"
)

// ZMQClientMetrics holds all metrics for the ZMQ client
type ZMQClientMetrics struct {
	podKey string

	// Connection metrics
	connectionCount    prometheus.Counter
	disconnectionCount prometheus.Counter
	reconnectAttempts  prometheus.Counter

	// Event metrics
	eventsReceived      *prometheus.CounterVec
	eventsProcessed     *prometheus.CounterVec
	eventProcessingTime prometheus.Observer
	missedEvents        prometheus.Counter

	// Replay metrics
	replayRequests prometheus.Counter
	replaySuccess  prometheus.Counter
	replayFailures prometheus.Counter

	// Error metrics
	errors *prometheus.CounterVec

	// State metrics
	connected      prometheus.Gauge
	lastSequenceID prometheus.Gauge
}

var (
	// Connection metrics
	zmqConnectionTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "aibrix_kvcache_zmq_connections_total",
			Help: "Total number of ZMQ connections established",
		},
		[]string{LabelPodKey},
	)

	zmqDisconnectionTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "aibrix_kvcache_zmq_disconnections_total",
			Help: "Total number of ZMQ disconnections",
		},
		[]string{LabelPodKey},
	)

	zmqReconnectAttemptsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "aibrix_kvcache_zmq_reconnect_attempts_total",
			Help: "Total number of ZMQ reconnection attempts",
		},
		[]string{LabelPodKey},
	)

	// Event metrics
	zmqEventsReceivedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "aibrix_kvcache_zmq_events_received_total",
			Help: "Total number of KV cache events received",
		},
		[]string{LabelPodKey, LabelEventType},
	)

	zmqEventsProcessedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "aibrix_kvcache_zmq_events_processed_total",
			Help: "Total number of KV cache events successfully processed",
		},
		[]string{LabelPodKey, LabelEventType},
	)

	zmqEventProcessingDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "aibrix_kvcache_zmq_event_processing_duration_seconds",
			Help:    "Time taken to process KV cache events",
			Buckets: prometheus.ExponentialBuckets(0.00001, 2, 15), // 10μs to ~160ms
		},
		[]string{LabelPodKey},
	)

	zmqMissedEventsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "aibrix_kvcache_zmq_missed_events_total",
			Help: "Total number of missed events detected",
		},
		[]string{LabelPodKey},
	)

	// Replay metrics
	zmqReplayRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "aibrix_kvcache_zmq_replay_requests_total",
			Help: "Total number of replay requests sent",
		},
		[]string{LabelPodKey},
	)

	zmqReplaySuccessTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "aibrix_kvcache_zmq_replay_success_total",
			Help: "Total number of successful replay responses",
		},
		[]string{LabelPodKey},
	)

	zmqReplayFailuresTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "aibrix_kvcache_zmq_replay_failures_total",
			Help: "Total number of failed replay requests",
		},
		[]string{LabelPodKey},
	)

	// Error metrics
	zmqErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "aibrix_kvcache_zmq_errors_total",
			Help: "Total number of errors encountered",
		},
		[]string{LabelPodKey, LabelErrorType},
	)

	// State metrics
	zmqConnectionStatus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "aibrix_kvcache_zmq_connection_status",
			Help: "Current connection status (1=connected, 0=disconnected)",
		},
		[]string{LabelPodKey},
	)

	zmqLastSequenceID = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "aibrix_kvcache_zmq_last_sequence_id",
			Help: "Last processed sequence ID",
		},
		[]string{LabelPodKey},
	)
)

// NewZMQClientMetrics creates a new metrics instance for a ZMQ client
func NewZMQClientMetrics(podKey string) *ZMQClientMetrics {
	return &ZMQClientMetrics{
		podKey:              podKey,
		connectionCount:     zmqConnectionTotal.WithLabelValues(podKey),
		disconnectionCount:  zmqDisconnectionTotal.WithLabelValues(podKey),
		reconnectAttempts:   zmqReconnectAttemptsTotal.WithLabelValues(podKey),
		eventsReceived:      zmqEventsReceivedTotal,
		eventsProcessed:     zmqEventsProcessedTotal,
		eventProcessingTime: zmqEventProcessingDuration.WithLabelValues(podKey),
		missedEvents:        zmqMissedEventsTotal.WithLabelValues(podKey),
		replayRequests:      zmqReplayRequestsTotal.WithLabelValues(podKey),
		replaySuccess:       zmqReplaySuccessTotal.WithLabelValues(podKey),
		replayFailures:      zmqReplayFailuresTotal.WithLabelValues(podKey),
		errors:              zmqErrorsTotal,
		connected:           zmqConnectionStatus.WithLabelValues(podKey),
		lastSequenceID:      zmqLastSequenceID.WithLabelValues(podKey),
	}
}

// IncrementConnectionCount increments the connection counter
func (m *ZMQClientMetrics) IncrementConnectionCount() {
	m.connectionCount.Inc()
	m.connected.Set(1)
}

// IncrementDisconnectionCount increments the disconnection counter
func (m *ZMQClientMetrics) IncrementDisconnectionCount() {
	m.disconnectionCount.Inc()
	m.connected.Set(0)
}

// IncrementReconnectAttempts increments the reconnect attempts counter
func (m *ZMQClientMetrics) IncrementReconnectAttempts() {
	m.reconnectAttempts.Inc()
}

// IncrementEventCount increments the event counter for a specific event type
func (m *ZMQClientMetrics) IncrementEventCount(eventType string) {
	m.eventsReceived.WithLabelValues(m.podKey, eventType).Inc()
	m.eventsProcessed.WithLabelValues(m.podKey, eventType).Inc()
}

// RecordEventProcessingLatency records the time taken to process an event
func (m *ZMQClientMetrics) RecordEventProcessingLatency(duration time.Duration) {
	m.eventProcessingTime.Observe(duration.Seconds())
}

// IncrementMissedEvents increments the missed events counter
func (m *ZMQClientMetrics) IncrementMissedEvents(count int64) {
	m.missedEvents.Add(float64(count))
}

// IncrementReplayCount increments the replay request counter
func (m *ZMQClientMetrics) IncrementReplayCount() {
	m.replayRequests.Inc()
}

// IncrementReplaySuccess increments the successful replay counter
func (m *ZMQClientMetrics) IncrementReplaySuccess() {
	m.replaySuccess.Inc()
}

// IncrementReplayFailure increments the failed replay counter
func (m *ZMQClientMetrics) IncrementReplayFailure() {
	m.replayFailures.Inc()
}

// IncrementErrorCount increments the error counter for a specific error type
func (m *ZMQClientMetrics) IncrementErrorCount(errorType string) {
	m.errors.WithLabelValues(m.podKey, errorType).Inc()
}

// UpdateLastSequenceID updates the last processed sequence ID gauge
func (m *ZMQClientMetrics) UpdateLastSequenceID(seqID int64) {
	m.lastSequenceID.Set(float64(seqID))
}

// Delete removes all metrics for this pod (useful for cleanup)
func (m *ZMQClientMetrics) Delete() {
	// Delete all labeled metrics
	zmqConnectionTotal.DeleteLabelValues(m.podKey)
	zmqDisconnectionTotal.DeleteLabelValues(m.podKey)
	zmqReconnectAttemptsTotal.DeleteLabelValues(m.podKey)
	zmqMissedEventsTotal.DeleteLabelValues(m.podKey)
	zmqReplayRequestsTotal.DeleteLabelValues(m.podKey)
	zmqReplaySuccessTotal.DeleteLabelValues(m.podKey)
	zmqReplayFailuresTotal.DeleteLabelValues(m.podKey)
	zmqConnectionStatus.DeleteLabelValues(m.podKey)
	zmqLastSequenceID.DeleteLabelValues(m.podKey)
	zmqEventProcessingDuration.DeleteLabelValues(m.podKey)

	// Delete event type specific metrics
	for _, eventType := range []string{
		string(EventTypeBlockStored),
		string(EventTypeBlockRemoved),
		string(EventTypeAllCleared),
	} {
		zmqEventsReceivedTotal.DeleteLabelValues(m.podKey, eventType)
		zmqEventsProcessedTotal.DeleteLabelValues(m.podKey, eventType)
	}

	// Delete error type specific metrics
	for _, errorType := range []string{
		"consume_events",
		"reconnect",
		"decode",
		"handle_event",
	} {
		zmqErrorsTotal.DeleteLabelValues(m.podKey, errorType)
	}
}
