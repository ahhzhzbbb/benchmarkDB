package dataset

import (
	"fmt"
	"math/rand"
	"strings"

	"benchmarkDB/internal/config"
	"benchmarkDB/internal/datastore"
)

// Generator is a deterministic data generator for the benchmark dataset.
// Given the same seed and configuration, it always produces the same records.
// This is critical for benchmark fairness (Section 28): both Aerospike and
// Redis must receive the exact same logical dataset.
type Generator struct {
	config     config.DatasetConfig
	fieldSpecs []FieldSpec
	seed       int64
}

// NewGenerator creates a new deterministic data generator.
// It selects the schema (SessionSchema or BearerSchema) based on the set name.
func NewGenerator(cfg config.DatasetConfig) *Generator {
	var specs []FieldSpec
	if strings.EqualFold(cfg.Set, "bearer") {
		specs = BearerSchema(cfg.StringCardinality, cfg.NumericMin, cfg.NumericMax)
	} else {
		specs = SessionSchema(cfg.StringCardinality, cfg.NumericMin, cfg.NumericMax)
	}

	return &Generator{
		config:     cfg,
		fieldSpecs: specs,
		seed:       cfg.Seed,
	}
}

// GenerateRecord produces a single deterministic record for the given index.
// The record content is derived entirely from seed + index, so the same
// (seed, index) pair always yields the same record.
func (g *Generator) GenerateRecord(index int) *Record {
	// Per-record RNG seeded deterministically from global seed + index.
	rng := rand.New(rand.NewSource(g.seed + int64(index)))

	key := fmt.Sprintf("%s:%d", g.config.Set, index)
	fields := make(map[string]interface{})
	var currentSize int

	for _, spec := range g.fieldSpecs {
		switch spec.Type {
		case FieldTypeString:
			var val string
			if spec.StringCardinality <= 0 {
				// Unique value per record.
				val = fmt.Sprintf("%s_%d", spec.Name, index)
			} else {
				hash := rng.Intn(spec.StringCardinality)
				val = fmt.Sprintf("%s_%d", spec.Name, hash)
			}
			fields[spec.Name] = val
			currentSize += len(spec.Name) + len(val)

		case FieldTypeNumeric:
			min := spec.NumericMin
			max := spec.NumericMax
			var val int64
			if max > min {
				val = min + rng.Int63n(max-min+1)
			} else {
				val = min
			}
			fields[spec.Name] = val
			currentSize += len(spec.Name) + 8 // 8 bytes for int64
		}
	}

	// Pad to reach target value size if specified (Section 8).
	if g.config.ValueSize > 0 {
		padLen := g.config.ValueSize - currentSize
		if padLen > len("_payload") {
			padBytesLen := padLen - len("_payload")
			padBytes := make([]byte, padBytesLen)
			for i := range padBytes {
				padBytes[i] = byte(97 + rng.Intn(26)) // lowercase a-z
			}
			fields["_payload"] = string(padBytes)
		}
	}

	return &Record{
		Key:       key,
		Namespace: g.config.Namespace,
		Set:       g.config.Set,
		Fields:    fields,
	}
}

// GenerateAll produces the complete dataset (KeyCount records).
func (g *Generator) GenerateAll() []*Record {
	records := make([]*Record, g.config.KeyCount)
	for i := 0; i < g.config.KeyCount; i++ {
		records[i] = g.GenerateRecord(i)
	}
	return records
}

// RandomKey returns a random existing primary key for GET/DELETE workloads.
func (g *Generator) RandomKey(rng *rand.Rand) string {
	if g.config.KeyCount <= 0 {
		return fmt.Sprintf("%s:0", g.config.Set)
	}
	idx := rng.Intn(g.config.KeyCount)
	return fmt.Sprintf("%s:%d", g.config.Set, idx)
}

