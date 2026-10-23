package cmd

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"

	"github.com/urfave/cli/v2"

	. "github.com/yao-jz/mlvm/rvgo/comp"
	. "github.com/yao-jz/mlvm/rvgo/include"
)

// SoftFloatVectors emits test vectors for the Yul port of SoftFloat in
// rvsol/src/SoftFloat64.sol.
//
// The Yul is a hand translation of rvgo/comp, and hand translations of branchy bit
// manipulation are exactly the kind of code that looks right and is not. Rather than
// reason about it, this generates (a, b, expected) triples from the Go implementation and
// lets a Foundry test assert the Yul reproduces them.
//
// The vectors are chosen to hit the paths that differ between the two, not to sample
// uniformly at random - a uniform sample of 64-bit patterns is almost entirely NaN and
// huge-exponent values, and would never exercise subnormals or the rounding tie case.

var SoftFloatVectorsOutFlag = &cli.PathFlag{
	Name:     "out",
	Usage:    "Where to write the JSON vectors",
	Required: true,
}

var SoftFloatVectorsCountFlag = &cli.IntFlag{
	Name:  "random",
	Usage: "How many additional randomised vectors to append",
	Value: 512,
}

// Flat parallel arrays rather than an array of objects. Foundry's JSON cheatcodes decode
// a `uint256[]` cheaply but blow the test's memory allocator on a thousand-element array
// of structs-with-strings.
type sfVectors struct {
	A   []string `json:"a"`
	B   []string `json:"b"`
	Sum []string `json:"sum"`
}

func hex64(v uint64) string { return fmt.Sprintf("0x%016x", v) }

func SoftFloatVectors(ctx *cli.Context) error {
	var vectors sfVectors
	seen := make(map[[2]uint64]bool)
	add := func(a, b uint64, note string) {
		// Deduplicate: the named cross-product and the targeted probes overlap heavily,
		// and duplicates cost test gas without adding coverage.
		if seen[[2]uint64{a, b}] {
			return
		}
		seen[[2]uint64{a, b}] = true
		vectors.A = append(vectors.A, hex64(a))
		vectors.B = append(vectors.B, hex64(b))
		vectors.Sum = append(vectors.Sum, hex64(uint64(F64_add(Float64_t(a), Float64_t(b)))))
	}

	// Named values covering the classes SoftFloat branches on.
	named := map[string]uint64{
		"+0":            0x0000000000000000,
		"-0":            0x8000000000000000,
		"+1":            math.Float64bits(1.0),
		"-1":            math.Float64bits(-1.0),
		"+2":            math.Float64bits(2.0),
		"+0.5":          math.Float64bits(0.5),
		"+pi":           math.Float64bits(math.Pi),
		"-pi":           math.Float64bits(-math.Pi),
		"+inf":          0x7FF0000000000000,
		"-inf":          0xFFF0000000000000,
		"qNaN":          0x7FF8000000000000,
		"sNaN":          0x7FF0000000000001,
		"maxNormal":     0x7FEFFFFFFFFFFFFF,
		"minNormal":     0x0010000000000000,
		"maxSubnormal":  0x000FFFFFFFFFFFFF,
		"minSubnormal":  0x0000000000000001,
		"midSubnormal":  0x0008000000000000,
		"1ulpBelow1":    math.Float64bits(1.0) - 1,
		"1ulpAbove1":    math.Float64bits(1.0) + 1,
		"2^-1074":       0x0000000000000001,
		"2^52":          math.Float64bits(1 << 52),
		"2^53":          math.Float64bits(1 << 53),
		"2^-52":         math.Float64bits(1.0 / (1 << 52)),
		"largeOdd":      math.Float64bits(9007199254740993.0),
		"tinyNormal":    0x0010000000000001,
		"negMaxNormal":  0xFFEFFFFFFFFFFFFF,
		"negMinSubnorm": 0x8000000000000001,
	}
	for na, a := range named {
		for nb, b := range named {
			add(a, b, na+" + "+nb)
		}
	}

	// Cases that specifically exercise round-to-nearest-even ties: x + ulp/2, where the
	// discarded bits are exactly half and the tie-break decides the answer.
	for _, base := range []float64{1.0, 2.0, 1024.0, 1e10, 1e-10} {
		ub := math.Float64bits(base)
		for _, d := range []uint64{1, 2, 3, 1 << 26, 1 << 27} {
			add(ub, ub-d, "tie probe")
			add(ub, (ub-d)|0x8000000000000000, "tie probe, opposite sign")
		}
	}

	// Exponent differences across the whole range, which selects between the shift-and-jam
	// path, the trivial path and the "operand vanishes" path.
	for diff := 0; diff < 64; diff++ {
		a := math.Float64bits(1.0)
		b := math.Float64bits(math.Ldexp(1.0, -diff))
		add(a, b, "expDiff")
		add(a, b|0x8000000000000000, "expDiff, opposite sign")
		add(b, a, "expDiff reversed")
	}

	// Subnormal arithmetic, including results that drift back into the normal range.
	for i := 0; i < 52; i++ {
		s := uint64(1) << uint(i)
		add(s, s, "subnormal doubling")
		add(0x000FFFFFFFFFFFFF, s, "subnormal carry into normal")
		add(s, s|0x8000000000000000, "subnormal cancellation")
	}

	// Randomised vectors, drawn so exponents stay in a range where the interesting paths
	// are reachable rather than everything being NaN or infinity.
	rng := rand.New(rand.NewSource(20260803))
	n := ctx.Int(SoftFloatVectorsCountFlag.Name)
	for i := 0; i < n; i++ {
		mk := func() uint64 {
			sign := uint64(rng.Intn(2)) << 63
			exp := uint64(rng.Intn(0x7FF))
			frac := rng.Uint64() & 0x000FFFFFFFFFFFFF
			return sign | (exp << 52) | frac
		}
		add(mk(), mk(), "")
	}

	out := ctx.Path(SoftFloatVectorsOutFlag.Name)
	data, err := json.MarshalIndent(vectors, "", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %d vectors to %s\n", len(vectors.A), out)
	return nil
}

var SoftFloatVectorsCommand = &cli.Command{
	Name:        "softfloat-vectors",
	Usage:       "Emit SoftFloat test vectors for the Yul port",
	Description: "Generate (a, b, a+b) triples from the Go SoftFloat implementation, for differential testing of rvsol/src/SoftFloat64.sol",
	Action:      SoftFloatVectors,
	Flags: []cli.Flag{
		SoftFloatVectorsOutFlag,
		SoftFloatVectorsCountFlag,
	},
}
