package benchmark

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"golang.org/x/sync/errgroup"

	"benchmarkDB/internal/config"
	"benchmarkDB/internal/dataset"
	"benchmarkDB/internal/datastore"
	"benchmarkDB/internal/output"
)

// Engine is the central benchmark orchestrator.
// It owns the datastore, dataset generator, and metrics collector.
// It runs benchmark phases in sequence and assembles the final report.
type Engine struct {
	Config *config.Config
	Store  datastore.DataStore
	Gen    *dataset.Generator
}

// NewEngine creates a benchmark engine.
// Does NOT connect to the database — call Connect() separately.
func NewEngine(cfg *config.Config, store datastore.DataStore, gen *dataset.Generator) *Engine {
	return &Engine{
		Config: cfg,
		Store:  store,
		Gen:    gen,
	}
}

// Run executes all configured benchmark phases in order and returns
// a FullReport ready for output formatting.
func (e *Engine) Run(ctx context.Context) (*output.FullReport, error) {
	slog.Info("Benchmark engine starting",
		"database", e.Store.Name(),
		"scenario", e.Config.Benchmark.Scenario,
	)

	metadata := output.CollectMetadata(e.Config, e.Store.Name())

	phases := resolvePhases(e.Config.Benchmark.Phases)
	if len(phases) == 0 {
		return nil, fmt.Errorf("no valid benchmark phases configured")
	}

	report := &output.FullReport{
		Metadata: metadata,
	}

	for _, phase := range phases {
		slog.Info("Starting phase", "phase", phase.Name())

		phaseOut, err := phase.Run(ctx, e)
		if err != nil {
			return nil, fmt.Errorf("phase %q failed: %w", phase.Name(), err)
		}

		// Convert PhaseOutput to output.PhaseResult.
		if phaseOut.SubResults != nil && len(phaseOut.SubResults) > 0 {
			// Concurrency/saturation phases produce multiple sub-results.
			for concurrency, result := range phaseOut.SubResults {
				pr := convertToPhaseResult(phaseOut.PhaseName, concurrency, result)
				report.Phases = append(report.Phases, pr)
			}
		} else if phaseOut.Result != nil {
			pr := convertToPhaseResult(phaseOut.PhaseName, phaseOut.Concurrency, phaseOut.Result)
			report.Phases = append(report.Phases, pr)
		} else if phaseOut.Passed != nil {
			// Connectivity phase — pass/fail only.
			report.Phases = append(report.Phases, output.PhaseResult{
				PhaseName: phaseOut.PhaseName,
				Passed:    phaseOut.Passed,
			})
		}
	}

	return report, nil
}

// LoadData loads the entire generated dataset into the datastore.
// This should be called before running benchmark phases.
func (e *Engine) LoadData(ctx context.Context) error {
	slog.Info("Loading dataset into datastore",
		"key_count", e.Gen.KeyCount(),
		"database", e.Store.Name(),
	)

	records := e.Gen.GenerateAll()
	loaded := 0
	batchReport := e.Gen.KeyCount() / 10
	if batchReport == 0 {
		batchReport = 1
	}

	for i, rec := range records {
		if err := e.Store.Set(ctx, rec.Namespace, rec.Set, rec.Key, rec.Fields, 0); err != nil {
			return fmt.Errorf("loading record %d (%s): %w", i, rec.Key, err)
		}
		loaded++
		if loaded%batchReport == 0 {
			slog.Info("Data loading progress",
				"loaded", loaded,
				"total", e.Gen.KeyCount(),
				"pct", fmt.Sprintf("%.0f%%", float64(loaded)/float64(e.Gen.KeyCount())*100),
			)
		}
	}

	slog.Info("Dataset loading complete", "loaded", loaded)
	return nil
}

