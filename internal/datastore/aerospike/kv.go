package aerospike

import (
	"context"
	"errors"
	"time"

	aero "github.com/aerospike/aerospike-client-go/v7"
	aerotypes "github.com/aerospike/aerospike-client-go/v7/types"

	"benchmarkDB/internal/datastore"
	bencherr "benchmarkDB/internal/errors"
)

// Set writes a record to Aerospike.
// If ttl > 0, the record-level TTL is set (Section 9 - Workload 4).
func (s *Store) Set(ctx context.Context, namespace, set, key string, fields map[string]interface{}, ttl time.Duration) error {
	aeroKey, err := aero.NewKey(namespace, set, key)
	if err != nil {
		return &bencherr.SerializationError{
			Database:  "Aerospike",
			Operation: "Set.NewKey",
			Cause:     err,
		}
	}

	bins := make(aero.BinMap, len(fields))
	for k, v := range fields {
		bins[k] = v
	}

	wp := s.writePolicy(ttl)
	if err := s.client.Put(wp, aeroKey, bins); err != nil {
		return &bencherr.ConnectionError{
			Database: "Aerospike",
			Host:     "",
			Cause:    err,
		}
	}

	return nil
}

// Get reads a record by primary key.
// Returns nil, nil if the key does not exist (not an error).
func (s *Store) Get(ctx context.Context, namespace, set, key string) (*datastore.Record, error) {
	aeroKey, err := aero.NewKey(namespace, set, key)
	if err != nil {
		return nil, &bencherr.SerializationError{
			Database:  "Aerospike",
			Operation: "Get.NewKey",
			Cause:     err,
		}
	}

	rec, err := s.client.Get(nil, aeroKey)
	if err != nil {
		return nil, &bencherr.ConnectionError{
			Database: "Aerospike",
			Host:     "",
			Cause:    err,
		}
	}

	// Key not found.
	if rec == nil {
		return nil, nil
	}

	fields := make(map[string]interface{}, len(rec.Bins))
	for k, v := range rec.Bins {
		fields[k] = v
	}

	return &datastore.Record{
		Key:    key,
		Fields: fields,
	}, nil
}

// Delete removes a record by primary key.
// Returns nil even if the key does not exist (idempotent delete).
func (s *Store) Delete(ctx context.Context, namespace, set, key string) error {
	aeroKey, err := aero.NewKey(namespace, set, key)
	if err != nil {
		return &bencherr.SerializationError{
			Database:  "Aerospike",
			Operation: "Delete.NewKey",
			Cause:     err,
		}
	}

	_, err = s.client.Delete(nil, aeroKey)
	if err != nil {
		// Aerospike returns an error for key-not-found on delete.
		// Treat as success for idempotent semantics.
		var ae *aero.AerospikeError
		if errors.As(err, &ae) && ae.ResultCode == aerotypes.KEY_NOT_FOUND_ERROR {
			return nil
		}
		return &bencherr.ConnectionError{
			Database: "Aerospike",
			Host:     "",
			Cause:    err,
		}
	}

	return nil
}
