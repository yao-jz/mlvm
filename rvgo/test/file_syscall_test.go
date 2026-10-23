package test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yao-jz/mlvm/rvgo/fast"
	"github.com/yao-jz/mlvm/rvgo/slow"
)

func newStagedSyscallState(t *testing.T, name string, contents []byte) *fast.VMState {
	t.Helper()
	state := fast.NewVMState()
	require.NoError(t, state.Memory.SetMemoryRange(0, bytes.NewReader([]byte{0x73, 0, 0, 0}))) // ECALL
	require.NoError(t, state.FileSystem.AddFile(name, uint64(len(contents)), state))
	file, ok := state.FileSystem.File(name)
	require.True(t, ok)
	if len(contents) != 0 {
		require.NoError(t, state.Memory.SetMemoryRange(file.Addr, bytes.NewReader(contents)))
	}
	return state
}

func assertFileSyscallSlowMatchesFast(t *testing.T, state *fast.VMState) *fast.StepWitness {
	t.Helper()
	witness, err := fast.NewInstrumentedState(state, nil, nil, nil).Step(true)
	require.NoError(t, err)
	got, err := slow.Step(witness.EncodeStepInput(fast.LocalContext{}), nil)
	require.NoError(t, err)
	want, err := state.EncodeWitness().StateHash()
	require.NoError(t, err)
	require.Equal(t, want, got)
	return witness
}

func TestStagedOpenatSlowMatchesFast(t *testing.T) {
	state := newStagedSyscallState(t, "input.bin", []byte("hello"))
	pathAddr := uint64(0x10_01f)
	require.NoError(t, state.Memory.SetMemoryRange(pathAddr, bytes.NewReader([]byte("input.bin\x00"))))
	state.Registers[17] = 56
	state.Registers[11] = pathAddr

	witness := assertFileSyscallSlowMatchesFast(t, state)
	require.Len(t, witness.FileSystemProof, fast.FileSystemProofSize)
}

func TestStagedReadSlowMatchesFast(t *testing.T) {
	contents := make([]byte, 64)
	for i := range contents {
		contents[i] = byte(i)
	}
	state := newStagedSyscallState(t, "input.bin", contents)
	fd, err := state.FileSystem.Open("input.bin")
	require.NoError(t, err)
	require.NoError(t, state.FileSystem.SetDescriptorOffset(fd, 31))
	state.Registers[17] = 63
	state.Registers[10] = fd
	state.Registers[11] = 0x20_01f
	state.Registers[12] = 4096

	assertFileSyscallSlowMatchesFast(t, state)
}

func TestStagedDescriptorSyscallsSlowMatchFast(t *testing.T) {
	tests := []struct {
		name      string
		syscall   uint64
		configure func(*fast.VMState, uint64)
	}{
		{
			name:    "fcntl-getfl",
			syscall: 25,
			configure: func(state *fast.VMState, fd uint64) {
				state.Registers[10] = fd
				state.Registers[11] = 3
			},
		},
		{
			name:    "lseek-set",
			syscall: 62,
			configure: func(state *fast.VMState, fd uint64) {
				state.Registers[10] = fd
				state.Registers[11] = 2
				state.Registers[12] = 0
			},
		},
		{
			name:    "close",
			syscall: 57,
			configure: func(state *fast.VMState, fd uint64) {
				state.Registers[10] = fd
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := newStagedSyscallState(t, "input.bin", []byte("hello"))
			fd, err := state.FileSystem.Open("input.bin")
			require.NoError(t, err)
			state.Registers[17] = test.syscall
			test.configure(state, fd)
			assertFileSyscallSlowMatchesFast(t, state)
		})
	}
}

func TestMissingStagedDescriptorSlowMatchesFast(t *testing.T) {
	state := newStagedSyscallState(t, "input.bin", []byte("hello"))
	state.Registers[17] = 63
	state.Registers[10] = 107
	state.Registers[11] = 0x20_000
	state.Registers[12] = 8

	assertFileSyscallSlowMatchesFast(t, state)
}

