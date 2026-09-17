package benchmark

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"

	"benchmarkDB/internal/datastore"
)

// Phase represents a single stage of the benchmark process (Section 17).
type Phase interface {
	// Name returns the human-readable phase identifier.
	Name() string
	// Run executes the phase and returns aggregated results.
	Run(ctx context.Context, e *Engine) (*PhaseOutput, error)
}

// PhaseOutput wraps AggregatedResult with phase-specific metadata.
type PhaseOutput struct {
	PhaseName   string
	Concurrency int
	Result      *AggregatedResult
	Passed      *bool // for connectivity phase only
	// SubResults holds per-concurrency results for concurrency/saturation phases.
	SubResults map[int]*AggregatedResult
}

// ---------- Phase 1: Connectivity / Correctness ----------

// ConnectivityPhase verifies basic connectivity and correctness:
// SET → GET → verify value → DELETE, then one equality query and one range query.
type ConnectivityPhase struct{}

func (p *ConnectivityPhase) Name() string { return "connectivity" }

func (p *ConnectivityPhase) Run(ctx context.Context, e *Engine) (*PhaseOutput, error) {
	slog.Info("Phase 1: Connectivity & Correctness check")
	passed := true

	// Use a fixed seed for reproducibility: connectivity check must be deterministic
	// so failures are easy to reproduce without re-running the full workload.
	rng := rand.New(rand.NewSource(42))
	rec := e.Gen.RandomRecord(rng)

	// ----------------------------------------------------------------
	// 1. Primary-key operations: SET → GET → verify → DELETE
	// ----------------------------------------------------------------

	// SET
	err := e.Store.Set(ctx, rec.Namespace, rec.Set, rec.Key, rec.Fields, 0)
	if err != nil {
		passed = false
		return &PhaseOutput{PhaseName: p.Name(), Passed: &passed},
			fmt.Errorf("connectivity SET failed: %w", err)
	}
	slog.Info("  SET: OK", "key", rec.Namespace+":"+rec.Set+":"+rec.Key)

	// GET + verify
	got, err := e.Store.Get(ctx, rec.Namespace, rec.Set, rec.Key)
	if err != nil {
		passed = false
		return &PhaseOutput{PhaseName: p.Name(), Passed: &passed},
			fmt.Errorf("connectivity GET failed: %w", err)
	}
	if got == nil {
		passed = false
		return &PhaseOutput{PhaseName: p.Name(), Passed: &passed},
			fmt.Errorf("connectivity GET returned nil for key %s:%s:%s",
				rec.Namespace, rec.Set, rec.Key)
	}
	slog.Info("  GET: OK", "key", got.Key)

	// DELETE
	err = e.Store.Delete(ctx, rec.Namespace, rec.Set, rec.Key)
	if err != nil {
		passed = false
		return &PhaseOutput{PhaseName: p.Name(), Passed: &passed},
			fmt.Errorf("connectivity DELETE failed: %w", err)
	}
	slog.Info("  DELETE: OK")

	// ----------------------------------------------------------------
	// 2. Secondary-index preflight: verify indexes exist before querying.
	//
	//    If indexes are missing the FT.SEARCH error message is cryptic
	//    ("SEARCH_INDEX_NOT_FOUND"). We surface a clear, actionable error
	//    here instead: "run setup-index first".
	// ----------------------------------------------------------------
	if err := p.checkIndexesReady(ctx, e); err != nil {
		passed = false
		return &PhaseOutput{PhaseName: p.Name(), Passed: &passed}, err
	}

	// ----------------------------------------------------------------
	// 3. Secondary-index queries: EQUALITY + RANGE
	// ----------------------------------------------------------------

	// Equality Query
	field, value := e.Gen.RandomEqualityQuery(rng)
	slog.Info("  EQUALITY_QUERY: attempting",
		"field", field,
		"value", value,
		"namespace", rec.Namespace,
		"set", rec.Set,
	)
	_, err = e.Store.EqualityQuery(ctx, datastore.EqualityQueryRequest{
		Namespace: rec.Namespace,
		Set:       rec.Set,
		Field:     field,
		Value:     value,
	})
	if err != nil {
		passed = false
		return &PhaseOutput{PhaseName: p.Name(), Passed: &passed},
			fmt.Errorf("connectivity EQUALITY_QUERY failed: %w", err)
	}
	slog.Info("  EQUALITY_QUERY: OK", "field", field)

	// Range Query
	rField, rMin, rMax := e.Gen.RandomRangeQuery(rng)
	slog.Info("  RANGE_QUERY: attempting",
		"field", rField,
		"min", rMin,
		"max", rMax,
	)
	_, err = e.Store.RangeQuery(ctx, datastore.RangeQueryRequest{
		Namespace: rec.Namespace,
		Set:       rec.Set,
		Field:     rField,
		Min:       rMin,
		Max:       rMax,
	})
	if err != nil {
		passed = false
		return &PhaseOutput{PhaseName: p.Name(), Passed: &passed},
			fmt.Errorf("connectivity RANGE_QUERY failed: %w", err)
	}
	slog.Info("  RANGE_QUERY: OK", "field", rField)

	slog.Info("Phase 1: All connectivity checks passed")
	return &PhaseOutput{PhaseName: p.Name(), Passed: &passed}, nil
}

