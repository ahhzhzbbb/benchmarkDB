// Package aerospike implements the DataStore interface for Aerospike clusters.
//
// client.go handles connection lifecycle and capability checks.
package aerospike

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	aero "github.com/aerospike/aerospike-client-go/v7"

	"benchmarkDB/internal/config"
	bencherr "benchmarkDB/internal/errors"
)

// Store implements datastore.DataStore for Aerospike.
type Store struct {
	client *aero.Client
	cfg    config.AerospikeConfig
	logger *slog.Logger
}

// New creates a new Aerospike store (does NOT connect yet).
func New(cfg config.AerospikeConfig) *Store {
	return &Store{
		cfg:    cfg,
		logger: slog.Default().With("adapter", "aerospike"),
	}
}

// Name returns the human-readable identifier.
func (s *Store) Name() string { return "Aerospike" }

// Connect establishes a connection to the Aerospike cluster.
func (s *Store) Connect(ctx context.Context) error {
	if len(s.cfg.Hosts) == 0 {
		return &bencherr.ConfigError{
			Field:   "aerospike.hosts",
			Message: "no hosts configured",
		}
	}

	hosts := make([]*aero.Host, 0, len(s.cfg.Hosts))
	for _, h := range s.cfg.Hosts {
		host, port, err := parseHostPort(h, 3000)
		if err != nil {
			return &bencherr.ConfigError{
				Field:   "aerospike.hosts",
				Message: fmt.Sprintf("invalid host %q: %v", h, err),
			}
		}
		hosts = append(hosts, aero.NewHost(host, port))
	}

	policy := aero.NewClientPolicy()

	if s.cfg.TotalTimeout != "" {
		d, err := time.ParseDuration(s.cfg.TotalTimeout)
		if err == nil {
			policy.Timeout = d
		}
	}

	if s.cfg.AuthUser != "" {
		policy.User = s.cfg.AuthUser
		policy.Password = s.cfg.AuthPassword
	}

	client, err := aero.NewClientWithPolicyAndHost(policy, hosts...)
	if err != nil {
		return &bencherr.ConnectionError{
			Database: "Aerospike",
			Host:     s.cfg.Hosts[0],
			Cause:    err,
		}
	}

	s.client = client
	s.logger.Info("Connected to Aerospike cluster",
		"hosts", s.cfg.Hosts,
		"nodes", len(client.GetNodes()),
	)
	return nil
}

// Close shuts down the Aerospike client.
func (s *Store) Close() error {
	if s.client != nil {
		s.client.Close()
		s.logger.Info("Aerospike connection closed")
	}
	return nil
}

// Ping checks that the cluster is reachable.
func (s *Store) Ping(ctx context.Context) error {
	if s.client == nil || !s.client.IsConnected() {
		return &bencherr.ConnectionError{
			Database: "Aerospike",
			Host:     strings.Join(s.cfg.Hosts, ","),
			Cause:    fmt.Errorf("client not connected"),
		}
	}

	nodes := s.client.GetNodes()
	if len(nodes) == 0 {
		return &bencherr.ConnectionError{
			Database: "Aerospike",
			Host:     strings.Join(s.cfg.Hosts, ","),
			Cause:    fmt.Errorf("no nodes available"),
		}
	}

	return nil
}

// CheckCapabilities verifies required features.
// Aerospike always supports secondary indexes, so this just checks connectivity.
func (s *Store) CheckCapabilities(ctx context.Context) error {
	return s.Ping(ctx)
}

// getClient returns the underlying Aerospike client (used by kv.go, search.go).
func (s *Store) getClient() *aero.Client {
	return s.client
}

// writePolicy returns a write policy with optional TTL.
func (s *Store) writePolicy(ttl time.Duration) *aero.WritePolicy {
	wp := aero.NewWritePolicy(0, 0)
	if ttl > 0 {
		wp.Expiration = uint32(ttl.Seconds())
	}
	return wp
}

// parseHostPort splits "host:port" strings, defaulting to the given port.
func parseHostPort(addr string, defaultPort int) (string, int, error) {
	parts := strings.SplitN(addr, ":", 2)
	host := parts[0]
	port := defaultPort

	if len(parts) == 2 {
		p, err := strconv.Atoi(parts[1])
		if err != nil {
			return "", 0, fmt.Errorf("invalid port: %w", err)
		}
		port = p
	}

	return host, port, nil
}
