package liar

import (
	"bytes"
	"encoding/json"
	"math/bits"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yao-jz/mlvm/rvgo/fast"
	"github.com/yao-jz/mlvm/rvgo/internal/e2edemo"
	"github.com/yao-jz/mlvm/rvgo/internal/xray"
)

func runParties(t *testing.T, g *e2edemo.Guest, fault Fault, interval uint64) (*Party, *Party) {
	t.Helper()
	honest := NewParty("honest", g.State, nil, interval)
	liar := NewParty("liar", g.State, &fault, interval)
	require.NoError(t, honest.Run(0, nil))
	require.NoError(t, liar.Run(0, nil))
	return honest, liar
}

func checkGame(t *testing.T, res *Result, honest *Party) {
	t.Helper()
	require.False(t, res.NoDispute)
	require.NotNil(t, res.Verdict)
	n := max(res.HonestSteps, res.LiarSteps)
	// Halving [0, n] down to one instruction takes floor or ceil of log2(n) questions.
	require.GreaterOrEqual(t, len(res.Rounds), bits.Len64(n)-1)
	require.LessOrEqual(t, len(res.Rounds), bits.Len64(n-1))
	v := res.Verdict
	require.Equal(t, res.Deviation.Step, v.Step, "bisection must land on the instruction the liar changed")
	require.Equal(t, "honest", v.Winner)
	require.Equal(t, v.HonestPost, v.RefereePost)
	require.NotEqual(t, v.LiarPost, v.RefereePost)
	agreed, err := honest.HashAt(v.Step)
	require.NoError(t, err)
	require.Equal(t, agreed, v.Agreed)

	// The record alone, after a trip through JSON, leads a referee to the same ruling.
	again, err := Recheck(roundTrip(t, res))
	require.NoError(t, err)
	require.Equal(t, v.RefereePost, again.RefereePost)
	require.Equal(t, v.Winner, again.Winner)
}

func roundTrip(t *testing.T, res *Result) *Result {
	t.Helper()
	data, err := json.Marshal(res)
	require.NoError(t, err)
	var out Result
	require.NoError(t, json.Unmarshal(data, &out))
	return &out
}

func TestULPLieOnMLPIsFound(t *testing.T) {
	g, err := e2edemo.NewGuest("mlp", "", 0)
	require.NoError(t, err)
	honest, liar := runParties(t, g, Fault{Kind: FaultULP, Mnemonic: "fmul.d", AtStep: 150_000, Window: 1 << 20}, 1<<15)
	res, err := Play(honest, liar, nil, nil)
	require.NoError(t, err)
	checkGame(t, res, honest)
	require.Equal(t, "float-arith", res.Verdict.Class)
	require.Contains(t, res.Verdict.Method, "SoftFloat")
	require.Contains(t, res.Verdict.Instruction, "fmul.d")
}

func TestForgedTokenIsFoundBySlowStep(t *testing.T) {
	if testing.Short() {
		t.Skip("decoder e2e")
	}
	g, err := e2edemo.NewDecoderGuest("Once upon a time", 2)
	require.NoError(t, err)
	// Token 0 is 432 (", "); 433 shares its high byte, so exactly one sb is forged.
	honest, liar := runParties(t, g, Fault{Kind: FaultToken, TokenIndex: 0, Token: 433}, 0)
	res, err := Play(honest, liar, nil, nil)
	require.NoError(t, err)
	checkGame(t, res, honest)
	require.Equal(t, "store", res.Verdict.Class)
	require.Contains(t, res.Verdict.Method, "slow.Step")
	require.NotNil(t, res.Verdict.EVM, "integer verdicts are also computed by RISCV.sol")
	require.True(t, res.Verdict.EVM.Matches)
	require.Equal(t, res.Verdict.HonestPost, res.Verdict.EVM.Post)
	t.Logf("RISCV.sol gas: %d", res.Verdict.EVM.GasUsed)

	honestTokens, err := e2edemo.ResultTokens(honest.Final())
	require.NoError(t, err)
	liarTokens, err := e2edemo.ResultTokens(liar.Final())
	require.NoError(t, err)
	require.Equal(t, uint16(432), honestTokens[0])
	require.Equal(t, uint16(433), liarTokens[0])
	require.Equal(t, honestTokens[1:], liarTokens[1:])
}

