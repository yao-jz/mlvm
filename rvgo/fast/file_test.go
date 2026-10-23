package fast

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestFileSystemCommitmentIsCanonical(t *testing.T) {
	fileA := &FileInfo{Name: "a.bin", Size: 3, Addr: DefaultFileBase}
	fileB := &FileInfo{Name: "b.bin", Size: 5, Addr: DefaultFileBase + FileRegionSize}

	left := NewFileSystem()
	left.files[fileA.Name] = fileA
	left.files[fileB.Name] = fileB
	left.descriptor2Name[101] = fileB.Name
	left.descriptor2Off[101] = 4
	left.descriptor2Name[100] = fileA.Name
	left.descriptor2Off[100] = 2
	left.fileNum = 102

	right := NewFileSystem()
	right.files[fileB.Name] = fileB
	right.files[fileA.Name] = fileA
	right.descriptor2Name[100] = fileA.Name
	right.descriptor2Off[100] = 2
	right.descriptor2Name[101] = fileB.Name
	right.descriptor2Off[101] = 4
	right.fileNum = 102

	require.Equal(t, left.Commitment(DefaultFileBase+2*FileRegionSize),
		right.Commitment(DefaultFileBase+2*FileRegionSize))
}

func TestFileSystemCommitmentBindsManifestAndDescriptorState(t *testing.T) {
	left := NewVMState()
	right := NewVMState()
	require.NoError(t, left.FileSystem.AddFile("left.bin", 8, left))
	require.NoError(t, right.FileSystem.AddFile("right.bin", 8, right))

	// The file bytes are both sparse zeros, so only the metadata distinguishes the states.
	require.Equal(t, left.Memory.MerkleRoot(), right.Memory.MerkleRoot())
	require.NotEqual(t,
		left.EncodeWitness()[FileSystemRootOffset:],
		right.EncodeWitness()[FileSystemRootOffset:])

	fd, err := left.FileSystem.Open("left.bin")
	require.NoError(t, err)
	require.NoError(t, left.FileSystem.SetDescriptorOffset(fd, 1))
	before := left.FileSystem.Commitment(left.FilePointer)
	require.NoError(t, left.FileSystem.SetDescriptorOffset(fd, 2))
	require.NotEqual(t, before, left.FileSystem.Commitment(left.FilePointer))
}

func TestFileSystemProofAuthenticatesAndUpdatesDescriptor(t *testing.T) {
	state := NewVMState()
	require.NoError(t, state.FileSystem.AddFile("input.bin", 64, state))
	fd, err := state.FileSystem.Open("input.bin")
	require.NoError(t, err)
	require.NoError(t, state.FileSystem.SetDescriptorOffset(fd, 7))

	preRoot := state.FileSystem.Commitment(state.FilePointer)
	proof, err := state.FileSystem.Proof(state.FilePointer, "input.bin", firstStagedFileDescriptor)
	require.NoError(t, err)
	require.NoError(t, proof.Verify(preRoot, firstStagedFileDescriptor))
	require.Equal(t, stagedFileID("input.bin"), proof.FileID)
	require.Equal(t, proof.FileID, proof.DescriptorFileID)
	require.Equal(t, uint64(7), proof.DescriptorOffset)

	wire := proof.Encode()
	require.Len(t, wire, FileSystemProofSize)
	decoded, err := DecodeFileSystemProof(wire, firstStagedFileDescriptor)
	require.NoError(t, err)
	require.NoError(t, decoded.Verify(preRoot, firstStagedFileDescriptor))
	require.Error(t, decoded.Verify(preRoot, firstStagedFileDescriptor+1))

	want, err := proof.UpdatedRoot(firstStagedFileDescriptor, proof.NextFD, proof.DescriptorFileID, 19)
	require.NoError(t, err)
	require.NoError(t, state.FileSystem.SetDescriptorOffset(firstStagedFileDescriptor, 19))
	require.Equal(t, state.FileSystem.Commitment(state.FilePointer), want)
}

