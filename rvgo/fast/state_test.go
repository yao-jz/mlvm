package fast

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

type stateCompatibilityExpectation struct {
	WitnessSize    int    `json:"witnessSize"`
	StateHash      string `json:"stateHash"`
	MemoryRoot     string `json:"memoryRoot"`
	FileSystemRoot string `json:"fileSystemRoot"`
}

func TestStateV2CompatibilityFixture(t *testing.T) {
	stateJSON, err := os.ReadFile("testdata/state-v2.json")
	require.NoError(t, err)
	var state VMState
	require.NoError(t, json.Unmarshal(stateJSON, &state))
	require.NoError(t, state.ValidateSnapshot())

	expectedJSON, err := os.ReadFile("testdata/state-v2.expected.json")
	require.NoError(t, err)
	var expected stateCompatibilityExpectation
	require.NoError(t, json.Unmarshal(expectedJSON, &expected))

	witness := state.EncodeWitness()
	require.Len(t, witness, expected.WitnessSize)
	stateHash, err := witness.StateHash()
	require.NoError(t, err)
	require.Equal(t, expected.StateHash, stateHash.Hex())
	require.Equal(t, expected.MemoryRoot, common.Hash(state.Memory.MerkleRoot()).Hex())
	require.Equal(t, expected.FileSystemRoot, state.FileSystem.Commitment(state.FilePointer).Hex())
}

func TestStateSnapshotSchemaValidation(t *testing.T) {
	state := NewVMState()
	require.Equal(t, uint64(3), StateSchemaVersion)
	require.Equal(t, StateSchemaVersion, state.SchemaVersion)
	require.NoError(t, state.ValidateSnapshot())

	encoded, err := json.Marshal(state)
	require.NoError(t, err)

	var restored VMState
	require.NoError(t, json.Unmarshal(encoded, &restored))
	require.NoError(t, restored.ValidateSnapshot())

	restored.SchemaVersion = 0
	require.ErrorContains(t, restored.ValidateSnapshot(), "has no schemaVersion")
	restored.SchemaVersion = StateSchemaVersion + 1
	require.ErrorContains(t, restored.ValidateSnapshot(), "unsupported state schemaVersion")

	// v3 rejects zeroed consensus fields that v2 allowed to slip through.
	v3 := NewVMState()
	v3.Heap = 0
	require.ErrorContains(t, v3.ValidateSnapshot(), "non-zero heap")
}

func TestStateSnapshotV3StrictJSON(t *testing.T) {
	state := NewVMState()
	encoded, err := json.Marshal(state)
	require.NoError(t, err)

	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &fields))

	delete(fields, "fcsr")
	missing, err := json.Marshal(fields)
	require.NoError(t, err)
	var restored VMState
	require.ErrorContains(t, json.Unmarshal(missing, &restored), `missing required field "fcsr"`)

	require.NoError(t, json.Unmarshal(encoded, &fields))
	fields["surprise"] = json.RawMessage(`true`)
	unknown, err := json.Marshal(fields)
	require.NoError(t, err)
	require.ErrorContains(t, json.Unmarshal(unknown, &restored), `unknown snapshot field "surprise"`)
}

func TestStateSnapshotV2AllowsLegacyUnknownJSON(t *testing.T) {
	raw, err := os.ReadFile("testdata/state-v2.json")
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &fields))
	fields["legacyMetadata"] = json.RawMessage(`{"ignored":true}`)
	encoded, err := json.Marshal(fields)
	require.NoError(t, err)

	var restored VMState
	require.NoError(t, json.Unmarshal(encoded, &restored))
	require.NoError(t, restored.ValidateSnapshot())
}

func TestStateSnapshotSchemaRejectsMissingStructures(t *testing.T) {
	state := NewVMState()
	state.Memory = nil
	require.ErrorContains(t, state.ValidateSnapshot(), "null memory")

	state.Memory = NewMemory()
	state.FileSystem = nil
	require.ErrorContains(t, state.ValidateSnapshot(), "null fileSystem")
}

func TestDefaultRandomnessIsSparseAndDeterministic(t *testing.T) {
	state := NewVMState()

	require.NoError(t, state.LoadRandomNumberFile(""))
	require.Equal(t, DefaultRandomFileAddr, state.RandomFileAddr)
	require.Equal(t, uint64(0), state.RandomFileOffset)
	var got [8]byte
	state.Memory.GetUnaligned(state.RandomFileAddr, got[:])
	require.Equal(t, [8]byte{}, got)
	require.Zero(t, state.Memory.PageCount())
}

func TestRandomnessFileIsMappedAtCanonicalAddress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "random.bin")
	want := []byte{0xde, 0xad, 0xbe, 0xef, 0x01}
	require.NoError(t, os.WriteFile(path, want, 0o600))

	state := NewVMState()
	require.NoError(t, state.LoadRandomNumberFile(path))
	require.Equal(t, DefaultRandomFileAddr, state.RandomFileAddr)
	got := make([]byte, len(want))
	state.Memory.GetUnaligned(DefaultRandomFileAddr, got)
	require.Equal(t, want, got)
	require.Equal(t, 1, state.Memory.PageCount())
}

func TestRandomnessFileRejectsBytesOutsideExposedWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "too-large.bin")
	require.NoError(t, os.WriteFile(path, nil, 0o600))
	require.NoError(t, os.Truncate(path, int64(RandomFileSize+1)))

	state := NewVMState()
	err := state.LoadRandomNumberFile(path)
	require.ErrorContains(t, err, "maximum is 5242880 bytes")
	require.Zero(t, state.Memory.PageCount())
}
