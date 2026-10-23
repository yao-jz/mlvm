package fast

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBindFloatInstructionAdd(t *testing.T) {
	// FADD.S rd=1, rs1=2, rs2=3, rm=000, fmt=S
	// opcode 0x53, rd=1, funct3=0, rs1=2, rs2=3, fmt=0, funct5=0
	instr := uint32(0x53) | (1 << 7) | (0 << 12) | (2 << 15) | (3 << 20) | (0 << 25) | (0 << 27)
	var x, f [32]uint64
	f[2] = 0xffffffff3f800000 // 1.0f NaN-boxed
	f[3] = 0xffffffff40000000 // 2.0f NaN-boxed
	b, err := BindFloatInstruction(0x1000, instr, &x, &f, 0)
	require.NoError(t, err)
	require.Equal(t, FloatOpAdd, b.Op)
	require.True(t, b.FormatF32)
	require.Equal(t, f[2], b.A)
	require.Equal(t, f[3], b.B)
	require.Equal(t, uint64(1), b.Rd)
}

func TestApplyFloatResultNaNBox(t *testing.T) {
	st := NewVMState()
	b := &OuterFloatBinding{Rd: 4, FormatF32: true}
	require.NoError(t, ApplyFloatResult(st, b, 0x3f800000, 0x1))
	require.Equal(t, uint64(0xffffffff3f800000), st.FRegisters[4])
	require.Equal(t, uint64(0x1), st.FCSR&0x1f)
}
