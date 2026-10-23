package fast

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"fmt"
	"io"
	"os"

	. "github.com/yao-jz/mlvm/rvgo/comp"
	. "github.com/yao-jz/mlvm/rvgo/include"
)

// Experimental nested f64 terminal component.
//
// RISCV.sol executes RV64IMA. It cannot execute FADD.D, and giving it the ability would
// mean writing IEEE-754 in Yul: thousands of lines of consensus-critical bit manipulation,
// and an audit. This is the alternative.
//
// A float operation compiled for RV64IMA - where the compiler has no float instructions to
// emit - becomes soft-float from compiler_builtins. Every instruction of it is one RISCV.sol
// already executes. The component derives a nested pre-state from supplied operands and can
// read a result from a supplied post-state proof. Tests cross-check the integer trace through
// the EVM verifier. Trace lengths are operation-dependent and run into the hundreds of steps.
//
// This package does not bind the operands to an outer VM witness, prove the nested post-state
// through an interactive game, apply the result and flags to the outer state, or implement
// trace commitments, bonds, and timeouts. It is a terminal-route building block, not a full
// fault-proof protocol.
//
// # Properties exercised by the component
//
// Two things, and both are load-bearing:
//
//  1. **The nested pre-state is a pure function of (image, op, a, b).** No arguments, no
//     environment, no stack-passed data, no randomness, no clock. Both parties derive the
//     same starting state or neither does. That is why the co-processor reads its operands
//     from fixed absolute addresses rather than from argv.
//
//  2. **compiler_builtins' soft-float agrees with rvgo/comp bit-for-bit.** Established by
//     scripts/float-placement.sh on a GEMM kernel and by TestFloatSubGameMatchesSoftFloat
//     on the same 1,522 differential vectors the Yul port is checked against. Without that,
//     the nested route would reproduce a different computation.

// Fixed I/O addresses in the nested machine. Protocol constants: they must match the
// `IO_BASE` block in examples/rust/float-coprocessor/src/main.rs exactly.
const (
	// FloatIOBase is where the nested machine's operands live. Exported because it is a
	// protocol constant: FloatSubGame.sol hardcodes the same value, and the merkle branch
	// for this address is what the contract verifies against the committed image.
	//
	// The split at +32 is deliberate. MLVM's merkle leaves are 32 bytes, so the four inputs
	// occupy one leaf and the output occupies the next - and because FloatIOBase is 64-byte
	// aligned, those two leaves are siblings. One merkle branch therefore carries both: the
	// contract writes the inputs into one leaf and reads the result out of the other, using
	// the same 60 proof nodes.
	FloatIOBase    = 0x3000_0000_0000_0000
	floatAddrOp    = FloatIOBase
	floatAddrA     = FloatIOBase + 8
	floatAddrB     = FloatIOBase + 16
	floatAddrC     = FloatIOBase + 24
	floatAddrOut   = FloatIOBase + 32
	floatAddrFlags = FloatIOBase + 40
)

// Operation selectors. Must match the OP_* block in the co-processor exactly.
//
// The two-operand arithmetic keeps the RISC-V funct5 values so the outer VM's decode passes
// straight through; the rest are MLVM's own, because RISC-V distinguishes them by opcode and
// by the rs2 field rather than by funct5 alone. FMIN and FMAX share funct5 0x05 and are told
// apart by funct3, so FMAX is folded to 0x15.
const (
	FloatOpAdd  uint64 = 0x00
	FloatOpSub  uint64 = 0x01
	FloatOpMul  uint64 = 0x02
	FloatOpDiv  uint64 = 0x03
	FloatOpMin  uint64 = 0x05
	FloatOpSqrt uint64 = 0x0B
	FloatOpMax  uint64 = 0x15

	// The multiply-add family, RISC-V opcodes 0x43/0x47/0x4B/0x4F. Three operands.
	FloatOpMAdd  uint64 = 0x20
	FloatOpMSub  uint64 = 0x21
	FloatOpNMSub uint64 = 0x22
	FloatOpNMAdd uint64 = 0x23

	// Integer to double.
	FloatOpCvtDW  uint64 = 0x30
	FloatOpCvtDWU uint64 = 0x31
	FloatOpCvtDL  uint64 = 0x32
	FloatOpCvtDLU uint64 = 0x33

	// Double to integer.
	FloatOpCvtWD  uint64 = 0x38
	FloatOpCvtWUD uint64 = 0x39
	FloatOpCvtLD  uint64 = 0x3A
	FloatOpCvtLUD uint64 = 0x3B

	// Comparisons. These write an integer register, so the result is 0 or 1.
	FloatOpEq uint64 = 0x40
	FloatOpLt uint64 = 0x41
	FloatOpLe uint64 = 0x42
)

