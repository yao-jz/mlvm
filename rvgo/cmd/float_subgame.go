package cmd

import (
	"encoding/json"
	"fmt"
	"math"
	"os"

	"github.com/urfave/cli/v2"

	"github.com/yao-jz/mlvm/rvgo/fast"
)

// FloatSubGameFixture emits what FloatSubGame.sol needs in order to be tested against the
// real thing rather than against hand-written constants.
//
// The contract's job is to derive the nested machine's pre-state hash from (op, a, b, c) and
// to read the result back out of the post-state. Both must agree with what rvgo/fast
// computes, exactly - a one-bit disagreement means the two parties are disputing different
// machines. So this runs the sub-game in Go and writes down every value the contract should
// reproduce.
//
// Run:  mlvm float-subgame-fixture --out <fixture.json>

var FloatSubGameOutFlag = &cli.PathFlag{
	Name:     "out",
	Usage:    "Where to write the JSON fixture",
	Required: true,
}

var FloatSubGameCoprocessorFlag = &cli.PathFlag{
	Name:  "coprocessor",
	Usage: "Path to the RV64IMA float co-processor ELF (defaults to the build output)",
}

// Flat parallel arrays, for the same reason the SoftFloat vectors use them: Foundry decodes
// a typed array cheaply but struggles with arrays of structs.
type floatSubGameFixture struct {
	ImageRoot  string `json:"imageRoot"`
	Entry      string `json:"entry"`
	ImageProof string `json:"imageProof"`

	Op   []string `json:"op"`
	A    []string `json:"a"`
	B    []string `json:"b"`
	C    []string `json:"c"`
	Name []string `json:"name"`

	PreStateHash []string `json:"preStateHash"`
	PostState    []string `json:"postState"`
	PostProof    []string `json:"postProof"`
	Result       []string `json:"result"`
	Flags        []string `json:"flags"`
	Steps        []uint64 `json:"steps"`
}

