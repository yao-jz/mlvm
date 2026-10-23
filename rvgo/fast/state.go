package fast

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

// page size must be at least 32 bytes (one merkle node)
// memory merkleization will look the same regardless of page size past 32.
const (
	pageAddrSize = 10
	pageKeySize  = 64 - pageAddrSize
	pageSize     = 1 << pageAddrSize
	pageAddrMask = pageSize - 1
	maxPageCount = 1 << pageKeySize

	// DefaultRandomFileAddr is outside the ELF, heap, stack, and staged-file regions.
	// RandomFileSize is the deterministic input window exposed by getrandom(2).
	DefaultRandomFileAddr uint64 = 0x15_00_00_00_00_00_00_00
	RandomFileSize        uint64 = 5 * 1024 * 1024

	// StateWitnessSize and FileSystemRootOffset describe the current pre-1.0 witness
	// encoding. New fields are appended so existing offsets remain stable.
	StateWitnessSize     = 674
	FileSystemRootOffset = 642

	// StateSchemaVersion identifies the top-level JSON snapshot format. It is deliberately
	// separate from the binary witness layout: snapshots contain reconstruction metadata
	// such as memory pages and staged-file descriptors, while witnesses contain commitments.
	//
	// Version 3 is the mlvm-1 profile default. Version 2 remains readable for prerelease
	// fixtures (see ValidateSnapshot).
	StateSchemaVersion   uint64 = 3
	StateSchemaVersionV2 uint64 = 2
)

type VMState struct {
	SchemaVersion uint64 `json:"schemaVersion"`

	Memory *Memory `json:"memory"`

	PreimageKey    [32]byte `json:"preimageKey"`
	PreimageOffset uint64   `json:"preimageOffset"`

	PC uint64 `json:"pc"`

	//0xF14: mhartid  - riscv tests use this. Always hart 0, no parallelism supported
	//CSR [4096]uint64 // 12 bit addressing space

	ExitCode uint8 `json:"exit"`
	Exited   bool  `json:"exited"`

	Step uint64 `json:"step"`

	Heap uint64 `json:"heap"` // for mmap to keep allocating new anon memory

	LoadReservation uint64 `json:"loadReservation"`

	Registers [32]uint64 `json:"registers"`

	FRegisters [32]uint64 `json:"fregisters"`

	// LastHint is optional metadata, and not part of the VM state itself.
	// It is used to remember the last pre-image hint,
	// so a VM can start from any state without fetching prior pre-images,
	// and instead just repeat the last hint on setup,
	// to make sure pre-image requests can be served.
	// The first 4 bytes are a uin32 length prefix.
	// Warning: the hint MAY NOT BE COMPLETE. I.e. this is buffered,
	// and should only be read when len(LastHint) > 4 && uint32(LastHint[:4]) >= len(LastHint[4:])
	LastHint hexutil.Bytes `json:"lastHint,omitempty"`

	// FileSystem holds staged-file metadata outside Merkle memory. EncodeWitness commits its
	// canonical digest; file bytes themselves live in Memory.
	FileSystem  *FileSystem `json:"fileSystem"`
	FilePointer uint64      `json:"filePointer"`

	// Random Generator, need to store in the state memory
	RandomFileAddr   uint64 `json:"randomFileAddr"`
	RandomFileOffset uint64 `json:"randomFileOffset"`

	// FCSR is the floating-point control and status register: fflags in bits [4:0] (NX, UF,
	// OF, DZ, NV) and the rounding mode frm in bits [7:5]. It is architectural state - a
	// program can read it with `frflags` and write it with `fscsr` - so it has to be part of
	// the commitment, or a dispute over an flag read could not be settled.
	FCSR uint64 `json:"fcsr"`

	// map of operations to count
	OpCount OpCounts `json:"opCount"`
}

var snapshotFields = map[string]bool{
	"schemaVersion":    true,
	"memory":           true,
	"preimageKey":      true,
	"preimageOffset":   true,
	"pc":               true,
	"exit":             true,
	"exited":           true,
	"step":             true,
	"heap":             true,
	"loadReservation":  true,
	"registers":        true,
	"fregisters":       true,
	"lastHint":         true,
	"fileSystem":       true,
	"filePointer":      true,
	"randomFileAddr":   true,
	"randomFileOffset": true,
	"fcsr":             true,
	"opCount":          true,
}