// FloatOpName is for diagnostics and test output.
func FloatOpName(op uint64) string {
	switch op {
	case FloatOpAdd:
		return "FADD.D"
	case FloatOpSub:
		return "FSUB.D"
	case FloatOpMul:
		return "FMUL.D"
	case FloatOpDiv:
		return "FDIV.D"
	case FloatOpMin:
		return "FMIN.D"
	case FloatOpMax:
		return "FMAX.D"
	case FloatOpSqrt:
		return "FSQRT.D"
	case FloatOpMAdd:
		return "FMADD.D"
	case FloatOpMSub:
		return "FMSUB.D"
	case FloatOpNMSub:
		return "FNMSUB.D"
	case FloatOpNMAdd:
		return "FNMADD.D"
	case FloatOpCvtDW:
		return "FCVT.D.W"
	case FloatOpCvtDWU:
		return "FCVT.D.WU"
	case FloatOpCvtDL:
		return "FCVT.D.L"
	case FloatOpCvtDLU:
		return "FCVT.D.LU"
	case FloatOpCvtWD:
		return "FCVT.W.D"
	case FloatOpCvtWUD:
		return "FCVT.WU.D"
	case FloatOpCvtLD:
		return "FCVT.L.D"
	case FloatOpCvtLUD:
		return "FCVT.LU.D"
	case FloatOpEq:
		return "FEQ.D"
	case FloatOpLt:
		return "FLT.D"
	case FloatOpLe:
		return "FLE.D"
	}
	return fmt.Sprintf("op(%#x)", op)
}

// FloatCoprocessor is the committed program image the sub-game runs.
//
// Its memory root is a protocol constant: both parties, and the contract, must agree on
// which binary is being executed, or they are disputing different computations.
type FloatCoprocessor struct {
	entry uint64
	image *Memory
	root  [32]byte
}

// LoadFloatCoprocessor reads the co-processor ELF and pre-computes its committed image.
//
// The ELF must be built for RV64IMA - no F, no D. A build that contains float instructions
// would defeat the entire purpose, so that is checked rather than assumed.
func LoadFloatCoprocessor(path string) (*FloatCoprocessor, error) {
	f, err := elf.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open float co-processor %q: %w", path, err)
	}
	defer f.Close()

	if f.Machine != elf.EM_RISCV {
		return nil, fmt.Errorf("float co-processor is not RISC-V, but %s", f.Machine)
	}

	state, err := LoadELF(f)
	if err != nil {
		return nil, fmt.Errorf("failed to load float co-processor: %w", err)
	}
	if err := assertNoFloatInstructions(f); err != nil {
		return nil, err
	}

	return &FloatCoprocessor{
		entry: state.PC,
		image: state.Memory,
		root:  state.Memory.MerkleRoot(),
	}, nil
}

// ImageRoot is the commitment to the co-processor program, before any operands are written.
func (c *FloatCoprocessor) ImageRoot() [32]byte { return c.root }

// Entry is the co-processor's entry point.
func (c *FloatCoprocessor) Entry() uint64 { return c.entry }

