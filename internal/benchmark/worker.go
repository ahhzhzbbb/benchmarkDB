package benchmark

import (
	"context"
	"log/slog"
	"math/rand"
	"time"

	"benchmarkDB/internal/config"
	"benchmarkDB/internal/dataset"
	"benchmarkDB/internal/datastore"
)

// Worker is a single goroutine that executes operations against the datastore.
// Each worker has its own PRNG to avoid contention on a shared random source.
type Worker struct {
	ID        int
	Store     datastore.DataStore
	Gen       *dataset.Generator
	Collector *MetricsCollector
	Selector  *WorkloadSelector
	Config    *config.Config
}

// Run executes operations in a tight loop until the context is cancelled.
// Errors are recorded as failures but do not stop the worker — this ensures
// the benchmark measures real error rates under load (Section 14, 22).
func (w *Worker) Run(ctx context.Context) error {
	// Each worker gets its own PRNG seeded from time + worker ID
	// to avoid lock contention on a shared source.
	rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(w.ID)))

	var ttlDuration time.Duration
	if w.Config != nil {
		ttlDuration, _ = w.Config.Dataset.DatasetTTL()
	}
	if ttlDuration == 0 {
		ttlDuration = 300 * time.Second // default TTL for TTL workload
	}

	ns := w.Config.Dataset.Namespace
	set := w.Config.Dataset.Set

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		opType := w.Selector.Next(rng)
		opMetrics := w.Collector.ForOperation(string(opType))

		start := time.Now()
		var err error

		switch opType {
		case OpGet:
			key := w.Gen.RandomKey(rng)
			_, err = w.Store.Get(ctx, ns, set, key)

		case OpSet:
			rec := w.Gen.RandomRecord(rng)
			err = w.Store.Set(ctx, rec.Namespace, rec.Set, rec.Key, rec.Fields, 0)

		case OpDelete:
			key := w.Gen.RandomKey(rng)
			err = w.Store.Delete(ctx, ns, set, key)

		case OpTTL:
			rec := w.Gen.RandomRecord(rng)
			err = w.Store.Set(ctx, rec.Namespace, rec.Set, rec.Key, rec.Fields, ttlDuration)

		case OpEqualityQuery:
			field, value := w.Gen.RandomEqualityQuery(rng)
			_, err = w.Store.EqualityQuery(ctx, datastore.EqualityQueryRequest{
				Namespace: ns,
				Set:       set,
				Field:     field,
				Value:     value,
			})

		case OpRangeQuery:
			field, min, max := w.Gen.RandomRangeQuery(rng)
			_, err = w.Store.RangeQuery(ctx, datastore.RangeQueryRequest{
				Namespace: ns,
				Set:       set,
				Field:     field,
				Min:       min,
				Max:       max,
			})
		}

		elapsed := time.Since(start)

		// Don't record metrics for cancelled context (shutdown).
		if ctx.Err() != nil {
			return nil
		}

		opMetrics.RecordLatency(elapsed)

		if err != nil {
			opMetrics.RecordFailure()
			slog.Debug("operation failed",
				"worker", w.ID,
				"op", string(opType),
				"error", err,
			)
		} else {
			opMetrics.RecordSuccess()
		}
	}
}
