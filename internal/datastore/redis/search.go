package redis

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"benchmarkDB/internal/datastore"
	bencherr "benchmarkDB/internal/errors"
)

// Redis Search / Query Engine adapter.
//
// This file implements SearchStore using FT.* commands from the Redis
// Query Engine (RediSearch module). It MUST NOT use SCAN + client-side
// filtering as a substitute (Section 7, 24).
//
// Key design decisions:
//   - String fields use TAG type (exact match), not TEXT (full-text).
//   - Numeric fields use NUMERIC type for equality and range queries.
//   - Index names incorporate namespace:set:indexName for uniqueness.
//   - FT.SEARCH responses are parsed manually since go-redis doesn't
//     have built-in FT.SEARCH result types.

// makeIndexName constructs a unique index name for Redis.
func makeIndexName(namespace, set, indexName string) string {
	return namespace + ":" + set + ":" + indexName
}

// makePrefix constructs the key prefix that the index should cover.
func makePrefix(namespace, set string) string {
	return namespace + ":" + set + ":"
}

// CreateIndex creates a secondary index using FT.CREATE.
// Idempotent: if the index already exists, logs a warning and returns nil.
func (s *Store) CreateIndex(ctx context.Context, def datastore.IndexDefinition) error {
	idxName := makeIndexName(def.Namespace, def.Set, def.IndexName)
	prefix := makePrefix(def.Namespace, def.Set)

	// Build FT.CREATE command arguments.
	args := []interface{}{
		"FT.CREATE", idxName,
		"ON", "HASH",
		"PREFIX", "1", prefix,
		"SCHEMA",
	}

	switch def.IndexType {
	case datastore.IndexTypeString:
		// Use TAG for exact-match equality queries (not TEXT for full-text search).
		args = append(args, def.Field, "TAG")
	case datastore.IndexTypeNumeric:
		args = append(args, def.Field, "NUMERIC")
	default:
		return &bencherr.IndexError{
			Database:  "Redis Cluster",
			IndexName: idxName,
			Operation: "create",
			Cause:     fmt.Errorf("unsupported index type: %v", def.IndexType),
		}
	}

	err := s.client.Do(ctx, args...).Err()
	if err != nil {
		// Handle "Index already exists" gracefully.
		if strings.Contains(err.Error(), "Index already exists") {
			s.logger.Warn("Index already exists, skipping",
				"name", idxName,
				"field", def.Field,
			)
			return nil
		}
		return &bencherr.IndexError{
			Database:  "Redis Cluster",
			IndexName: idxName,
			Operation: "create",
			Cause:     err,
		}
	}

	s.logger.Info("Index created",
		"name", idxName,
		"field", def.Field,
		"type", def.IndexType.String(),
	)
	return nil
}

// DropIndex removes a secondary index.
func (s *Store) DropIndex(ctx context.Context, namespace, set, indexName string) error {
	fullIndexName := makeIndexName(namespace, set, indexName)
	err := s.client.Do(ctx, "FT.DROPINDEX", fullIndexName).Err()
	if err != nil {
		return &bencherr.IndexError{
			Database:  "Redis Cluster",
			IndexName: fullIndexName,
			Operation: "drop",
			Cause:     err,
		}
	}
	return nil
}

// EqualityQuery executes an equality query using FT.SEARCH.
//   - String: @field:{value}  (TAG syntax)
//   - Numeric: @field:[value value]
func (s *Store) EqualityQuery(ctx context.Context, req datastore.EqualityQueryRequest) (*datastore.QueryResult, error) {
	idxName := makeIndexName(req.Namespace, req.Set, "idx_"+req.Field)

	var query string
	switch v := req.Value.(type) {
	case string:
		// TAG query syntax: @field:{value}
		// Escape special characters in the value.
		escaped := escapeTagValue(v)
		query = fmt.Sprintf("@%s:{%s}", req.Field, escaped)
	case int64:
		query = fmt.Sprintf("@%s:[%d %d]", req.Field, v, v)
	case int:
		query = fmt.Sprintf("@%s:[%d %d]", req.Field, v, v)
	default:
		return nil, &bencherr.QueryError{
			Database:  "Redis Cluster",
			QueryType: "equality",
			Field:     req.Field,
			Cause:     fmt.Errorf("unsupported value type: %T", req.Value),
		}
	}

	return s.executeFTSearch(ctx, idxName, query, "equality", req.Field)
}