func TestFileSystemProofRejectsTampering(t *testing.T) {
	state := NewVMState()
	require.NoError(t, state.FileSystem.AddFile("input.bin", 64, state))
	root := state.FileSystem.Commitment(state.FilePointer)
	proof, err := state.FileSystem.Proof(state.FilePointer, "input.bin", firstStagedFileDescriptor)
	require.NoError(t, err)
	require.NoError(t, proof.Verify(root, firstStagedFileDescriptor))

	proof.FileSize++
	require.ErrorContains(t, proof.Verify(root, firstStagedFileDescriptor), "file Merkle branch")
	proof.FileSize--
	proof.DescriptorProof[0][0] ^= 1
	require.ErrorContains(t, proof.Verify(root, firstStagedFileDescriptor), "descriptor Merkle branch")
}

func TestFileSystemOpenProofAuthenticatesEmptyNextDescriptor(t *testing.T) {
	state := NewVMState()
	require.NoError(t, state.FileSystem.AddFile("input.bin", 3, state))
	root := state.FileSystem.Commitment(state.FilePointer)
	proof, err := state.FileSystem.Proof(state.FilePointer, "input.bin", firstStagedFileDescriptor)
	require.NoError(t, err)
	require.Zero(t, proof.DescriptorFileID)
	require.NoError(t, proof.Verify(root, firstStagedFileDescriptor))

	postRoot, err := proof.UpdatedRoot(firstStagedFileDescriptor, firstStagedFileDescriptor+1, proof.FileID, 0)
	require.NoError(t, err)
	fd, err := state.FileSystem.Open("input.bin")
	require.NoError(t, err)
	require.Equal(t, uint64(firstStagedFileDescriptor), fd)
	require.Equal(t, state.FileSystem.Commitment(state.FilePointer), postRoot)
}

func TestFileSystemProofAuthenticatesMissingDescriptorWithFiles(t *testing.T) {
	state := NewVMState()
	require.NoError(t, state.FileSystem.AddFile("input.bin", 3, state))
	root := state.FileSystem.Commitment(state.FilePointer)
	proof, err := state.FileSystem.Proof(state.FilePointer, "", firstStagedFileDescriptor+7)
	require.NoError(t, err)
	require.NotZero(t, proof.FileID)
	require.Zero(t, proof.DescriptorFileID)
	require.NoError(t, proof.Verify(root, firstStagedFileDescriptor+7))
}

