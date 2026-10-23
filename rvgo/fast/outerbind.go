package fast

import (
	"encoding/binary"
	"fmt"
)

// OuterFloatBinding extracts the operands a nested float sub-game must use from a
// concrete outer prestate and instruction. Callers may not invent (op,a,b,c).
//
// This is the binding layer required before FloatSubGame.nestedPreState is sound.
type OuterFloatBinding struct {
	PC        uint64
	Instr     uint32
	Op        uint64
	A, B, C   uint64
	Rd        uint64
	Frm       uint64
	FormatF32 bool
}

// BindFloatInstruction decodes a float arithmetic instruction against the supplied
// register file and returns the nested-game operands.
func BindFloatInstruction(pc uint64, instr uint32, xregs, fregs *[32]uint64, fcsr uint64) (*OuterFloatBinding, error) {
	opcode := instr & 0x7f
	rd := (instr >> 7) & 0x1f
	funct3 := (instr >> 12) & 0x7
	rs1 := (instr >> 15) & 0x1f
	rs2 := (instr >> 20) & 0x1f
	funct5 := (instr >> 27) & 0x1f
	fmtBit := (instr >> 25) & 0x3

	b := &OuterFloatBinding{PC: pc, Instr: instr, Rd: uint64(rd), Frm: uint64((fcsr >> 5) & 7)}
	switch opcode {
	case 0x53:
		b.FormatF32 = fmtBit == 0
		b.A = fregs[rs1]
		b.B = fregs[rs2]
		switch funct5 {
		case 0x00, 0x01, 0x02, 0x03, 0x0B:
			b.Op = uint64(funct5)
		case 0x05:
			if funct3 == 1 {
				b.Op = FloatOpMax
			} else {
				b.Op = FloatOpMin
			}
		default:
			return nil, fmt.Errorf("float op funct5=0x%x not bound for nested game", funct5)
		}
		if funct3 != 7 {
			b.Frm = uint64(funct3) // static rm encoded in instr
		}
		return b, nil
	case 0x43, 0x47, 0x4B, 0x4F:
		rs3 := (instr >> 27) & 0x1f
		b.FormatF32 = fmtBit == 0
		b.A = fregs[rs1]
		b.B = fregs[rs2]
		b.C = fregs[rs3]
		b.Op = map[uint32]uint64{0x43: FloatOpMAdd, 0x47: FloatOpMSub, 0x4B: FloatOpNMSub, 0x4F: FloatOpNMAdd}[opcode]
		return b, nil
	default:
		return nil, fmt.Errorf("instruction 0x%x is not a float arithmetic op", instr)
	}
}

// ApplyFloatResult writes a nested-game result and flags back into an outer post-state.
func ApplyFloatResult(state *VMState, binding *OuterFloatBinding, result, flags uint64) error {
	if binding == nil {
		return fmt.Errorf("nil binding")
	}
	if binding.Rd != 0 {
		if binding.FormatF32 {
			// NaN-box f32 results into the 64-bit register.
			state.FRegisters[binding.Rd] = result&0xffffffff | 0xffffffff00000000
		} else {
			state.FRegisters[binding.Rd] = result
		}
	}
	state.FCSR = (state.FCSR &^ 0x1f) | (flags & 0x1f)
	return nil
}

// InstrAt returns the 32-bit little-endian instruction at pc.
func InstrAt(state *VMState, pc uint64) uint32 {
	var buf [4]byte
	state.Memory.GetUnaligned(pc, buf[:])
	return binary.LittleEndian.Uint32(buf[:])
}
