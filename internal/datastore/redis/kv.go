package redis

import (
	"context"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"benchmarkDB/internal/datastore"
	bencherr "benchmarkDB/internal/errors"
)

// makeKey constructs the Redis key from namespace, set, and key components.
// Format: "namespace:set:key" — distributed naturally across hash slots
// without using hash tags (Section 6, 24: must not route all traffic to
// a single Redis pod).
func makeKey(namespace, set, key string) string {
	return namespace + ":" + set + ":" + key
}

// Set writes a record as a Redis HASH with optional TTL.
// Uses a pipeline for atomicity of HSET + EXPIRE.
func (s *Store) Set(ctx context.Context, namespace, set, key string, fields map[string]interface{}, ttl time.Duration) error {
	rkey := makeKey(namespace, set, key)

	// Convert all values to strings for Redis HASH.
	strFields := make(map[string]interface{}, len(fields))
	for k, v := range fields {
		switch val := v.(type) {
		case string:
			strFields[k] = val
		case int64:
			strFields[k] = strconv.FormatInt(val, 10)
		case int:
			strFields[k] = strconv.Itoa(val)
		case float64:
			strFields[k] = strconv.FormatFloat(val, 'f', -1, 64)
		default:
			strFields[k] = v
		}
	}

	if ttl > 0 {
		// Use pipeline for atomic HSET + EXPIRE.
		pipe := s.client.Pipeline()
		pipe.HSet(ctx, rkey, strFields)
		pipe.Expire(ctx, rkey, ttl)
		_, err := pipe.Exec(ctx)
		if err != nil {
			return &bencherr.ConnectionError{
				Database: "Redis Cluster",
				Host:     "",
				Cause:    err,
			}
		}
		return nil
	}

	// No TTL: simple HSET.
	if err := s.client.HSet(ctx, rkey, strFields).Err(); err != nil {
		return &bencherr.ConnectionError{
			Database: "Redis Cluster",
			Host:     "",
			Cause:    err,
		}
	}
	return nil
}

// Get reads a record by primary key using HGETALL.
// Returns nil, nil if the key does not exist.
func (s *Store) Get(ctx context.Context, namespace, set, key string) (*datastore.Record, error) {
	rkey := makeKey(namespace, set, key)

	result, err := s.client.HGetAll(ctx, rkey).Result()
	if err != nil {
		if err == goredis.Nil {
			return nil, nil
		}
		return nil, &bencherr.ConnectionError{
			Database: "Redis Cluster",
			Host:     "",
			Cause:    err,
		}
	}

	// Empty result = key not found.
	if len(result) == 0 {
		return nil, nil
	}

	// Convert string map back to interface map.
	// Try to parse numeric strings back to int64 for consistent behavior.
	fields := make(map[string]interface{}, len(result))
	for k, v := range result {
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			fields[k] = i
		} else {
			fields[k] = v
		}
	}

	return &datastore.Record{
		Key:    key,
		Fields: fields,
	}, nil
}

// Delete removes a record by primary key.
// Idempotent: returns nil even if the key does not exist.
func (s *Store) Delete(ctx context.Context, namespace, set, key string) error {
	rkey := makeKey(namespace, set, key)

	if err := s.client.Del(ctx, rkey).Err(); err != nil {
		return &bencherr.ConnectionError{
			Database: "Redis Cluster",
			Host:     "",
			Cause:    err,
		}
	}
	return nil
}
