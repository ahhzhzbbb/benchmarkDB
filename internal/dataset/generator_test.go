package dataset

import (
	"math/rand"
	"testing"

	"benchmarkDB/internal/config"
)

// TestGeneratorDeterminism verifies that the same seed always produces
// the same records — this is the most critical property for benchmark
// fairness (Section 28).
func TestGeneratorDeterminism(t *testing.T) {
	cfg := config.DatasetConfig{
		KeyCount:          100,
		ValueSize:         256,
		Seed:              12345,
		Namespace:         "benchmark",
		Set:               "session",
		StringCardinality: 50,
		NumericMin:        1700000000,
		NumericMax:        1710000000,
	}

	gen1 := NewGenerator(cfg)
	gen2 := NewGenerator(cfg)

	for i := 0; i < cfg.KeyCount; i++ {
		r1 := gen1.GenerateRecord(i)
		r2 := gen2.GenerateRecord(i)

		if r1.Key != r2.Key {
			t.Errorf("record %d: keys differ: %s vs %s", i, r1.Key, r2.Key)
		}

		for field, v1 := range r1.Fields {
			v2, ok := r2.Fields[field]
			if !ok {
				t.Errorf("record %d: field %q missing in second generator", i, field)
				continue
			}
			if v1 != v2 {
				t.Errorf("record %d field %q: %v != %v", i, field, v1, v2)
			}
		}
	}
}

// TestGeneratorDifferentSeeds ensures different seeds produce different data.
func TestGeneratorDifferentSeeds(t *testing.T) {
	cfg1 := config.DatasetConfig{
		KeyCount:          10,
		ValueSize:         128,
		Seed:              12345,
		Namespace:         "benchmark",
		Set:               "session",
		StringCardinality: 50,
		NumericMin:        1700000000,
		NumericMax:        1710000000,
	}
	cfg2 := cfg1
	cfg2.Seed = 54321

	gen1 := NewGenerator(cfg1)
	gen2 := NewGenerator(cfg2)

	// At least some records should differ.
	allSame := true
	for i := 0; i < cfg1.KeyCount; i++ {
		r1 := gen1.GenerateRecord(i)
		r2 := gen2.GenerateRecord(i)
		// Keys are index-based, so they'll be the same.
		// Check field values.
		for field, v1 := range r1.Fields {
			if field == "_payload" {
				continue
			}
			v2 := r2.Fields[field]
			if v1 != v2 {
				allSame = false
				break
			}
		}
		if !allSame {
			break
		}
	}

	if allSame {
		t.Error("different seeds produced identical data")
	}
}

// TestGenerateAll checks that GenerateAll produces the expected count.
func TestGenerateAll(t *testing.T) {
	cfg := config.DatasetConfig{
		KeyCount:          50,
		ValueSize:         64,
		Seed:              99,
		Namespace:         "test",
		Set:               "session",
		StringCardinality: 10,
		NumericMin:        0,
		NumericMax:        1000,
	}

	gen := NewGenerator(cfg)
	records := gen.GenerateAll()

	if len(records) != cfg.KeyCount {
		t.Errorf("expected %d records, got %d", cfg.KeyCount, len(records))
	}
}

// TestRandomKey ensures generated keys are within bounds.
func TestRandomKey(t *testing.T) {
	cfg := config.DatasetConfig{
		KeyCount:          100,
		Seed:              42,
		Namespace:         "test",
		Set:               "session",
		StringCardinality: 10,
		NumericMin:        0,
		NumericMax:        100,
	}

	gen := NewGenerator(cfg)
	rng := rand.New(rand.NewSource(42))

	for i := 0; i < 1000; i++ {
		key := gen.RandomKey(rng)
		if key == "" {
			t.Error("RandomKey returned empty string")
		}
	}
}

// TestIndexDefinitions checks that indexes match the schema.
func TestIndexDefinitions(t *testing.T) {
	cfg := config.DatasetConfig{
		KeyCount:          10,
		Seed:              1,
		Namespace:         "bench",
		Set:               "session",
		StringCardinality: 5,
		NumericMin:        0,
		NumericMax:        100,
	}

	gen := NewGenerator(cfg)
	defs := gen.GetIndexDefinitions()

	if len(defs) == 0 {
		t.Fatal("expected at least one index definition")
	}

	for _, def := range defs {
		if def.Namespace != "bench" {
			t.Errorf("index %s: expected namespace 'bench', got '%s'", def.IndexName, def.Namespace)
		}
		if def.Set != "session" {
			t.Errorf("index %s: expected set 'session', got '%s'", def.IndexName, def.Set)
		}
		if def.Field == "" {
			t.Errorf("index %s: field name is empty", def.IndexName)
		}
	}
}

// TestBearerSchema checks that bearer schema is selected for "bearer" set.
func TestBearerSchema(t *testing.T) {
	cfg := config.DatasetConfig{
		KeyCount:          5,
		Seed:              1,
		Namespace:         "test",
		Set:               "bearer",
		StringCardinality: 10,
		NumericMin:        0,
		NumericMax:        100,
	}

	gen := NewGenerator(cfg)
	rec := gen.GenerateRecord(0)

	// Bearer schema should have "imsi" field.
	if _, ok := rec.Fields["imsi"]; !ok {
		t.Error("bearer schema: expected 'imsi' field")
	}
	// And should NOT have "FTEIDC" (session-only field).
	if _, ok := rec.Fields["FTEIDC"]; ok {
		t.Error("bearer schema: unexpected 'FTEIDC' field")
	}
}