// ImageProof is the merkle branch for the I/O leaf against the committed image root.
//
// The contract needs it to derive the nested pre-state: writing the operands is a single
// branch update, because op, a, b and the result all live in bytes [0, 32) of one page,
// which is exactly one leaf. That is not luck - FloatIOBase was chosen for it.
func (c *FloatCoprocessor) ImageProof() [ProofLen * 32]byte {
	return c.image.MerkleProof(FloatIOBase)
}

// assertNoFloatInstructions scans the co-processor's code and rejects the image if it
// contains an instruction the on-chain verifier cannot execute.
//
// This is the check that makes the whole approach honest. If the co-processor were built for
// a target with F/D, its arithmetic would be one FADD.D again and the nesting would have
// bought nothing - but it would still run correctly off chain, so nothing else would notice.
//
// It scans executable *sections*, not the executable segment. The distinction matters: the
// linker merges .text and .rodata into a single PT_LOAD, so a segment-level scan reads
// constant tables as if they were instructions. That is not hypothetical - adding libm for
// sqrt and rint brought in tables whose bytes happen to have the low bits of a float opcode,
// and a segment scan rejected a perfectly good rv64ima image because of them.
//
// The remaining imprecision is that a compiler may place a constant pool inside .text. LLVM
// does not do so for this target, but a future toolchain could, and the honest failure mode
// then is a false rejection rather than a false acceptance - which is the right way round.
func assertNoFloatInstructions(f *elf.File) error {
	executable := 0
	for _, sec := range f.Sections {
		if sec.Type != elf.SHT_PROGBITS || sec.Flags&elf.SHF_EXECINSTR == 0 {
			continue
		}
		executable++
		code, err := sec.Data()
		if err != nil {
			return fmt.Errorf("failed to read section %q: %w", sec.Name, err)
		}
		for off := 0; off+4 <= len(code); off += 4 {
			switch binary.LittleEndian.Uint32(code[off:]) & 0x7F {
			case 0x07, 0x27, 0x43, 0x47, 0x4B, 0x4F, 0x53:
				return fmt.Errorf(
					"float co-processor contains a floating-point instruction in %s at offset %#x; "+
						"it must be built for rv64ima so every instruction is one RISCV.sol can execute",
					sec.Name, off)
			}
		}
	}
	// A stripped image cannot be checked, and an unchecked image is exactly the failure this
	// function exists to prevent. Fail rather than wave it through.
	if executable == 0 {
		return fmt.Errorf("float co-processor has no executable sections to verify; " +
			"it must not be stripped, or the no-floating-point guarantee cannot be checked")
	}
	return nil
}

// NewFloatSubGame derives the nested machine's initial state from an operation and its
// operands.
//
// Deterministic by construction: the co-processor image is fixed, the three input words go
// to fixed addresses, and nothing else differs between two derivations. Both disputants
// compute the same state or the sub-game does not start.
func (c *FloatCoprocessor) NewFloatSubGame(op, a, b, cc uint64) *VMState {
	state := &VMState{
		SchemaVersion: StateSchemaVersion,
		PC:            c.entry,
		Memory:        NewMemory(),
		Registers:     [32]uint64{},
		FRegisters:    [32]uint64{},
		FileSystem:    NewFileSystem(),
		// The nested machine has no heap, no files and no randomness. Leaving these zero
		// is not an oversight: an mmap, an openat or a getrandom inside the co-processor
		// would be a bug, and zeroing the cursors makes such a bug fail loudly.
	}

	// Copy the committed image page by page rather than re-parsing the ELF, so a sub-game
	// derived here is bit-identical to the committed root.
	if err := c.image.ForEachPage(func(pageIndex uint64, page *Page) error {
		return state.Memory.SetMemoryRange(pageIndex<<PageAddrSize, bytes.NewReader(page[:]))
	}); err != nil {
		panic(fmt.Errorf("failed to copy float co-processor image: %w", err))
	}

	// The co-processor uses no stack, but _start still needs a valid stack pointer for the
	// compiler's prologue. A fixed value keeps the derivation canonical.
	state.Registers[2] = 0x1000_0000_0000_0000

	var word [8]byte
	put := func(addr, v uint64) {
		binary.LittleEndian.PutUint64(word[:], v)
		if err := state.Memory.SetMemoryRange(addr, bytes.NewReader(word[:])); err != nil {
			panic(fmt.Errorf("failed to write sub-game input: %w", err))
		}
	}
	put(floatAddrOp, op)
	put(floatAddrA, a)
	put(floatAddrB, b)
	put(floatAddrC, cc)

	return state
}

