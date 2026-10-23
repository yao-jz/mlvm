// Package rvdis decodes RV64IMAFD instruction words into mnemonics, operand text and an
// instruction class. It exists so the profiler and the dispute explorer can say *which*
// instruction they are talking about ("fadd.s fa5,fa5,fa4 in matmul") instead of printing
// a hex word.
//
// The text follows GNU objdump with `-M no-aliases`: raw instructions, ABI register names,
// branch and jump targets as absolute hex addresses. It is a display aid, not part of the
// VM's semantics; fast/vm.go remains the only decoder that executes anything.
package rvdis

import (
	"fmt"
	"strings"
)

// Class groups instructions the way the profiler reports them.
type Class string

const (
	ClassIntALU     Class = "int-alu"
	ClassMulDiv     Class = "int-muldiv"
	ClassLoad       Class = "load"
	ClassStore      Class = "store"
	ClassBranch     Class = "branch"
	ClassJump       Class = "jump"
	ClassAtomic     Class = "atomic"
	ClassSystem     Class = "system"
	ClassFloatMem   Class = "float-loadstore"
	ClassFloatMove  Class = "float-move" // sign injection, moves, compares, classify
	ClassFloatArith Class = "float-arith"
	ClassUnknown    Class = "unknown"
)

// Classes lists every class in report order.
var Classes = []Class{
	ClassIntALU, ClassMulDiv, ClassLoad, ClassStore, ClassBranch, ClassJump, ClassAtomic,
	ClassSystem, ClassFloatMem, ClassFloatMove, ClassFloatArith, ClassUnknown,
}

// IsFloat reports whether the class touches the F or D extension.
func (c Class) IsFloat() bool {
	return c == ClassFloatMem || c == ClassFloatMove || c == ClassFloatArith
}

// Inst is one decoded instruction.
type Inst struct {
	Word     uint32
	Mnemonic string
	Operands string
	Class    Class
	// Rd is the destination register number (integer or float, see FloatRd), or -1.
	Rd      int
	FloatRd bool
	// Precision is "s" or "d" for F/D arithmetic, otherwise empty.
	Precision string
}

func (i Inst) String() string {
	if i.Operands == "" {
		return i.Mnemonic
	}
	return i.Mnemonic + "\t" + i.Operands
}

// Text is String with a space instead of a tab, for single-line displays.
func (i Inst) Text() string {
	if i.Operands == "" {
		return i.Mnemonic
	}
	return i.Mnemonic + " " + i.Operands
}

var xNames = [32]string{
	"zero", "ra", "sp", "gp", "tp", "t0", "t1", "t2", "s0", "s1", "a0", "a1", "a2", "a3", "a4", "a5",
	"a6", "a7", "s2", "s3", "s4", "s5", "s6", "s7", "s8", "s9", "s10", "s11", "t3", "t4", "t5", "t6",
}

var fNames = [32]string{
	"ft0", "ft1", "ft2", "ft3", "ft4", "ft5", "ft6", "ft7", "fs0", "fs1", "fa0", "fa1", "fa2", "fa3", "fa4", "fa5",
	"fa6", "fa7", "fs2", "fs3", "fs4", "fs5", "fs6", "fs7", "fs8", "fs9", "fs10", "fs11", "ft8", "ft9", "ft10", "ft11",
}

// XReg and FReg return ABI register names.
func XReg(n int) string { return xNames[n&31] }
func FReg(n int) string { return fNames[n&31] }

var roundingModes = [8]string{"rne", "rtz", "rdn", "rup", "rmm", "", "", "dyn"}

func bits(w uint32, hi, lo uint) uint32 { return (w >> lo) & ((1 << (hi - lo + 1)) - 1) }

func signExtend(v uint32, width uint) int64 {
	shift := 64 - width
	return int64(uint64(v)<<shift) >> shift
}

