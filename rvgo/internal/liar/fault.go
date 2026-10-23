package liar

import (
	"fmt"
	"math"

	"github.com/yao-jz/mlvm/rvgo/fast"
	"github.com/yao-jz/mlvm/rvgo/internal/rvdis"
)

// FaultKind names a way the dishonest party deviates.
type FaultKind string

const (
	// FaultULP flips the lowest mantissa bit of one floating-point arithmetic result:
	// the smallest possible lie about a float, one unit in the last place.
	FaultULP FaultKind = "ulp"
	// FaultInt flips the lowest bit of one integer ALU result: the integer counterpart of
	// FaultULP, for code where arithmetic is integer instructions, such as a soft-float
	// build. Integer verdicts are RISCV.sol's to give.
	FaultInt FaultKind = "int"
	// FaultLoad flips the lowest bit of the value one floating-point load (flw, fld)
	// brings into a register: the machine misreads one weight or activation from memory.
	FaultLoad FaultKind = "load"
	// FaultToken forges one token of the answer the guest commits at MLVM_OUT, by
	// replacing the bytes of an integer store.
	FaultToken FaultKind = "token"
	// FaultOutput forges bytes the guest prints to stdout: right before the write(2) that
	// prints them, the liar's machine changes them in the guest's buffer.
	FaultOutput FaultKind = "output"
	// FaultCommit forges bytes of the answer any guest commits at MLVM_OUT (see
	// fast.MLResult and examples/mlvm_result.h): every store into those bytes leaves the
	// forged value behind. FaultToken is the decoder's token-level form of the same lie.
	FaultCommit FaultKind = "commit"
)

// lockstep reports whether a fault is tried in lockstep with an honest shadow (see
// Party.tryLie), because rounding or later code can erase it.
func (k FaultKind) lockstep() bool { return k == FaultULP || k == FaultLoad || k == FaultInt }

// DefaultLieWindow is how long a one-ULP lie must survive in lockstep with an honest
// shadow before the liar keeps it. It is a little longer than one TinyStories forward
// pass (about 2.04M instructions), so a difference that is still there has reached state
// that later passes keep, such as the KV cache, rather than a scratch buffer.
const DefaultLieWindow = 2_500_000

// Fault configures the dishonest party.
type Fault struct {
	Kind FaultKind
	// AtStep: the lie targets the first matching instruction at or after this step.
	AtStep uint64
	// Mnemonic optionally restricts FaultULP and FaultLoad to one instruction, such as
	// "fadd.s", "fcvt.d.s" or "fld". Without it, Precision chooses the candidates.
	Mnemonic string
	// Precision is the result format a FaultULP or FaultLoad lie targets without a
	// Mnemonic: "s" (f32, the default when empty), "d" (f64) or "any".
	Precision string
	// InFunction, if set, restricts FaultULP, FaultLoad and FaultInt to instructions it
	// accepts by program counter, for example those of one function; Function names it.
	InFunction func(pc uint64) bool
	Function   string
	// Window overrides DefaultLieWindow for FaultULP.
	Window uint64
	// StepLimit, if set, makes RunUntilDisputed give up on a run that has not halted after
	// this many instructions, as it does on a run that faults: a kept integer lie can send
	// the program into an endless loop.
	StepLimit uint64
	// TokenIndex and Token: FaultToken replaces generated token TokenIndex with Token.
	TokenIndex int
	Token      uint16
	// OutputOffset and Output: FaultOutput prints Output in place of the bytes at
	// OutputOffset of the guest's stdout; FaultCommit commits Output in place of the bytes
	// at OutputOffset of the committed payload.
	OutputOffset uint64
	Output       []byte
}

func (f Fault) precision() string {
	if f.Precision == "" {
		return "s"
	}
	return f.Precision
}

func (f Fault) target(what string) string {
	if f.Mnemonic != "" {
		return f.Mnemonic
	}
	switch f.precision() {
	case "s":
		return "f32 " + what
	case "d":
		return "f64 " + what
	}
	return "floating-point " + what
}

func (f Fault) String() string {
	in := ""
	if f.Function != "" {
		in = " in " + f.Function
	}
	switch f.Kind {
	case FaultULP:
		return fmt.Sprintf("1 ULP on a %s%s at or after step %d", f.target("arithmetic instruction"), in, f.AtStep)
	case FaultLoad:
		return fmt.Sprintf("flip the lowest bit of a %s%s at or after step %d", f.target("load"), in, f.AtStep)
	case FaultInt:
		what := "an integer result"
		if f.Mnemonic != "" {
			what = "the result of a " + f.Mnemonic
		}
		return fmt.Sprintf("flip the lowest bit of %s%s at or after step %d", what, in, f.AtStep)
	case FaultToken:
		return fmt.Sprintf("forge token %d as id %d", f.TokenIndex, f.Token)
	case FaultOutput:
		return fmt.Sprintf("print %q at byte %d of stdout", f.Output, f.OutputOffset)
	case FaultCommit:
		return fmt.Sprintf("commit %q at byte %d of the answer at MLVM_OUT", f.Output, f.OutputOffset)
	}
	return string(f.Kind)
}

