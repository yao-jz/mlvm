package dispute

import (
	"debug/elf"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/yao-jz/mlvm/rvgo/fast"
)

func TestMidpoint(t *testing.T) {
	if Midpoint(0, 10) != 5 {
		t.Fatalf("got %d", Midpoint(0, 10))
	}
	if Midpoint(0, 1) != 1 {
		t.Fatalf("adjacent got %d", Midpoint(0, 1))
	}
}

func TestTamperRootLoses(t *testing.T) {
	h0 := common.HexToHash("0x01")
	h1 := common.HexToHash("0x02")
	h2 := common.HexToHash("0x03")
	g := &Game{
		PreHash:  h0,
		PostHash: h2,
		MaxSteps: 2,
		Trace:    []common.Hash{h0, h1, h2},
		Claims:   []Claim{{Index: 0, Parent: -1, Lo: 0, Hi: 2, Position: 2, StateHash: h2}},
	}
	if v, err := g.ResolveClaim(0); err != nil || v != DefenderWins {
		t.Fatalf("honest: %v %s", err, v)
	}
	g.TamperPostHash(common.HexToHash("0xdead"))
	if v, err := g.ResolveClaim(0); err != nil || v != ChallengerWins {
		t.Fatalf("tampered: %v %s", err, v)
	}
}

func TestBuildTraceBisectMLP(t *testing.T) {
	const path = "../e2edemo/assets/mlp-rv64imafd.elf"
	f, err := elf.Open(path)
	if err != nil {
		t.Skipf("mlp elf: %v", err)
	}
	defer f.Close()
	state, err := fast.LoadELF(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := fast.PatchVM(f, state); err != nil {
		t.Fatal(err)
	}
	_ = state.LoadRandomNumberFile("")
	g, err := BuildTrace(state, 32)
	if err != nil {
		t.Fatalf("BuildTrace: %v", err)
	}
	if g.MaxSteps == 0 {
		t.Fatal("expected steps")
	}
	mid, err := g.Bisect(0)
	if err != nil {
		t.Fatal(err)
	}
	if v, err := g.ResolveClaim(mid); err != nil || v != DefenderWins {
		t.Fatalf("mid: %v %s", err, v)
	}
}

func TestBisectConverges(t *testing.T) {
	n := 16
	trace := make([]common.Hash, n+1)
	for i := 0; i <= n; i++ {
		trace[i] = common.BytesToHash([]byte{byte(i + 1)})
	}
	g := &Game{
		PreHash:  trace[0],
		PostHash: trace[n],
		MaxSteps: uint64(n),
		Trace:    trace,
		Claims: []Claim{{
			Index: 0, Parent: -1, Lo: 0, Hi: uint64(n), Position: uint64(n), StateHash: trace[n],
		}},
	}
	parent := 0
	for g.Claims[parent].Hi > g.Claims[parent].Lo+1 {
		idx, err := g.Bisect(parent)
		if err != nil {
			t.Fatalf("bisect %d: %v", parent, err)
		}
		child := g.Claims[idx]
		if child.Hi-child.Lo >= g.Claims[parent].Hi-g.Claims[parent].Lo {
			t.Fatalf("interval did not shrink: parent [%d,%d] child [%d,%d]",
				g.Claims[parent].Lo, g.Claims[parent].Hi, child.Lo, child.Hi)
		}
		parent = idx
	}
	if _, err := g.Bisect(parent); err == nil {
		t.Fatal("adjacent interval should refuse further bisection")
	}
}
