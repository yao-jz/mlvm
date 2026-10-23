package test

import (
	"debug/elf"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/yao-jz/mlvm/rvgo/fast"
	"github.com/yao-jz/mlvm/rvgo/slow"
)

// TestFloatTerminalRoute integrates the experimental f64 terminal components around an
// instruction selected from a real program, without implementing IEEE-754 in Solidity.
//
// It exercises the following route locally:
//
//	act 1  a neural network runs and the test selects a routable instruction
//	act 2  a local bisection algorithm isolates a synthetic divergent hash
//	act 3  that instruction is FADD.D, and the on-chain verifier refuses to execute it
//	act 4  the disputed operation becomes a nested RISC-V machine, derived on chain
//	act 5  another local bisection isolates one instruction of the nested machine
//	act 6  that instruction is RV64I, so RISCV.sol executes it - unchanged
//	act 7  the result is read back on chain, and it is bit-identical to the VM's float unit
//
// The execution and EVM calls are real, but this is not a complete dispute game: there is
// no on-chain outer bisection, trace commitment, bond/timeout logic, binding of caller-
// supplied operands to the outer witness, or application of the nested result to that
// witness. See docs/STATUS.md.
//
// Run:  ./scripts/float-dispute.sh
func TestFloatTerminalRoute(t *testing.T) {
	if _, err := os.Stat("../../rvsol/out/RISCV.sol/RISCV.json"); os.IsNotExist(err) {
		t.Skip("no forge artifacts; run `forge build` in rvsol first")
	}
	const programPath = "../../examples/build/mlp"
	if _, err := os.Stat(programPath); os.IsNotExist(err) {
		t.Skip("examples not built; run `make examples`")
	}
	coprocessor := loadCoprocessor(t)

	// Printed rather than t.Logf-ed: the narrative is the point of this test, and a
	// "float_dispute_test.go:50:" prefix on every line makes it unreadable.
	say := func(format string, args ...any) { fmt.Printf(format+"\n", args...) }
	rule := func(title string) {
		say(" ")
		say("──────────────────────────────────────────────────────────────────")
		say("  %s", title)
		say("──────────────────────────────────────────────────────────────────")
	}

	// ═══ Act 1 ═══ A real program, and a disagreement about what it computed. ═══════════

	rule("ACT 1 · a neural network runs, and the test selects a float transition")

	loadProgram := func() *fast.VMState {
		f, err := elf.Open(programPath)
		require.NoError(t, err)
		defer f.Close()
		state, err := fast.LoadELF(f)
		require.NoError(t, err)
		require.NoError(t, fast.PatchVM(f, state))
		require.NoError(t, state.LoadRandomNumberFile("../random"))
		return state
	}

	// Run once, honestly, and find the first float instruction the program executes. That
	// is what the two parties will end up disputing.
	honest := loadProgram()
	in := fast.NewInstrumentedState(honest, nil, io.Discard, io.Discard)

	// One pass over the whole trace: count what fraction of the program's float instructions
	// can actually be settled, and pick the one to dispute.
	//
	// Routability is decided by the shared decoder, not by a test-local guess. If
	// fast.DecodeFloatSubGame says an instruction has no sub-game route, the dispute genuinely
	// cannot be settled, and the demo must not pretend otherwise - so the unroutable ones are
	// counted and reported rather than skipped over quietly.
	var disputedStep uint64
	var disputedInstr uint32
	var nFloat, nSubGame, nNative, nUnroutable uint64
	var arithStep uint64
	var arithInstr uint32
	var foundFlagRaiser bool
	for !honest.Exited {
		instr := honest.Instr()
		switch class := classifyFloat(instr, &honest.Registers, &honest.FRegisters); class {
		case floatRoutable:
			nFloat++
			nSubGame++
			// Dispute an arithmetic instruction rather than the first routable one of any
			// kind: arithmetic is what implementing IEEE-754 in Yul would have been for, so
			// it is the honest headline. Conversions and comparisons route the same way.
			//
			// Prefer one that also raises an exception, so the demonstration covers both
			// halves of what a float instruction does. Most operations are exact and leave
			// fcsr alone, in which case proving the value proves the whole transition.
			if isFloatArithmetic(instr) {
				if disputedInstr == 0 {
					disputedStep, disputedInstr = honest.Step, instr
				}
				arithStep, arithInstr = honest.Step, instr
			}
		case floatNative:
			nFloat++
			nNative++
		case floatUnroutable:
			nFloat++
			nUnroutable++
		}
		before := honest.FCSR
		_, err := in.Step(false)
		require.NoError(t, err)
		if arithInstr != 0 && honest.FCSR != before && !foundFlagRaiser {
			disputedStep, disputedInstr = arithStep, arithInstr
			foundFlagRaiser = true
		}
		arithInstr = 0
	}
	require.NotZero(t, disputedInstr, "the program executed no routable float arithmetic")
	totalSteps := honest.Step

	rd := (disputedInstr >> 7) & 0x1F
	say("program        %s  (a 3-layer MLP: GEMM + bias + tanh + softmax)", programPath)
	say("total steps    %d", totalSteps)
	say(" ")
	say("float instructions executed   %d", nFloat)
	say("  locally routable to f64     %6d  (%.1f%%)", nSubGame, 100*float64(nSubGame)/float64(nFloat))
	say("  executed by RISCV.sol       %6d  (%.1f%%)   bit manipulation, no arithmetic", nNative, 100*float64(nNative)/float64(nFloat))
	say("  no settlement route         %6d  (%.1f%%)", nUnroutable, 100*float64(nUnroutable)/float64(nFloat))
	say(" ")
	say("disputed step  %d, instruction %#08x", disputedStep, disputedInstr)

	// ═══ Act 2 ═══ The outer bisection, run for real. ═══════════════════════════════════

	rule("ACT 2 · local bisection isolates a synthetic divergence")

	// The honest party can answer "what is the state hash after n steps?" for any n, by
	// re-executing. That is the only capability bisection needs.
	hashAfter := func(n uint64) common.Hash {
		s := loadProgram()
		i := fast.NewInstrumentedState(s, nil, io.Discard, io.Discard)
		for s.Step < n && !s.Exited {
			_, err := i.Step(false)
			require.NoError(t, err)
		}
		h, err := s.EncodeWitness().StateHash()
		require.NoError(t, err)
		return h
	}

	// The dishonest party agrees about everything up to the float instruction and lies from
	// there on - the cheapest possible attack, and the hardest to localise.
	dishonestHashAfter := func(n uint64) common.Hash {
		h := hashAfter(n)
		if n > disputedStep {
			h[31] ^= 0x01
		}
		return h
	}

	// Standard bisection: maintain a range where the two agree at lo and disagree at hi.
	lo, hi := uint64(0), totalSteps
	require.Equal(t, hashAfter(lo), dishonestHashAfter(lo), "the parties must start from a common state")
	require.NotEqual(t, hashAfter(hi), dishonestHashAfter(hi), "there must be something to dispute")

	rounds := 0
	for hi-lo > 1 {
		mid := lo + (hi-lo)/2
		if hashAfter(mid) == dishonestHashAfter(mid) {
			lo = mid
		} else {
			hi = mid
		}
		rounds++
	}
	say("range          [0, %d]  →  %d local hash-query rounds", totalSteps, rounds)
	say("converged on   step %d, the transition from state %d to %d", lo, lo, hi)
	require.Equal(t, disputedStep, lo, "bisection must land on the instruction that was faked")

	// ═══ Act 3 ═══ The wall: the on-chain verifier cannot execute this instruction. ═════

	rule("ACT 3 · RISCV.sol refuses the disputed instruction")

	contracts := testContracts(t)
	env := newEVMEnv(t, contracts, testAddrs)

	// Re-derive the pre-state of the disputed step and produce its witness, which is exactly
	// what a disputant would submit on chain.
	preState := loadProgram()
	preIn := fast.NewInstrumentedState(preState, nil, io.Discard, io.Discard)
	for preState.Step < disputedStep {
		_, err := preIn.Step(false)
		require.NoError(t, err)
	}
	fcsrBefore := preState.FCSR
	// Decode before stepping: rd may alias a source register, and the conversions read an
	// integer register rather than a float one.
	op, a, b, cc, ok := fast.DecodeFloatSubGame(disputedInstr, &preState.Registers, &preState.FRegisters)
	require.True(t, ok)
	mnemonic := fast.FloatOpName(op)

	preWitness, err := preIn.Step(true)
	require.NoError(t, err)

	// What the VM's own float unit produced. A float instruction updates *two* pieces of
	// committed state, and the sub-game has to prove both: the destination register and the
	// exception flags in fcsr.
	claimed := preState.FRegisters[rd]
	if fast.FloatSubGameWritesInteger(op) {
		claimed = preState.Registers[rd]
	}
	claimedFlags := preState.FCSR &^ fcsrBefore

	say("instruction    %s", mnemonic)
	say("operands       a = %#016x  (%v)", a, math.Float64frombits(a))
	say("               b = %#016x  (%v)", b, math.Float64frombits(b))
	say("VM result      %#016x  (%v)", claimed, math.Float64frombits(claimed))

	// Submitting this step to the verifier reverts. That is intentional: a silent no-op
	// would produce a well-formed but incorrect post-state.
	input := preWitness.EncodeStepInput(fast.LocalContext{})
	_, _, evmErr := env.Call(vm.AccountRef(testAddrs.Sender), testAddrs.RISCV, input, 30_000_000, big.NewInt(0))
	require.Error(t, evmErr, "RISCV.sol must refuse a float instruction rather than mis-execute it")
	say("on-chain step  reverted — RISCV.sol implements RV64IMA, not RV64IMAFD")
	say("               (the alternative would be ~3,000 lines of IEEE-754 in Yul)")

	// ═══ Act 4 ═══ The disputed operation becomes a machine, derived on chain. ══════════

	rule("ACT 4 · the operation becomes a nested machine, derived on chain")

	subGameAddr := deployFloatSubGame(t, env, coprocessor)

	// Both parties derive the nested pre-state independently. The contract derives it too,
	// from (imageRoot, op, a, b) and one merkle branch - and all three must agree, or they
	// are not disputing the same machine.
	nestedPre := coprocessor.NewFloatSubGame(op, a, b, cc)
	nestedPreHash, err := nestedPre.EncodeWitness().StateHash()
	require.NoError(t, err)

	imageProof := coprocessor.ImageProof()
	onChainPre, preGas := callNestedPreState(t, env, subGameAddr, op, a, b, cc, imageProof[:])
	require.Equal(t, nestedPreHash, onChainPre, "the chain must derive the same nested machine both parties do")

	say("co-processor   %x  (RV64IMA, verified float-instruction-free at load)", coprocessor.ImageRoot())
	say("nested pre     %x", onChainPre)
	say("derived by     FloatSubGame.nestedPreState(%s, a, b, c, proof)   %d gas", mnemonic, preGas)
	say("boundary       operands are supplied by this test, not bound to the outer witness")

	// ═══ Act 5 ═══ The inner bisection, also run for real. ══════════════════════════════

	rule("ACT 5 · local bisection isolates one nested instruction")

	nestedFinal, nestedSteps, err := coprocessor.ExecuteFloatSubGame(op, a, b, cc)
	require.NoError(t, err)

	nestedHashAfter := func(n uint64) common.Hash {
		s := coprocessor.NewFloatSubGame(op, a, b, cc)
		i := fast.NewInstrumentedState(s, nil, io.Discard, io.Discard)
		for s.Step < n && !s.Exited {
			_, err := i.Step(false)
			require.NoError(t, err)
		}
		h, err := s.EncodeWitness().StateHash()
		require.NoError(t, err)
		return h
	}

	// The dishonest party has to lie somewhere inside the nested execution now, and whatever
	// it picks is an RV64I instruction the verifier can check. Put the lie in the middle of
	// the trace, which is the worst case for bisection depth.
	fakedNestedStep := nestedSteps / 2
	dishonestNestedHashAfter := func(n uint64) common.Hash {
		h := nestedHashAfter(n)
		if n > fakedNestedStep {
			h[31] ^= 0x01
		}
		return h
	}

	nestedLo, nestedHi := uint64(0), nestedSteps
	require.Equal(t, nestedHashAfter(nestedLo), dishonestNestedHashAfter(nestedLo))
	require.NotEqual(t, nestedHashAfter(nestedHi), dishonestNestedHashAfter(nestedHi))

	nestedRounds := 0
	for nestedHi-nestedLo > 1 {
		mid := nestedLo + (nestedHi-nestedLo)/2
		if nestedHashAfter(mid) == dishonestNestedHashAfter(mid) {
			nestedLo = mid
		} else {
			nestedHi = mid
		}
		nestedRounds++
	}
	require.Equal(t, fakedNestedStep, nestedLo, "the inner bisection must land on the faked nested step")
	say("nested trace   %d instructions  →  %d rounds", nestedSteps, nestedRounds)
	say("converged on   nested step %d", nestedLo)

	// ═══ Act 6 ═══ RISCV.sol executes it. Unchanged. ═══════════════════════════════════

	rule("ACT 6 · RISCV.sol executes the nested step — no new instruction semantics")

	nested := coprocessor.NewFloatSubGame(op, a, b, cc)
	nestedIn := fast.NewInstrumentedState(nested, nil, io.Discard, io.Discard)
	for nested.Step < nestedLo {
		_, err := nestedIn.Step(false)
		require.NoError(t, err)
	}
	nestedInstr := nested.Instr()
	nestedWitness, err := nestedIn.Step(true)
	require.NoError(t, err)

	fastHash, err := nested.EncodeWitness().StateHash()
	require.NoError(t, err)
	slowHash, err := slow.Step(nestedWitness.EncodeStepInput(fast.LocalContext{}), nil)
	require.NoError(t, err)
	require.Equal(t, fastHash, slowHash)

	_, evmHash, stepGas := stepEVM(t, env, nestedWitness, testAddrs, nestedLo)
	require.Equal(t, fastHash, evmHash, "the three implementations must agree on the nested step")

	say("nested instr   %#08x at PC %#x", nestedInstr, nested.PC)
	say("post-state     %x", evmHash)
	say("agreement      fast == slow == EVM")
	say("cost           %d gas — the same verifier that passes 84/84 conformance suites", stepGas)

	// ═══ Act 7 ═══ The answer comes back on chain. ═════════════════════════════════════

	rule("ACT 7 · the result is read back on chain")

	postProof := nestedFinal.Memory.MerkleProof(fast.FloatIOBase)
	result, flags, resultGas := callNestedResult(t, env, subGameAddr, nestedFinal.EncodeWitness(), postProof[:])

	// The three things that must all be the same number - and the flags alongside them.
	softfloat, softflags, ok := fast.FloatReferenceWithFlags(op, a, b, cc)
	require.True(t, ok)

	require.Equal(t, claimed, result, "the chain's answer must match what the VM computed")
	require.Equal(t, softfloat, result, "and must match the VM's SoftFloat directly")
	require.Equal(t, claimedFlags, flags, "the chain must also settle the fcsr delta")
	require.Equal(t, uint64(softflags), flags, "and it must match the VM's SoftFloat flags")

	say("value")
	say("  on-chain       %#016x  (%v)", result, math.Float64frombits(result))
	say("  VM float unit  %#016x", claimed)
	say("  rvgo/comp      %#016x", softfloat)
	say("fflags   (the other half of the instruction: fcsr is committed state too)")
	say("  on-chain       %s", flagNames(flags))
	say("  VM float unit  %s", flagNames(claimedFlags))
	say("  rvgo/comp      %s", flagNames(uint64(softflags)))
	say("agreement      value and flags, all three identical      %d gas", resultGas)

	// ═══ Verdict ═══════════════════════════════════════════════════════════════════════

	rule("RESULT")
	say("The experimental terminal route reproduces one %s value and its flags.", mnemonic)
	say(" ")
	say("  local outer search   %2d rounds over %d steps", rounds, totalSteps)
	say("  local inner search   %2d rounds over %d steps", nestedRounds, nestedSteps)
	say("  total depth          %2d rounds", rounds+nestedRounds)
	say(" ")
	say("  derive nested state  %7d gas   (terminal component)", preGas)
	say("  check one step       %7d gas   (RISCV.sol, unmodified)", stepGas)
	say("  read result          %7d gas   (terminal component)", resultGas)
	say("  measured calls       %7d gas", preGas+stepGas+resultGas)
	say("  excluded                      calldata, game bookkeeping, all bisection rounds")
	say(" ")
	say("  value read back                yes")
	say("  fflags read back               yes")
	say("  IEEE-754 in Solidity           none")
	say("  bound to outer transition      no")
	say("  complete dispute game          no")
}