// Deviation records exactly what the dishonest party changed.
type Deviation struct {
	Kind        FaultKind `json:"kind"`
	Step        uint64    `json:"step"` // the instruction whose effect was altered
	PC          uint64    `json:"pc"`
	Instruction string    `json:"instruction"`
	Location    string    `json:"location"` // register or memory address that differs
	Honest      string    `json:"honest"`   // what the instruction actually produced
	Forged      string    `json:"forged"`   // what the dishonest machine kept instead
}

// Attempt is one lie the dishonest party tried. Most one-ULP lies are absorbed by later
// rounding: the two machines become bit-identical again and the lie leaves no trace.
type Attempt struct {
	Step        uint64 `json:"step"`
	PC          uint64 `json:"pc"`
	Instruction string `json:"instruction"`
	// ErasedAfter is how many instructions later the machines were identical again.
	ErasedAfter uint64 `json:"erasedAfter,omitempty"`
	// Faulted: the lying copy hit an error, during the lockstep window or (for a kept lie)
	// later in the run, so the lie was abandoned.
	Faulted bool `json:"faulted,omitempty"`
	// Kept: the lie outlived the window. Vanished: it was kept, but the final state
	// matched the honest one anyway, so the liar tried again (see RunUntilDisputed).
	Kept     bool `json:"kept,omitempty"`
	Vanished bool `json:"vanished,omitempty"`
}

type action struct {
	step      uint64
	deviation *Deviation
	mutate    func(*fast.VMState)
}

type plan struct {
	fault    Fault
	actions  []action
	attempts []Attempt
	seen     int    // token bytes examined (FaultToken)
	printed  uint64 // bytes of stdout written so far (FaultOutput)
}

func (pl *plan) done() bool {
	switch pl.fault.Kind {
	case FaultToken:
		return pl.seen >= 2
	case FaultOutput:
		return pl.printed >= pl.fault.OutputOffset+uint64(len(pl.fault.Output))
	case FaultCommit:
		return false // a later store could rewrite the forged bytes
	default:
		return len(pl.actions) > 0
	}
}

func f32Text(bits uint32) string {
	return fmt.Sprintf("%.9g (0x%08x)", math.Float32frombits(bits), bits)
}

func f64Text(bits uint64) string {
	return fmt.Sprintf("%.17g (0x%016x)", math.Float64frombits(bits), bits)
}

// candidate returns a lie about the instruction about to execute (one ULP on its result,
// or one bit of the value it loads), if that instruction is one the fault targets.
func (pl *plan) candidate(state *fast.VMState) *action {
	if state.Step < pl.fault.AtStep {
		return nil
	}
	word := state.Instr()
	op := word & 0x7f
	var precision, location string // the destination format, and what the lie changes
	var inst rvdis.Inst
	switch pl.fault.Kind {
	case FaultULP:
		if op != 0x53 && op != 0x43 && op != 0x47 && op != 0x4b && op != 0x4f {
			return nil
		}
		inst = rvdis.Decode(word, state.PC)
		if inst.Class != rvdis.ClassFloatArith || !inst.FloatRd {
			return nil
		}
		precision = inst.Precision
		if m := inst.Mnemonic; m == "fmin."+precision || m == "fmax."+precision {
			return nil // these select an operand; they do not round
		}
		location = rvdis.FReg(inst.Rd)
	case FaultLoad:
		funct3 := (word >> 12) & 7
		if op != 0x07 || (funct3 != 2 && funct3 != 3) {
			return nil
		}
		inst = rvdis.Decode(word, state.PC)
		precision = "s"
		if funct3 == 3 {
			precision = "d"
		}
		imm := int64(int32(word) >> 20) // I-type immediate
		addr := state.Registers[(word>>15)&31] + uint64(imm)
		location = fmt.Sprintf("%s, loaded from 0x%x", rvdis.FReg(inst.Rd), addr)
	case FaultInt:
		if op != 0x13 && op != 0x33 && op != 0x1b && op != 0x3b { // OP-IMM, OP, OP-IMM-32, OP-32
			return nil
		}
		inst = rvdis.Decode(word, state.PC)
		// Not ra, sp, gp or tp: a lie about a pointer register says little about the
		// computation and usually just crashes the program.
		if inst.Rd <= 4 || inst.FloatRd || (inst.Class != rvdis.ClassIntALU && inst.Class != rvdis.ClassMulDiv) {
			return nil
		}
		location = rvdis.XReg(inst.Rd)
	default:
		return nil
	}
	if pl.fault.Mnemonic != "" {
		if inst.Mnemonic != pl.fault.Mnemonic {
			return nil
		}
	} else if want := pl.fault.precision(); pl.fault.Kind != FaultInt && want != "any" && precision != want {
		return nil
	}
	if pl.fault.InFunction != nil && !pl.fault.InFunction(state.PC) {
		return nil
	}
	rd := inst.Rd
	d := &Deviation{
		Kind:        pl.fault.Kind,
		Step:        state.Step,
		PC:          state.PC,
		Instruction: inst.Text(),
		Location:    location,
	}
	return &action{
		step:      state.Step,
		deviation: d,
		mutate: func(s *fast.VMState) {
			if pl.fault.Kind == FaultInt {
				honest := s.Registers[rd]
				s.Registers[rd] ^= 1
				if d.Honest == "" {
					d.Honest, d.Forged = fmt.Sprintf("0x%x", honest), fmt.Sprintf("0x%x", s.Registers[rd])
				}
				return
			}
			honest := s.FRegisters[rd]
			s.FRegisters[rd] ^= 1 // the lowest mantissa bit; f32 NaN-boxing is untouched
			if d.Honest == "" {
				if precision == "s" {
					d.Honest, d.Forged = f32Text(uint32(honest)), f32Text(uint32(s.FRegisters[rd]))
				} else {
					d.Honest, d.Forged = f64Text(honest), f64Text(s.FRegisters[rd])
				}
			}
		},
	}
}

