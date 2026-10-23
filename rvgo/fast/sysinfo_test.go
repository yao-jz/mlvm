package fast_test

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yao-jz/mlvm/rvgo/fast"
	"github.com/yao-jz/mlvm/rvgo/slow"
)

func TestSysinfoFastSlowWitness(t *testing.T) {
	const (
		pc   = uint64(0x1000)
		addr = uint64(0x2008) // exercise chunks that cross 32-byte leaf boundaries
	)
	state := fast.NewVMState()
	state.PC = pc
	state.Memory.SetUnaligned(pc, []byte{0x73, 0x00, 0x00, 0x00}) // ECALL
	state.Registers[17] = 179                                     // a7 = sysinfo
	state.Registers[10] = addr                                    // a0 = struct sysinfo*

	witness, err := fast.NewInstrumentedState(state, nil, nil, nil).Step(true)
	require.NoError(t, err)
	require.NotNil(t, witness)
	require.Greater(t, len(witness.MemProof), fast.ProofLen*32)

	var info [112]byte
	for off := 0; off < len(info); off += 32 {
		end := off + 32
		if end > len(info) {
			end = len(info)
		}
		state.Memory.GetUnaligned(addr+uint64(off), info[off:end])
	}
	require.Equal(t, uint64(1337), binary.LittleEndian.Uint64(info[0:8]))
	require.Equal(t, uint64(1<<30), binary.LittleEndian.Uint64(info[32:40]))
	require.Equal(t, uint64(1<<30), binary.LittleEndian.Uint64(info[40:48]))
	require.Equal(t, uint32(1), binary.LittleEndian.Uint32(info[104:108]))
	for i, b := range info {
		if (i >= 0 && i < 8) || (i >= 32 && i < 48) || (i >= 104 && i < 108) {
			continue
		}
		require.Zero(t, b, "byte %d", i)
	}

	slowPost, err := slow.Step(witness.EncodeStepInput(fast.LocalContext{}), nil)
	require.NoError(t, err)
	fastPost, err := state.EncodeWitness().StateHash()
	require.NoError(t, err)
	require.Equal(t, fastPost, slowPost)
}
