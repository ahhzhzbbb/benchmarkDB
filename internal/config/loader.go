package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Load reads configuration from a YAML file (if provided) and merges
// CLI flag overrides. It applies defaults and validates the result.
func Load(cfgFile string) (*Config, error) {
	v := viper.New()

	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("reading config file %s: %w", cfgFile, err)
		}
	}

	// Allow environment variable overrides with BENCH_ prefix.
	v.SetEnvPrefix("BENCH")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshalling config: %w", err)
	}

	cfg.ApplyDefaults()

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// BindFlags registers CLI flags on the given cobra command and binds
// them to viper so they override config-file values.
func BindFlags(cmd *cobra.Command, v *viper.Viper) {
	flags := cmd.Flags()

	// Database
	flags.String("database-type", "", "Database to benchmark: redis or aerospike")

	// Redis
	flags.StringSlice("redis-startup-nodes", nil, "Redis Cluster seed nodes (host:port)")
	flags.String("redis-password", "", "Redis password")

	// Aerospike
	flags.StringSlice("aerospike-hosts", nil, "Aerospike seed hosts (host:port)")
	flags.String("aerospike-namespace", "", "Aerospike namespace")

	// Workload
	flags.String("workload-type", "", "Workload type: get/set/delete/ttl/equality_query/range_query/mixed")

	// Dataset
	flags.Int("dataset-key-count", 0, "Number of keys in the dataset")
	flags.Int("dataset-value-size", 0, "Value size in bytes")
	flags.Int64("dataset-seed", 0, "Deterministic seed for data generation")
	flags.String("dataset-namespace", "", "Namespace for the dataset")
	flags.String("dataset-set", "", "Set name for the dataset")

	// Benchmark
	flags.String("benchmark-warmup", "", "Warm-up duration (e.g. 10s)")
	flags.String("benchmark-duration", "", "Measurement duration (e.g. 60s)")
	flags.Int("benchmark-concurrency", 0, "Number of concurrent workers")
	flags.String("benchmark-scenario", "", "Scenario: equal_resource or production")

	// Output
	flags.String("output-format", "", "Output format: text/json/both")
	flags.String("output-file", "", "Path for JSON output file")

	// Bind to viper with dot-notation keys.
	bindings := map[string]string{
		"database-type":         "database.type",
		"redis-startup-nodes":   "redis.startup_nodes",
		"redis-password":        "redis.password",
		"aerospike-hosts":       "aerospike.hosts",
		"aerospike-namespace":   "aerospike.namespace",
		"workload-type":         "workload.type",
		"dataset-key-count":     "dataset.key_count",
		"dataset-value-size":    "dataset.value_size",
		"dataset-seed":          "dataset.seed",
		"dataset-namespace":     "dataset.namespace",
		"dataset-set":           "dataset.set",
		"benchmark-warmup":      "benchmark.warmup",
		"benchmark-duration":    "benchmark.duration",
		"benchmark-concurrency": "benchmark.concurrency",
		"benchmark-scenario":    "benchmark.scenario",
		"output-format":         "output.format",
		"output-file":           "output.file",
	}

	for flag, key := range bindings {
		_ = v.BindPFlag(key, flags.Lookup(flag))
	}
}

// LoadWithViper loads config using a pre-configured viper instance
// (useful when flags have been bound).
func LoadWithViper(v *viper.Viper) (*Config, error) {
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshalling config: %w", err)
	}

	cfg.ApplyDefaults()

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// MustLoad is like Load but exits the process on error.
func MustLoad(cfgFile string) *Config {
	cfg, err := Load(cfgFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Fatal: %v\n", err)
		os.Exit(1)
	}
	return cfg
}