// inspect handles FaultToken: it watches byte stores into the committed answer and
// schedules a forged byte right after the store of the targeted token.
func (pl *plan) inspect(state *fast.VMState) {
	if state.Step < pl.fault.AtStep {
		return
	}
	word := state.Instr()
	if word&0x7f != 0x23 || (word>>12)&7 != 0 { // sb
		return
	}
	rs1, rs2 := (word>>15)&31, (word>>20)&31
	imm := int64(int32((word>>25)<<5|(word>>7)&31)<<20) >> 20
	addr := state.Registers[rs1] + uint64(imm)
	base := fast.MLResultBase + 16 + 4 + 2*uint64(pl.fault.TokenIndex)
	if addr != base && addr != base+1 {
		return
	}
	pl.seen++
	shift := 8 * (addr - base)
	honest := byte(state.Registers[rs2])
	forged := byte(pl.fault.Token >> shift)
	if honest == forged {
		return
	}
	inst := rvdis.Decode(word, state.PC)
	pl.actions = append(pl.actions, action{
		step: state.Step,
		deviation: &Deviation{
			Kind:        FaultToken,
			Step:        state.Step,
			PC:          state.PC,
			Instruction: inst.Text(),
			Location:    fmt.Sprintf("MLVM_OUT+0x%x (token %d, byte %d)", addr-fast.MLResultBase, pl.fault.TokenIndex, shift/8),
			Honest:      fmt.Sprintf("0x%02x", honest),
			Forged:      fmt.Sprintf("0x%02x", forged),
		},
		mutate: func(s *fast.VMState) {
			s.Memory.SetUnaligned(addr, []byte{forged})
		},
	})
}

// inspectCommit handles FaultCommit. Before each store it checks whether the store will
// write any of the targeted payload bytes, and if it will, schedules those bytes to hold
// the forged values right after it. Every such store is forged, because a guest may write
// a byte more than once and only the last write is committed; the first store whose
// effect actually changes is the deviation bisection finds.
func (pl *plan) inspectCommit(state *fast.VMState) {
	word := state.Instr()
	op := word & 0x7f
	if op != 0x23 && op != 0x27 { // STORE, STORE-FP
		return
	}
	funct3 := (word >> 12) & 7
	if funct3 > 3 {
		return
	}
	size := uint64(1) << funct3 // sb sh sw sd, fsw fsd
	addr, _ := storeAddress(word, state)
	lo := fast.MLResultBase + 16 + pl.fault.OutputOffset
	hi := lo + uint64(len(pl.fault.Output))
	if addr+size <= lo || addr >= hi {
		return
	}
	from, to := max(addr, lo), min(addr+size, hi)
	value := state.Registers[(word>>20)&31]
	if op == 0x27 {
		value = state.FRegisters[(word>>20)&31]
	}
	honest := make([]byte, to-from)
	for i := range honest {
		honest[i] = byte(value >> (8 * (from - addr + uint64(i))))
	}
	forged := pl.fault.Output[from-lo : to-lo]
	if string(honest) == string(forged) {
		return
	}
	inst := rvdis.Decode(word, state.PC)
	pl.actions = append(pl.actions, action{
		step: state.Step,
		deviation: &Deviation{
			Kind:        FaultCommit,
			Step:        state.Step,
			PC:          state.PC,
			Instruction: inst.Text(),
			Location:    fmt.Sprintf("MLVM_OUT+0x%x, bytes %d..%d of the committed answer", from-fast.MLResultBase, from-lo+pl.fault.OutputOffset, to-lo+pl.fault.OutputOffset-1),
			Honest:      fmt.Sprintf("%q", honest),
			Forged:      fmt.Sprintf("%q", forged),
		},
		mutate: func(s *fast.VMState) {
			for i, b := range forged {
				s.Memory.SetUnaligned(from+uint64(i), []byte{b})
			}
		},
	})
}

