// Package dispute implements the local demo bisection orchestrator used by
// `mlvm demo --web` and tamper tests. It mirrors MLDisputeGame.sol without bonds.
package dispute

import (
	"fmt"
	"io"

	"github.com/ethereum/go-ethereum/common"

	"github.com/yao-jz/mlvm/rvgo/fast"
)

// Claim is one node in a bisection tree over instruction indices.
// Lo/Hi are the contested instruction-index interval; Position is the
// instruction index whose state hash this node asserts (Hi for the root).
type Claim struct {
	Index      int         `json:"index"`
	Parent     int         `json:"parent"`
	Lo         uint64      `json:"lo"`
	Hi         uint64      `json:"hi"`
	Position   uint64      `json:"position"`
	StateHash  common.Hash `json:"stateHash"`
	Challenger bool        `json:"challenger"`
}

// Game is an in-memory bisection over a recorded fast trace.
type Game struct {
	PreHash  common.Hash   `json:"preHash"`
	PostHash common.Hash   `json:"postHash"`
	MaxSteps uint64        `json:"maxSteps"`
	Claims   []Claim       `json:"claims"`
	Trace    []common.Hash `json:"-"`
}

// BuildTrace runs state for at most maxSteps (or until exit) and records every state hash.
func BuildTrace(state *fast.VMState, maxSteps uint64) (*Game, error) {
	if state == nil {
		return nil, fmt.Errorf("nil state")
	}
	pre, err := state.EncodeWitness().StateHash()
	if err != nil {
		return nil, err
	}
	trace := []common.Hash{pre}
	inst := fast.NewInstrumentedState(state, nil, io.Discard, io.Discard)
	var step uint64
	for !state.Exited && (maxSteps == 0 || step < maxSteps) {
		if _, err := inst.Step(false); err != nil {
			return nil, fmt.Errorf("step %d: %w", step, err)
		}
		step++
		h, err := state.EncodeWitness().StateHash()
		if err != nil {
			return nil, err
		}
		trace = append(trace, h)
	}
	post := trace[len(trace)-1]
	return &Game{
		PreHash:  pre,
		PostHash: post,
		MaxSteps: uint64(len(trace) - 1),
		Trace:    trace,
		Claims: []Claim{{
			Index:     0,
			Parent:    -1,
			Lo:        0,
			Hi:        uint64(len(trace) - 1),
			Position:  uint64(len(trace) - 1),
			StateHash: post,
		}},
	}, nil
}

// Midpoint returns the instruction index between lo and hi.
func Midpoint(lo, hi uint64) uint64 {
	if hi <= lo+1 {
		return hi
	}
	return lo + (hi-lo)/2
}

// Bisect appends a claim at the midpoint of the parent's contested range.
// Each round strictly shrinks [Lo, Hi] until the interval is adjacent.
func (g *Game) Bisect(parentIndex int) (int, error) {
	if parentIndex < 0 || parentIndex >= len(g.Claims) {
		return -1, fmt.Errorf("invalid parent")
	}
	parent := g.Claims[parentIndex]
	if parent.Hi <= parent.Lo+1 {
		return -1, fmt.Errorf("interval [%d, %d] is already adjacent", parent.Lo, parent.Hi)
	}
	mid := Midpoint(parent.Lo, parent.Hi)
	if mid >= uint64(len(g.Trace)) {
		return -1, fmt.Errorf("midpoint %d out of trace", mid)
	}
	if mid <= parent.Lo || mid >= parent.Hi {
		return -1, fmt.Errorf("bisection made no progress on [%d, %d]", parent.Lo, parent.Hi)
	}
	idx := len(g.Claims)
	g.Claims = append(g.Claims, Claim{
		Index:      idx,
		Parent:     parentIndex,
		Lo:         parent.Lo,
		Hi:         mid,
		Position:   mid,
		StateHash:  g.Trace[mid],
		Challenger: true,
	})
	return idx, nil
}

// TamperPostHash replaces the root claim with a forged hash (demo attack).
func (g *Game) TamperPostHash(forged common.Hash) {
	g.Claims[0].StateHash = forged
	g.PostHash = forged
}

// Verdict is the demo resolution outcome.
type Verdict string

const (
	DefenderWins   Verdict = "defender"
	ChallengerWins Verdict = "challenger"
)

// ResolveClaim checks the claim hash against the recorded honest trace.
func (g *Game) ResolveClaim(claimIndex int) (Verdict, error) {
	if claimIndex < 0 || claimIndex >= len(g.Claims) {
		return "", fmt.Errorf("invalid claim")
	}
	c := g.Claims[claimIndex]
	if c.Position >= uint64(len(g.Trace)) {
		return ChallengerWins, nil
	}
	if g.Trace[c.Position] != c.StateHash {
		return ChallengerWins, nil
	}
	return DefenderWins, nil
}