// SetupIndexes creates all required secondary indexes and waits for
// them to become ready before returning.
// Index creation time is NOT included in benchmark measurements (Section 27).
func (e *Engine) SetupIndexes(ctx context.Context) error {
	defs := e.Gen.GetIndexDefinitions()
	slog.Info("Creating secondary indexes", "count", len(defs))

	for _, def := range defs {
		slog.Info("  Creating index",
			"name", def.IndexName,
			"field", def.Field,
			"type", def.IndexType.String(),
		)
		if err := e.Store.CreateIndex(ctx, def); err != nil {
			return fmt.Errorf("creating index %s: %w", def.IndexName, err)
		}
	}

	// Wait for all indexes to be ready.
	slog.Info("Waiting for indexes to be ready...")
	for _, def := range defs {
		for attempts := 0; attempts < 120; attempts++ { // up to 2 minutes
			ready, err := e.Store.IndexReady(ctx, def.Namespace, def.Set, def.IndexName, e.Gen.KeyCount())
			if err != nil {
				slog.Warn("Index readiness check failed",
					"index", def.IndexName,
					"error", err,
				)
			}
			if ready {
				slog.Info("  Index ready", "name", def.IndexName)
				break
			}
			if attempts == 119 {
				slog.Warn("  Index may not be fully ready, proceeding anyway",
					"name", def.IndexName,
				)
			}
			time.Sleep(1 * time.Second)
		}
	}

	slog.Info("All indexes created")
	return nil
}

// runWorkerPool creates a bounded pool of workers, runs them through
// warmup and measurement phases, and returns aggregated results.
//
// Critical design decisions:
//   - Warmup metrics are collected but discarded (Section 12).
//   - Measurement metrics start fresh after warmup.
//   - Workers are stopped via context cancellation to ensure clean shutdown.
func (e *Engine) runWorkerPool(
	ctx context.Context,
	concurrency int,
	duration time.Duration,
	warmup time.Duration,
	selector *WorkloadSelector,
) (*AggregatedResult, error) {

	// --- Warmup phase ---
	if warmup > 0 {
		slog.Info("  Warmup phase", "duration", warmup, "concurrency", concurrency)
		warmupCollector := NewMetricsCollector()
		warmupCtx, warmupCancel := context.WithTimeout(ctx, warmup)

		var warmupGroup errgroup.Group
		for i := 0; i < concurrency; i++ {
			w := &Worker{
				ID:        i,
				Store:     e.Store,
				Gen:       e.Gen,
				Collector: warmupCollector,
				Selector:  selector,
				Config:    e.Config,
			}
			warmupGroup.Go(func() error {
				return w.Run(warmupCtx)
			})
		}

		_ = warmupGroup.Wait()
		warmupCancel()
		// Warmup metrics are intentionally discarded.
	}

	// --- Measurement phase ---
	slog.Info("  Measurement phase", "duration", duration, "concurrency", concurrency)
	measureCollector := NewMetricsCollector()
	measureCtx, measureCancel := context.WithTimeout(ctx, duration)
	defer measureCancel()

	var measureGroup errgroup.Group
	measureCollector.Start()

	for i := 0; i < concurrency; i++ {
		w := &Worker{
			ID:        i,
			Store:     e.Store,
			Gen:       e.Gen,
			Collector: measureCollector,
			Selector:  selector,
			Config:    e.Config,
		}
		measureGroup.Go(func() error {
			return w.Run(measureCtx)
		})
	}

	_ = measureGroup.Wait()
	measureCollector.Stop()

	return measureCollector.Aggregate(), nil
}

// convertToPhaseResult maps internal AggregatedResult to the output format.
func convertToPhaseResult(phaseName string, concurrency int, result *AggregatedResult) output.PhaseResult {
	pr := output.PhaseResult{
		PhaseName:       phaseName,
		Concurrency:     concurrency,
		DurationSeconds: result.Duration.Seconds(),
		TotalThroughput: result.Throughput,
	}

	var totalOps, failOps int64
	for name, snap := range result.Operations {
		opResult := output.OperationResult{
			Name:       name,
			TotalOps:   snap.TotalOps,
			SuccessOps: snap.SuccessOps,
			FailedOps:  snap.FailedOps,
			MinMs:      snap.Min,
			AvgMs:      snap.Avg,
			P50Ms:      snap.P50,
			P95Ms:      snap.P95,
			P99Ms:      snap.P99,
			MaxMs:      snap.Max,
		}
		if result.Duration.Seconds() > 0 {
			opResult.Throughput = float64(snap.SuccessOps) / result.Duration.Seconds()
		}
		if snap.TotalOps > 0 {
			opResult.ErrorRate = float64(snap.FailedOps) / float64(snap.TotalOps)
		}

		pr.Operations = append(pr.Operations, opResult)
		totalOps += snap.TotalOps
		failOps += snap.FailedOps
	}

	if totalOps > 0 {
		pr.OverallErrorRate = float64(failOps) / float64(totalOps)
	}

	return pr
}
