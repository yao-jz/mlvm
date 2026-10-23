package liar

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/yao-jz/mlvm/rvgo/fast"
	"github.com/yao-jz/mlvm/rvgo/internal/evmstep"
	"github.com/yao-jz/mlvm/rvgo/internal/rvdis"
	"github.com/yao-jz/mlvm/rvgo/slow"
)

// Round is one bisection question: both parties report their state hash at Mid.
type Round struct {
	N      int         `json:"n"`
	Lo     uint64      `json:"lo"`
	Hi     uint64      `json:"hi"`
	Mid    uint64      `json:"mid"`
	Honest common.Hash `json:"honest"`
	Liar   common.Hash `json:"liar"`
	Agree  bool        `json:"agree"`
}

// Verdict is the referee's ruling on the one instruction bisection isolated.
type Verdict struct {
	Step         uint64      `json:"step"` // executing instruction Step moves state Step to Step+1
	PC           uint64      `json:"pc"`
	Word         string      `json:"word"`
	Instruction  string      `json:"instruction"`
	Function     string      `json:"function,omitempty"`
	Class        string      `json:"class"`
	Agreed       common.Hash `json:"agreedPrestate"`
	HonestPost   common.Hash `json:"honestPost"`
	LiarPost     common.Hash `json:"liarPost"`
	RefereePost  common.Hash `json:"refereePost"`
	Method       string      `json:"method"`
	Winner       string      `json:"winner"`
	StateWitness string      `json:"stateWitness"`
	// MemProof and FSProof are the Merkle proofs the instruction consumes, so the verdict
	// can be checked again from the record alone (Recheck, `mlvm referee`).
	MemProof   string `json:"memProof"`
	FSProof    string `json:"fsProof,omitempty"`
	ProofBytes int    `json:"proofBytes"`
	Boundary   string `json:"boundary"`
	// EVM is set when the referee also ran RISCV.sol's compiled bytecode on the same input.
	EVM *EVMCheck `json:"evm,omitempty"`
}

// EVMCheck is RISCV.sol's own answer for an integer transition.
type EVMCheck struct {
	Post    common.Hash `json:"post"`
	GasUsed uint64      `json:"gasUsed"`
	Matches bool        `json:"matchesSlowStep"`
}

// Result is a finished dispute.
type Result struct {
	HonestSteps uint64      `json:"honestSteps"`
	LiarSteps   uint64      `json:"liarSteps"`
	Prestate    common.Hash `json:"prestate"`
	HonestPost  common.Hash `json:"honestPost"`
	LiarPost    common.Hash `json:"liarPost"`
	Deviation   *Deviation  `json:"deviation,omitempty"`
	Rounds      []Round     `json:"rounds"`
	Verdict     *Verdict    `json:"verdict,omitempty"`
	// NoDispute is set when the deviation left no trace in the final state, so there is
	// nothing to challenge (for example, a rounding change later absorbed).
	NoDispute bool `json:"noDispute,omitempty"`
}

// Namer names the function containing a program counter; xray.Symbolizer satisfies it
// through FunctionName.
type Namer func(pc uint64) string

