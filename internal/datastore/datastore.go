// Package datastore defines the port interfaces for the benchmark tool.
//
// The benchmark engine operates exclusively through these interfaces,
// ensuring it remains database-agnostic. Concrete implementations (Redis
// Cluster adapter, Aerospike adapter) reside in sub-packages.
//
// Design rationale (Hexagonal Architecture / Ports & Adapters):
//   - KVStore: primary-key CRUD + TTL operations.
//   - SearchStore: secondary-index equality and range queries.
//   - DataStore: the combined "port" that adapters must implement.
//   - The engine never imports Redis or Aerospike SDKs directly.
package datastore

import (
	"context"
	"time"
)

// ---------- Value types ----------

// IndexType enumerates the types of secondary indexes.
type IndexType int

const (
	// IndexTypeString is used for string equality indexes.
	IndexTypeString IndexType = iota
	// IndexTypeNumeric is used for numeric equality and range indexes.
	IndexTypeNumeric
)

// String returns a human-readable label.
func (t IndexType) String() string {
	switch t {
	case IndexTypeString:
		return "STRING"
	case IndexTypeNumeric:
		return "NUMERIC"
	default:
		return "UNKNOWN"
	}
}

// Record represents a single logical database record.
// Key is the primary key; Fields holds the column/bin data.
type Record struct {
	Key    string
	Fields map[string]interface{}
}

// IndexDefinition describes a secondary index to create.
type IndexDefinition struct {
	Namespace string
	Set       string
	Field     string    // the bin/field name to index
	IndexName string    // unique name for the index
	IndexType IndexType // string or numeric
}

// EqualityQueryRequest describes an equality lookup on a secondary index.
type EqualityQueryRequest struct {
	Namespace string
	Set       string
	Field     string
	Value     interface{} // string or int64
}

// RangeQueryRequest describes a numeric range lookup on a secondary index.
type RangeQueryRequest struct {
	Namespace string
	Set       string
	Field     string
	Min       int64
	Max       int64
}

// QueryResult holds the records returned by a secondary-index query.
type QueryResult struct {
	Records []Record
	Count   int
}

// ---------- Port interfaces ----------

// KVStore defines primary-key operations.
//
// Implementations:
//   - Aerospike: maps to Put/Get/Delete with record-level TTL.
//   - Redis:     maps to HSET+EXPIRE / HGETALL / DEL on Redis Cluster.
type KVStore interface {
	// Set writes a record. A zero TTL means no expiration.
	Set(ctx context.Context, namespace, set, key string, fields map[string]interface{}, ttl time.Duration) error

	// Get reads a record by primary key. Returns nil Record and no error
	// if the key does not exist.
	Get(ctx context.Context, namespace, set, key string) (*Record, error)

	// Delete removes a record by primary key.
	Delete(ctx context.Context, namespace, set, key string) error
}

// SearchStore defines secondary-index operations.
//
// Implementations:
//   - Aerospike: maps to aerospike.Statement + Filter.Equal / Filter.Range.
//   - Redis:     maps to FT.CREATE / FT.SEARCH via Redis Query Engine.
type SearchStore interface {
	// CreateIndex creates a secondary index. Idempotent: must not fail
	// if the index already exists (log a warning instead).
	CreateIndex(ctx context.Context, def IndexDefinition) error

	// DropIndex removes a secondary index by name.
	DropIndex(ctx context.Context, namespace, indexName string) error

	// EqualityQuery executes an equality lookup on an indexed field.
	EqualityQuery(ctx context.Context, req EqualityQueryRequest) (*QueryResult, error)

	// RangeQuery executes a numeric range lookup on an indexed field.
	RangeQuery(ctx context.Context, req RangeQueryRequest) (*QueryResult, error)

	// IndexReady checks whether the given index has finished building
	// and is ready for queries. Returns true when the index covers the
	// expected number of documents (within tolerance).
	IndexReady(ctx context.Context, namespace, indexName string, expectedDocs int) (bool, error)
}

// DataStore is the combined port that each database adapter must implement.
// It adds lifecycle methods on top of KV and Search capabilities.
type DataStore interface {
	KVStore
	SearchStore

	// Name returns a human-readable identifier (e.g. "Redis Cluster", "Aerospike").
	Name() string

	// Connect establishes a connection to the database cluster.
	Connect(ctx context.Context) error

	// Close gracefully shuts down the connection.
	Close() error

	// CheckCapabilities verifies that all features required by the
	// benchmark are available. For Redis this MUST check that the
	// Redis Query Engine (RediSearch) module is loaded. If a required
	// capability is missing, return a *errors.CapabilityError.
	CheckCapabilities(ctx context.Context) error

	// Ping is a lightweight connectivity check.
	Ping(ctx context.Context) error
}
