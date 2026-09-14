package benchmark

import (
	"math/rand"
	"testing"
	"time"

	"benchmarkDB/internal/config"
)

// TestWorkloadSelectorSingle ensures that a single-operation workload
// always returns that operation.
func TestWorkloadSelectorSingle(t *testing.T) {
	cfg := config.WorkloadConfig{
		Type: "set",
	}

	selector := NewWorkloadSelector(cfg)
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	for i := 0; i < 100; i++ {
		op := selector.Next(rng)
		if op != OpSet {
			t.Errorf("expected %v, got %v", OpSet, op)
		}
	}
}

// TestWorkloadSelectorMixed ensures that the weighted random selection
// roughly matches the configured proportions over a large number of samples.
func TestWorkloadSelectorMixed(t *testing.T) {
	cfg := config.WorkloadConfig{
		Type: "mixed",
		Operations: config.OperationsRatio{
			Get:           50,
			Set:           20,
			Delete:        10,
			TTL:           0,
			EqualityQuery: 15,
			RangeQuery:    5,
		},
	}

	selector := NewWorkloadSelector(cfg)
	rng := rand.New(rand.NewSource(12345)) // Deterministic seed for stable test

	counts := make(map[OperationType]int)
	iterations := 100000

	for i := 0; i < iterations; i++ {
		op := selector.Next(rng)
		counts[op]++
	}

	// Verify ratios (allow some statistical variance ~2%).
	verifyRatio(t, counts[OpGet], iterations, 0.50)
	verifyRatio(t, counts[OpSet], iterations, 0.20)
	verifyRatio(t, counts[OpDelete], iterations, 0.10)
	verifyRatio(t, counts[OpEqualityQuery], iterations, 0.15)
	verifyRatio(t, counts[OpRangeQuery], iterations, 0.05)
	
	if counts[OpTTL] != 0 {
		t.Errorf("expected 0 TTL ops, got %d", counts[OpTTL])
	}
}

func verifyRatio(t *testing.T, count, total int, expected float64) {
	t.Helper()
	actual := float64(count) / float64(total)
	diff := actual - expected
	if diff < 0 {
		diff = -diff
	}
	if diff > 0.02 { // 2% tolerance
		t.Errorf("ratio mismatch: expected %.2f, got %.2f (diff %.2f)", expected, actual, diff)
	}
}

// TestWorkloadSelectorZeroTotal tests behavior when operations total weight is 0.
func TestWorkloadSelectorZeroTotal(t *testing.T) {
	cfg := config.WorkloadConfig{
		Type: "mixed",
		Operations: config.OperationsRatio{},
	}
	selector := NewWorkloadSelector(cfg)
	rng := rand.New(rand.NewSource(1))
	
	op := selector.Next(rng)
	if op != OpGet {
		t.Errorf("fallback for 0 weight should be get, got %v", op)
	}
}