// Play bisects between two parties that have both Run, then has the referee check the
// isolated instruction. onRound, if set, sees every question as it is answered.
func Play(honest, liar *Party, onRound func(Round), name Namer) (*Result, error) {
	pre0, err := honest.HashAt(0)
	if err != nil {
		return nil, err
	}
	pre1, err := liar.HashAt(0)
	if err != nil {
		return nil, err
	}
	if pre0 != pre1 {
		return nil, fmt.Errorf("parties start from different prestates: %s vs %s", pre0, pre1)
	}
	res := &Result{
		HonestSteps: honest.Steps(),
		LiarSteps:   liar.Steps(),
		Prestate:    pre0,
		HonestPost:  honest.FinalHash(),
		LiarPost:    liar.FinalHash(),
		Deviation:   liar.Deviation(),
	}
	if res.HonestPost == res.LiarPost {
		res.NoDispute = true
		return res, nil
	}

	// Invariant: the parties agree on the state after lo instructions and disagree after hi.
	lo, hi := uint64(0), max(honest.Steps(), liar.Steps())
	for n := 1; hi-lo > 1; n++ {
		mid := lo + (hi-lo)/2
		h, err := honest.HashAt(mid)
		if err != nil {
			return nil, err
		}
		l, err := liar.HashAt(mid)
		if err != nil {
			return nil, err
		}
		r := Round{N: n, Lo: lo, Hi: hi, Mid: mid, Honest: h, Liar: l, Agree: h == l}
		res.Rounds = append(res.Rounds, r)
		honest.Observe(r)
		liar.Observe(r)
		if onRound != nil {
			onRound(r)
		}
		if r.Agree {
			lo = mid
		} else {
			hi = mid
		}
	}

	t, err := honest.TransitionAt(lo)
	if err != nil {
		return nil, err
	}
	lt, err := liar.HashAt(lo + 1)
	if err != nil {
		return nil, err
	}
	agreed, err := liar.HashAt(lo)
	if err != nil {
		return nil, err
	}
	if agreed != t.Pre {
		return nil, fmt.Errorf("bisection ended on a step the parties do not agree on")
	}
	v, err := Referee(t)
	if err != nil {
		return nil, err
	}
	v.HonestPost, v.LiarPost = t.Post, lt
	if name != nil {
		v.Function = name(v.PC)
	}
	switch v.RefereePost {
	case v.HonestPost:
		v.Winner = honest.Name
	case v.LiarPost:
		v.Winner = liar.Name
	default:
		v.Winner = "neither"
	}
	res.Verdict = v
	return res, nil
}

// Witness field offsets, from fast.VMState.EncodeWitness (mlvm.state-witness/1).
const (
	witPC     = 72
	witExited = 81
	witStep   = 82
	witRegs   = 106
	witFRegs  = 362
	witFCSR   = 634
)

// Referee recomputes the state after one instruction from the defended transition's
// witness alone, without trusting either party's machine.
//
// Integer and memory instructions go through slow.Step, the Go mirror of RISCV.sol's
// step function, which checks every Merkle proof it consumes. Floating-point arithmetic is
// not implemented by slow.Step or RISCV.sol; for it the referee verifies the instruction
// word's Merkle proof against the witness memory root, re-executes that one instruction
// with MLVM's SoftFloat from the witness registers, and re-encodes the post-state witness.
func Referee(t *Transition) (*Verdict, error) {
	sw := fast.StateWitness(t.Witness.State)
	pre, err := sw.StateHash()
	if err != nil {
		return nil, err
	}
	if pre != t.Pre {
		return nil, fmt.Errorf("witness does not hash to the agreed prestate")
	}
	if sw[witExited] == 1 {
		return nil, fmt.Errorf("the agreed state has already halted; there is no instruction to check")
	}
	// Everything below comes from the witness and its proofs, never from the party's
	// description of the step: the program counter from the witness, and the instruction
	// word from the Merkle proof of the fetch, checked against the witness memory root.
	pc := binary.BigEndian.Uint64(sw[witPC:])
	word, err := provenInstruction(sw, t.Witness.MemProof, pc)
	if err != nil {
		return nil, err
	}
	if t.PC != pc || t.Word != word {
		return nil, fmt.Errorf("transition claims pc 0x%x word 0x%08x, but the witness proves pc 0x%x word 0x%08x", t.PC, t.Word, pc, word)
	}
	inst := rvdis.Decode(word, pc)
	v := &Verdict{
		Step:         t.Step,
		PC:           pc,
		Word:         fmt.Sprintf("0x%08x", word),
		Instruction:  inst.Text(),
		Class:        string(inst.Class),
		Agreed:       pre,
		StateWitness: hexutil.Encode(sw),
		MemProof:     hexutil.Encode(t.Witness.MemProof),
		ProofBytes:   len(t.Witness.ProofData()),
	}
	if len(t.Witness.FileSystemProof) > 0 {
		v.FSProof = hexutil.Encode(t.Witness.FileSystemProof)
	}
	if !isFloatOp(word) {
		input := t.Witness.EncodeStepInput(fast.LocalContext{})
		post, err := slow.Step(input, nil)
		if err != nil {
			return nil, fmt.Errorf("slow.Step rejected the transition: %w", err)
		}
		v.RefereePost = common.Hash(post)
		v.Method = "slow.Step, the Go mirror of RISCV.sol's step function, from the state witness and Merkle proofs"
		v.Boundary = "Nothing is deployed and there are no bonds, clocks or challenge period: this is the shape of an optimistic dispute, run locally."
		if evmstep.Available {
			evmPost, gas, err := evmstep.Step(input)
			if err != nil {
				return nil, err
			}
			v.EVM = &EVMCheck{Post: evmPost, GasUsed: gas, Matches: evmPost == v.RefereePost}
			v.Method = "RISCV.sol's compiled step function in an in-process EVM, cross-checked by slow.Step, from the state witness and Merkle proofs"
		}
		return v, nil
	}

	post, err := refereeFloatStep(sw, pc, word)
	if err != nil {
		return nil, err
	}
	v.RefereePost = post
	v.Method = "SoftFloat re-execution of this one instruction from the 674-byte state witness, after checking the instruction word's Merkle proof against the witness memory root"
	v.Boundary = "Not on-chain settlement: RISCV.sol does not implement floating-point arithmetic, and MLVM's experimental nested float sub-game covers only f64 in round-to-nearest-even mode."
	return v, nil
}

