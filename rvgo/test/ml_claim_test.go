package test

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/yao-jz/mlvm/rvgo/fast"
)

// TestMLClaimReadback takes a neural network from "it ran somewhere" to "a contract can
// read the answer committed in this post-state".
//
// A fault proof settles "this machine went from state A to state B". That is not yet a claim
// about a model. What closes the gap is that the guest writes its answer into its own memory,
// so the final memory root commits to it and a contract reads it back with one merkle branch
// — the same trick the float sub-game uses, one level up.
//
// The property that matters is the last act: a different answer cannot be read against the
// same post-state, because the answer is inside its memory root. A separate dispute protocol
// would still have to establish which post-state is authoritative.
//
// Run:  ./scripts/ml-claim.sh
func TestMLClaimReadback(t *testing.T) {
	if _, err := os.Stat("../../rvsol/out/MLClaim.sol/MLClaim.json"); os.IsNotExist(err) {
		t.Skip("no forge artifacts; run `forge build` in rvsol first")
	}
	const programPath = "../../examples/build/mlp"
	if _, err := os.Stat(programPath); os.IsNotExist(err) {
		t.Skip("examples not built; run `make examples`")
	}

	say := func(format string, args ...any) { fmt.Printf(format+"\n", args...) }
	rule := func(title string) {
		say(" ")
		say("──────────────────────────────────────────────────────────────────")
		say("  %s", title)
		say("──────────────────────────────────────────────────────────────────")
	}

	// ═══ Act 1 ═══ The prestate: what is being claimed about. ═══════════════════════════

	rule("ACT 1 · the prestate commits to the program and its input")

	load := func() *fast.VMState {
		f, err := elf.Open(programPath)
		require.NoError(t, err)
		defer f.Close()
		state, err := fast.LoadELF(f)
		require.NoError(t, err)
		require.NoError(t, fast.PatchVM(f, state))
		require.NoError(t, state.LoadRandomNumberFile("../random"))
		return state
	}

	state := load()
	preHash, err := state.EncodeWitness().StateHash()
	require.NoError(t, err)

	// A prestate that depended on anything but the committed inputs would make the claim
	// meaningless: two honest parties would compute different ones.
	again, err := load().EncodeWitness().StateHash()
	require.NoError(t, err)
	require.Equal(t, preHash, again, "the prestate must be a pure function of the inputs")

	say("program        %s", programPath)
	say("prestate       %x", preHash)
	say("               reproducible: `bin/mlvm ml-claim --path ... --prestate`")
	say("               anyone holding the same program and input derives this and no other")

	// ═══ Act 2 ═══ Run it. The answer goes into memory, not into stdout. ════════════════

	rule("ACT 2 · the network runs and commits its answer to its own memory")

	claim, err := fast.MakeMLClaim(state, 0)
	require.NoError(t, err)
	require.Equal(t, preHash, claim.PreState)

	probs, argmax := decodeMLPAnswer(t, claim.Answer)
	say("steps          %d", claim.Steps)
	say("poststate      %x", claim.PostState)
	say("answer         %d bytes committed at %#x", len(claim.Answer), fast.MLResultBase)
	for i, p := range probs {
		say("  p[%d]         %.17g", i, p)
	}
	say("  argmax       %d", argmax)

	// ═══ Act 3 ═══ The chain reads it, from the state rather than from a promise. ═══════

	rule("ACT 3 · a contract reads the answer out of the committed state")

	env := newEVMEnv(t, testContracts(t), testAddrs)
	addr := deployMLClaim(t, env)

	proof := fast.MLResultProof(state)
	stateData := state.EncodeWitness()
	onChain, gas := callReadAnswer(t, env, addr, stateData, proof[:])

	require.Equal(t, []byte(claim.Answer), onChain, "the chain must read what the guest wrote")
	say("MLClaim.readAnswer(poststate, proof)")
	say("  returned     %x", onChain)
	say("  matches      the bytes the network computed")
	say("  cost         %d gas, one merkle branch", gas)

	// ═══ Act 4 ═══ Why it cannot be forged. ════════════════════════════════════════════

	rule("ACT 4 · the answer cannot be changed without changing the state")

	// Flip one bit of the committed answer and re-derive the state hash. If the answer were
	// merely printed, this would cost nothing; because it lives in the memory the state root
	// covers, it moves the very hash the dispute game is about.
	tampered := load()
	tin := fast.NewInstrumentedState(tampered, nil, os.Stdout, os.Stderr)
	for !tampered.Exited {
		_, err := tin.Step(false)
		require.NoError(t, err)
	}
	honestHash, err := tampered.EncodeWitness().StateHash()
	require.NoError(t, err)
	require.Equal(t, claim.PostState, honestHash)

	var one [1]byte
	tampered.Memory.GetUnaligned(fast.MLResultBase+16, one[:])
	one[0] ^= 0x01
	require.NoError(t, tampered.Memory.SetMemoryRange(fast.MLResultBase+16, bytes.NewReader(one[:])))
	forgedHash, err := tampered.EncodeWitness().StateHash()
	require.NoError(t, err)

	require.NotEqual(t, claim.PostState, forgedHash, "a changed answer must change the state hash")
	say("honest poststate  %x", claim.PostState)
	say("one bit flipped   %x", forgedHash)
	say("                  a changed answer produces a different committed machine")

	// And the contract rejects the forged answer against the honest state, because the
	// branch no longer reconstructs.
	forgedProof := tampered.Memory.MerkleProof(fast.MLResultBase)
	_, _, callErr := rawReadAnswer(t, env, addr, stateData, forgedProof[:])
	require.Error(t, callErr, "an answer from another state must not read against this one")
	say("                  and MLClaim.readAnswer rejects it against the honest state")

	// ═══ Verdict ═══════════════════════════════════════════════════════════════════════

	id := claimIDOf(t, env, addr, claim.PreState, claim.PostState, claim.Answer)
	rule("VERDICT")
	say("The claim is one word:")
	say(" ")
	say("  claimId        %x", id)
	say("  = keccak(prestate, poststate, keccak(answer))")
	say(" ")
	say("  prestate       binds it to this program and this input")
	say("  poststate      can be supplied to a future dispute protocol")
	say("  answer         is read out of the poststate, not taken on trust")
	say(" ")
	say("What this does NOT do: establish that the poststate is correct. That needs an")
	say("outer game, trace binding, and a challenger. This component only binds the answer")
	say("bytes to a particular poststate.")
}