func immI(w uint32) int64 { return signExtend(bits(w, 31, 20), 12) }
func immS(w uint32) int64 { return signExtend(bits(w, 31, 25)<<5|bits(w, 11, 7), 12) }
func immB(w uint32) int64 {
	v := bits(w, 31, 31)<<12 | bits(w, 7, 7)<<11 | bits(w, 30, 25)<<5 | bits(w, 11, 8)<<1
	return signExtend(v, 13)
}
func immJ(w uint32) int64 {
	v := bits(w, 31, 31)<<20 | bits(w, 19, 12)<<12 | bits(w, 20, 20)<<11 | bits(w, 30, 21)<<1
	return signExtend(v, 21)
}

func csrName(csr uint32) string {
	switch csr {
	case 0x001:
		return "fflags"
	case 0x002:
		return "frm"
	case 0x003:
		return "fcsr"
	case 0xc00:
		return "cycle"
	case 0xc01:
		return "time"
	case 0xc02:
		return "instret"
	case 0xc22:
		return "vlenb"
	case 0xf14:
		return "mhartid"
	}
	return fmt.Sprintf("0x%x", csr)
}

func fenceSet(v uint32) string {
	var b strings.Builder
	for i, c := range "iorw" {
		if v&(1<<(3-i)) != 0 {
			b.WriteRune(c)
		}
	}
	if b.Len() == 0 {
		return "0"
	}
	return b.String()
}

func amoSuffix(w uint32) string {
	switch bits(w, 26, 25) {
	case 2:
		return ".aq"
	case 1:
		return ".rl"
	case 3:
		return ".aqrl"
	}
	return ""
}

func unknown(w uint32) Inst {
	return Inst{Word: w, Mnemonic: ".word", Operands: fmt.Sprintf("0x%08x", w), Class: ClassUnknown, Rd: -1}
}