// deployFloatSubGame deploys the real contract into the EVM, constructor and all, so the
// image root and entry point are baked into its runtime code exactly as they would be on a
// live chain.
func deployFloatSubGame(t *testing.T, env *vm.EVM, c *fast.FloatCoprocessor) common.Address {
	t.Helper()
	dat, err := os.ReadFile("../../rvsol/out/FloatSubGame.sol/FloatSubGame.json")
	require.NoError(t, err, "run `forge build` in rvsol")
	var artifact Contract
	require.NoError(t, json.Unmarshal(dat, &artifact))
	require.NotEmpty(t, artifact.Bytecode.Object, "FloatSubGame creation bytecode missing")

	root := c.ImageRoot()
	args := make([]byte, 0, 64)
	args = append(args, root[:]...)
	args = append(args, common.LeftPadBytes(new(big.Int).SetUint64(c.Entry()).Bytes(), 32)...)

	_, addr, _, err := env.Create(
		vm.AccountRef(testAddrs.Sender),
		append(append([]byte{}, artifact.Bytecode.Object...), args...),
		30_000_000, big.NewInt(0))
	require.NoError(t, err, "FloatSubGame deployment must succeed")
	return addr
}

func callNestedPreState(t *testing.T, env *vm.EVM, addr common.Address, op, a, b, c uint64, proof []byte) (common.Hash, uint64) {
	t.Helper()
	sel := crypto.Keccak256([]byte("nestedPreState(uint64,uint64,uint64,uint64,bytes)"))[:4]
	input := append([]byte{}, sel...)
	for _, v := range []uint64{op, a, b, c} {
		input = append(input, common.LeftPadBytes(new(big.Int).SetUint64(v).Bytes(), 32)...)
	}
	input = append(input, common.LeftPadBytes(big.NewInt(5*32).Bytes(), 32)...) // offset of `proof`
	input = append(input, encodeBytes(proof)...)

	const startGas = 30_000_000
	ret, left, err := env.Call(vm.AccountRef(testAddrs.Sender), addr, input, startGas, big.NewInt(0))
	require.NoError(t, err, "nestedPreState reverted (ret %x)", ret)
	require.Len(t, ret, 32)
	return common.BytesToHash(ret), startGas - left
}