func decodeMLPAnswer(t *testing.T, answer []byte) (probs []float64, argmax uint64) {
	t.Helper()
	require.Len(t, answer, 40, "4 doubles plus an argmax")
	for i := 0; i < 4; i++ {
		probs = append(probs, math.Float64frombits(binary.LittleEndian.Uint64(answer[i*8:])))
	}
	sum := 0.0
	for _, p := range probs {
		sum += p
	}
	require.InDelta(t, 1.0, sum, 1e-12, "softmax outputs must be a distribution")
	return probs, binary.LittleEndian.Uint64(answer[32:])
}

func deployMLClaim(t *testing.T, env *vm.EVM) common.Address {
	t.Helper()
	dat, err := os.ReadFile("../../rvsol/out/MLClaim.sol/MLClaim.json")
	require.NoError(t, err)
	var artifact Contract
	require.NoError(t, json.Unmarshal(dat, &artifact))
	require.NotEmpty(t, artifact.Bytecode.Object)

	_, addr, _, err := env.Create(vm.AccountRef(testAddrs.Sender),
		artifact.Bytecode.Object, 30_000_000, big.NewInt(0))
	require.NoError(t, err)
	return addr
}

func rawReadAnswer(t *testing.T, env *vm.EVM, addr common.Address, state, proof []byte) ([]byte, uint64, error) {
	t.Helper()
	sel := crypto.Keccak256([]byte("readAnswer(bytes,bytes)"))[:4]
	stateEnc, proofEnc := encodeBytes(state), encodeBytes(proof)
	input := append([]byte{}, sel...)
	input = append(input, common.LeftPadBytes(big.NewInt(2*32).Bytes(), 32)...)
	input = append(input, common.LeftPadBytes(big.NewInt(int64(2*32+len(stateEnc))).Bytes(), 32)...)
	input = append(input, stateEnc...)
	input = append(input, proofEnc...)

	const startGas = 30_000_000
	ret, left, err := env.Call(vm.AccountRef(testAddrs.Sender), addr, input, startGas, big.NewInt(0))
	return ret, startGas - left, err
}

func callReadAnswer(t *testing.T, env *vm.EVM, addr common.Address, state, proof []byte) ([]byte, uint64) {
	t.Helper()
	ret, gas, err := rawReadAnswer(t, env, addr, state, proof)
	require.NoError(t, err, "readAnswer reverted (ret %x)", ret)
	// ABI: offset, length, data
	require.GreaterOrEqual(t, len(ret), 64)
	n := new(big.Int).SetBytes(ret[32:64]).Uint64()
	return ret[64 : 64+n], gas
}

func claimIDOf(t *testing.T, env *vm.EVM, addr common.Address, pre, post common.Hash, answer []byte) common.Hash {
	t.Helper()
	sel := crypto.Keccak256([]byte("claimId(bytes32,bytes32,bytes)"))[:4]
	input := append([]byte{}, sel...)
	input = append(input, pre[:]...)
	input = append(input, post[:]...)
	input = append(input, common.LeftPadBytes(big.NewInt(3*32).Bytes(), 32)...)
	input = append(input, encodeBytes(answer)...)

	ret, _, err := env.Call(vm.AccountRef(testAddrs.Sender), addr, input, 30_000_000, big.NewInt(0))
	require.NoError(t, err)
	require.Len(t, ret, 32)
	return common.BytesToHash(ret)
}