// Decode disassembles one 32-bit instruction located at pc. pc is only used to print
// absolute branch and jump targets.
func Decode(w uint32, pc uint64) Inst {
	rd := int(bits(w, 11, 7))
	rs1 := int(bits(w, 19, 15))
	rs2 := int(bits(w, 24, 20))
	funct3 := bits(w, 14, 12)
	funct7 := bits(w, 31, 25)
	out := Inst{Word: w, Rd: -1}
	set := func(m string, c Class, ops string) Inst {
		out.Mnemonic, out.Class, out.Operands = m, c, ops
		return out
	}
	setRd := func(m string, c Class, ops string) Inst {
		out.Rd = rd
		return set(m, c, ops)
	}
	setFRd := func(m string, c Class, ops string) Inst {
		out.Rd, out.FloatRd = rd, true
		return set(m, c, ops)
	}

	switch w & 0x7f {
	case 0x37:
		return setRd("lui", ClassIntALU, fmt.Sprintf("%s,0x%x", XReg(rd), bits(w, 31, 12)))
	case 0x17:
		return setRd("auipc", ClassIntALU, fmt.Sprintf("%s,0x%x", XReg(rd), bits(w, 31, 12)))
	case 0x6f:
		return setRd("jal", ClassJump, fmt.Sprintf("%s,%x", XReg(rd), pc+uint64(immJ(w))))
	case 0x67:
		if funct3 != 0 {
			return unknown(w)
		}
		return setRd("jalr", ClassJump, fmt.Sprintf("%s,%d(%s)", XReg(rd), immI(w), XReg(rs1)))
	case 0x63:
		names := [8]string{"beq", "bne", "", "", "blt", "bge", "bltu", "bgeu"}
		if names[funct3] == "" {
			return unknown(w)
		}
		return set(names[funct3], ClassBranch, fmt.Sprintf("%s,%s,%x", XReg(rs1), XReg(rs2), pc+uint64(immB(w))))
	case 0x03:
		names := [8]string{"lb", "lh", "lw", "ld", "lbu", "lhu", "lwu", ""}
		if names[funct3] == "" {
			return unknown(w)
		}
		return setRd(names[funct3], ClassLoad, fmt.Sprintf("%s,%d(%s)", XReg(rd), immI(w), XReg(rs1)))
	case 0x23:
		names := [8]string{"sb", "sh", "sw", "sd", "", "", "", ""}
		if names[funct3] == "" {
			return unknown(w)
		}
		return set(names[funct3], ClassStore, fmt.Sprintf("%s,%d(%s)", XReg(rs2), immS(w), XReg(rs1)))
	case 0x13:
		switch funct3 {
		case 1:
			if bits(w, 31, 26) != 0 {
				return unknown(w)
			}
			return setRd("slli", ClassIntALU, fmt.Sprintf("%s,%s,0x%x", XReg(rd), XReg(rs1), bits(w, 25, 20)))
		case 5:
			name := map[uint32]string{0x00: "srli", 0x10: "srai"}[bits(w, 31, 26)]
			if name == "" {
				return unknown(w)
			}
			return setRd(name, ClassIntALU, fmt.Sprintf("%s,%s,0x%x", XReg(rd), XReg(rs1), bits(w, 25, 20)))
		}
		names := [8]string{"addi", "", "slti", "sltiu", "xori", "", "ori", "andi"}
		return setRd(names[funct3], ClassIntALU, fmt.Sprintf("%s,%s,%d", XReg(rd), XReg(rs1), immI(w)))
	case 0x1b:
		switch funct3 {
		case 0:
			return setRd("addiw", ClassIntALU, fmt.Sprintf("%s,%s,%d", XReg(rd), XReg(rs1), immI(w)))
		case 1:
			if funct7 != 0 {
				return unknown(w)
			}
			return setRd("slliw", ClassIntALU, fmt.Sprintf("%s,%s,0x%x", XReg(rd), XReg(rs1), rs2))
		case 5:
			name := map[uint32]string{0x00: "srliw", 0x20: "sraiw"}[funct7]
			if name == "" {
				return unknown(w)
			}
			return setRd(name, ClassIntALU, fmt.Sprintf("%s,%s,0x%x", XReg(rd), XReg(rs1), rs2))
		}
		return unknown(w)
	case 0x33:
		ops := fmt.Sprintf("%s,%s,%s", XReg(rd), XReg(rs1), XReg(rs2))
		switch funct7 {
		case 0x00:
			names := [8]string{"add", "sll", "slt", "sltu", "xor", "srl", "or", "and"}
			return setRd(names[funct3], ClassIntALU, ops)
		case 0x20:
			name := map[uint32]string{0: "sub", 5: "sra"}[funct3]
			if name == "" {
				return unknown(w)
			}
			return setRd(name, ClassIntALU, ops)
		case 0x01:
			names := [8]string{"mul", "mulh", "mulhsu", "mulhu", "div", "divu", "rem", "remu"}
			return setRd(names[funct3], ClassMulDiv, ops)
		}
		return unknown(w)
	case 0x3b:
		ops := fmt.Sprintf("%s,%s,%s", XReg(rd), XReg(rs1), XReg(rs2))
		var name string
		class := ClassIntALU
		switch funct7 {
		case 0x00:
			name = map[uint32]string{0: "addw", 1: "sllw", 5: "srlw"}[funct3]
		case 0x20:
			name = map[uint32]string{0: "subw", 5: "sraw"}[funct3]
		case 0x01:
			name = map[uint32]string{0: "mulw", 4: "divw", 5: "divuw", 6: "remw", 7: "remuw"}[funct3]
			class = ClassMulDiv
		}
		if name == "" {
			return unknown(w)
		}
		return setRd(name, class, ops)
	case 0x0f:
		switch funct3 {
		case 0:
			if bits(w, 31, 28) == 8 && bits(w, 27, 20) == 0x33 {
				return set("fence.tso", ClassSystem, "")
			}
			return set("fence", ClassSystem, fenceSet(bits(w, 27, 24))+","+fenceSet(bits(w, 23, 20)))
		case 1:
			return set("fence.i", ClassSystem, "")
		}
		return unknown(w)
	case 0x73:
		csr := bits(w, 31, 20)
		switch funct3 {
		case 0:
			switch w {
			case 0x00000073:
				return set("ecall", ClassSystem, "")
			case 0x00100073:
				return set("ebreak", ClassSystem, "")
			}
			return unknown(w)
		case 1, 2, 3:
			names := [4]string{"", "csrrw", "csrrs", "csrrc"}
			return setRd(names[funct3], ClassSystem, fmt.Sprintf("%s,%s,%s", XReg(rd), csrName(csr), XReg(rs1)))
		case 5, 6, 7:
			names := [8]string{"", "", "", "", "", "csrrwi", "csrrsi", "csrrci"}
			return setRd(names[funct3], ClassSystem, fmt.Sprintf("%s,%s,%d", XReg(rd), csrName(csr), rs1))
		}
		return unknown(w)
	case 0x2f:
		var width string
		switch funct3 {
		case 2:
			width = "w"
		case 3:
			width = "d"
		default:
			return unknown(w)
		}
		funct5 := bits(w, 31, 27)
		name := map[uint32]string{
			0x02: "lr", 0x03: "sc", 0x01: "amoswap", 0x00: "amoadd", 0x04: "amoxor", 0x0c: "amoand",
			0x08: "amoor", 0x10: "amomin", 0x14: "amomax", 0x18: "amominu", 0x1c: "amomaxu",
		}[funct5]
		if name == "" {
			return unknown(w)
		}
		mnemonic := name + "." + width + amoSuffix(w)
		if funct5 == 0x02 {
			return setRd(mnemonic, ClassAtomic, fmt.Sprintf("%s,(%s)", XReg(rd), XReg(rs1)))
		}
		return setRd(mnemonic, ClassAtomic, fmt.Sprintf("%s,%s,(%s)", XReg(rd), XReg(rs2), XReg(rs1)))
	case 0x07:
		name := map[uint32]string{2: "flw", 3: "fld"}[funct3]
		if name == "" {
			return unknown(w)
		}
		return setFRd(name, ClassFloatMem, fmt.Sprintf("%s,%d(%s)", FReg(rd), immI(w), XReg(rs1)))
	case 0x27:
		name := map[uint32]string{2: "fsw", 3: "fsd"}[funct3]
		if name == "" {
			return unknown(w)
		}
		return set(name, ClassFloatMem, fmt.Sprintf("%s,%d(%s)", FReg(rs2), immS(w), XReg(rs1)))
	case 0x43, 0x47, 0x4b, 0x4f:
		prec := map[uint32]string{0: "s", 1: "d"}[bits(w, 26, 25)]
		if prec == "" {
			return unknown(w)
		}
		name := map[uint32]string{0x43: "fmadd", 0x47: "fmsub", 0x4b: "fnmsub", 0x4f: "fnmadd"}[w&0x7f]
		out.Precision = prec
		ops := fmt.Sprintf("%s,%s,%s,%s", FReg(rd), FReg(rs1), FReg(rs2), FReg(int(bits(w, 31, 27))))
		return setFRd(name+"."+prec, ClassFloatArith, withRM(ops, funct3))
	case 0x53:
		return decodeOpFP(w, out, rd, rs1, rs2, funct3, funct7)
	}
	return unknown(w)
}