func callNestedResult(t *testing.T, env *vm.EVM, addr common.Address, state, proof []byte) (uint64, uint64, uint64) {
	t.Helper()
	sel := crypto.Keccak256([]byte("nestedResult(bytes,bytes)"))[:4]
	stateEnc, proofEnc := encodeBytes(state), encodeBytes(proof)
	input := append([]byte{}, sel...)
	input = append(input, common.LeftPadBytes(big.NewInt(2*32).Bytes(), 32)...)
	input = append(input, common.LeftPadBytes(big.NewInt(int64(2*32+len(stateEnc))).Bytes(), 32)...)
	input = append(input, stateEnc...)
	input = append(input, proofEnc...)

	const startGas = 30_000_000
	ret, left, err := env.Call(vm.AccountRef(testAddrs.Sender), addr, input, startGas, big.NewInt(0))
	require.NoError(t, err, "nestedResult reverted (ret %x)", ret)
	require.Len(t, ret, 64) // (uint64 result, uint64 flags)
	return binary.BigEndian.Uint64(ret[24:32]), binary.BigEndian.Uint64(ret[56:]), startGas - left
}

// encodeBytes ABI-encodes a dynamic `bytes` payload: length word then 32-byte-padded data.
func encodeBytes(b []byte) []byte {
	out := common.LeftPadBytes(big.NewInt(int64(len(b))).Bytes(), 32)
	out = append(out, b...)
	if pad := len(b) % 32; pad != 0 {
		out = append(out, make([]byte, 32-pad)...)
	}
	return out
}

