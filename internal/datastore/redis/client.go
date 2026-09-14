// Package redis implements the DataStore interface for Redis Cluster.
//
// client.go handles cluster connection, topology discovery, and
// capability checks (especially Redis Query Engine availability).
//
// CRITICAL (Section 6, 24):
//   - MUST use Redis Cluster-aware client (go-redis ClusterClient).
//   - MUST NOT send all traffic to a single Redis pod.
//   - MUST verify Redis Query Engine availability before search benchmarks.
package redis

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"benchmarkDB/internal/config"
	bencherr "benchmarkDB/internal/errors"
)

// Store implements datastore.DataStore for Redis Cluster.
type Store struct {
	client *goredis.ClusterClient
	cfg    config.RedisConfig
	logger *slog.Logger
}

// New creates a new Redis Cluster store (does NOT connect yet).
func New(cfg config.RedisConfig) *Store {
	return &Store{
		cfg:    cfg,
		logger: slog.Default().With("adapter", "redis"),
	}
}

// Name returns the human-readable identifier.
func (s *Store) Name() string { return "Redis Cluster" }

// Connect establishes a connection to the Redis Cluster using
// the configured startup nodes for topology discovery.
func (s *Store) Connect(ctx context.Context) error {
	if len(s.cfg.StartupNodes) == 0 {
		return &bencherr.ConfigError{
			Field:   "redis.startup_nodes",
			Message: "no startup nodes configured",
		}
	}

	readTimeout := parseDurationOrDefault(s.cfg.ReadTimeout, 3*time.Second)
	writeTimeout := parseDurationOrDefault(s.cfg.WriteTimeout, 3*time.Second)

	opts := &goredis.ClusterOptions{
		Addrs:        s.cfg.StartupNodes,
		Password:     s.cfg.Password,
		MaxRetries:   s.cfg.MaxRetries,
		PoolSize:     s.cfg.PoolSize,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		// RouteByLatency enables routing read commands to the nearest node.
		RouteByLatency: true,
	}

	client := goredis.NewClusterClient(opts)

	// Verify connectivity.
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return &bencherr.ConnectionError{
			Database: "Redis Cluster",
			Host:     s.cfg.StartupNodes[0],
			Cause:    err,
		}
	}

	s.client = client
	s.logger.Info("Connected to Redis Cluster",
		"startup_nodes", s.cfg.StartupNodes,
	)
	return nil
}

// Close shuts down the Redis Cluster client.
func (s *Store) Close() error {
	if s.client != nil {
		err := s.client.Close()
		s.logger.Info("Redis Cluster connection closed")
		return err
	}
	return nil
}

// Ping checks cluster connectivity.
func (s *Store) Ping(ctx context.Context) error {
	if s.client == nil {
		return &bencherr.ConnectionError{
			Database: "Redis Cluster",
			Host:     "",
			Cause:    fmt.Errorf("client not initialized"),
		}
	}
	if err := s.client.Ping(ctx).Err(); err != nil {
		return &bencherr.ConnectionError{
			Database: "Redis Cluster",
			Host:     "",
			Cause:    err,
		}
	}
	return nil
}

// CheckCapabilities verifies that the Redis Query Engine (RediSearch)
// module is available. This is a HARD REQUIREMENT for secondary index
// benchmarks (Section 7, 24).
//
// If the module is not available, returns a *CapabilityError.
// This MUST NOT silently fall back to SCAN + client-side filtering.
func (s *Store) CheckCapabilities(ctx context.Context) error {
	if err := s.Ping(ctx); err != nil {
		return err
	}

	// Method 1: Try FT._LIST command.
	err := s.client.Do(ctx, "FT._LIST").Err()
	if err == nil {
		s.logger.Info("Redis Query Engine (RediSearch) is available")
		return nil
	}

	// Method 2: Try MODULE LIST and look for "search" module.
	result, err := s.client.Do(ctx, "MODULE", "LIST").Result()
	if err == nil {
		if modules, ok := result.([]interface{}); ok {
			for _, mod := range modules {
				if modInfo, ok := mod.([]interface{}); ok {
					for i, item := range modInfo {
						if str, ok := item.(string); ok && str == "name" && i+1 < len(modInfo) {
							if name, ok := modInfo[i+1].(string); ok {
								if name == "search" || name == "ft" || name == "ReJSON" {
									s.logger.Info("Redis Query Engine module found", "module", name)
									return nil
								}
							}
						}
					}
				}
			}
		}
	}

	return &bencherr.CapabilityError{
		Database:   "Redis Cluster",
		Capability: "Redis Query Engine/Search",
		Message:    "Redis Query Engine/Search capability is required for Secondary Index benchmark but is not available. " +
			"Please deploy Redis with the RediSearch module enabled.",
	}
}

// getClient returns the underlying ClusterClient (used by kv.go, search.go).
func (s *Store) getClient() *goredis.ClusterClient {
	return s.client
}

// parseDurationOrDefault parses a duration string, returning the default on error.
func parseDurationOrDefault(s string, def time.Duration) time.Duration {
	if s == "" {
		return def
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return def
	}
	return d
}