func TestKernelPseudoOpenatNeedsNoFileSystemProof(t *testing.T) {
	state := newStagedSyscallState(t, "input.bin", nil)
	pathAddr := uint64(0x10_000)
	require.NoError(t, state.Memory.SetMemoryRange(pathAddr, bytes.NewReader([]byte("/proc/self/maps\x00"))))
	state.Registers[17] = 56
	state.Registers[11] = pathAddr

	witness := assertFileSyscallSlowMatchesFast(t, state)
	require.Empty(t, witness.FileSystemProof)
}

func TestSlowRejectsTamperedFileSystemProof(t *testing.T) {
	state := newStagedSyscallState(t, "input.bin", []byte("hello"))
	fd, err := state.FileSystem.Open("input.bin")
	require.NoError(t, err)
	state.Registers[17] = 25
	state.Registers[10] = fd
	state.Registers[11] = 1

	witness, err := fast.NewInstrumentedState(state, nil, nil, nil).Step(true)
	require.NoError(t, err)
	witness.FileSystemProof[len(witness.FileSystemProof)-1] ^= 1
	_, err = slow.Step(witness.EncodeStepInput(fast.LocalContext{}), nil)
	require.ErrorContains(t, err, "descriptor Merkle branch")
}

func TestStagedFileSyscallsEVMMatchFast(t *testing.T) {
	if _, err := os.Stat("../../rvsol/out/RISCV.sol/RISCV.json"); os.IsNotExist(err) {
		t.Skip("no forge artifacts; run `forge build` in rvsol first")
	}
	contracts := testContracts(t)
	env := newEVMEnv(t, contracts, testAddrs)

	states := make([]*fast.VMState, 0, 7)

	openState := newStagedSyscallState(t, "input.bin", []byte("hello"))
	pathAddr := uint64(0x10_01f)
	require.NoError(t, openState.Memory.SetMemoryRange(pathAddr, bytes.NewReader([]byte("input.bin\x00"))))
	openState.Registers[17], openState.Registers[11] = 56, pathAddr
	states = append(states, openState)

	readState := newStagedSyscallState(t, "input.bin", bytes.Repeat([]byte{0x5a}, 64))
	readFD, err := readState.FileSystem.Open("input.bin")
	require.NoError(t, err)
	require.NoError(t, readState.FileSystem.SetDescriptorOffset(readFD, 31))
	readState.Registers[17], readState.Registers[10] = 63, readFD
	readState.Registers[11], readState.Registers[12] = 0x20_01f, 4096
	states = append(states, readState)

	for _, syscall := range []uint64{25, 62, 57} {
		state := newStagedSyscallState(t, "input.bin", []byte("hello"))
		fd, openErr := state.FileSystem.Open("input.bin")
		require.NoError(t, openErr)
		state.Registers[17], state.Registers[10] = syscall, fd
		switch syscall {
		case 25:
			state.Registers[11] = 3 // F_GETFL
		case 62:
			state.Registers[11], state.Registers[12] = 2, 0 // SEEK_SET
		}
		states = append(states, state)
	}

	missingState := newStagedSyscallState(t, "input.bin", nil)
	missingState.Registers[17], missingState.Registers[10] = 63, 107
	missingState.Registers[11], missingState.Registers[12] = 0x30_000, 8
	states = append(states, missingState)

	pseudoState := newStagedSyscallState(t, "input.bin", nil)
	pseudoAddr := uint64(0x40_000)
	require.NoError(t, pseudoState.Memory.SetMemoryRange(pseudoAddr, bytes.NewReader([]byte("/proc/self/maps\x00"))))
	pseudoState.Registers[17], pseudoState.Registers[11] = 56, pseudoAddr
	states = append(states, pseudoState)

	for i, state := range states {
		witness := assertFileSyscallSlowMatchesFast(t, state)
		_, got, _ := stepEVM(t, env, witness, testAddrs, uint64(i))
		want, hashErr := state.EncodeWitness().StateHash()
		require.NoError(t, hashErr)
		require.Equalf(t, want, got, "file syscall case %d", i)
	}
}