// How a float instruction settles on chain, if it does.
const (
	floatNotFloat = iota
	floatRoutable // via the nested sub-game
	floatNative   // RISCV.sol executes it directly: it is bit manipulation, not arithmetic
	floatUnroutable
)

func classifyFloat(instr uint32, iRegs, fRegs *[32]uint64) int {
	switch instr & 0x7F {
	case 0x43, 0x47, 0x4B, 0x4F: // the multiply-add family
	case 0x53: // OP-FP
		// FSGNJ, FMV.X.D/FCLASS.D and FMV.D.X move and mask bits without doing arithmetic,
		// so RISCV.sol implements them directly and no sub-game is needed.
		switch (instr >> 27) & 0x1F {
		case 0x04, 0x1C, 0x1E:
			return floatNative
		}
	default:
		// Float loads and stores (0x07 / 0x27) are memory operations; the verifier handles
		// them as such. They are not counted as float instructions here.
		return floatNotFloat
	}
	if _, _, _, _, ok := fast.DecodeFloatSubGame(instr, iRegs, fRegs); ok {
		return floatRoutable
	}
	return floatUnroutable
}

// isFloatArithmetic reports whether an instruction actually computes, as opposed to
// converting or comparing.
func isFloatArithmetic(instr uint32) bool {
	switch instr & 0x7F {
	case 0x43, 0x47, 0x4B, 0x4F:
		return true
	case 0x53:
		switch (instr >> 27) & 0x1F {
		case 0x00, 0x01, 0x02, 0x03, 0x0B:
			return true
		}
	}
	return false
}