// FloatSubGameResult reads the operation's result out of a completed nested machine.
func FloatSubGameResult(state *VMState) uint64 {
	var out [8]byte
	state.Memory.GetUnaligned(floatAddrOut, out[:])
	return binary.LittleEndian.Uint64(out[:])
}

// FloatSubGameFlags reads the IEEE-754 exceptions the nested machine determined the
// operation raises.
//
// This is the other half of what a float instruction does. Without it the nested route
// would cover the destination register but omit the fcsr delta, which is also committed
// state.
//
// It costs nothing to carry: the result leaf has 32 bytes and the result uses 8, so the
// flags sit in the same leaf, under the same merkle branch, already committed.
func FloatSubGameFlags(state *VMState) uint64 {
	var out [8]byte
	state.Memory.GetUnaligned(floatAddrFlags, out[:])
	return binary.LittleEndian.Uint64(out[:])
}

// RunFloatSubGame executes the nested machine to completion and returns the result with the
// number of steps it took.
//
// This is the off-chain path - what an honest party runs to know the answer. The on-chain
// path never runs this; it bisects the same trace and executes one step of it.
func (c *FloatCoprocessor) RunFloatSubGame(op, a, b, cc uint64) (result uint64, flags uint64, steps uint64, err error) {
	state, steps, err := c.ExecuteFloatSubGame(op, a, b, cc)
	if err != nil {
		return 0, 0, steps, err
	}
	return FloatSubGameResult(state), FloatSubGameFlags(state), steps, nil
}

// ExecuteFloatSubGame runs the nested machine to completion and returns its final state.
//
// The state is what the contract boundary consumes: its witness carries the memory root the
// result is read from, and its status byte is how the outer game learns the sub-game halted
// cleanly rather than faulting.
func (c *FloatCoprocessor) ExecuteFloatSubGame(op, a, b, cc uint64) (state *VMState, steps uint64, err error) {
	state = c.NewFloatSubGame(op, a, b, cc)
	in := NewInstrumentedState(state, nil, io.Discard, io.Discard)
	const maxSteps = 100_000 // a float operation is 115-178 steps; this is a runaway guard
	for steps = 0; steps < maxSteps; steps++ {
		if _, err = in.Step(false); err != nil {
			return nil, steps, fmt.Errorf("float sub-game failed at step %d, PC %#x: %w", steps, state.PC, err)
		}
		if state.Exited {
			if state.ExitCode != 0 {
				return nil, steps, fmt.Errorf("float sub-game exited with code %d", state.ExitCode)
			}
			return state, steps + 1, nil
		}
	}
	return nil, steps, fmt.Errorf("float sub-game did not terminate within %d steps", maxSteps)
}

// DefaultFloatCoprocessorPath is where the build script puts the co-processor.
const DefaultFloatCoprocessorPath = "examples/rust/float-coprocessor/target/riscv64ima-unknown-linux-gnu/release/float-coprocessor"

// FindFloatCoprocessor locates the co-processor relative to the repository root, searching
// upwards so it works from any package directory.
func FindFloatCoprocessor() (string, error) {
	dir := "."
	for i := 0; i < 6; i++ {
		p := dir + "/" + DefaultFloatCoprocessorPath
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		dir += "/.."
	}
	return "", fmt.Errorf("float co-processor not built; run: cd examples/rust/float-coprocessor && " +
		"cargo build --target riscv64ima-unknown-linux-gnu.json " +
		"-Zbuild-std=core,compiler_builtins -Zbuild-std-features=compiler-builtins-mem --release")
}

