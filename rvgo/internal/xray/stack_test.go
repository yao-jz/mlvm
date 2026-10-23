package xray

// A synthetic guest for the shadow call stack: main calls f, which recurses depth times.

import (
	"encoding/binary"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yao-jz/mlvm/rvgo/fast"
	"github.com/yao-jz/mlvm/rvgo/internal/rvdis"
)

func iType(op, rd, f3, rs1 uint32, imm int32) uint32 {
	return uint32(imm)<<20 | rs1<<15 | f3<<12 | rd<<7 | op
}
func sType(op, f3, rs1, rs2 uint32, imm int32) uint32 {
	u := uint32(imm) & 0xfff
	return (u>>5)<<25 | rs2<<20 | rs1<<15 | f3<<12 | (u&31)<<7 | op
}
func bType(f3, rs1, rs2 uint32, imm int32) uint32 {
	u := uint32(imm)
	return (u>>12&1)<<31 | (u>>5&0x3f)<<25 | rs2<<20 | rs1<<15 | f3<<12 | (u>>1&0xf)<<8 | (u>>11&1)<<7 | 0x63
}
func jType(rd uint32, imm int32) uint32 {
	u := uint32(imm)
	return (u>>20&1)<<31 | (u>>1&0x3ff)<<21 | (u>>11&1)<<20 | (u>>12&0xff)<<12 | rd<<7 | 0x6f
}

func testProfiler(syms []symbol) *Profiler {
	s := &Symbolizer{syms: syms}
	for _, x := range syms {
		s.names = append(s.names, x.name)
	}
	s.names = append(s.names, unknownSymbol)
	p := &Profiler{
		sym: s, cache: map[uint64]*decoded{}, fastTag: make([]uint64, fastCacheSize), fastEnt: make([]*decoded, fastCacheSize),
		mnemIndex: map[string]int{}, classIdx: map[rvdis.Class]int{}, classes: make([]uint64, len(rvdis.Classes)),
		frames: []frame{{parent: -1, sym: -1, children: map[int]int{}}},
	}
	for i, c := range rvdis.Classes {
		p.classIdx[c] = i
	}
	return p
}

func recursionProfile(t *testing.T, depth int32) *Report {
	const ra, sp, a0, a7 = 1, 2, 10, 17
	f := []uint32{
		iType(0x13, a0, 0, a0, -1),  // 0x1000 addi a0,a0,-1
		bType(0, a0, 0, 24),         // 0x1004 beq a0,zero,0x101c
		iType(0x13, sp, 0, sp, -16), // 0x1008
		sType(0x23, 3, sp, ra, 0),   // 0x100c sd ra,0(sp)
		jType(ra, -0x10),            // 0x1010 jal ra,f
		iType(0x03, ra, 3, sp, 0),   // 0x1014 ld ra,0(sp)
		iType(0x13, sp, 0, sp, 16),  // 0x1018
		iType(0x67, 0, 0, ra, 0),    // 0x101c jalr zero,0(ra)
	}
	main := []uint32{
		iType(0x13, a0, 0, 0, depth), // 0x2000 addi a0,zero,depth
		jType(ra, 0x1000-0x2004),     // 0x2004 jal ra,f
		iType(0x13, a7, 0, 0, 93),    // 0x2008 addi a7,zero,93
		0x00000073,                   // 0x200c ecall
	}
	state := fast.NewVMState()
	for i, w := range f {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], w)
		state.Memory.SetUnaligned(0x1000+uint64(4*i), b[:])
	}
	for i, w := range main {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], w)
		state.Memory.SetUnaligned(0x2000+uint64(4*i), b[:])
	}
	state.PC = 0x2000
	state.Registers[sp] = 0x100000
	p := testProfiler([]symbol{{"f", 0x1000, 0x1020}, {"main", 0x2000, 0x2010}})
	inst := fast.NewInstrumentedState(state, nil, io.Discard, io.Discard)
	for !state.Exited {
		require.NoError(t, p.Step(inst, state))
	}
	return p.Report()
}

// Recursion deeper than maxDepth must not pop frames it never pushed: every instruction
// of f stays under main.
func TestDeepRecursionStaysUnderMain(t *testing.T) {
	for _, depth := range []int32{100, maxDepth + 72} {
		r := recursionProfile(t, depth)
		require.Len(t, r.Tree.Children, 1, "depth %d", depth)
		require.Equal(t, "main", r.Tree.Children[0].Name, "depth %d", depth)
		require.Equal(t, r.Steps, r.Tree.Children[0].Value, "depth %d: main must include all of f", depth)
	}
}