func withRM(ops string, rm uint32) string {
	if rm == 7 {
		return ops
	}
	if roundingModes[rm] == "" {
		return ops + fmt.Sprintf(",%d", rm)
	}
	return ops + "," + roundingModes[rm]
}

func decodeOpFP(w uint32, out Inst, rd, rs1, rs2 int, funct3, funct7 uint32) Inst {
	prec := map[uint32]string{0: "s", 1: "d"}[funct7&3]
	if prec == "" {
		return unknown(w)
	}
	set := func(m string, c Class, ops string, floatRd bool) Inst {
		out.Mnemonic, out.Class, out.Operands = m, c, ops
		out.Rd, out.FloatRd = rd, floatRd
		if c == ClassFloatArith {
			out.Precision = prec
		}
		return out
	}
	fff := fmt.Sprintf("%s,%s,%s", FReg(rd), FReg(rs1), FReg(rs2))
	switch funct7 >> 2 {
	case 0x00, 0x01, 0x02, 0x03:
		name := [4]string{"fadd", "fsub", "fmul", "fdiv"}[funct7>>2]
		return set(name+"."+prec, ClassFloatArith, withRM(fff, funct3), true)
	case 0x0b:
		if rs2 != 0 {
			return unknown(w)
		}
		return set("fsqrt."+prec, ClassFloatArith, withRM(fmt.Sprintf("%s,%s", FReg(rd), FReg(rs1)), funct3), true)
	case 0x04:
		name := map[uint32]string{0: "fsgnj", 1: "fsgnjn", 2: "fsgnjx"}[funct3]
		if name == "" {
			return unknown(w)
		}
		return set(name+"."+prec, ClassFloatMove, fff, true)
	case 0x05:
		name := map[uint32]string{0: "fmin", 1: "fmax"}[funct3]
		if name == "" {
			return unknown(w)
		}
		return set(name+"."+prec, ClassFloatArith, fff, true)
	case 0x08:
		// fcvt.s.d (funct7 0x20, rs2 1) and fcvt.d.s (funct7 0x21, rs2 0)
		from := map[int]string{0: "s", 1: "d"}[rs2]
		if from == "" || from == prec {
			return unknown(w)
		}
		ops := fmt.Sprintf("%s,%s", FReg(rd), FReg(rs1))
		if prec == "d" { // widening is exact, so objdump prints no rounding mode
			return set("fcvt.d.s", ClassFloatArith, ops, true)
		}
		return set("fcvt.s.d", ClassFloatArith, withRM(ops, funct3), true)
	case 0x14:
		name := map[uint32]string{0: "fle", 1: "flt", 2: "feq"}[funct3]
		if name == "" {
			return unknown(w)
		}
		return set(name+"."+prec, ClassFloatMove, fmt.Sprintf("%s,%s,%s", XReg(rd), FReg(rs1), FReg(rs2)), false)
	case 0x18:
		to := map[int]string{0: "w", 1: "wu", 2: "l", 3: "lu"}[rs2]
		if to == "" {
			return unknown(w)
		}
		return set("fcvt."+to+"."+prec, ClassFloatArith, withRM(fmt.Sprintf("%s,%s", XReg(rd), FReg(rs1)), funct3), false)
	case 0x1a:
		from := map[int]string{0: "w", 1: "wu", 2: "l", 3: "lu"}[rs2]
		if from == "" {
			return unknown(w)
		}
		ops := fmt.Sprintf("%s,%s", FReg(rd), XReg(rs1))
		if prec == "d" && (from == "w" || from == "wu") { // every 32-bit integer is exact in f64
			return set("fcvt.d."+from, ClassFloatArith, ops, true)
		}
		return set("fcvt."+prec+"."+from, ClassFloatArith, withRM(ops, funct3), true)
	case 0x1c:
		if rs2 != 0 {
			return unknown(w)
		}
		switch funct3 {
		case 0:
			name := map[string]string{"s": "fmv.x.w", "d": "fmv.x.d"}[prec]
			return set(name, ClassFloatMove, fmt.Sprintf("%s,%s", XReg(rd), FReg(rs1)), false)
		case 1:
			return set("fclass."+prec, ClassFloatMove, fmt.Sprintf("%s,%s", XReg(rd), FReg(rs1)), false)
		}
		return unknown(w)
	case 0x1e:
		if rs2 != 0 || funct3 != 0 {
			return unknown(w)
		}
		name := map[string]string{"s": "fmv.w.x", "d": "fmv.d.x"}[prec]
		return set(name, ClassFloatMove, fmt.Sprintf("%s,%s", FReg(rd), XReg(rs1)), true)
	}
	return unknown(w)
}