// RangeQuery executes a numeric range query using FT.SEARCH.
// Query: @field:[min max]
func (s *Store) RangeQuery(ctx context.Context, req datastore.RangeQueryRequest) (*datastore.QueryResult, error) {
	idxName := makeIndexName(req.Namespace, req.Set, "idx_"+req.Field)

	query := fmt.Sprintf("@%s:[%d %d]", req.Field, req.Min, req.Max)

	return s.executeFTSearch(ctx, idxName, query, "range", req.Field)
}

// IndexReady checks whether an index has finished building by calling FT.INFO.
//
// Design decisions:
//   - Uses the full qualified index name (namespace:set:indexName) to match
//     the name used at FT.CREATE time.
//   - Treats "Unknown Index" / "SEARCH_INDEX_NOT_FOUND" errors as "not yet
//     ready" (returns false, nil) so the caller can retry silently instead of
//     logging a WARN on every poll tick.
//   - Handles both RESP2 flat-array and RESP3 map responses from go-redis via
//     a type switch, avoiding the hard []interface{} assertion that causes
//     "unexpected FT.INFO response type" on newer Redis Stack builds.
func (s *Store) IndexReady(ctx context.Context, namespace, set, indexName string, expectedDocs int) (bool, error) {
	fullIndexName := makeIndexName(namespace, set, indexName)

	result, err := s.client.Do(ctx, "FT.INFO", fullIndexName).Result()
	if err != nil {
		// Index hasn't been created yet or is still initialising on this shard.
		// These are transient conditions — treat them as "not ready" so the
		// engine keeps polling rather than surfacing a WARN.
		errStr := err.Error()
		if strings.Contains(errStr, "Unknown Index") ||
			strings.Contains(errStr, "SEARCH_INDEX_NOT_FOUND") {
			slog.Debug("Index not found yet, will retry",
				"index", fullIndexName,
			)
			return false, nil
		}
		// Any other error (network, auth, etc.) is a real failure.
		return false, fmt.Errorf("FT.INFO %s: %w", fullIndexName, err)
	}

	// parseFTInfo extracts num_docs and indexing status from the FT.INFO
	// response. go-redis may return either:
	//   - RESP2: []interface{}{"key", value, "key", value, ...}  (flat array)
	//   - RESP3: map[interface{}]interface{}{"key": value, ...}  (map)
	numDocs, indexing := parseFTInfo(result)

	ready := !indexing && numDocs >= int64(expectedDocs)
	if !ready {
		slog.Debug("Index not ready yet",
			"index", fullIndexName,
			"num_docs", numDocs,
			"expected", expectedDocs,
			"still_indexing", indexing,
		)
	}
	return ready, nil
}

// parseFTInfo extracts num_docs and indexing flag from a raw FT.INFO response.
//
// It handles the two wire formats that go-redis may return depending on the
// RESP protocol version negotiated with the server:
//
//	RESP2 → []interface{}{"num_docs", "1000000", "indexing", "0", ...}
//	RESP3 → map[interface{}]interface{}{"num_docs": int64(1000000), ...}
func parseFTInfo(raw interface{}) (numDocs int64, indexing bool) {
	switch v := raw.(type) {

	// --- RESP2: flat key-value array ---
	case []interface{}:
		for i := 0; i+1 < len(v); i += 2 {
			key, ok := v[i].(string)
			if !ok {
				continue
			}
			switch key {
			case "num_docs":
				numDocs = toInt64(v[i+1])
			case "indexing":
				indexing = toInt64(v[i+1]) == 1
			}
		}

	// --- RESP3: map ---
	case map[interface{}]interface{}:
		for k, val := range v {
			key, ok := k.(string)
			if !ok {
				continue
			}
			switch key {
			case "num_docs":
				numDocs = toInt64(val)
			case "indexing":
				indexing = toInt64(val) == 1
			}
		}

	// --- RESP3: string-keyed map (some go-redis versions) ---
	case map[string]interface{}:
		for key, val := range v {
			switch key {
			case "num_docs":
				numDocs = toInt64(val)
			case "indexing":
				indexing = toInt64(val) == 1
			}
		}

	default:
		// Unknown format — log and treat as not-ready so polling continues.
		slog.Warn("Unrecognised FT.INFO response type; treating index as not ready",
			"type", fmt.Sprintf("%T", raw),
		)
	}
	return numDocs, indexing
}

