// Package errors provides typed error definitions for the benchmark tool.
// Each error type corresponds to a specific failure mode, enabling callers
// to classify errors without string matching.
package errors

import "fmt"

// ConnectionError indicates a failure to connect to the database.
type ConnectionError struct {
	Database string
	Host     string
	Cause    error
}

func (e *ConnectionError) Error() string {
	return fmt.Sprintf("connection error [%s @ %s]: %v", e.Database, e.Host, e.Cause)
}

func (e *ConnectionError) Unwrap() error { return e.Cause }

// TimeoutError indicates an operation exceeded its deadline.
type TimeoutError struct {
	Database  string
	Operation string
	Cause     error
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("timeout [%s.%s]: %v", e.Database, e.Operation, e.Cause)
}

func (e *TimeoutError) Unwrap() error { return e.Cause }

// CapabilityError indicates a required database feature is unavailable.
// For example, Redis Query Engine not being available.
type CapabilityError struct {
	Database   string
	Capability string
	Message    string
}

func (e *CapabilityError) Error() string {
	return fmt.Sprintf("capability unavailable [%s]: %s - %s", e.Database, e.Capability, e.Message)
}

// ConfigError indicates invalid or missing configuration.
type ConfigError struct {
	Field   string
	Message string
}

func (e *ConfigError) Error() string {
	return fmt.Sprintf("config error [%s]: %s", e.Field, e.Message)
}

// DatasetError indicates a failure during data generation or loading.
type DatasetError struct {
	Operation string
	Cause     error
}

func (e *DatasetError) Error() string {
	return fmt.Sprintf("dataset error [%s]: %v", e.Operation, e.Cause)
}

func (e *DatasetError) Unwrap() error { return e.Cause }

// QueryError indicates a failure during a secondary index query.
type QueryError struct {
	Database  string
	QueryType string // "equality" or "range"
	Field     string
	Cause     error
}

func (e *QueryError) Error() string {
	return fmt.Sprintf("query error [%s.%s on field %q]: %v", e.Database, e.QueryType, e.Field, e.Cause)
}

func (e *QueryError) Unwrap() error { return e.Cause }

// IndexError indicates a failure creating or verifying a secondary index.
type IndexError struct {
	Database  string
	IndexName string
	Operation string // "create", "drop", "verify"
	Cause     error
}

func (e *IndexError) Error() string {
	return fmt.Sprintf("index error [%s.%s.%s]: %v", e.Database, e.IndexName, e.Operation, e.Cause)
}

func (e *IndexError) Unwrap() error { return e.Cause }

// SerializationError indicates a failure encoding/decoding data.
type SerializationError struct {
	Database  string
	Operation string
	Cause     error
}

func (e *SerializationError) Error() string {
	return fmt.Sprintf("serialization error [%s.%s]: %v", e.Database, e.Operation, e.Cause)
}

func (e *SerializationError) Unwrap() error { return e.Cause }
