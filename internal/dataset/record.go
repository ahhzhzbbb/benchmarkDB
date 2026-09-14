// Package dataset provides deterministic data generation for the benchmark.
//
// record.go defines the logical record structure that mirrors the production
// Aerospike schema (namespace smf / set Session and namespace sgwc / set bearer).
// The same logical records are loaded into both Aerospike and Redis so that
// benchmark results are comparable.
package dataset

// Record is the logical representation of a benchmark data record.
// It is intentionally database-agnostic. Adapters convert it to their
// native format (Aerospike BinMap, Redis HASH fields, etc.).
type Record struct {
	// Key is the primary key (e.g. "session:100001").
	Key string

	// Namespace and Set identify the logical location of the record.
	// In Aerospike these map directly; in Redis they are encoded into
	// the key prefix and index name.
	Namespace string
	Set       string

	// Fields holds the record data. Keys are field/bin names,
	// values are string or int64 (matching Aerospike bin types).
	Fields map[string]interface{}
}

// FieldSpec describes a single field in the generated dataset.
// It is used by the generator to produce deterministic, controllable data.
type FieldSpec struct {
	Name string
	Type FieldType
	// StringCardinality controls the number of distinct values for string fields.
	// 0 means each record gets a unique string.
	StringCardinality int
	// NumericMin/Max define the range for numeric fields.
	NumericMin int64
	NumericMax int64
}

// FieldType enumerates field value types.
type FieldType int

const (
	FieldTypeString  FieldType = iota
	FieldTypeNumeric
)

// SessionSchema returns the field specifications for the "Session" set,
// modeled after the production smf.Session schema described in the spec.
//
// Fields: FTEIDC, TEIDC, SEID, Dnn, IPType, IP, State, SNSSAI (string)
//
//	UpfSelection, Timer, attachTime, startTime (numeric)
func SessionSchema(stringCardinality int, numericMin, numericMax int64) []FieldSpec {
	return []FieldSpec{
		{Name: "FTEIDC", Type: FieldTypeString, StringCardinality: stringCardinality},
		{Name: "TEIDC", Type: FieldTypeString, StringCardinality: stringCardinality},
		{Name: "SEID", Type: FieldTypeString, StringCardinality: stringCardinality},
		{Name: "Dnn", Type: FieldTypeString, StringCardinality: stringCardinality / 10}, // fewer distinct DNNs
		{Name: "IPType", Type: FieldTypeString, StringCardinality: 3},                   // e.g. "IPv4", "IPv6", "IPv4v6"
		{Name: "IP", Type: FieldTypeString, StringCardinality: stringCardinality},
		{Name: "State", Type: FieldTypeString, StringCardinality: 5}, // e.g. "active","idle",...
		{Name: "SNSSAI", Type: FieldTypeString, StringCardinality: 10},
		{Name: "UpfSelection", Type: FieldTypeNumeric, NumericMin: 0, NumericMax: 100},
		{Name: "Timer", Type: FieldTypeNumeric, NumericMin: numericMin, NumericMax: numericMax},
		{Name: "attachTime", Type: FieldTypeNumeric, NumericMin: numericMin, NumericMax: numericMax},
		{Name: "startTime", Type: FieldTypeNumeric, NumericMin: numericMin, NumericMax: numericMax},
	}
}

// BearerSchema returns the field specifications for the "bearer" set,
// modeled after the production sgwc.bearer schema.
//
// Fields: imsi (string), sgwcS11teid, sgwcS5teid, sgwcSeid, startTime (numeric)
func BearerSchema(stringCardinality int, numericMin, numericMax int64) []FieldSpec {
	return []FieldSpec{
		{Name: "imsi", Type: FieldTypeString, StringCardinality: stringCardinality},
		{Name: "sgwcS11teid", Type: FieldTypeNumeric, NumericMin: 1, NumericMax: numericMax},
		{Name: "sgwcS5teid", Type: FieldTypeNumeric, NumericMin: 1, NumericMax: numericMax},
		{Name: "sgwcSeid", Type: FieldTypeNumeric, NumericMin: 1, NumericMax: numericMax},
		{Name: "startTime", Type: FieldTypeNumeric, NumericMin: numericMin, NumericMax: numericMax},
	}
}

// IndexableStringFields returns the list of string field names that should
// have secondary indexes created, matching the production configuration.
func IndexableStringFields() []string {
	return []string{
		"FTEIDC", "TEIDC", "SEID", "Dnn", "IPType", "IP", "State", "SNSSAI", "imsi",
	}
}

// IndexableNumericFields returns the list of numeric field names that should
// have secondary indexes created, matching the production configuration.
func IndexableNumericFields() []string {
	return []string{
		"UpfSelection", "Timer", "attachTime", "startTime",
		"sgwcS11teid", "sgwcS5teid", "sgwcSeid",
	}
}