// isFloatOp reports whether a word is in the OP-FP or fused multiply-add opcode space:
// instructions that read and write only registers and fcsr.
func isFloatOp(word uint32) bool {
	switch word & 0x7f {
	case 0x53, 0x43, 0x47, 0x4b, 0x4f:
		return true
	}
	return false
}

// provenInstruction checks the fetch proof (the first memory proof of every step)
// against the witness memory root, the way slow.Step's getMemoryB32 does, and returns the
// instruction word at pc.
func provenInstruction(sw fast.StateWitness, memProof []byte, pc uint64) (uint32, error) {
	const leafProofSize = 60 * 32
	if len(memProof) < leafProofSize {
		return 0, fmt.Errorf("missing instruction proof")
	}
	if pc&3 != 0 {
		return 0, fmt.Errorf("pc 0x%x is not 4-byte aligned", pc)
	}
	leaf := memProof[:32]
	node := common.BytesToHash(leaf)
	path := pc >> 5
	for i := 0; i < 64-5; i++ {
		sibling := memProof[32*(i+1) : 32*(i+2)]
		if (path>>i)&1 == 0 {
			node = crypto.Keccak256Hash(node[:], sibling)
		} else {
			node = crypto.Keccak256Hash(sibling, node[:])
		}
	}
	if node != common.BytesToHash(sw[:32]) {
		return 0, fmt.Errorf("instruction proof does not match the witness memory root")
	}
	off := pc & 31
	return binary.LittleEndian.Uint32(leaf[off : off+4]), nil
}

func refereeFloatStep(sw fast.StateWitness, pc uint64, word uint32) (common.Hash, error) {
	if !isFloatOp(word) {
		return common.Hash{}, fmt.Errorf("0x%08x is not a floating-point register instruction", word)
	}
	// A machine that holds only what an f32/f64 register operation can read: the
	// instruction word, the registers and fcsr.
	m := fast.NewVMState()
	m.PC = pc
	m.Step = binary.BigEndian.Uint64(sw[witStep:])
	for i := 0; i < 32; i++ {
		m.Registers[i] = binary.BigEndian.Uint64(sw[witRegs+8*i:])
		m.FRegisters[i] = binary.BigEndian.Uint64(sw[witFRegs+8*i:])
	}
	m.FCSR = binary.BigEndian.Uint64(sw[witFCSR:])
	var w [4]byte
	binary.LittleEndian.PutUint32(w[:], word)
	m.Memory.SetUnaligned(pc, w[:])
	inst := fast.NewInstrumentedState(m, nil, io.Discard, io.Discard)
	if _, err := inst.Step(false); err != nil {
		return common.Hash{}, fmt.Errorf("re-executing 0x%08x: %w", word, err)
	}
	if m.PC != pc+4 || m.Exited {
		return common.Hash{}, fmt.Errorf("0x%08x is not a register-only float instruction", word)
	}

	post := append(fast.StateWitness(nil), sw...)
	binary.BigEndian.PutUint64(post[witPC:], m.PC)
	binary.BigEndian.PutUint64(post[witStep:], m.Step)
	for i := 0; i < 32; i++ {
		binary.BigEndian.PutUint64(post[witRegs+8*i:], m.Registers[i])
		binary.BigEndian.PutUint64(post[witFRegs+8*i:], m.FRegisters[i])
	}
	binary.BigEndian.PutUint64(post[witFCSR:], m.FCSR)
	return post.StateHash()
}