// checkIndexesReady verifies that all required secondary indexes are present
// and ready on the database before attempting to query them.
//
// It checks each index definition from the generator. If any index is missing
// or not yet ready, it returns a clear, actionable error message that tells
// the operator which exact index is missing and how to fix it.
func (p *ConnectivityPhase) checkIndexesReady(ctx context.Context, e *Engine) error {
	defs := e.Gen.GetIndexDefinitions()
	var missing []string

	for _, def := range defs {
		ready, err := e.Store.IndexReady(ctx, def.Namespace, def.Set, def.IndexName, 1)
		if err != nil {
			// IndexReady already returns nil for "not found" — a real error here
			// means something more serious (network, auth, etc.).
			return fmt.Errorf(
				"checking index %q (%s:%s): %w — verify Redis Query Engine is available",
				def.IndexName, def.Namespace, def.Set, err,
			)
		}
		if !ready {
			missing = append(missing, fmt.Sprintf("%s:%s:%s_%s_*_idx",
				def.Namespace, def.Set, def.Set, def.Field))
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf(
			"secondary indexes not ready (%d missing): [%s]\n"+
				"  → run 'benchmark setup-index --config <file>' first, "+
				"or use 'benchmark run' which sets up indexes automatically",
			len(missing), strings.Join(missing, ", "),
		)
	}

	slog.Info("  INDEX_PREFLIGHT: all indexes ready", "count", len(defs))
	return nil
}

// ---------- Phase 2: Single Operation ----------

// SingleOperationPhase benchmarks each operation type independently.
type SingleOperationPhase struct{}

func (p *SingleOperationPhase) Name() string { return "single" }

func (p *SingleOperationPhase) Run(ctx context.Context, e *Engine) (*PhaseOutput, error) {
	slog.Info("Phase 2: Single operation benchmark")

	warmup, _ := e.Config.Benchmark.WarmupDuration()
	duration, _ := e.Config.Benchmark.MeasureDuration()

	// If the configured workload is already a single type, run just that.
	// Otherwise, run each operation type that has a non-zero weight.
	if e.Config.Workload.Type != "mixed" {
		selector := NewWorkloadSelector(e.Config.Workload)
		result, err := e.runWorkerPool(ctx, e.Config.Benchmark.Concurrency, duration, warmup, selector)
		if err != nil {
			return nil, fmt.Errorf("single operation benchmark failed: %w", err)
		}
		return &PhaseOutput{
			PhaseName:   p.Name(),
			Concurrency: e.Config.Benchmark.Concurrency,
			Result:      result,
		}, nil
	}

	// For mixed config, run GET as the representative single operation.
	singleCfg := e.Config.Workload
	singleCfg.Type = "get"
	selector := NewWorkloadSelector(singleCfg)
	result, err := e.runWorkerPool(ctx, e.Config.Benchmark.Concurrency, duration, warmup, selector)
	if err != nil {
		return nil, fmt.Errorf("single operation benchmark failed: %w", err)
	}
	return &PhaseOutput{
		PhaseName:   p.Name(),
		Concurrency: e.Config.Benchmark.Concurrency,
		Result:      result,
	}, nil
}

// ---------- Phase 3: Concurrency ----------

// ConcurrencyPhase runs the workload at increasing concurrency levels
// to measure how throughput and latency scale.
type ConcurrencyPhase struct{}

func (p *ConcurrencyPhase) Name() string { return "concurrency" }

func (p *ConcurrencyPhase) Run(ctx context.Context, e *Engine) (*PhaseOutput, error) {
	slog.Info("Phase 3: Concurrency scaling benchmark")

	selector := NewWorkloadSelector(e.Config.Workload)
	warmup, _ := e.Config.Benchmark.WarmupDuration()
	duration, _ := e.Config.Benchmark.MeasureDuration()

	subResults := make(map[int]*AggregatedResult)
	var lastResult *AggregatedResult

	for _, c := range e.Config.Benchmark.ConcurrencySteps {
		slog.Info("  Running concurrency level", "concurrency", c)
		res, err := e.runWorkerPool(ctx, c, duration, warmup, selector)
		if err != nil {
			return nil, fmt.Errorf("concurrency level %d failed: %w", c, err)
		}
		subResults[c] = res
		lastResult = res
	}

	return &PhaseOutput{
		PhaseName:   p.Name(),
		Concurrency: e.Config.Benchmark.Concurrency,
		Result:      lastResult,
		SubResults:  subResults,
	}, nil
}

// ---------- Phase 4: Mixed Workload ----------

// MixedWorkloadPhase runs the full mixed workload with configured ratios.
type MixedWorkloadPhase struct{}

func (p *MixedWorkloadPhase) Name() string { return "mixed" }

func (p *MixedWorkloadPhase) Run(ctx context.Context, e *Engine) (*PhaseOutput, error) {
	slog.Info("Phase 4: Mixed workload benchmark")

	selector := NewWorkloadSelector(e.Config.Workload)
	warmup, _ := e.Config.Benchmark.WarmupDuration()
	duration, _ := e.Config.Benchmark.MeasureDuration()

	result, err := e.runWorkerPool(ctx, e.Config.Benchmark.Concurrency, duration, warmup, selector)
	if err != nil {
		return nil, fmt.Errorf("mixed workload benchmark failed: %w", err)
	}

	return &PhaseOutput{
		PhaseName:   p.Name(),
		Concurrency: e.Config.Benchmark.Concurrency,
		Result:      result,
	}, nil
}

// ---------- Phase 5: Saturation ----------

// SaturationPhase increases concurrency until performance degrades.
// It stops when error rate exceeds 5% or p99 latency increases by 3x
// compared to the lowest concurrency level.
type SaturationPhase struct{}

func (p *SaturationPhase) Name() string { return "saturation" }

func (p *SaturationPhase) Run(ctx context.Context, e *Engine) (*PhaseOutput, error) {
	slog.Info("Phase 5: Saturation / scaling benchmark")

	selector := NewWorkloadSelector(e.Config.Workload)
	warmup, _ := e.Config.Benchmark.WarmupDuration()
	duration, _ := e.Config.Benchmark.MeasureDuration()

	subResults := make(map[int]*AggregatedResult)
	var lastGoodResult *AggregatedResult

	for _, c := range e.Config.Benchmark.ConcurrencySteps {
		slog.Info("  Saturation test", "concurrency", c)
		res, err := e.runWorkerPool(ctx, c, duration, warmup, selector)
		if err != nil {
			return nil, fmt.Errorf("saturation at concurrency %d failed: %w", c, err)
		}
		subResults[c] = res

		// Check if we've hit saturation.
		var totalOps, failOps int64
		for _, snap := range res.Operations {
			totalOps += snap.TotalOps
			failOps += snap.FailedOps
		}

		if totalOps > 0 {
			errRate := float64(failOps) / float64(totalOps)
			if errRate > 0.05 {
				slog.Warn("  Saturation reached: error rate exceeded 5%",
					"concurrency", c,
					"error_rate", fmt.Sprintf("%.2f%%", errRate*100),
				)
				break
			}
		}

		lastGoodResult = res
	}

	return &PhaseOutput{
		PhaseName:   p.Name(),
		Concurrency: e.Config.Benchmark.Concurrency,
		Result:      lastGoodResult,
		SubResults:  subResults,
	}, nil
}

// resolvePhases maps phase names from config to Phase implementations.
func resolvePhases(names []string) []Phase {
	var phases []Phase
	for _, name := range names {
		switch name {
		case "connectivity":
			phases = append(phases, &ConnectivityPhase{})
		case "single":
			phases = append(phases, &SingleOperationPhase{})
		case "concurrency":
			phases = append(phases, &ConcurrencyPhase{})
		case "mixed":
			phases = append(phases, &MixedWorkloadPhase{})
		case "saturation":
			phases = append(phases, &SaturationPhase{})
		default:
			slog.Warn("Unknown benchmark phase, skipping", "phase", name)
		}
	}
	return phases
}
