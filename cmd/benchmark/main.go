// Package main provides the CLI entrypoint for the benchmark tool.
//
// Usage:
//
//	benchmark run --config benchmark.yaml
//	benchmark setup-index --config benchmark.yaml
//	benchmark load-data --config benchmark.yaml
//	benchmark check --config benchmark.yaml
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"benchmarkDB/internal/benchmark"
	"benchmarkDB/internal/config"
	"benchmarkDB/internal/dataset"
	"benchmarkDB/internal/datastore"
	aeroAdapter "benchmarkDB/internal/datastore/aerospike"
	redisAdapter "benchmarkDB/internal/datastore/redis"
	"benchmarkDB/internal/output"
)

var cfgFile string

func main() {
	rootCmd := &cobra.Command{
		Use:   "benchmark",
		Short: "Database benchmark tool for Aerospike vs Redis Cluster",
		Long: `A benchmark tool to evaluate whether Redis Cluster can replace Aerospike 
in a 5G telecom system. Measures throughput, latency percentiles, and error rates
for primary-key operations and secondary-index queries.

WARNING: All workload ratios are synthetic unless based on production traffic data.`,
	}

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file path (e.g. benchmark.yaml)")

	v := viper.New()
	config.BindFlags(rootCmd, v)

	// --- run command ---
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run the full benchmark",
		Long:  "Execute all configured benchmark phases (connectivity, single, concurrency, mixed, saturation).",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBenchmark(cmd.Context())
		},
	}

	// --- setup-index command (Section 27) ---
	setupIndexCmd := &cobra.Command{
		Use:   "setup-index",
		Short: "Create secondary indexes on the target database",
		Long:  "Creates all required secondary indexes without running the benchmark. Index creation time is not included in benchmark measurements.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetupIndex(cmd.Context())
		},
	}

	// --- load-data command ---
	loadDataCmd := &cobra.Command{
		Use:   "load-data",
		Short: "Load the benchmark dataset into the target database",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLoadData(cmd.Context())
		},
	}

	// --- check command (connectivity/capability check) ---
	checkCmd := &cobra.Command{
		Use:   "check",
		Short: "Check database connectivity and capabilities",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCheck(cmd.Context())
		},
	}

	rootCmd.AddCommand(runCmd, setupIndexCmd, loadDataCmd, checkCmd)

	// Handle graceful shutdown.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		slog.Info("Received shutdown signal, stopping benchmark...")
		cancel()
	}()

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}

// runBenchmark executes the full benchmark pipeline:
// connect → check capabilities → load data → setup indexes → run phases → report.
func runBenchmark(ctx context.Context) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	store, err := createStore(cfg)
	if err != nil {
		return err
	}

	// Connect.
	slog.Info("Connecting to database", "type", cfg.Database.Type)
	if err := store.Connect(ctx); err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer store.Close()

	// Check capabilities.
	slog.Info("Checking database capabilities")
	if err := store.CheckCapabilities(ctx); err != nil {
		return fmt.Errorf("capability check failed: %w", err)
	}

	gen := dataset.NewGenerator(cfg.Dataset)
	engine := benchmark.NewEngine(cfg, store, gen)

	// Load data.
	slog.Info("Loading benchmark dataset")
	if err := engine.LoadData(ctx); err != nil {
		return fmt.Errorf("loading data: %w", err)
	}

	// Setup indexes.
	slog.Info("Setting up secondary indexes")
	if err := engine.SetupIndexes(ctx); err != nil {
		return fmt.Errorf("setting up indexes: %w", err)
	}

	// Run benchmark.
	slog.Info("Starting benchmark execution")
	report, err := engine.Run(ctx)
	if err != nil {
		return fmt.Errorf("benchmark execution failed: %w", err)
	}

	// Output results.
	if err := output.WriteReport(report, cfg.Output.Format, cfg.Output.File); err != nil {
		return fmt.Errorf("writing report: %w", err)
	}

	return nil
}

// runSetupIndex creates secondary indexes without running the benchmark.
func runSetupIndex(ctx context.Context) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	store, err := createStore(cfg)
	if err != nil {
		return err
	}

	if err := store.Connect(ctx); err != nil {
		return fmt.Errorf("connecting: %w", err)
	}
	defer store.Close()

	if err := store.CheckCapabilities(ctx); err != nil {
		return fmt.Errorf("capability check: %w", err)
	}

	gen := dataset.NewGenerator(cfg.Dataset)
	engine := benchmark.NewEngine(cfg, store, gen)

	return engine.SetupIndexes(ctx)
}

// runLoadData loads the dataset without running the benchmark.
func runLoadData(ctx context.Context) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	store, err := createStore(cfg)
	if err != nil {
		return err
	}

	if err := store.Connect(ctx); err != nil {
		return fmt.Errorf("connecting: %w", err)
	}
	defer store.Close()

	gen := dataset.NewGenerator(cfg.Dataset)
	engine := benchmark.NewEngine(cfg, store, gen)

	return engine.LoadData(ctx)
}

// runCheck verifies connectivity and capabilities.
func runCheck(ctx context.Context) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	store, err := createStore(cfg)
	if err != nil {
		return err
	}

	if err := store.Connect(ctx); err != nil {
		return fmt.Errorf("connecting: %w", err)
	}
	defer store.Close()

	if err := store.Ping(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "PING: FAILED — %v\n", err)
		return err
	}
	fmt.Println("PING: OK")

	if err := store.CheckCapabilities(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "CAPABILITIES: FAILED — %v\n", err)
		return err
	}
	fmt.Println("CAPABILITIES: OK")

	return nil
}

// createStore instantiates the correct DataStore adapter based on config.
func createStore(cfg *config.Config) (datastore.DataStore, error) {
	switch cfg.Database.Type {
	case "redis":
		return redisAdapter.New(cfg.Redis), nil
	case "aerospike":
		return aeroAdapter.New(cfg.Aerospike), nil
	default:
		return nil, fmt.Errorf("unsupported database type: %s", cfg.Database.Type)
	}
}
