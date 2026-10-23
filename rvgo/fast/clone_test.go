package fast

import (
	"bytes"
	"encoding/binary"
	"io"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"
)

// loopState is a three-instruction program that keeps changing a register and a memory
// word: addi x1,x1,1; sd x1,0(x2); jal x0,-8.
func loopState(t *testing.T) *VMState {
	t.Helper()
	state := NewVMState()
	state.PC = 0x1000
	state.Registers[2] = 0x2000
	var prog [12]byte
	binary.LittleEndian.PutUint32(prog[0:], 0x00108093)
	binary.LittleEndian.PutUint32(prog[4:], 0x00113023)
	binary.LittleEndian.PutUint32(prog[8:], 0xff9ff06f)
	state.Memory.SetUnaligned(0x1000, prog[:])
	return state
}

func stepN(t *testing.T, state *VMState, n int) {
	t.Helper()
	inst := NewInstrumentedState(state, nil, io.Discard, io.Discard)
	for i := 0; i < n; i++ {
		_, err := inst.Step(false)
		require.NoError(t, err)
	}
}

func stateHash(t *testing.T, state *VMState) [32]byte {
	t.Helper()
	h, err := state.EncodeWitness().StateHash()
	require.NoError(t, err)
	return h
}

func TestCloneEvolvesIndependently(t *testing.T) {
	original := loopState(t)
	stepN(t, original, 10)
	copied := original.Clone()
	require.Equal(t, stateHash(t, original), stateHash(t, copied))

	// The copy and the original take the same path when stepped the same amount.
	stepN(t, original, 9)
	stepN(t, copied, 9)
	require.Equal(t, stateHash(t, original), stateHash(t, copied))

	// Stepping only the copy must not move the original, including through the
	// Merkle caches that both started with.
	before := stateHash(t, original)
	stepN(t, copied, 3)
	require.Equal(t, before, stateHash(t, original))
	require.NotEqual(t, before, stateHash(t, copied))

	// And writes to the original must not leak into the copy.
	afterCopy := stateHash(t, copied)
	original.Memory.SetUnaligned(0x2000, []byte{0xde, 0xad})
	require.Equal(t, afterCopy, stateHash(t, copied))
}

func TestCloneCopiesFileSystem(t *testing.T) {
	state := loopState(t)
	require.NoError(t, StageFileBytes("model.bin", []byte("weights"), state))
	copied := state.Clone()
	require.Equal(t, stateHash(t, state), stateHash(t, copied))

	_, err := copied.FileSystem.Open("model.bin")
	require.NoError(t, err)
	require.NotEqual(t, stateHash(t, state), stateHash(t, copied))
	_, _, open := state.FileSystem.Descriptor(firstStagedFileDescriptor)
	require.False(t, open, "opening a file in the copy must not open it in the original")
}

// rebuild returns a fresh Memory holding the same bytes as m, without any page or node
// shared with it, so its root is computed from scratch.
func rebuild(t *testing.T, m *Memory) *Memory {
	t.Helper()
	out := NewMemory()
	require.NoError(t, m.ForEachPage(func(pageIndex uint64, page *Page) error {
		return out.SetMemoryRange(pageIndex<<PageAddrSize, bytes.NewReader(page[:]))
	}))
	return out
}

func TestCloneSharesPagesUntilWritten(t *testing.T) {
	m := NewMemory()
	for i := uint64(0); i < 8; i++ {
		m.SetUnaligned(i*PageSize+5, []byte{byte(i + 1)})
	}
	_ = m.MerkleRoot()
	c := m.Clone()
	for i := uint64(0); i < 8; i++ {
		require.Same(t, m.pages[i], c.pages[i], "a fresh clone shares every page")
	}

	c.SetUnaligned(3*PageSize+7, []byte{0xaa})
	require.NotSame(t, m.pages[3], c.pages[3], "the written page is copied")
	require.Same(t, m.pages[4], c.pages[4], "pages nobody wrote stay shared")
	var b [1]byte
	m.GetUnaligned(3*PageSize+7, b[:])
	require.Zero(t, b[0], "the original does not see the clone's write")

	// The original lost ownership of the shared pages too, so its writes copy as well.
	m.SetUnaligned(4*PageSize, []byte{0xbb})
	require.NotSame(t, m.pages[4], c.pages[4])
	c.GetUnaligned(4*PageSize, b[:])
	require.Zero(t, b[0], "the clone does not see the original's write")

	// A page owned since the clone is written in place.
	owned := c.pages[3]
	c.SetUnaligned(3*PageSize+8, []byte{0xcc})
	require.Same(t, owned, c.pages[3])
}

// TestCloneFamilyRoots writes random bytes across a family of clones, each cloned from a
// random earlier member, and checks every member's root against a memory rebuilt from
// its bytes, so stale node or page caches anywhere in the family would show.
func TestCloneFamilyRoots(t *testing.T) {
	for seed := uint64(0); seed < 20; seed++ {
		testCloneFamilyRoots(t, seed)
	}
}

func testCloneFamilyRoots(t *testing.T, seed uint64) {
	rng := rand.New(rand.NewPCG(seed, 2))
	family := []*Memory{NewMemory()}
	write := func(m *Memory) {
		addr := rng.Uint64N(16 * PageSize)
		if rng.IntN(8) == 0 {
			// a range write that may span pages, through SetMemoryRange
			data := make([]byte, 1+rng.IntN(2*PageSize))
			for i := range data {
				data[i] = byte(rng.Uint32())
			}
			require.NoError(t, m.SetMemoryRange(addr, bytes.NewReader(data)))
			return
		}
		data := make([]byte, 1+rng.IntN(32))
		for i := range data {
			data[i] = byte(rng.Uint32())
		}
		m.SetUnaligned(addr, data)
	}
	for round := 0; round < 200; round++ {
		m := family[rng.IntN(len(family))]
		switch rng.IntN(4) {
		case 0:
			family = append(family, m.Clone())
		case 1:
			_ = m.MerkleRoot() // fill caches that a later write must invalidate correctly
		default:
			write(m)
		}
	}
	for i, m := range family {
		require.Equal(t, rebuild(t, m).MerkleRoot(), m.MerkleRoot(), "seed %d member %d", seed, i)
	}
}

// A write that crosses a 32-byte leaf inside a page must invalidate both leaves, or a
// root computed before it is reused for the second leaf.
func TestSetUnalignedAcrossLeaves(t *testing.T) {
	m := NewMemory()
	m.SetUnaligned(0x1000, []byte{1})
	_ = m.MerkleRoot()
	m.SetUnaligned(0x1000+30, []byte{2, 3, 4, 5})
	require.Equal(t, rebuild(t, m).MerkleRoot(), m.MerkleRoot())
}
