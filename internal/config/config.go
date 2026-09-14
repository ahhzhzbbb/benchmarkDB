// Package config defines the structured configuration for the benchmark tool.
// Configuration can be loaded from a YAML file and/or overridden via CLI flags.
package config

import (
	"fmt"
	"time"
)

// Config is the top-level configuration structure.
type Config struct {
	Database  DatabaseConfig  `yaml:"database"  mapstructure:"database"`
	Redis     RedisConfig     `yaml:"redis"     mapstructure:"redis"`
	Aerospike AerospikeConfig `yaml:"aerospike" mapstructure:"aerospike"`
	Workload  WorkloadConfig  `yaml:"workload"  mapstructure:"workload"`
	Dataset   DatasetConfig   `yaml:"dataset"   mapstructure:"dataset"`
	Benchmark BenchmarkConfig `yaml:"benchmark" mapstructure:"benchmark"`
	Output    OutputConfig    `yaml:"output"    mapstructure:"output"`
}

// DatabaseConfig specifies which database to benchmark.
type DatabaseConfig struct {
	// Type must be "redis" or "aerospike".
	Type string `yaml:"type" mapstructure:"type"`
}

// RedisConfig holds Redis Cluster connection parameters.
type RedisConfig struct {
	// StartupNodes is the list of seed nodes for cluster discovery.
	StartupNodes []string `yaml:"startup_nodes" mapstructure:"startup_nodes"`
	Password     string   `yaml:"password"      mapstructure:"password"`
	MaxRetries   int      `yaml:"max_retries"   mapstructure:"max_retries"`
	PoolSize     int      `yaml:"pool_size"     mapstructure:"pool_size"`
	ReadTimeout  string   `yaml:"read_timeout"  mapstructure:"read_timeout"`
	WriteTimeout string   `yaml:"write_timeout" mapstructure:"write_timeout"`
}

// AerospikeConfig holds Aerospike cluster connection parameters.
type AerospikeConfig struct {
	// Hosts is the list of seed hosts in "host:port" format.
	Hosts         []string `yaml:"hosts"          mapstructure:"hosts"`
	Namespace     string   `yaml:"namespace"      mapstructure:"namespace"`
	AuthUser      string   `yaml:"auth_user"      mapstructure:"auth_user"`
	AuthPassword  string   `yaml:"auth_password"  mapstructure:"auth_password"`
	TotalTimeout  string   `yaml:"total_timeout"  mapstructure:"total_timeout"`
	SocketTimeout string   `yaml:"socket_timeout" mapstructure:"socket_timeout"`
}

// WorkloadConfig defines the workload mix.
type WorkloadConfig struct {
	// Type: "get", "set", "delete", "ttl", "equality_query", "range_query", "mixed".
	Type string `yaml:"type" mapstructure:"type"`
	// Operations specifies the ratio for mixed workloads.
	Operations OperationsRatio `yaml:"operations" mapstructure:"operations"`
}

// OperationsRatio defines the weighted ratio of each operation in a mixed workload.
// Values are relative weights (e.g. get:50 set:20 → 50/100 and 20/100).
type OperationsRatio struct {
	Get           int `yaml:"get"            mapstructure:"get"`
	Set           int `yaml:"set"            mapstructure:"set"`
	Delete        int `yaml:"delete"         mapstructure:"delete"`
	TTL           int `yaml:"ttl"            mapstructure:"ttl"`
	EqualityQuery int `yaml:"equality_query" mapstructure:"equality_query"`
	RangeQuery    int `yaml:"range_query"    mapstructure:"range_query"`
}

// Total returns the sum of all operation weights.
func (o *OperationsRatio) Total() int {
	return o.Get + o.Set + o.Delete + o.TTL + o.EqualityQuery + o.RangeQuery
}