var snapshotV3RequiredFields = []string{
	"schemaVersion",
	"memory",
	"preimageKey",
	"preimageOffset",
	"pc",
	"exit",
	"exited",
	"step",
	"heap",
	"loadReservation",
	"registers",
	"fregisters",
	"fileSystem",
	"filePointer",
	"randomFileAddr",
	"randomFileOffset",
	"fcsr",
}

// UnmarshalJSON keeps v2 readable while making mlvm.snapshot/3 strict: every
// consensus field must be present and unknown fields are rejected.
func (state *VMState) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	var version uint64
	if raw, ok := fields["schemaVersion"]; ok {
		if err := json.Unmarshal(raw, &version); err != nil {
			return fmt.Errorf("invalid schemaVersion: %w", err)
		}
	}
	if version >= StateSchemaVersion {
		for name := range fields {
			if !snapshotFields[name] {
				return fmt.Errorf("unknown snapshot field %q", name)
			}
		}
		for _, name := range snapshotV3RequiredFields {
			if _, ok := fields[name]; !ok {
				return fmt.Errorf("snapshot v3 missing required field %q", name)
			}
		}
	}

	type stateAlias VMState
	var decoded stateAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*state = VMState(decoded)
	return nil
}

func NewVMState() *VMState {
	return &VMState{
		SchemaVersion:    StateSchemaVersion,
		Memory:           NewMemory(),
		Heap:             1 << 28, // 0.25 GiB of program code space
		FileSystem:       NewFileSystem(),
		FilePointer:      DefaultFileBase,
		RandomFileAddr:   DefaultRandomFileAddr,
		RandomFileOffset: 0,
	}
}

// ValidateSnapshot rejects ambiguous or unsupported JSON state before it is executed or
// converted into a witness. In particular, pre-versioning snapshots must not silently
// acquire the zero values of fields introduced later.
func (state *VMState) ValidateSnapshot() error {
	if state == nil {
		return fmt.Errorf("state snapshot is null")
	}
	if state.SchemaVersion == 0 {
		return fmt.Errorf("state snapshot has no schemaVersion; regenerate it with the current mlvm load-elf")
	}
	switch state.SchemaVersion {
	case StateSchemaVersionV2, StateSchemaVersion:
		// ok
	default:
		return fmt.Errorf("unsupported state schemaVersion %d; this build supports versions %d and %d",
			state.SchemaVersion, StateSchemaVersionV2, StateSchemaVersion)
	}
	if state.Memory == nil {
		return fmt.Errorf("state snapshot has null memory")
	}
	if state.FileSystem == nil {
		return fmt.Errorf("state snapshot has null fileSystem")
	}
	if state.SchemaVersion >= StateSchemaVersion {
		if state.Heap == 0 {
			return fmt.Errorf("state snapshot v3 requires non-zero heap")
		}
		if state.RandomFileAddr == 0 {
			return fmt.Errorf("state snapshot v3 requires randomFileAddr")
		}
		if state.FilePointer == 0 {
			return fmt.Errorf("state snapshot v3 requires filePointer")
		}
	}
	return nil
}

// LoadRandomNumberFile maps a blob of pre-generated bytes at RandomFileAddr, which is
// what the getrandom(2) syscall serves from.
//
// The VM cannot call the host's RNG: two parties replaying the same trace have to observe
// the same "random" bytes or they will disagree about a state that neither of them got
// wrong. So randomness is an input to the execution, committed to like any other memory,
// and getrandom just walks a cursor through it. See docs/SYSCALLS.md#randomness.
func (state *VMState) LoadRandomNumberFile(name string) error {
	// An omitted blob means deterministic zero-filled randomness. Sparse memory already
	// reads as zero, so there is nothing to allocate or commit here. Callers that need a
	// different deterministic input can provide an explicit file.
	if name == "" {
		tracef("using zero-filled deterministic randomness at addr = %x\n", state.RandomFileAddr)
		return nil
	}
	file, err := os.Open(name)
	if err != nil {
		return fmt.Errorf("failed to open randomness file %q: %w (fix the path or omit --random-file for deterministic zeros)", name, err)
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat randomness file %q: %w", name, err)
	}
	fileSize := uint64(fileInfo.Size())
	if fileSize > RandomFileSize {
		return fmt.Errorf("randomness file %q is %d bytes; maximum is %d bytes", name, fileSize, RandomFileSize)
	}
	tracef("loading random number file: %s, fileSize = %v, to addr = %x\n", name, fileSize, state.RandomFileAddr)
	if err := state.Memory.SetMemoryRange(state.RandomFileAddr, file); err != nil {
		return fmt.Errorf("failed to read randomness file %q: %w", name, err)
	}
	return nil
}