func TestULPLieOnDecoderIsFound(t *testing.T) {
	if testing.Short() {
		t.Skip("decoder e2e")
	}
	g, err := e2edemo.NewDecoderGuest("Once upon a time", 2)
	require.NoError(t, err)
	honest, liar := runParties(t, g, Fault{Kind: FaultULP, Mnemonic: "fmadd.s", AtStep: 7_000_000}, 0)
	res, err := Play(honest, liar, nil, nil)
	require.NoError(t, err)
	checkGame(t, res, honest)
	require.Contains(t, res.Verdict.Instruction, "fmadd.s")
	require.Contains(t, res.Verdict.Method, "SoftFloat")
	t.Logf("deviation %+v", *res.Deviation)
	for _, a := range liar.Attempts() {
		t.Logf("attempt %+v", a)
	}
	t.Logf("rounds %d, honest replayed %d, liar replayed %d", len(res.Rounds), honest.Replayed, liar.Replayed)
}

func TestRefereeRejectsForgedWitness(t *testing.T) {
	g, err := e2edemo.NewGuest("mlp", "", 0)
	require.NoError(t, err)
	honest := NewParty("honest", g.State, nil, 1<<15)
	require.NoError(t, honest.Run(0, nil))
	tr, err := honest.TransitionAt(1000)
	require.NoError(t, err)
	_, err = Referee(tr)
	require.NoError(t, err)
	tr.Witness.State[witRegs+8*10] ^= 1 // change a0 in the witness
	_, err = Referee(tr)
	require.ErrorContains(t, err, "agreed prestate")
}

// A short generation where the first kept lie vanishes before the end (found by review:
// `mlvm liar --max-new-tokens 2 --at 50%`). RunUntilDisputed must try again until the
// final states differ.
func TestRunUntilDisputedRetriesVanishedLie(t *testing.T) {
	if testing.Short() {
		t.Skip("decoder e2e")
	}
	g, err := e2edemo.NewDecoderGuest("Once upon a time", 2)
	require.NoError(t, err)
	honest := NewParty("honest", g.State, nil, 0)
	require.NoError(t, honest.Run(0, nil))
	fault := Fault{Kind: FaultULP, AtStep: honest.Steps() / 2}

	once := NewParty("liar", g.State, &fault, 0)
	require.NoError(t, once.Run(0, nil))
	if once.FinalHash() != honest.FinalHash() {
		t.Skip("this build's first kept lie already survives; nothing to retry")
	}

	liar, err := RunUntilDisputed("liar", g.State, fault, 0, honest.FinalHash(), 5, nil)
	require.NoError(t, err)
	require.NotEqual(t, honest.FinalHash(), liar.FinalHash())
	vanished := 0
	for _, a := range liar.Attempts() {
		if a.Vanished {
			vanished++
		}
	}
	require.GreaterOrEqual(t, vanished, 1)
	res, err := Play(honest, liar, nil, nil)
	require.NoError(t, err)
	checkGame(t, res, honest)
}

// The referee must take the instruction from the proven fetch, not from the party.
func TestRefereeRejectsMisreportedInstruction(t *testing.T) {
	g, err := e2edemo.NewGuest("mlp", "", 0)
	require.NoError(t, err)
	honest := NewParty("honest", g.State, nil, 1<<15)
	require.NoError(t, honest.Run(0, nil))
	tr, err := honest.TransitionAt(1001)
	require.NoError(t, err)
	tr.Word = 0x53 // claim a float op for what is really a load
	_, err = Referee(tr)
	require.ErrorContains(t, err, "witness proves")

	tr, err = honest.TransitionAt(1001)
	require.NoError(t, err)
	tr.Witness.MemProof[40] ^= 1 // corrupt a sibling of the fetch proof
	_, err = Referee(tr)
	require.ErrorContains(t, err, "memory root")
}

func TestMnemonicTargetsConversions(t *testing.T) {
	g, err := e2edemo.NewGuest("mlp", "", 0)
	require.NoError(t, err)
	liar := NewParty("liar", g.State, &Fault{Kind: FaultULP, Mnemonic: "fcvt.d.w", Window: 1 << 20}, 1<<15)
	err = liar.Run(0, nil)
	if err != nil {
		require.NotContains(t, err.Error(), "no instruction matched", "fcvt.d.w exists in the MLP guest")
	}
	require.NotEmpty(t, liar.Attempts())
	require.Contains(t, liar.Attempts()[0].Instruction, "fcvt.d.w")
}

