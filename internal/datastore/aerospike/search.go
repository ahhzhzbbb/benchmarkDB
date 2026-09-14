package aerospike

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	aero "github.com/aerospike/aerospike-client-go/v7"

	"benchmarkDB/internal/datastore"
	bencherr "benchmarkDB/internal/errors"
)

// CreateIndex creates a secondary index on Aerospike.
// Idempotent: if the index already exists, logs a warning and returns nil.
func (s *Store) CreateIndex(ctx context.Context, def datastore.IndexDefinition) error {
	var indexType aero.IndexType
	switch def.IndexType {
	case datastore.IndexTypeString:
		indexType = aero.STRING
	case datastore.IndexTypeNumeric:
		indexType = aero.NUMERIC
	default:
		return &bencherr.IndexError{
			Database:  "Aerospike",
			IndexName: def.IndexName,
			Operation: "create",
			Cause:     fmt.Errorf("unsupported index type: %v", def.IndexType),
		}
	}

	task, err := s.client.CreateIndex(nil, def.Namespace, def.Set, def.IndexName, def.Field, indexType)
	if err != nil {
		// Handle "index already exists" gracefully.
		if strings.Contains(err.Error(), "Index already exists") ||
			strings.Contains(err.Error(), "FAIL:200") {
			s.logger.Warn("Index already exists, skipping",
				"name", def.IndexName,
				"field", def.Field,
			)
			return nil
		}
		return &bencherr.IndexError{
			Database:  "Aerospike",
			IndexName: def.IndexName,
			Operation: "create",
			Cause:     err,
		}
	}

	// Wait for index creation to complete.
	if task != nil {
		for err := range task.OnComplete() {
			if err != nil {
				s.logger.Warn("Index creation task error",
					"name", def.IndexName,
					"error", err,
				)
			}
		}
	}

	return nil
}

// DropIndex removes a secondary index.
func (s *Store) DropIndex(ctx context.Context, namespace, set, indexName string) error {
	if err := s.client.DropIndex(nil, namespace, "", indexName); err != nil {
		return &bencherr.IndexError{
			Database:  "Aerospike",
			IndexName: indexName,
			Operation: "drop",
			Cause:     err,
		}
	}
	return nil
}

// EqualityQuery performs a secondary index equality lookup.
// Maps to Aerospike Filter.Equal() (Section 3A, 26).
func (s *Store) EqualityQuery(ctx context.Context, req datastore.EqualityQueryRequest) (*datastore.QueryResult, error) {
	stmt := aero.NewStatement(req.Namespace, req.Set)

	var filter *aero.Filter
	switch v := req.Value.(type) {
	case string:
		filter = aero.NewEqualFilter(req.Field, v)
	case int64:
		filter = aero.NewEqualFilter(req.Field, v)
	case int:
		filter = aero.NewEqualFilter(req.Field, int64(v))
	default:
		return nil, &bencherr.QueryError{
			Database:  "Aerospike",
			QueryType: "equality",
			Field:     req.Field,
			Cause:     fmt.Errorf("unsupported value type: %T", req.Value),
		}
	}

	if err := stmt.SetFilter(filter); err != nil {
		return nil, &bencherr.QueryError{
			Database:  "Aerospike",
			QueryType: "equality",
			Field:     req.Field,
			Cause:     err,
		}
	}

	return s.executeQuery(stmt, "equality", req.Field)
}

// RangeQuery performs a secondary index range lookup.
// Maps to Aerospike Filter.Range() (Section 3B, 26).
func (s *Store) RangeQuery(ctx context.Context, req datastore.RangeQueryRequest) (*datastore.QueryResult, error) {
	stmt := aero.NewStatement(req.Namespace, req.Set)

	filter := aero.NewRangeFilter(req.Field, req.Min, req.Max)
	if err := stmt.SetFilter(filter); err != nil {
		return nil, &bencherr.QueryError{
			Database:  "Aerospike",
			QueryType: "range",
			Field:     req.Field,
			Cause:     err,
		}
	}

	return s.executeQuery(stmt, "range", req.Field)
}

// IndexReady checks whether a secondary index has finished building.
// Uses sindex info command to check load percentage.
func (s *Store) IndexReady(ctx context.Context, namespace, set, indexName string, expectedDocs int) (bool, error) {
	nodes := s.client.GetNodes()
	if len(nodes) == 0 {
		return false, fmt.Errorf("no nodes available")
	}

	// Query the first node for index info.
	infoKey := fmt.Sprintf("sindex/%s/%s", namespace, indexName)
	result, err := nodes[0].RequestInfo(nil, infoKey)
	if err != nil {
		return false, err
	}

	info, ok := result[infoKey]
	if !ok {
		return false, nil
	}

	// Parse response for load_pct.
	for _, part := range strings.Split(info, ";") {
		for _, kv := range strings.Split(part, ":") {
			if strings.HasPrefix(kv, "load_pct=") {
				val := strings.TrimPrefix(kv, "load_pct=")
				if val == "100" {
					return true, nil
				}
				slog.Debug("Index loading", "index", indexName, "load_pct", val)
				return false, nil
			}
		}
	}

	// If we can query the index info without error, assume it's ready.
	return true, nil
}

// executeQuery runs a Statement and collects results.
func (s *Store) executeQuery(stmt *aero.Statement, queryType, field string) (*datastore.QueryResult, error) {
	qp := aero.NewQueryPolicy()

	rs, err := s.client.Query(qp, stmt)
	if err != nil {
		return nil, &bencherr.QueryError{
			Database:  "Aerospike",
			QueryType: queryType,
			Field:     field,
			Cause:     err,
		}
	}
	defer rs.Close()

	var records []datastore.Record
	for rec := range rs.Results() {
		if rec.Err != nil {
			return nil, &bencherr.QueryError{
				Database:  "Aerospike",
				QueryType: queryType,
				Field:     field,
				Cause:     rec.Err,
			}
		}

		fields := make(map[string]interface{}, len(rec.Record.Bins))
		for k, v := range rec.Record.Bins {
			fields[k] = v
		}

		var key string
		if rec.Record.Key != nil && rec.Record.Key.Value() != nil {
			key = fmt.Sprintf("%v", rec.Record.Key.Value())
		}

		records = append(records, datastore.Record{
			Key:    key,
			Fields: fields,
		})
	}

	return &datastore.QueryResult{
		Records: records,
		Count:   len(records),
	}, nil
}