// DatasetConfig defines how the benchmark dataset is generated.
type DatasetConfig struct {
	KeyCount          int    `yaml:"key_count"          mapstructure:"key_count"`
	ValueSize         int    `yaml:"value_size"         mapstructure:"value_size"`         // bytes
	Seed              int64  `yaml:"seed"               mapstructure:"seed"`
	Namespace         string `yaml:"namespace"          mapstructure:"namespace"`
	Set               string `yaml:"set"                mapstructure:"set"`
	StringCardinality int    `yaml:"string_cardinality" mapstructure:"string_cardinality"` // distinct values for string fields
	NumericMin        int64  `yaml:"numeric_min"        mapstructure:"numeric_min"`
	NumericMax        int64  `yaml:"numeric_max"        mapstructure:"numeric_max"`
	TTL               string `yaml:"ttl"                mapstructure:"ttl"` // for TTL workload, e.g. "300s"
}

// BenchmarkConfig defines execution parameters.
type BenchmarkConfig struct {
	Warmup      string   `yaml:"warmup"      mapstructure:"warmup"`      // e.g. "10s"
	Duration    string   `yaml:"duration"    mapstructure:"duration"`    // e.g. "60s"
	Concurrency int      `yaml:"concurrency" mapstructure:"concurrency"`
	Phases      []string `yaml:"phases"      mapstructure:"phases"`      // e.g. ["connectivity","single","concurrency","mixed","saturation"]
	// ConcurrencySteps is used in Phase 3 (concurrency) and Phase 5 (saturation).
	ConcurrencySteps []int `yaml:"concurrency_steps" mapstructure:"concurrency_steps"` // e.g. [1,10,50,100,200,500]
	// Scenario: "equal_resource" or "production".
	Scenario string `yaml:"scenario" mapstructure:"scenario"`
}

// OutputConfig controls how results are reported.
type OutputConfig struct {
	// Format: "text", "json", or "both".
	Format string `yaml:"format" mapstructure:"format"`
	// File is the path for JSON output (optional).
	File string `yaml:"file" mapstructure:"file"`
}

// ---------- Parsed duration helpers ----------

// WarmupDuration parses the Warmup string.
func (b *BenchmarkConfig) WarmupDuration() (time.Duration, error) {
	if b.Warmup == "" {
		return 10 * time.Second, nil // default
	}
	return time.ParseDuration(b.Warmup)
}

// MeasureDuration parses the Duration string.
func (b *BenchmarkConfig) MeasureDuration() (time.Duration, error) {
	if b.Duration == "" {
		return 60 * time.Second, nil // default
	}
	return time.ParseDuration(b.Duration)
}

// DatasetTTL parses the TTL string in DatasetConfig.
func (d *DatasetConfig) DatasetTTL() (time.Duration, error) {
	if d.TTL == "" {
		return 0, nil // no TTL
	}
	return time.ParseDuration(d.TTL)
}

// ---------- Validation ----------

// Validate checks that the configuration is internally consistent and
// all required fields are present. Returns a descriptive error on failure.
func (c *Config) Validate() error {
	// Database type
	switch c.Database.Type {
	case "redis", "aerospike":
	default:
		return fmt.Errorf("database.type must be 'redis' or 'aerospike', got %q", c.Database.Type)
	}

	// Connection endpoints
	if c.Database.Type == "redis" && len(c.Redis.StartupNodes) == 0 {
		return fmt.Errorf("redis.startup_nodes must not be empty when database.type is 'redis'")
	}
	if c.Database.Type == "aerospike" && len(c.Aerospike.Hosts) == 0 {
		return fmt.Errorf("aerospike.hosts must not be empty when database.type is 'aerospike'")
	}

	// Workload
	switch c.Workload.Type {
	case "get", "set", "delete", "ttl", "equality_query", "range_query", "mixed":
	default:
		return fmt.Errorf("workload.type must be one of get/set/delete/ttl/equality_query/range_query/mixed, got %q", c.Workload.Type)
	}
	if c.Workload.Type == "mixed" && c.Workload.Operations.Total() == 0 {
		return fmt.Errorf("workload.operations must have at least one non-zero weight for mixed workload")
	}

	// Dataset
	if c.Dataset.KeyCount <= 0 {
		return fmt.Errorf("dataset.key_count must be > 0")
	}
	if c.Dataset.ValueSize <= 0 {
		return fmt.Errorf("dataset.value_size must be > 0")
	}
	if c.Dataset.Namespace == "" {
		return fmt.Errorf("dataset.namespace must not be empty")
	}
	if c.Dataset.Set == "" {
		return fmt.Errorf("dataset.set must not be empty")
	}

	// Benchmark
	if c.Benchmark.Concurrency <= 0 {
		return fmt.Errorf("benchmark.concurrency must be > 0")
	}
	if _, err := c.Benchmark.WarmupDuration(); err != nil {
		return fmt.Errorf("benchmark.warmup: %w", err)
	}
	if _, err := c.Benchmark.MeasureDuration(); err != nil {
		return fmt.Errorf("benchmark.duration: %w", err)
	}

	// Output
	switch c.Output.Format {
	case "", "text", "json", "both":
	default:
		return fmt.Errorf("output.format must be text/json/both, got %q", c.Output.Format)
	}

	return nil
}

