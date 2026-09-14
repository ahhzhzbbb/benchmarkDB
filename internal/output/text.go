package output

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// OperationResult holds the benchmark result for a single operation type.
// This is the common format consumed by both text and JSON formatters.
type OperationResult struct {
	Name       string  `json:"name"`
	Throughput float64 `json:"throughput_ops"` // ops/sec
	TotalOps   int64   `json:"total_ops"`
	SuccessOps int64   `json:"success_ops"`
	FailedOps  int64   `json:"failed_ops"`
	MinMs      float64 `json:"min_ms"`
	AvgMs      float64 `json:"avg_ms"`
	P50Ms      float64 `json:"p50_ms"`
	P95Ms      float64 `json:"p95_ms"`
	P99Ms      float64 `json:"p99_ms"`
	MaxMs      float64 `json:"max_ms"`
	ErrorRate  float64 `json:"error_rate"` // 0.0 – 1.0
}

// PhaseResult holds results for a single benchmark phase.
type PhaseResult struct {
	PhaseName       string            `json:"phase_name"`
	Concurrency     int               `json:"concurrency"`
	DurationSeconds float64           `json:"duration_seconds"`
	Operations      []OperationResult `json:"operations"`
	TotalThroughput float64           `json:"total_throughput_ops"`
	OverallErrorRate float64          `json:"overall_error_rate"`
	// Passed is used for connectivity phase.
	Passed *bool `json:"passed,omitempty"`
}

// FullReport is the complete benchmark output.
type FullReport struct {
	Metadata *Metadata     `json:"metadata"`
	Phases   []PhaseResult `json:"phases"`
}

// WriteText renders the full report in human-readable tabular format.
// Matches the output example from Section 19 of the spec.
func WriteText(w io.Writer, report *FullReport) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)

	// Header
	fmt.Fprintln(w, strings.Repeat("=", 80))
	fmt.Fprintln(w, "  BENCHMARK REPORT")
	fmt.Fprintln(w, strings.Repeat("=", 80))
	fmt.Fprintln(w)

	// Metadata
	m := report.Metadata
	fmt.Fprintf(w, "Database:        %s\n", m.DatabaseName)
	fmt.Fprintf(w, "Database Type:   %s\n", m.DatabaseType)
	if len(m.Topology.Hosts) > 0 {
		fmt.Fprintf(w, "Topology:        %s\n", strings.Join(m.Topology.Hosts, ", "))
	}
	if m.Topology.Masters > 0 {
		fmt.Fprintf(w, "                 %d masters / %d replicas\n", m.Topology.Masters, m.Topology.Replicas)
	}
	fmt.Fprintln(w)

	fmt.Fprintln(w, "Benchmark Client:")
	fmt.Fprintf(w, "  Hostname:      %s\n", m.Client.Hostname)
	if m.Client.KubernetesNode != "" {
		fmt.Fprintf(w, "  K8s Node:      %s\n", m.Client.KubernetesNode)
	}
	if m.Client.PodName != "" {
		fmt.Fprintf(w, "  Pod Name:      %s\n", m.Client.PodName)
	}
	fmt.Fprintf(w, "  Go Version:    %s\n", m.Client.GoVersion)
	fmt.Fprintf(w, "  CPUs:          %d\n", m.Client.NumCPU)
	fmt.Fprintln(w)

	fmt.Fprintln(w, "Workload:")
	fmt.Fprintf(w, "  Type:          %s\n", m.Workload.Type)
	if len(m.Workload.Operations) > 0 {
		for op, weight := range m.Workload.Operations {
			if weight > 0 {
				fmt.Fprintf(w, "  %-14s %d%%\n", op+":", weight)
			}
		}
	}
	fmt.Fprintln(w)

	fmt.Fprintln(w, "Dataset:")
	fmt.Fprintf(w, "  Keys:          %s\n", formatNumber(m.Dataset.KeyCount))
	fmt.Fprintf(w, "  Value Size:    %s\n", formatBytes(m.Dataset.ValueSize))
	fmt.Fprintf(w, "  Seed:          %d\n", m.Dataset.Seed)
	fmt.Fprintln(w)

	fmt.Fprintln(w, "Benchmark:")
	fmt.Fprintf(w, "  Concurrency:   %d\n", m.Benchmark.Concurrency)
	fmt.Fprintf(w, "  Warmup:        %s\n", m.Benchmark.Warmup)
	fmt.Fprintf(w, "  Duration:      %s\n", m.Benchmark.Duration)
	fmt.Fprintf(w, "  Scenario:      %s\n", m.Benchmark.Scenario)
	fmt.Fprintln(w)

	// Phases
	for _, phase := range report.Phases {
		fmt.Fprintln(w, strings.Repeat("-", 80))
		fmt.Fprintf(w, "  Phase: %s", phase.PhaseName)
		if phase.Concurrency > 0 {
			fmt.Fprintf(w, " (concurrency: %d)", phase.Concurrency)
		}
		fmt.Fprintln(w)
		fmt.Fprintln(w, strings.Repeat("-", 80))

		if phase.Passed != nil {
			if *phase.Passed {
				fmt.Fprintln(w, "  Status: PASSED ✓")
			} else {
				fmt.Fprintln(w, "  Status: FAILED ✗")
			}
			fmt.Fprintln(w)
			continue
		}

		if len(phase.Operations) == 0 {
			fmt.Fprintln(w, "  No operations recorded.")
			fmt.Fprintln(w)
			continue
		}

		// Results table
		fmt.Fprintf(tw, "  Operation\tOPS\tP50(ms)\tP95(ms)\tP99(ms)\tMin(ms)\tMax(ms)\tErrors\tErr%%\t\n")
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t\n",
			"─────────", "─────", "──────", "──────", "──────", "──────", "──────", "──────", "────")

		for _, op := range phase.Operations {
			fmt.Fprintf(tw, "  %s\t%.0f\t%.2f\t%.2f\t%.2f\t%.2f\t%.2f\t%d\t%.1f%%\t\n",
				op.Name,
				op.Throughput,
				op.P50Ms,
				op.P95Ms,
				op.P99Ms,
				op.MinMs,
				op.MaxMs,
				op.FailedOps,
				op.ErrorRate*100,
			)
		}
		tw.Flush()

		fmt.Fprintln(w)
		fmt.Fprintf(w, "  Total Throughput: %.0f ops/sec\n", phase.TotalThroughput)
		fmt.Fprintf(w, "  Duration:         %.1fs\n", phase.DurationSeconds)
		if phase.OverallErrorRate > 0 {
			fmt.Fprintf(w, "  Error Rate:       %.2f%%\n", phase.OverallErrorRate*100)
		}
		fmt.Fprintln(w)
	}

	// Disclaimer
	fmt.Fprintln(w, strings.Repeat("=", 80))
	if m.SyntheticDisclaimer != "" {
		fmt.Fprintf(w, "⚠  %s\n", m.SyntheticDisclaimer)
	}
	fmt.Fprintln(w, strings.Repeat("=", 80))

	return nil
}

// formatNumber adds thousand separators.
func formatNumber(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}

	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

// formatBytes returns a human-friendly byte size.
func formatBytes(b int) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%d MB", b/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%d KB", b/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
