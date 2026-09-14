package dataset

import (
	"math/rand"
)

// Distribution controls how keys are selected during benchmark operations.
// Different distributions model different access patterns:
//   - Uniform: all keys equally likely (cold-start, scan-like workloads)
//   - Zipfian: few keys very hot, long tail (realistic production traffic)
//   - Sequential: iterate keys in order (data loading, range scan)
type Distribution interface {
	// Next returns the next index in [0, max).
	Next(rng *rand.Rand) int
}

// UniformDistribution selects keys with equal probability.
type UniformDistribution struct {
	max int
}

// Next returns a uniformly random index.
func (d *UniformDistribution) Next(rng *rand.Rand) int {
	if d.max <= 0 {
		return 0
	}
	return rng.Intn(d.max)
}

// ZipfianDistribution selects keys according to Zipf's law.
// This models "hot key" access patterns where a small number of keys
// receive a disproportionate share of traffic.
type ZipfianDistribution struct {
	zipf *rand.Zipf
	max  int
}

// Next returns a Zipfian-distributed index.
func (d *ZipfianDistribution) Next(rng *rand.Rand) int {
	if d.max <= 0 {
		return 0
	}
	val := d.zipf.Uint64()
	if val >= uint64(d.max) {
		val = uint64(d.max - 1)
	}
	return int(val)
}

// SequentialDistribution iterates through keys in order, wrapping around.
type SequentialDistribution struct {
	max     int
	current int
}

// Next returns the next sequential index.
func (d *SequentialDistribution) Next(_ *rand.Rand) int {
	if d.max <= 0 {
		return 0
	}
	val := d.current % d.max
	d.current++
	return val
}

// NewDistribution creates a key distribution by name.
// Supported names: "uniform" (default), "zipfian", "sequential".
func NewDistribution(name string, max int) Distribution {
	switch name {
	case "zipfian":
		// Use a fixed seed for the Zipf generator so results are reproducible.
		initRng := rand.New(rand.NewSource(1234))
		var imax uint64
		if max > 0 {
			imax = uint64(max - 1)
		}
		// s=1.1 gives moderate skew; v=1.0 is standard.
		zipf := rand.NewZipf(initRng, 1.1, 1.0, imax)
		return &ZipfianDistribution{zipf: zipf, max: max}
	case "sequential":
		return &SequentialDistribution{max: max, current: 0}
	default: // "uniform"
		return &UniformDistribution{max: max}
	}
}
