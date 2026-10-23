package xray

import (
	"io"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/yao-jz/mlvm/rvgo/fast"
	"github.com/yao-jz/mlvm/rvgo/internal/e2edemo"
)

func profileGuest(t *testing.T, g *e2edemo.Guest) (*Report, common.Hash) {
	t.Helper()
	p, err := NewProfiler(g.ELF)
	require.NoError(t, err)
	inst := fast.NewInstrumentedState(g.State, nil, io.Discard, io.Discard)
	for !g.State.Exited {
		if err := p.Step(inst, g.State); err != nil {
			t.Fatal(err)
		}
	}
	post, err := g.State.EncodeWitness().StateHash()
	require.NoError(t, err)
	return p.Report(), post
}

func sumSelf(n *Node) uint64 {
	total := n.Self
	for _, c := range n.Children {
		total += sumSelf(c)
	}
	return total
}

func TestProfileMLP(t *testing.T) {
	g, err := e2edemo.NewGuest("mlp", "", 0)
	require.NoError(t, err)
	rep, post := profileGuest(t, g)

	// Profiling must not change execution: same step count and poststate as `mlvm demo`.
	res, err := e2edemo.Run(e2edemo.Options{Demo: "mlp"})
	require.NoError(t, err)
	require.Equal(t, res.Receipt.Steps, rep.Steps)
	require.Equal(t, res.Receipt.PostState, post.Hex())

	require.Equal(t, rep.Steps, sumSelf(rep.Tree), "every instruction lands in exactly one frame")
	require.Equal(t, rep.Steps, rep.Tree.Value)
	var classTotal, mnemTotal uint64
	for _, c := range rep.Classes {
		classTotal += c.Count
	}
	for _, m := range rep.Mnemonics {
		mnemTotal += m.Count
	}
	require.Equal(t, rep.Steps, classTotal)
	require.Equal(t, rep.Steps, mnemTotal)
	require.Greater(t, rep.FloatShare, 0.0)

	names := map[string]bool{}
	for _, f := range rep.Functions {
		names[f.Name] = true
	}
	require.True(t, names["main"], "main should appear in the profile")
	require.True(t, strings.Contains(rep.Folded(), "main"), "folded stacks should go through main")
}

func TestProfileDecoder(t *testing.T) {
	if testing.Short() {
		t.Skip("decoder e2e")
	}
	g, err := e2edemo.NewDecoderGuest("Once upon a time", 4)
	require.NoError(t, err)
	rep, _ := profileGuest(t, g)
	require.Equal(t, rep.Steps, sumSelf(rep.Tree))
	for _, f := range rep.Functions[:12] {
		t.Logf("%-28s self %5.1f%%  total %11d", f.Name, 100*f.Share, f.Total)
	}
	for _, c := range rep.Classes {
		t.Logf("%-16s %5.1f%%", c.Name, 100*c.Share)
	}
	t.Logf("precision %v float share %.3f", rep.Precision, rep.FloatShare)
	require.Equal(t, "matmul", rep.Functions[0].Name, "a transformer spends most of its instructions in matmul")
	require.Greater(t, rep.Precision["s"], rep.Precision["d"], "the decoder is single precision")
}