// ApplyDefaults fills in zero-value fields with sensible defaults.
func (c *Config) ApplyDefaults() {
	if c.Benchmark.Warmup == "" {
		c.Benchmark.Warmup = "10s"
	}
	if c.Benchmark.Duration == "" {
		c.Benchmark.Duration = "60s"
	}
	if c.Benchmark.Concurrency == 0 {
		c.Benchmark.Concurrency = 10
	}
	if len(c.Benchmark.Phases) == 0 {
		c.Benchmark.Phases = []string{"connectivity", "single", "mixed"}
	}
	if len(c.Benchmark.ConcurrencySteps) == 0 {
		c.Benchmark.ConcurrencySteps = []int{1, 10, 50, 100}
	}
	if c.Benchmark.Scenario == "" {
		c.Benchmark.Scenario = "equal_resource"
	}

	if c.Dataset.KeyCount == 0 {
		c.Dataset.KeyCount = 100000
	}
	if c.Dataset.ValueSize == 0 {
		c.Dataset.ValueSize = 1024
	}
	if c.Dataset.Seed == 0 {
		c.Dataset.Seed = 12345
	}
	if c.Dataset.Namespace == "" {
		c.Dataset.Namespace = "benchmark"
	}
	if c.Dataset.Set == "" {
		c.Dataset.Set = "session"
	}
	if c.Dataset.StringCardinality == 0 {
		c.Dataset.StringCardinality = 1000
	}
	if c.Dataset.NumericMin == 0 && c.Dataset.NumericMax == 0 {
		c.Dataset.NumericMin = 1700000000
		c.Dataset.NumericMax = 1710000000
	}

	if c.Output.Format == "" {
		c.Output.Format = "text"
	}

	if c.Redis.MaxRetries == 0 {
		c.Redis.MaxRetries = 3
	}
	if c.Redis.PoolSize == 0 {
		c.Redis.PoolSize = 100
	}
	if c.Redis.ReadTimeout == "" {
		c.Redis.ReadTimeout = "3s"
	}
	if c.Redis.WriteTimeout == "" {
		c.Redis.WriteTimeout = "3s"
	}

	if c.Aerospike.TotalTimeout == "" {
		c.Aerospike.TotalTimeout = "5s"
	}
	if c.Aerospike.SocketTimeout == "" {
		c.Aerospike.SocketTimeout = "3s"
	}
}

// ---------- Preset profiles (Section 10) ----------

// ReadHeavyProfile returns a read-dominated operation ratio.
// Labeled explicitly as synthetic workload.
func ReadHeavyProfile() OperationsRatio {
	return OperationsRatio{
		Get:           70,
		Set:           15,
		Delete:        5,
		EqualityQuery: 7,
		RangeQuery:    3,
	}
}

// BalancedProfile returns an even operation ratio.
// Labeled explicitly as synthetic workload.
func BalancedProfile() OperationsRatio {
	return OperationsRatio{
		Get:           40,
		Set:           25,
		Delete:        10,
		EqualityQuery: 15,
		RangeQuery:    10,
	}
}

// QueryHeavyProfile returns a query-dominated operation ratio.
// Labeled explicitly as synthetic workload.
func QueryHeavyProfile() OperationsRatio {
	return OperationsRatio{
		Get:           20,
		Set:           10,
		Delete:        5,
		EqualityQuery: 40,
		RangeQuery:    25,
	}
}