// A misread weight is a load, not arithmetic, so RISCV.sol itself can rule on it.
func TestLoadLieOnMLPIsFoundByRISCVSol(t *testing.T) {
	g, err := e2edemo.NewGuest("mlp", "", 0)
	require.NoError(t, err)
	honest := NewParty("honest", g.State, nil, 1<<15)
	require.NoError(t, honest.Run(0, nil))
	fault := Fault{Kind: FaultLoad, Precision: "any", AtStep: 150_000, Window: 1 << 20}
	liar, err := RunUntilDisputed("liar", g.State, fault, 1<<15, honest.FinalHash(), 5, nil)
	require.NoError(t, err)
	res, err := Play(honest, liar, nil, nil)
	require.NoError(t, err)
	checkGame(t, res, honest)
	require.Equal(t, "float-loadstore", res.Verdict.Class)
	require.Contains(t, res.Verdict.Instruction, "fld")
	require.Contains(t, res.Verdict.Method, "slow.Step")
	require.NotNil(t, res.Verdict.EVM)
	require.True(t, res.Verdict.EVM.Matches)
}

func TestForgedOutputIsFound(t *testing.T) {
	g, err := e2edemo.NewGuest("mlp", "", 0)
	require.NoError(t, err)
	honest := NewParty("honest", g.State, nil, 1<<15)
	var honestOut bytes.Buffer
	honest.SetStdout(&honestOut)
	require.NoError(t, honest.Run(0, nil))
	at := bytes.Index(honestOut.Bytes(), []byte("67.66"))
	require.GreaterOrEqual(t, at, 0, "the MLP prints its checksum: %q", honestOut.String())

	fault := Fault{Kind: FaultOutput, OutputOffset: uint64(at), Output: []byte("97.66")}
	liar := NewParty("liar", g.State, &fault, 1<<15)
	var liarOut bytes.Buffer
	liar.SetStdout(&liarOut)
	require.NoError(t, liar.Run(0, nil))
	require.Equal(t, strings.Replace(honestOut.String(), "67.66", "97.66", 1), liarOut.String())

	res, err := Play(honest, liar, nil, nil)
	require.NoError(t, err)
	checkGame(t, res, honest)
	require.NotNil(t, res.Verdict.EVM)
	require.True(t, res.Verdict.EVM.Matches)

	// Forging bytes the program prints anyway is no lie.
	same := Fault{Kind: FaultOutput, OutputOffset: uint64(at), Output: []byte("67.66")}
	require.ErrorContains(t, NewParty("liar", g.State, &same, 1<<15).Run(0, nil), "no instruction matched")
}

// A run much longer than MaxCheckpoints intervals keeps at most MaxCheckpoints copies,
// and answers every question as a party that kept them all would.
func TestCheckpointsAreThinned(t *testing.T) {
	g, err := e2edemo.NewGuest("mlp", "", 0)
	require.NoError(t, err)
	dense := NewParty("dense", g.State, nil, 1<<10)
	require.NoError(t, dense.Run(0, nil))
	require.LessOrEqual(t, len(dense.checkpoints), MaxCheckpoints)
	require.Greater(t, dense.interval, uint64(1<<10))
	for i, c := range dense.checkpoints {
		require.Zero(t, c.Step%dense.interval)
		if i > 0 {
			require.Greater(t, c.Step, dense.checkpoints[i-1].Step)
		}
	}
	sparse := NewParty("sparse", g.State, nil, 1<<30)
	require.NoError(t, sparse.Run(0, nil))
	for _, step := range []uint64{0, 1, 1023, 1024, 77_777, dense.Steps() - 1} {
		a, err := dense.HashAt(step)
		require.NoError(t, err)
		b, err := sparse.HashAt(step)
		require.NoError(t, err)
		require.Equal(t, b, a, "step %d", step)
	}
}

// Forging the answer a guest commits at MLVM_OUT is a store lie, ruled on by RISCV.sol.
func TestForgedCommitIsFoundByRISCVSol(t *testing.T) {
	g, err := e2edemo.NewGuest("mlp", "", 0)
	require.NoError(t, err)
	honest := NewParty("honest", g.State, nil, 1<<15)
	require.NoError(t, honest.Run(0, nil))
	want, ok := fast.MLResult(honest.Final())
	require.True(t, ok, "the MLP commits its answer")
	require.Len(t, want, 40) // four f64 probabilities, then the argmax as a u64

	fault := Fault{Kind: FaultCommit, OutputOffset: 32, Output: []byte{3}} // argmax := 3
	liar := NewParty("liar", g.State, &fault, 1<<15)
	require.NoError(t, liar.Run(0, nil))
	got, ok := fast.MLResult(liar.Final())
	require.True(t, ok)
	require.Equal(t, byte(3), got[32])
	require.Equal(t, want[:32], got[:32])

	res, err := Play(honest, liar, nil, nil)
	require.NoError(t, err)
	checkGame(t, res, honest)
	require.Equal(t, "store", res.Verdict.Class)
	require.NotNil(t, res.Verdict.EVM)
	require.True(t, res.Verdict.EVM.Matches)
}