func FloatSubGameFixture(ctx *cli.Context) error {
	path := ctx.Path(FloatSubGameCoprocessorFlag.Name)
	if path == "" {
		var err error
		if path, err = fast.FindFloatCoprocessor(); err != nil {
			return err
		}
	}
	c, err := fast.LoadFloatCoprocessor(path)
	if err != nil {
		return err
	}

	imageRoot := c.ImageRoot()
	imageProof := c.ImageProof()
	fixture := floatSubGameFixture{
		ImageRoot:  fmt.Sprintf("0x%x", imageRoot),
		Entry:      hex64(c.Entry()), // fixed width: Foundry mis-parses odd-nibble hex strings
		ImageProof: fmt.Sprintf("0x%x", imageProof),
	}

	// One case per operation, so a change to any of them fails the contract test rather than
	// only the Go one. The exhaustive check against rvgo/comp is
	// TestFloatSubGameMatchesSoftFloat; this is the on-chain spot check.
	bits := math.Float64bits
	u64 := func(v int64) uint64 { return uint64(v) }
	cases := []struct {
		op      uint64
		a, b, c uint64
	}{
		{fast.FloatOpAdd, bits(math.Pi), bits(math.E), 0},
		{fast.FloatOpAdd, bits(math.MaxFloat64), bits(math.MaxFloat64), 0}, // -> inf
		{fast.FloatOpSub, bits(math.Pi), bits(math.E), 0},
		{fast.FloatOpMul, bits(math.Pi), bits(math.E), 0},
		{fast.FloatOpMul, bits(5e-324), bits(2.0), 0}, // subnormal
		{fast.FloatOpDiv, bits(math.Pi), bits(math.E), 0},
		{fast.FloatOpDiv, bits(1.0), bits(0.0), 0}, // -> inf
		{fast.FloatOpMin, bits(math.Pi), bits(math.E), 0},
		{fast.FloatOpMax, bits(math.Pi), bits(math.E), 0},
		{fast.FloatOpSqrt, bits(2.0), 0, 0},
		{fast.FloatOpMAdd, bits(math.Pi), bits(math.E), bits(1.5)},
		{fast.FloatOpMSub, bits(math.Pi), bits(math.E), bits(1.5)},
		{fast.FloatOpNMSub, bits(math.Pi), bits(math.E), bits(1.5)},
		{fast.FloatOpNMAdd, bits(math.Pi), bits(math.E), bits(1.5)},
		{fast.FloatOpCvtDW, u64(int64(int32(-12345))), 0, 0},
		{fast.FloatOpCvtDWU, 0xDEADBEEF, 0, 0},
		{fast.FloatOpCvtDL, u64(-1234567890123), 0, 0},
		{fast.FloatOpCvtDLU, 0xFFFF_FFFF_FFFF_FFFF, 0, 0},
		{fast.FloatOpCvtWD, bits(-2.6), 0, 0}, // rounds to nearest, not toward zero
		{fast.FloatOpCvtWUD, bits(2.5), 0, 0}, // ties to even
		{fast.FloatOpCvtLD, bits(1e18), 0, 0},
		{fast.FloatOpCvtLUD, bits(1e19), 0, 0},
		{fast.FloatOpEq, bits(math.Pi), bits(math.Pi), 0},
		{fast.FloatOpLt, bits(math.E), bits(math.Pi), 0},
		{fast.FloatOpLe, bits(math.Pi), bits(math.E), 0},
	}

	for _, tc := range cases {
		name := fast.FloatOpName(tc.op)

		// The pre-state, which is what the contract derives.
		preHash, err := c.NewFloatSubGame(tc.op, tc.a, tc.b, tc.c).EncodeWitness().StateHash()
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}

		// The post-state, which is what the contract reads the result out of.
		state, steps, err := c.ExecuteFloatSubGame(tc.op, tc.a, tc.b, tc.c)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		result := fast.FloatSubGameResult(state)
		flags := fast.FloatSubGameFlags(state)

		// Cross-check against the VM's own float unit before writing it down. A fixture
		// generated from a sub-game that disagrees with rvgo/comp would make the Foundry
		// test pass while proving nothing.
		want, wantFlags, ok := fast.FloatReferenceWithFlags(tc.op, tc.a, tc.b, tc.c)
		if ok && result != want {
			return fmt.Errorf("%s: sub-game gave %#016x, but rvgo/comp gives %#016x", name, result, want)
		}
		if ok && flags != uint64(wantFlags) {
			return fmt.Errorf("%s: sub-game flags %#x, but rvgo/comp raises %#x", name, flags, wantFlags)
		}

		postProof := state.Memory.MerkleProof(fast.FloatIOBase)
		fixture.Op = append(fixture.Op, hex64(tc.op))
		fixture.A = append(fixture.A, hex64(tc.a))
		fixture.B = append(fixture.B, hex64(tc.b))
		fixture.C = append(fixture.C, hex64(tc.c))
		fixture.Name = append(fixture.Name, name)
		fixture.PreStateHash = append(fixture.PreStateHash, fmt.Sprintf("0x%x", preHash))
		fixture.PostState = append(fixture.PostState, fmt.Sprintf("0x%x", []byte(state.EncodeWitness())))
		fixture.PostProof = append(fixture.PostProof, fmt.Sprintf("0x%x", postProof))
		fixture.Result = append(fixture.Result, hex64(result))
		fixture.Flags = append(fixture.Flags, hex64(flags))
		fixture.Steps = append(fixture.Steps, steps)
	}

	out, err := json.MarshalIndent(&fixture, "", "  ")
	if err != nil {
		return err
	}
	dest := ctx.Path(FloatSubGameOutFlag.Name)
	if err := os.WriteFile(dest, out, 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %d float sub-game cases to %s\n", len(fixture.Op), dest)
	fmt.Printf("  co-processor  %s\n", path)
	fmt.Printf("  image root    %s\n", fixture.ImageRoot)
	fmt.Printf("  entry         %s\n", fixture.Entry)
	return nil
}

var FloatSubGameFixtureCommand = &cli.Command{
	Name:        "float-subgame-fixture",
	Usage:       "Emit the float sub-game fixture for FloatSubGame.sol",
	Description: "Runs the nested float machine in Go and records what the contract must reproduce.",
	Action:      FloatSubGameFixture,
	Flags: []cli.Flag{
		FloatSubGameOutFlag,
		FloatSubGameCoprocessorFlag,
	},
}