func TestFileSystemJSONRoundTripPreservesStateHash(t *testing.T) {
	state := NewVMState()
	require.NoError(t, state.FileSystem.AddFile("model.bin", 16, state))
	fd, err := state.FileSystem.Open("model.bin")
	require.NoError(t, err)
	require.NoError(t, state.FileSystem.SetDescriptorOffset(fd, 7))

	wantHash, err := state.EncodeWitness().StateHash()
	require.NoError(t, err)
	raw, err := json.Marshal(state)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"version":2`)
	require.Contains(t, string(raw), `"offset":7`)

	var restored VMState
	require.NoError(t, json.Unmarshal(raw, &restored))
	gotHash, err := restored.EncodeWitness().StateHash()
	require.NoError(t, err)
	require.Equal(t, wantHash, gotHash)
	require.Equal(t, "model.bin", restored.FileSystem.descriptor2Name[100])
	require.Equal(t, uint64(7), restored.FileSystem.descriptor2Off[100])
	require.Equal(t, uint64(101), restored.FileSystem.fileNum)
}

func TestFileSystemJSONRejectsDanglingDescriptor(t *testing.T) {
	var fs FileSystem
	err := json.Unmarshal([]byte(`{
		"version": 2,
        "files": [],
        "descriptors": [{"fd": 100, "name": "missing.bin", "offset": 0}],
        "nextFd": 101
    }`), &fs)
	require.ErrorContains(t, err, `refers to unstaged file "missing.bin"`)
}

func TestFileSystemJSONRequiresCurrentVersion(t *testing.T) {
	for _, raw := range []string{`{}`, `{"version": 1, "files": [], "descriptors": [], "nextFd": 100}`} {
		var fs FileSystem
		err := json.Unmarshal([]byte(raw), &fs)
		require.ErrorContains(t, err, "unsupported file-system encoding version")
	}
}

func TestLoadFileFailureDoesNotMutateManifest(t *testing.T) {
	state := NewVMState()
	beforePointer := state.FilePointer
	err := LoadFile([]string{filepath.Join(t.TempDir(), "missing.bin")}, state)
	require.Error(t, err)
	require.Empty(t, state.FileSystem.files)
	require.Equal(t, beforePointer, state.FilePointer)
}

func TestStateWitnessIncludesFileSystemRoot(t *testing.T) {
	state := NewVMState()
	witness := state.EncodeWitness()
	require.Len(t, witness, StateWitnessSize)
	root := state.FileSystem.Commitment(state.FilePointer)
	require.Equal(t, root[:], []byte(witness[FileSystemRootOffset:]))
}

func TestAddFileRejectsUnprovableName(t *testing.T) {
	accepted := NewVMState()
	require.NoError(t, accepted.FileSystem.AddFile(strings.Repeat("x", MaxStagedFileNameLength), 0, accepted))

	state := NewVMState()
	err := state.FileSystem.AddFile(strings.Repeat("x", MaxStagedFileNameLength+1), 0, state)
	require.ErrorContains(t, err, "maximum is 255")
	require.Empty(t, state.FileSystem.files)
}

func TestFileSystemJSONRejectsOverflowingAddressRange(t *testing.T) {
	var fs FileSystem
	err := json.Unmarshal([]byte(`{
		"version": 2,
        "files": [{"name": "overflow.bin", "size": 2, "addr": 18446744073709551615}],
        "descriptors": [],
        "nextFd": 100
    }`), &fs)
	require.ErrorContains(t, err, "address range overflows uint64")
}

func TestOpenatPathMemoryIsIncludedInProof(t *testing.T) {
	state := newFileSyscallState(t, "input.bin", nil)
	pathAddr := uint64(0x10_01f) // force the pathname across a 32-byte leaf boundary
	require.NoError(t, state.Memory.SetMemoryRange(pathAddr, bytes.NewReader([]byte("input.bin\x00"))))
	state.Registers[17] = 56 // openat
	state.Registers[11] = pathAddr
	before := state.FileSystem.Commitment(state.FilePointer)

	witness, err := NewInstrumentedState(state, nil, nil, nil).Step(true)
	require.NoError(t, err)
	require.Len(t, witness.MemProof, 3*memProofSize) // instruction + two pathname leaves
	require.Len(t, witness.FileSystemProof, FileSystemProofSize)
	require.Equal(t, uint64(firstStagedFileDescriptor), state.Registers[10])
	require.Zero(t, state.Registers[11])
	require.Equal(t, "input.bin", state.FileSystem.descriptor2Name[firstStagedFileDescriptor])
	require.NotEqual(t, before, state.FileSystem.Commitment(state.FilePointer))
	assertStepFileSystemProof(t, witness, firstStagedFileDescriptor, state.FileSystem.Commitment(state.FilePointer))
}

func TestOpenatProofIsBoundedAtMaxPath(t *testing.T) {
	name := strings.Repeat("x", MaxStagedFileNameLength)
	state := newFileSyscallState(t, name, nil)
	pathAddr := uint64(0x10_01f) // worst alignment: one byte in the first leaf
	require.NoError(t, state.Memory.SetMemoryRange(pathAddr, bytes.NewReader(append([]byte(name), 0))))
	state.Registers[17] = 56 // openat
	state.Registers[11] = pathAddr

	witness, err := NewInstrumentedState(state, nil, nil, nil).Step(true)
	require.NoError(t, err)
	require.Len(t, witness.MemProof, 10*memProofSize) // instruction + nine pathname leaves
	require.Len(t, witness.FileSystemProof, FileSystemProofSize)
	require.Equal(t, uint64(firstStagedFileDescriptor), state.Registers[10])
}

func TestStagedFileReadIsBoundedAndFullyProven(t *testing.T) {
	contents := make([]byte, 64)
	for i := range contents {
		contents[i] = byte(i)
	}
	state := newFileSyscallState(t, "input.bin", contents)
	fd, err := state.FileSystem.Open("input.bin")
	require.NoError(t, err)
	require.NoError(t, state.FileSystem.SetDescriptorOffset(fd, 31)) // cross a source leaf

	dest := uint64(0x20_01f) // cross a destination leaf too
	preserved := bytes.Repeat([]byte{0xaa}, MaxStagedFileReadSize+2)
	require.NoError(t, state.Memory.SetMemoryRange(dest-1, bytes.NewReader(preserved)))
	state.Registers[17] = 63 // read
	state.Registers[10] = firstStagedFileDescriptor
	state.Registers[11] = dest
	state.Registers[12] = 4096
	before := state.FileSystem.Commitment(state.FilePointer)

	witness, err := NewInstrumentedState(state, nil, nil, nil).Step(true)
	require.NoError(t, err)
	require.Len(t, witness.MemProof, 5*memProofSize) // instruction + two source + two destination
	require.Len(t, witness.FileSystemProof, FileSystemProofSize)
	require.Equal(t, uint64(MaxStagedFileReadSize), state.Registers[10])
	require.Zero(t, state.Registers[11])
	require.Equal(t, uint64(31+MaxStagedFileReadSize), state.FileSystem.descriptor2Off[firstStagedFileDescriptor])
	require.NotEqual(t, before, state.FileSystem.Commitment(state.FilePointer))
	assertStepFileSystemProof(t, witness, firstStagedFileDescriptor, state.FileSystem.Commitment(state.FilePointer))

	got, err := io.ReadAll(state.Memory.ReadMemoryRange(dest-1, MaxStagedFileReadSize+2))
	require.NoError(t, err)
	require.Equal(t, byte(0xaa), got[0])
	require.Equal(t, contents[31:31+MaxStagedFileReadSize], got[1:1+MaxStagedFileReadSize])
	require.Equal(t, byte(0xaa), got[len(got)-1])
}

func TestStagedFileReadAtEOFOnlyNeedsInstructionProof(t *testing.T) {
	state := newFileSyscallState(t, "empty.bin", nil)
	fd, err := state.FileSystem.Open("empty.bin")
	require.NoError(t, err)
	require.Equal(t, uint64(firstStagedFileDescriptor), fd)
	state.Registers[17] = 63 // read
	state.Registers[10] = firstStagedFileDescriptor
	state.Registers[11] = 0x30_000
	state.Registers[12] = 64

	witness, err := NewInstrumentedState(state, nil, nil, nil).Step(true)
	require.NoError(t, err)
	require.Len(t, witness.MemProof, memProofSize)
	require.Len(t, witness.FileSystemProof, FileSystemProofSize)
	require.Zero(t, state.Registers[10])
	require.Zero(t, state.Registers[11])
}

func newFileSyscallState(t *testing.T, name string, contents []byte) *VMState {
	t.Helper()
	state := NewVMState()
	// ECALL, little-endian, at the initial PC.
	require.NoError(t, state.Memory.SetMemoryRange(0, bytes.NewReader([]byte{0x73, 0x00, 0x00, 0x00})))
	require.NoError(t, state.FileSystem.AddFile(name, uint64(len(contents)), state))
	if len(contents) != 0 {
		file := state.FileSystem.files[name]
		require.NoError(t, state.Memory.SetMemoryRange(file.Addr, bytes.NewReader(contents)))
	}
	return state
}

func assertStepFileSystemProof(t *testing.T, witness *StepWitness, fd uint64, postRoot common.Hash) {
	t.Helper()
	preRoot := common.BytesToHash(witness.State[FileSystemRootOffset:])
	proof, err := DecodeFileSystemProof(witness.FileSystemProof, fd)
	require.NoError(t, err)
	require.NoError(t, proof.Verify(preRoot, fd))

	fileID, offset, nextFD := proof.DescriptorFileID, proof.DescriptorOffset, proof.NextFD
	if fileID == (common.Hash{}) { // openat inserts the selected file at nextFD
		fileID = proof.FileID
		offset = 0
		nextFD++
	} else { // the read test advances by its returned byte count
		offset += MaxStagedFileReadSize
	}
	got, err := proof.UpdatedRoot(fd, nextFD, fileID, offset)
	require.NoError(t, err)
	require.Equal(t, postRoot, got)
}