func (state *VMState) ReadFileRange(name string, offset, length uint64) io.Reader {
	file, ok := state.FileSystem.files[name]
	if !ok {
		panic("file not found")
	}
	if offset+length > file.Size {
		panic("read file out of bounds")
	}
	return &memReader{state.Memory, file.Addr + offset, length}
}

func (state *VMState) GetStep() uint64 { return state.Step }

func (state *VMState) EncodeWitness() StateWitness {
	out := make([]byte, 0, StateWitnessSize)
	memRoot := state.Memory.MerkleRoot()
	out = append(out, memRoot[:]...)
	out = append(out, state.PreimageKey[:]...)
	out = binary.BigEndian.AppendUint64(out, state.PreimageOffset)
	out = binary.BigEndian.AppendUint64(out, state.PC)
	out = append(out, state.ExitCode)
	if state.Exited {
		out = append(out, 1)
	} else {
		out = append(out, 0)
	}
	out = binary.BigEndian.AppendUint64(out, state.Step)
	out = binary.BigEndian.AppendUint64(out, state.Heap)
	out = binary.BigEndian.AppendUint64(out, state.LoadReservation)
	for _, r := range state.Registers {
		out = binary.BigEndian.AppendUint64(out, r)
	}
	for _, r := range state.FRegisters {
		out = binary.BigEndian.AppendUint64(out, r)
	}
	// somewhere else use this witness?
	out = binary.BigEndian.AppendUint64(out, state.RandomFileAddr)
	out = binary.BigEndian.AppendUint64(out, state.RandomFileOffset)
	// Appended here so every earlier field offset remains stable.
	out = binary.BigEndian.AppendUint64(out, state.FCSR)
	// File bytes live in committed memory. This digest binds the host-side manifest and open
	// descriptor state that determines how staged-file syscalls interpret those bytes.
	fileSystemRoot := state.FileSystem.Commitment(state.FilePointer)
	out = append(out, fileSystemRoot[:]...)
	if len(out) != StateWitnessSize {
		panic(fmt.Sprintf("state witness encoding has %d bytes, expected %d", len(out), StateWitnessSize))
	}
	return out
}

func (state *VMState) Instr() uint32 { // for debugging and testing
	var out [4]byte
	state.Memory.GetUnaligned(state.PC, out[:])
	return binary.LittleEndian.Uint32(out[:])
}

type StateWitness []byte

const (
	VMStatusValid      = 0
	VMStatusInvalid    = 1
	VMStatusPanic      = 2
	VMStatusUnfinished = 3
)

// StateHash returns the hash of the state witness.
// The first byte of the hash is the VM status.
// return value is 32 bytes
func (sw StateWitness) StateHash() (common.Hash, error) {
	if err := sw.Validate(); err != nil {
		return common.Hash{}, err
	}
	offset := 32 + 32 + 8 + 8 // mem-root, preimage-key, preimage-offset, PC
	hash := crypto.Keccak256Hash(sw)
	exitCode := sw[offset]
	exited := sw[offset+1]
	status := vmStatus(exited == 1, exitCode)
	hash[0] = status
	return hash, nil
}

// Validate enforces the mlvm.state-witness/1 rules: exact length and a canonical exited flag.
func (sw StateWitness) Validate() error {
	if len(sw) != StateWitnessSize {
		return fmt.Errorf("state witness has %d bytes, want exactly %d", len(sw), StateWitnessSize)
	}
	exited := sw[81]
	if exited != 0 && exited != 1 {
		return fmt.Errorf("state witness exited flag must be 0 or 1, got %d", exited)
	}
	return nil
}

func vmStatus(exited bool, exitCode uint8) uint8 {
	if !exited {
		return VMStatusUnfinished
	}
	switch exitCode {
	case 0:
		return VMStatusValid
	case 1:
		return VMStatusInvalid
	default:
		return VMStatusPanic
	}
}