// DecodeFloatSubGame maps an f64 instruction to the experimental nested route.
//
// Given an instruction and its register files, it returns the operation selector and the
// three operand words the nested machine needs. `ok` is false when the instruction has no
// route. A future outer game must establish that these inputs came from its committed
// pre-state; this helper alone does not perform that binding.
//
// Note which operands come from which register file. Most read the float registers, but the
// integer-to-double conversions read an *integer* register - and the comparisons write one.
func DecodeFloatSubGame(instr uint32, iRegs, fRegs *[32]uint64) (op, a, b, cc uint64, ok bool) {
	opcode := instr & 0x7F
	rs1 := (instr >> 15) & 0x1F
	rs2 := (instr >> 20) & 0x1F
	rs3 := (instr >> 27) & 0x1F
	funct3 := (instr >> 12) & 0x7
	funct5 := (instr >> 27) & 0x1F
	format := (instr >> 25) & 0x3

	// Single precision is not routed: the co-processor computes in double, and MLVM's F32
	// paths NaN-box their results, so the bits would not match.
	if format != 1 {
		return 0, 0, 0, 0, false
	}

	switch opcode {
	case 0x43, 0x47, 0x4B, 0x4F: // FMADD / FMSUB / FNMSUB / FNMADD
		sel := map[uint32]uint64{0x43: FloatOpMAdd, 0x47: FloatOpMSub, 0x4B: FloatOpNMSub, 0x4F: FloatOpNMAdd}[opcode]
		return sel, fRegs[rs1], fRegs[rs2], fRegs[rs3], true

	case 0x53:
		switch funct5 {
		case 0x00, 0x01, 0x02, 0x03:
			return uint64(funct5), fRegs[rs1], fRegs[rs2], 0, true
		case 0x05: // FMIN.D / FMAX.D, told apart by funct3
			if funct3 == 0 {
				return FloatOpMin, fRegs[rs1], fRegs[rs2], 0, true
			}
			return FloatOpMax, fRegs[rs1], fRegs[rs2], 0, true
		case 0x0B: // FSQRT.D - one operand
			return FloatOpSqrt, fRegs[rs1], 0, 0, true
		case 0x14: // FEQ.D / FLT.D / FLE.D, told apart by funct3
			switch funct3 {
			case 0x02:
				return FloatOpEq, fRegs[rs1], fRegs[rs2], 0, true
			case 0x01:
				return FloatOpLt, fRegs[rs1], fRegs[rs2], 0, true
			case 0x00:
				return FloatOpLe, fRegs[rs1], fRegs[rs2], 0, true
			}
		case 0x18: // FCVT.{W,WU,L,LU}.D - the variant is in rs2
			if rs2 <= 3 {
				return FloatOpCvtWD + uint64(rs2), fRegs[rs1], 0, 0, true
			}
		case 0x1A: // FCVT.D.{W,WU,L,LU} - reads an INTEGER register
			if rs2 <= 3 {
				return FloatOpCvtDW + uint64(rs2), iRegs[rs1], 0, 0, true
			}
		}
	}
	return 0, 0, 0, 0, false
}

// FloatSubGameWritesInteger reports whether an operation's result goes to an integer
// register rather than a float one. The outer game needs to know which file to check.
func FloatSubGameWritesInteger(op uint64) bool {
	return op == FloatOpEq || op == FloatOpLt || op == FloatOpLe ||
		(op >= FloatOpCvtWD && op <= FloatOpCvtLUD)
}

// signBitF64 is the sign bit of a double, flipped to negate an operand.
const signBitF64 = 0x8000_0000_0000_0000