// toInt64 coerces common numeric and string types returned by go-redis to int64.
func toInt64(v interface{}) int64 {
	switch val := v.(type) {
	case int64:
		return val
	case int:
		return int64(val)
	case float64:
		return int64(val)
	case string:
		n, _ := strconv.ParseInt(val, 10, 64)
		return n
	}
	return 0
}

// executeFTSearch runs FT.SEARCH and parses the response into QueryResult.
//
// FT.SEARCH response format:
//
//	[total_count, key1, [field1, val1, field2, val2, ...], key2, [...], ...]
func (s *Store) executeFTSearch(ctx context.Context, indexName, query, queryType, field string) (*datastore.QueryResult, error) {
	result, err := s.client.Do(ctx, "FT.SEARCH", indexName, query).Result()
	if err != nil {
		return nil, &bencherr.QueryError{
			Database:  "Redis Cluster",
			QueryType: queryType,
			Field:     field,
			Cause:     err,
		}
	}

	records, count, err := parseFTSearchResult(result)
	if err != nil {
		return nil, &bencherr.QueryError{
			Database:  "Redis Cluster",
			QueryType: queryType,
			Field:     field,
			Cause:     fmt.Errorf("parsing FT.SEARCH result: %w", err),
		}
	}

	return &datastore.QueryResult{
		Records: records,
		Count:   count,
	}, nil
}

// parseFTSearchResult parses the raw FT.SEARCH response.
// Format: [total_count, key1, [field1, val1, ...], key2, [...], ...]
func parseFTSearchResult(raw interface{}) ([]datastore.Record, int, error) {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil, 0, fmt.Errorf("expected array response, got %T", raw)
	}

	if len(arr) == 0 {
		return nil, 0, nil
	}

	// First element is the total count.
	var totalCount int
	switch v := arr[0].(type) {
	case int64:
		totalCount = int(v)
	case string:
		parsed, _ := strconv.Atoi(v)
		totalCount = parsed
	}

	var records []datastore.Record

	// Remaining elements are (key, field_array) pairs.
	i := 1
	for i < len(arr) {
		// Key.
		var key string
		if k, ok := arr[i].(string); ok {
			key = k
		}
		i++

		if i >= len(arr) {
			break
		}

		// Fields array: [field1, val1, field2, val2, ...].
		fields := make(map[string]interface{})
		if fieldArr, ok := arr[i].([]interface{}); ok {
			for j := 0; j+1 < len(fieldArr); j += 2 {
				fname, _ := fieldArr[j].(string)
				fval := fieldArr[j+1]
				if fname != "" {
					// Try to parse numeric strings.
					if sv, ok := fval.(string); ok {
						if iv, err := strconv.ParseInt(sv, 10, 64); err == nil {
							fields[fname] = iv
						} else {
							fields[fname] = sv
						}
					} else {
						fields[fname] = fval
					}
				}
			}
		}
		i++

		records = append(records, datastore.Record{
			Key:    key,
			Fields: fields,
		})
	}

	return records, totalCount, nil
}

// escapeTagValue escapes special characters in TAG query values.
// Redis TAG query syntax requires escaping: , . < > { } [ ] " ' : ; ! @ # $ % ^ & * ( ) - + = ~
func escapeTagValue(s string) string {
	var b strings.Builder
	for _, c := range s {
		switch c {
		case ',', '.', '<', '>', '{', '}', '[', ']', '"', '\'',
			':', ';', '!', '@', '#', '$', '%', '^', '&', '*',
			'(', ')', '-', '+', '=', '~', ' ':
			b.WriteRune('\\')
		}
		b.WriteRune(c)
	}
	return b.String()
}