// inspectOutput handles FaultOutput. It runs after instruction at executed and before
// the next one: if the next instruction is a write(2) to stdout that prints any of the
// bytes to forge, it changes them in the guest's buffer now, and records the change as the
// effect of instruction at (prevPC, prevWord), the one the liar misstates.
func (pl *plan) inspectOutput(at, prevPC uint64, prevWord uint32, state *fast.VMState) {
	const ecall, sysWrite, stdout = 0x00000073, 64, 1
	if state.Exited || state.Instr() != ecall || state.Registers[17] != sysWrite || state.Registers[10] != stdout {
		return
	}
	buf, count := state.Registers[11], state.Registers[12]
	start := pl.printed
	pl.printed += count
	from, to := pl.fault.OutputOffset, pl.fault.OutputOffset+uint64(len(pl.fault.Output))
	if to <= start || from >= pl.printed {
		return
	}
	lo, hi := max(from, start), min(to, pl.printed)
	addr := buf + (lo - start)
	forged := pl.fault.Output[lo-from : hi-from]
	honest := make([]byte, len(forged))
	for i := range honest {
		state.Memory.GetUnaligned(addr+uint64(i), honest[i:i+1])
	}
	if string(honest) == string(forged) {
		return
	}
	inst := rvdis.Decode(prevWord, prevPC)
	act := action{
		step: at,
		deviation: &Deviation{
			Kind:        FaultOutput,
			Step:        at,
			PC:          prevPC,
			Instruction: inst.Text(),
			Location:    fmt.Sprintf("0x%x, the buffer of the next write(2) to stdout", addr),
			Honest:      fmt.Sprintf("%q", honest),
			Forged:      fmt.Sprintf("%q", forged),
		},
		mutate: func(s *fast.VMState) {
			for i, b := range forged {
				s.Memory.SetUnaligned(addr+uint64(i), []byte{b})
			}
		},
	}
	act.mutate(state)
	pl.actions = append(pl.actions, act)
}

// apply runs the deviation scheduled for step at, after that instruction executed.
func (pl *plan) apply(at uint64, state *fast.VMState) {
	for i := range pl.actions {
		if pl.actions[i].step == at {
			pl.actions[i].mutate(state)
		}
	}
}

// storeAddress returns the address an instruction writes to, if it is a store.
func storeAddress(word uint32, s *fast.VMState) (uint64, bool) {
	switch word & 0x7f {
	case 0x23, 0x27: // STORE, STORE-FP
		imm := int64(int32((word>>25)<<5|(word>>7)&31)<<20) >> 20
		return s.Registers[(word>>15)&31] + uint64(imm), true
	case 0x2f: // AMO, SC
		return s.Registers[(word>>15)&31], true
	}
	return 0, false
}

// trackStore updates the set of 8-byte memory words that differ between two machines
// after both executed the instruction word, whose store addresses were computed before
// the step in each machine.
func trackStore(addrs []uint64, a, b *fast.VMState, dirty map[uint64]bool) {
	for _, addr := range addrs {
		for _, w := range []uint64{addr &^ 7, (addr + 7) &^ 7} {
			var x, y [8]byte
			a.Memory.GetUnaligned(w, x[:])
			b.Memory.GetUnaligned(w, y[:])
			if x == y {
				delete(dirty, w)
			} else {
				dirty[w] = true
			}
		}
	}
}

// converged reports whether two machines that ran the same instructions are identical:
// same registers and control state, and no memory word left that differs.
func converged(a, b *fast.VMState, dirty map[uint64]bool) bool {
	return len(dirty) == 0 &&
		a.PC == b.PC &&
		a.Registers == b.Registers &&
		a.FRegisters == b.FRegisters &&
		a.FCSR == b.FCSR &&
		a.LoadReservation == b.LoadReservation &&
		a.Heap == b.Heap &&
		a.Exited == b.Exited
}
