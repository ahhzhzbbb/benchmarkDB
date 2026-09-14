// Package output provides formatters for benchmark results.
//
// metadata.go collects and structures environment metadata so that every
// benchmark run is self-describing: which database, what topology, which
// Kubernetes node, etc.  This is critical for reproducibility (Section 16).
package output

import (
	"os"
	"runtime"
	"time"

	"benchmarkDB/internal/config"
)

// Metadata captures the full context of a benchmark run.
// It is embedded in both human-readable and JSON outputs.
type Metadata struct {
	// Timestamp is when the benchmark was executed.
	Timestamp string `json:"timestamp"`

	// Database identification.
	DatabaseType string `json:"database_type"`
	DatabaseName string `json:"database_name"` // e.g. "Redis Cluster", "Aerospike"

	// Topology info.
	Topology TopologyInfo `json:"topology"`

	// Benchmark client info.
	Client ClientInfo `json:"client"`

	// Benchmark parameters.
	Benchmark BenchmarkInfo `json:"benchmark"`

	// Dataset parameters.
	Dataset DatasetInfo `json:"dataset"`

	// Workload configuration.
	Workload WorkloadInfo `json:"workload"`

	// Scenario: "equal_resource" or "production" (Section 25).
	Scenario string `json:"scenario"`

	// SyntheticDisclaimer is always set for non-production workloads.
	SyntheticDisclaimer string `json:"synthetic_disclaimer,omitempty"`
}

// TopologyInfo describes the database cluster topology.
type TopologyInfo struct {
	Masters  int      `json:"masters,omitempty"`
	Replicas int      `json:"replicas,omitempty"`
	Nodes    int      `json:"nodes,omitempty"`
	Hosts    []string `json:"hosts,omitempty"`
}

// ClientInfo describes the benchmark client environment.
type ClientInfo struct {
	Hostname       string `json:"hostname"`
	KubernetesNode string `json:"kubernetes_node,omitempty"`
	PodName        string `json:"pod_name,omitempty"`
	GoVersion      string `json:"go_version"`
	GOOS           string `json:"goos"`
	GOARCH         string `json:"goarch"`
	NumCPU         int    `json:"num_cpu"`
}

// BenchmarkInfo summarizes the benchmark execution parameters.
type BenchmarkInfo struct {
	Warmup      string `json:"warmup"`
	Duration    string `json:"duration"`
	Concurrency int    `json:"concurrency"`
	Scenario    string `json:"scenario"`
}

// DatasetInfo summarizes dataset parameters.
type DatasetInfo struct {
	KeyCount  int    `json:"key_count"`
	ValueSize int    `json:"value_size"`
	Seed      int64  `json:"seed"`
	Namespace string `json:"namespace"`
	Set       string `json:"set"`
}

// WorkloadInfo summarizes the workload mix.
type WorkloadInfo struct {
	Type       string         `json:"type"`
	Operations map[string]int `json:"operations,omitempty"`
}

// CollectMetadata gathers environment metadata from the runtime
// and the provided configuration.
func CollectMetadata(cfg *config.Config, databaseName string) *Metadata {
	hostname, _ := os.Hostname()

	m := &Metadata{
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		DatabaseType: cfg.Database.Type,
		DatabaseName: databaseName,
		Client: ClientInfo{
			Hostname:       hostname,
			KubernetesNode: os.Getenv("KUBERNETES_NODE_NAME"),
			PodName:        os.Getenv("HOSTNAME"),
			GoVersion:      runtime.Version(),
			GOOS:           runtime.GOOS,
			GOARCH:         runtime.GOARCH,
			NumCPU:         runtime.NumCPU(),
		},
		Benchmark: BenchmarkInfo{
			Warmup:      cfg.Benchmark.Warmup,
			Duration:    cfg.Benchmark.Duration,
			Concurrency: cfg.Benchmark.Concurrency,
			Scenario:    cfg.Benchmark.Scenario,
		},
		Dataset: DatasetInfo{
			KeyCount:  cfg.Dataset.KeyCount,
			ValueSize: cfg.Dataset.ValueSize,
			Seed:      cfg.Dataset.Seed,
			Namespace: cfg.Dataset.Namespace,
			Set:       cfg.Dataset.Set,
		},
		Workload: WorkloadInfo{
			Type: cfg.Workload.Type,
		},
		Scenario: cfg.Benchmark.Scenario,
	}

	// Populate topology based on database type.
	switch cfg.Database.Type {
	case "redis":
		m.Topology = TopologyInfo{
			Hosts: cfg.Redis.StartupNodes,
		}
	case "aerospike":
		m.Topology = TopologyInfo{
			Hosts: cfg.Aerospike.Hosts,
		}
	}

	// Populate workload operations if mixed.
	if cfg.Workload.Type == "mixed" {
		ops := cfg.Workload.Operations
		m.Workload.Operations = map[string]int{
			"get":            ops.Get,
			"set":            ops.Set,
			"delete":         ops.Delete,
			"ttl":            ops.TTL,
			"equality_query": ops.EqualityQuery,
			"range_query":    ops.RangeQuery,
		}
	}

	// Section 10: always disclose synthetic workloads.
	m.SyntheticDisclaimer = "WARNING: This is a synthetic benchmark workload. " +
		"Operation ratios do NOT represent production traffic patterns. " +
		"Results should be interpreted in context of actual production workload."

	return m
}
