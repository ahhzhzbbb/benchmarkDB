// Package benchmark implements the core benchmark engine.
//
// metrics.go provides latency and throughput measurement using HDR Histogram.
// HDR Histogram uses O(1) memory regardless of the number of recorded samples,
// which is critical when benchmarking millions of operations (Section 13).
package benchmark

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/HdrHistogram/hdrhistogram-go"
)

// MetricsSnapshot is a point-in-time summary of an operation's performance.
// All latency values are in milliseconds.
type MetricsSnapshot struct {
	Min        float64 `json:"min_ms"`
	Avg        float64 `json:"avg_ms"`
	P50        float64 `json:"p50_ms"`
	P95        float64 `json:"p95_ms"`
	P99        float64 `json:"p99_ms"`
	Max        float64 `json:"max_ms"`
	TotalOps   int64   `json:"total_ops"`
	SuccessOps int64   `json:"success_ops"`
	FailedOps  int64   `json:"failed_ops"`
}

// AggregatedResult holds the metrics for all operations in a benchmark run.
type AggregatedResult struct {
	Operations map[string]MetricsSnapshot `json:"operations"`
	Throughput float64                    `json:"throughput_ops_sec"`
	Duration   time.Duration             `json:"duration"`
}

// OperationMetrics tracks latency and counts for a single operation type.
// Thread-safe: multiple workers record concurrently.
type OperationMetrics struct {
	mu         sync.Mutex
	histogram  *hdrhistogram.Histogram
	totalOps   int64 // atomic
	successOps int64 // atomic
	failedOps  int64 // atomic
}

// NewOperationMetrics creates a new metrics tracker.
// Range: 1 microsecond to 60 seconds, 3 significant digits.
func NewOperationMetrics() *OperationMetrics {
	return &OperationMetrics{
		histogram: hdrhistogram.New(1, 60_000_000, 3),
	}
}

// RecordLatency records a single operation's latency.
func (m *OperationMetrics) RecordLatency(d time.Duration) {
	micros := d.Microseconds()
	if micros < 1 {
		micros = 1
	} else if micros > 60_000_000 {
		micros = 60_000_000
	}
	m.mu.Lock()
	m.histogram.RecordValue(micros)
	m.mu.Unlock()
}

// RecordSuccess increments the success counter.
func (m *OperationMetrics) RecordSuccess() {
	atomic.AddInt64(&m.totalOps, 1)
	atomic.AddInt64(&m.successOps, 1)
}

// RecordFailure increments the failure counter.
func (m *OperationMetrics) RecordFailure() {
	atomic.AddInt64(&m.totalOps, 1)
	atomic.AddInt64(&m.failedOps, 1)
}

// Snapshot returns a point-in-time copy of the metrics.
// Latency values are converted from microseconds to milliseconds.
func (m *OperationMetrics) Snapshot() MetricsSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	return MetricsSnapshot{
		Min:        float64(m.histogram.Min()) / 1000.0,
		Avg:        m.histogram.Mean() / 1000.0,
		P50:        float64(m.histogram.ValueAtQuantile(50)) / 1000.0,
		P95:        float64(m.histogram.ValueAtQuantile(95)) / 1000.0,
		P99:        float64(m.histogram.ValueAtQuantile(99)) / 1000.0,
		Max:        float64(m.histogram.Max()) / 1000.0,
		TotalOps:   atomic.LoadInt64(&m.totalOps),
		SuccessOps: atomic.LoadInt64(&m.successOps),
		FailedOps:  atomic.LoadInt64(&m.failedOps),
	}
}

// Reset clears all counters and histogram data.
func (m *OperationMetrics) Reset() {
	m.mu.Lock()
	m.histogram.Reset()
	m.mu.Unlock()
	atomic.StoreInt64(&m.totalOps, 0)
	atomic.StoreInt64(&m.successOps, 0)
	atomic.StoreInt64(&m.failedOps, 0)
}

// MetricsCollector manages per-operation metrics and tracks overall timing.
type MetricsCollector struct {
	mu         sync.Mutex
	operations map[string]*OperationMetrics
	startTime  time.Time
	endTime    time.Time
}

// NewMetricsCollector creates a new collector.
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		operations: make(map[string]*OperationMetrics),
	}
}

// ForOperation returns (or creates) the metrics tracker for the named operation.
func (c *MetricsCollector) ForOperation(name string) *OperationMetrics {
	c.mu.Lock()
	defer c.mu.Unlock()

	if op, exists := c.operations[name]; exists {
		return op
	}
	op := NewOperationMetrics()
	c.operations[name] = op
	return op
}

// Start records the beginning of the measurement phase.
func (c *MetricsCollector) Start() {
	c.startTime = time.Now()
}

// Stop records the end of the measurement phase.
func (c *MetricsCollector) Stop() {
	c.endTime = time.Now()
}

// Reset clears all operation metrics (used between warmup and measurement).
func (c *MetricsCollector) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, op := range c.operations {
		op.Reset()
	}
}

// Aggregate computes the final results from all operation metrics.
func (c *MetricsCollector) Aggregate() *AggregatedResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	duration := c.endTime.Sub(c.startTime)
	if duration <= 0 {
		duration = time.Millisecond // avoid division by zero
	}

	result := &AggregatedResult{
		Operations: make(map[string]MetricsSnapshot),
		Duration:   duration,
	}

	var totalSuccessOps int64
	for name, op := range c.operations {
		snap := op.Snapshot()
		result.Operations[name] = snap
		totalSuccessOps += snap.SuccessOps
	}

	// Section 14: throughput = successful_operations / measurement_duration
	result.Throughput = float64(totalSuccessOps) / duration.Seconds()
	return result
}
