package gateway

import (
	"sync"
	"time"
)

type metricsCollector struct {
	mu              sync.Mutex
	requestsTotal   int64
	totalDurationMs int64
	statusCounts    map[string]int64
}

type metricsSnapshot struct {
	RequestsTotal     int64            `json:"requestsTotal"`
	StatusCounts      map[string]int64 `json:"statusCounts"`
	AverageDurationMs float64          `json:"averageDurationMs"`
}

func newMetricsCollector() *metricsCollector {
	return &metricsCollector{
		statusCounts: map[string]int64{
			"1xx": 0,
			"2xx": 0,
			"3xx": 0,
			"4xx": 0,
			"5xx": 0,
		},
	}
}

func (m *metricsCollector) Record(status int, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.requestsTotal++
	m.totalDurationMs += duration.Milliseconds()
	m.statusCounts[statusClass(status)]++
}

func (m *metricsCollector) Snapshot() metricsSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	statusCounts := make(map[string]int64, len(m.statusCounts))
	for key, value := range m.statusCounts {
		statusCounts[key] = value
	}

	var average float64
	if m.requestsTotal > 0 {
		average = float64(m.totalDurationMs) / float64(m.requestsTotal)
	}

	return metricsSnapshot{
		RequestsTotal:     m.requestsTotal,
		StatusCounts:      statusCounts,
		AverageDurationMs: average,
	}
}

func (m *metricsCollector) SnapshotWithPending(status int) metricsSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	statusCounts := make(map[string]int64, len(m.statusCounts))
	for key, value := range m.statusCounts {
		statusCounts[key] = value
	}
	statusCounts[statusClass(status)]++

	requestsTotal := m.requestsTotal + 1
	var average float64
	if requestsTotal > 0 {
		average = float64(m.totalDurationMs) / float64(requestsTotal)
	}

	return metricsSnapshot{
		RequestsTotal:     requestsTotal,
		StatusCounts:      statusCounts,
		AverageDurationMs: average,
	}
}

func statusClass(status int) string {
	switch {
	case status >= 100 && status < 200:
		return "1xx"
	case status >= 200 && status < 300:
		return "2xx"
	case status >= 300 && status < 400:
		return "3xx"
	case status >= 400 && status < 500:
		return "4xx"
	default:
		return "5xx"
	}
}