// An integer lie inside one function: most crash the program or vanish, and the liar keeps
// trying. Integer verdicts are RISCV.sol's.
func TestIntegerLieInFunctionIsFoundByRISCVSol(t *testing.T) {
	g, err := e2edemo.NewGuest("mlp", "", 0)
	require.NoError(t, err)
	sym, err := xray.NewSymbolizer(g.ELF)
	require.NoError(t, err)
	inDense := func(pc uint64) bool { return sym.Name(sym.Lookup(pc)) == "dense" }

	honest := NewParty("honest", g.State, nil, 1<<15)
	require.NoError(t, honest.Run(0, nil))
	fault := Fault{Kind: FaultInt, InFunction: inDense, Function: "dense", Window: 1 << 20, StepLimit: 2*honest.Steps() + 1<<20}
	liar, err := RunUntilDisputed("liar", g.State, fault, 1<<15, honest.FinalHash(), 10, nil)
	require.NoError(t, err)
	require.True(t, inDense(liar.Deviation().PC))
	res, err := Play(honest, liar, nil, nil)
	require.NoError(t, err)
	checkGame(t, res, honest)
	require.NotNil(t, res.Verdict.EVM)
	require.True(t, res.Verdict.EVM.Matches)
}

func TestRecheckRejectsTamperedRecords(t *testing.T) {
	g, err := e2edemo.NewGuest("mlp", "", 0)
	require.NoError(t, err)
	honest := NewParty("honest", g.State, nil, 1<<15)
	require.NoError(t, honest.Run(0, nil))
	fault := Fault{Kind: FaultLoad, Precision: "any", AtStep: 150_000, Window: 1 << 20}
	liar, err := RunUntilDisputed("liar", g.State, fault, 1<<15, honest.FinalHash(), 5, nil)
	require.NoError(t, err)
	res, err := Play(honest, liar, nil, nil)
	require.NoError(t, err)
	last := len(res.Rounds) - 1
	lastAgreed := -1
	for i, r := range res.Rounds {
		if r.Agree {
			lastAgreed = i
		}
	}
	require.GreaterOrEqual(t, lastAgreed, 0)
	flipHex := func(s string, at int) string {
		b := []byte(s)
		if b[at] == '0' {
			b[at] = '1'
		} else {
			b[at] = '0'
		}
		return string(b)
	}

	for name, tc := range map[string]struct {
		tamper func(r *Result)
		want   string
	}{
		"a question off the midpoint":  {func(r *Result) { r.Rounds[3].Mid++ }, "bisection asks"},
		"an answer marked agree":       {func(r *Result) { r.Rounds[last].Agree = !r.Rounds[last].Agree }, "marked agree"},
		"a question dropped":           {func(r *Result) { r.Rounds = r.Rounds[:last] }, "still in dispute"},
		"a verdict about another step": {func(r *Result) { r.Verdict.Step++ }, "isolated step"},
		"another agreed state": {func(r *Result) {
			r.Rounds[lastAgreed].Honest[5] ^= 1
			r.Rounds[lastAgreed].Liar = r.Rounds[lastAgreed].Honest
			r.Verdict.Agreed = r.Rounds[lastAgreed].Honest
		}, "does not hash to the agreed prestate"},
		"a forged register in the witness": {func(r *Result) {
			r.Verdict.StateWitness = flipHex(r.Verdict.StateWitness, 2+2*(witRegs+8*10)+15)
		}, "does not hash"},
		"a forged sibling in the instruction proof": {func(r *Result) {
			r.Verdict.MemProof = flipHex(r.Verdict.MemProof, 2+2*32*7)
		}, "instruction proof"},
	} {
		t.Run(name, func(t *testing.T) {
			r := roundTrip(t, res)
			tc.tamper(r)
			_, err := Recheck(r)
			require.ErrorContains(t, err, tc.want)
		})
	}

	// Swap the parties' names throughout: the referee rules for whichever claim it
	// reproduces, now called the liar's.
	r := roundTrip(t, res)
	r.HonestPost, r.LiarPost = r.LiarPost, r.HonestPost
	r.HonestSteps, r.LiarSteps = r.LiarSteps, r.HonestSteps
	for i := range r.Rounds {
		r.Rounds[i].Honest, r.Rounds[i].Liar = r.Rounds[i].Liar, r.Rounds[i].Honest
	}
	r.Verdict.HonestPost, r.Verdict.LiarPost = r.Verdict.LiarPost, r.Verdict.HonestPost
	v, err := Recheck(r)
	require.NoError(t, err)
	require.Equal(t, "liar", v.Winner)
}
