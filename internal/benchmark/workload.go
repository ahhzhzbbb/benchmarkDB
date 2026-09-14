package benchmark

import (
	"math/rand"

	"benchmarkDB/internal/config"
)

// OperationType identifies the kind of database operation being performed.
type OperationType string

const (
	OpGet           OperationType = "get"
	OpSet           OperationType = "set"
	OpDelete        OperationType = "delete"
	OpTTL           OperationType = "ttl"
	OpEqualityQuery OperationType = "equality_query"
	OpRangeQuery    OperationType = "range_query"
)

// AllOperationTypes returns all valid operation types, used for reporting.
func AllOperationTypes() []OperationType {
	return []OperationType{OpGet, OpSet, OpDelete, OpTTL, OpEqualityQuery, OpRangeQuery}
}

// WorkloadSelector picks the next operation based on weighted random selection.
// For single-operation workloads, it always returns that operation.
type WorkloadSelector struct {
	cfg         config.WorkloadConfig
	singleOp    OperationType
	totalWeight int
	isMixed     bool
}

// NewWorkloadSelector creates a selector from the workload configuration.
func NewWorkloadSelector(cfg config.WorkloadConfig) *WorkloadSelector {
	s := &WorkloadSelector{
		cfg: cfg,
	}

	if cfg.Type == "mixed" {
		s.isMixed = true
		s.totalWeight = cfg.Operations.Total()
	} else {
		s.singleOp = OperationType(cfg.Type)
	}

	return s
}

// Next returns the next operation to execute.
// For mixed workloads, operations are selected using weighted random sampling
// according to the configured ratios.
func (w *WorkloadSelector) Next(rng *rand.Rand) OperationType {
	if !w.isMixed {
		return w.singleOp
	}

	if w.totalWeight <= 0 {
		return OpGet // safe fallback
	}

	val := rng.Intn(w.totalWeight)
	ops := w.cfg.Operations

	// Walk through cumulative weights.
	if val < ops.Get {
		return OpGet
	}
	val -= ops.Get

	if val < ops.Set {
		return OpSet
	}
	val -= ops.Set

	if val < ops.Delete {
		return OpDelete
	}
	val -= ops.Delete

	if val < ops.TTL {
		return OpTTL
	}
	val -= ops.TTL

	if val < ops.EqualityQuery {
		return OpEqualityQuery
	}

	return OpRangeQuery
}