// FloatReferenceWithFlags is what MLVM's own float unit computes for a sub-game operation,
// together with the IEEE-754 exceptions it raises.
//
// The flags matter as much as the value. A float instruction updates two pieces of committed
// state - the destination register and fcsr - so a route that checked only the value would
// omit part of the transition.
//
// This is the reference the co-processor's own flag computation is checked against.
func FloatReferenceWithFlags(op, a, b, c uint64) (value uint64, flags uint8, ok bool) {
	Softfloat_exceptionFlags = 0
	Softfloat_roundingMode = Softfloat_round_near_even
	value, ok = FloatReference(op, a, b, c)
	return value, Softfloat_exceptionFlags, ok
}

// FloatReference is what MLVM's own float unit computes for a sub-game operation.
//
// It exists so the differential test has something to compare against, and it is written to
// mirror the corresponding arm of fast/vm.go expression for expression rather than to be
// idiomatic. Where vm.go departs from IEEE-754 or from RISC-V, so does this - because the
// co-processor's job is to reproduce what the outer VM did, not what the standard says it
// should have done.
//
// Returns ok=false for operations with no sub-game route.
func FloatReference(op, a, b, c uint64) (uint64, bool) {
	switch op {
	case FloatOpAdd:
		return uint64(F64_add(Float64_t(a), Float64_t(b))), true
	case FloatOpSub:
		return uint64(F64_sub(Float64_t(a), Float64_t(b))), true
	case FloatOpMul:
		return uint64(F64_mul(Float64_t(a), Float64_t(b))), true
	case FloatOpDiv:
		return uint64(F64_div(Float64_t(a), Float64_t(b))), true
	case FloatOpSqrt:
		return uint64(F64_sqrt(Float64_t(a))), true

	case FloatOpMin:
		return uint64(F64_min(Float64_t(a), Float64_t(b))), true
	case FloatOpMax:
		return uint64(F64_max(Float64_t(a), Float64_t(b))), true

	// Genuinely fused: one rounding, not two. FNMSUB and FNMADD negate the product, and
	// FMSUB and FNMADD negate the addend, by flipping sign bits rather than by using
	// Berkeley's op field - FNMADD needs both negations and the op field only offers one.
	case FloatOpMAdd:
		return uint64(Softfloat_mulAddF64(a, b, c, 0)), true
	case FloatOpMSub:
		return uint64(Softfloat_mulAddF64(a, b, c^signBitF64, 0)), true
	case FloatOpNMSub:
		return uint64(Softfloat_mulAddF64(a^signBitF64, b, c, 0)), true
	case FloatOpNMAdd:
		return uint64(Softfloat_mulAddF64(a^signBitF64, b, c^signBitF64, 0)), true

	// The W forms take the low 32 bits of the integer register, not the whole thing.
	case FloatOpCvtDW:
		return uint64(I32_to_f64(int32(a & 0xFFFF_FFFF))), true
	case FloatOpCvtDWU:
		return uint64(UI32ToF64(uint32(a & 0xFFFF_FFFF))), true
	case FloatOpCvtDL:
		return uint64(I64_to_f64(int64(a))), true
	case FloatOpCvtDLU:
		return uint64(UI64ToF64(a)), true

	// SoftFloat rounds to nearest with ties to even here - it does not truncate toward zero
	// the way a C cast would - and maps NaN to INT_MAX rather than 0.
	case FloatOpCvtWD:
		return signExtend64(uint64(F64_to_i32(Float64_t(a), true)), toU64(31)), true
	case FloatOpCvtWUD:
		return signExtend64(uint64(F64_to_ui32(Float64_t(a), true)), toU64(31)), true
	case FloatOpCvtLD:
		return uint64(F64_to_i64(Float64_t(a), true)), true
	case FloatOpCvtLUD:
		return uint64(F64_to_ui64(Float64_t(a), true)), true

	case FloatOpEq:
		if F64_eq(Float64_t(a), Float64_t(b)) {
			return 1, true
		}
		return 0, true
	case FloatOpLt:
		if F64_lt(Float64_t(a), Float64_t(b)) {
			return 1, true
		}
		return 0, true
	case FloatOpLe:
		if F64_le(Float64_t(a), Float64_t(b)) {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}
