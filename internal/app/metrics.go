package app

import (
	"sync"
	"time"
)

// RecordingMetrics is an in-memory Metrics for tests.
type RecordingMetrics struct {
	mu                        sync.Mutex
	ReconciliationDivergences int
	HTTP                      []HTTPObservation
	Operations                []OperationObservation
	Duplicates                map[string]int
	Conflicts                 map[string]int
	SQSRetries                int
	SQSDLQ                    int
	OutboxUnpublished         int
	OutboxOldest              time.Duration
	OutboxPublished           int
	OutboxPublishFailed       int
}

// HTTPObservation is one ObserveHTTP call.
type HTTPObservation struct {
	Method   string
	Pattern  string
	Status   int
	Duration time.Duration
}

// OperationObservation is one ObserveOperation call.
type OperationObservation struct {
	Kind     string
	Status   string
	Channel  string
	Duration time.Duration
}

func (m *RecordingMetrics) IncReconciliationDivergence() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ReconciliationDivergences++
}

func (m *RecordingMetrics) ObserveHTTP(method, pattern string, status int, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.HTTP = append(m.HTTP, HTTPObservation{Method: method, Pattern: pattern, Status: status, Duration: d})
}

func (m *RecordingMetrics) ObserveOperation(kind, status, channel string, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Operations = append(m.Operations, OperationObservation{Kind: kind, Status: status, Channel: channel, Duration: d})
}

func (m *RecordingMetrics) IncDuplicate(kind, channel string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Duplicates == nil {
		m.Duplicates = map[string]int{}
	}
	m.Duplicates[kind+"|"+channel]++
}

func (m *RecordingMetrics) IncConflict(reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Conflicts == nil {
		m.Conflicts = map[string]int{}
	}
	m.Conflicts[reason]++
}

func (m *RecordingMetrics) IncSQSRetry() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SQSRetries++
}

func (m *RecordingMetrics) IncSQSDLQ() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SQSDLQ++
}

func (m *RecordingMetrics) SetOutboxLag(unpublished int, oldest time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.OutboxUnpublished = unpublished
	m.OutboxOldest = oldest
}

func (m *RecordingMetrics) IncOutboxPublished(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.OutboxPublished += n
}

func (m *RecordingMetrics) IncOutboxPublishFailed(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.OutboxPublishFailed += n
}

func (m *RecordingMetrics) DuplicateCount(kind, channel string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Duplicates[kind+"|"+channel]
}

func (m *RecordingMetrics) ConflictCount(reason string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Conflicts[reason]
}

func (m *RecordingMetrics) OperationCount(kind, status, channel string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, op := range m.Operations {
		if op.Kind == kind && op.Status == status && op.Channel == channel {
			n++
		}
	}
	return n
}