// RandomRecord returns a random existing record for SET/TTL workloads.
func (g *Generator) RandomRecord(rng *rand.Rand) *Record {
	if g.config.KeyCount <= 0 {
		return g.GenerateRecord(0)
	}
	idx := rng.Intn(g.config.KeyCount)
	return g.GenerateRecord(idx)
}

// RandomEqualityQuery returns a random field name and value that exists in the
// dataset, suitable for testing equality queries on secondary indexes.
func (g *Generator) RandomEqualityQuery(rng *rand.Rand) (field string, value interface{}) {
	strFields := IndexableStringFields()
	var validFields []string

	for _, f := range strFields {
		for _, s := range g.fieldSpecs {
			if s.Name == f {
				validFields = append(validFields, f)
				break
			}
		}
	}

	if len(validFields) == 0 {
		return "dummy", "val"
	}

	field = validFields[rng.Intn(len(validFields))]

	var spec FieldSpec
	for _, s := range g.fieldSpecs {
		if s.Name == field {
			spec = s
			break
		}
	}

	if spec.StringCardinality <= 0 {
		idx := rng.Intn(g.config.KeyCount)
		return field, fmt.Sprintf("%s_%d", field, idx)
	}

	valIdx := rng.Intn(spec.StringCardinality)
	return field, fmt.Sprintf("%s_%d", field, valIdx)
}

// RandomRangeQuery returns a numeric field and a [min, max] range covering
// approximately 1% of the data range. This gives a controlled selectivity
// for benchmark query performance measurement (Section 28).
func (g *Generator) RandomRangeQuery(rng *rand.Rand) (field string, min, max int64) {
	numFields := IndexableNumericFields()
	var validFields []string

	for _, f := range numFields {
		for _, s := range g.fieldSpecs {
			if s.Name == f {
				validFields = append(validFields, f)
				break
			}
		}
	}

	if len(validFields) == 0 {
		return "dummy", 0, 10
	}

	field = validFields[rng.Intn(len(validFields))]

	var spec FieldSpec
	for _, s := range g.fieldSpecs {
		if s.Name == field {
			spec = s
			break
		}
	}

	fMin := spec.NumericMin
	fMax := spec.NumericMax
	if fMax <= fMin {
		return field, fMin, fMax
	}

	// ~1% of the total range.
	rangeSize := (fMax - fMin) / 100
	if rangeSize < 1 {
		rangeSize = 1
	}

	maxStart := fMax - rangeSize
	if maxStart <= fMin {
		return field, fMin, fMax
	}

	min = fMin + rng.Int63n(maxStart-fMin+1)
	max = min + rangeSize
	return field, min, max
}

// GetIndexDefinitions returns all secondary index definitions needed
// for the dataset, ready to be passed to DataStore.CreateIndex().
func (g *Generator) GetIndexDefinitions() []datastore.IndexDefinition {
	var defs []datastore.IndexDefinition

	for _, f := range IndexableStringFields() {
		for _, s := range g.fieldSpecs {
			if s.Name == f {
				defs = append(defs, datastore.IndexDefinition{
					Namespace: g.config.Namespace,
					Set:       g.config.Set,
					Field:     f,
					IndexName: fmt.Sprintf("%s_%s_str_idx", g.config.Set, f),
					IndexType: datastore.IndexTypeString,
				})
				break
			}
		}
	}

	for _, f := range IndexableNumericFields() {
		for _, s := range g.fieldSpecs {
			if s.Name == f {
				defs = append(defs, datastore.IndexDefinition{
					Namespace: g.config.Namespace,
					Set:       g.config.Set,
					Field:     f,
					IndexName: fmt.Sprintf("%s_%s_num_idx", g.config.Set, f),
					IndexType: datastore.IndexTypeNumeric,
				})
				break
			}
		}
	}

	return defs
}

// Namespace returns the configured namespace.
func (g *Generator) Namespace() string { return g.config.Namespace }

// Set returns the configured set name.
func (g *Generator) Set() string { return g.config.Set }

// KeyCount returns the configured number of keys.
func (g *Generator) KeyCount() int { return g.config.KeyCount }
