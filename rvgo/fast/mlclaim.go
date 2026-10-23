package fast

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// The inference claim.
//
// A fault proof settles "this machine went from state A to state B". That is not yet a claim
// about a model: nothing binds B to "the answer was Y". Printing the answer does not help,
// because stdout is not part of the state commitment - a dishonest party could print
// anything and the chain would have no way to tell.
//
// So the guest writes its answer into memory at a fixed absolute address, and the final
// memory root commits to it. A contract reads it back with a single merkle branch, exactly
// the way FloatSubGame.sol reads a float result out of a nested machine. The claim becomes
//
//	(prestate, poststate, answer)
//
// where a future dispute game could establish prestate -> poststate and MLClaim.sol reads
// `answer` out of that poststate. Changing the answer changes the memory and state roots.
//
// # What this does and does not establish
//
// It binds the answer bytes to a particular poststate. It does not establish that execution
// reached that poststate or that the prestate is authoritative. The prestate commits the
// program image, staged bytes, randomness memory, and the staged-file metadata digest, and
// can be recomputed with `mlvm ml-claim --prestate`. File syscalls are not yet mirrored by
// the slow and Solidity steppers; see docs/STATUS.md.

const (
	// MLResultBase is where a guest writes its answer. Chosen 64-byte aligned so the two
	// 32-byte merkle leaves covering the window are siblings, and one branch carries both.
	// Clear of every other MLVM region, including the float sub-game's operands at 0x3000...
	MLResultBase = 0x3100_0000_0000_0000

	// MLResultMagic is "MLVM_OUT". Without it an untouched page - which reads as zeros, and
	// therefore as a zero-length answer - would be indistinguishable from a real one.
	MLResultMagic = 0x4D4C564D5F4F5554

	// MLResultMax is the payload capacity: 64 bytes of window minus the 16-byte header.
	MLResultMax = 48
)

// MLResult reads the answer a guest committed, and reports whether it committed one at all.
//
// ok is false when the magic is absent (the guest never called mlvm_commit_result) or the
// length is out of range. Both are honest answers rather than errors: a program that does
// not commit a result simply has no claim attached to it.
func MLResult(state *VMState) (payload []byte, ok bool) {
	// Two reads, not one: GetUnaligned is capped at 32 bytes because that is a merkle leaf,
	// and the window deliberately spans two of them.
	var window [64]byte
	state.Memory.GetUnaligned(MLResultBase, window[0:32])
	state.Memory.GetUnaligned(MLResultBase+32, window[32:64])

	if binary.LittleEndian.Uint64(window[0:8]) != MLResultMagic {
		return nil, false
	}
	n := binary.LittleEndian.Uint64(window[8:16])
	if n > MLResultMax {
		return nil, false
	}
	return window[16 : 16+n], true
}

// MLResultProof is the merkle branch a contract needs to read the answer out of a committed
// state. One branch covers the whole window.
func MLResultProof(state *VMState) [ProofLen * 32]byte {
	return state.Memory.MerkleProof(MLResultBase)
}

// VerifyMLResultProof authenticates the result window against a halted state witness.
// The proof format is identical to MLClaim.sol: the first 32 bytes are the result leaf,
// followed by its 59 Merkle siblings.
func VerifyMLResultProof(stateWitness StateWitness, proof []byte) ([]byte, error) {
	if err := stateWitness.Validate(); err != nil {
		return nil, err
	}
	if stateWitness[81] != 1 {
		return nil, fmt.Errorf("machine is not halted")
	}
	if stateWitness[80] != 0 {
		return nil, fmt.Errorf("machine halted with exit code %d", stateWitness[80])
	}
	if len(proof) != ProofLen*32 {
		return nil, fmt.Errorf("result proof has %d bytes, want %d", len(proof), ProofLen*32)
	}

	var node [32]byte
	copy(node[:], proof[:32])
	path := MLResultBase >> 5
	for i := 0; i < ProofLen-1; i++ {
		var sibling [32]byte
		copy(sibling[:], proof[(i+1)*32:(i+2)*32])
		if (path>>i)&1 == 0 {
			node = HashPair(node, sibling)
		} else {
			node = HashPair(sibling, node)
		}
	}
	if !bytes.Equal(node[:], stateWitness[:32]) {
		return nil, fmt.Errorf("result proof does not match poststate memory root")
	}

	// RESULT_BASE is 64-byte aligned, so proof[1] is the sibling leaf containing
	// payload bytes 16..47.
	var window [64]byte
	copy(window[:32], proof[:32])
	copy(window[32:], proof[32:64])
	if binary.LittleEndian.Uint64(window[:8]) != MLResultMagic {
		return nil, fmt.Errorf("result magic is absent")
	}
	n := binary.LittleEndian.Uint64(window[8:16])
	if n > MLResultMax {
		return nil, fmt.Errorf("result length %d exceeds %d", n, MLResultMax)
	}
	return append([]byte(nil), window[16:16+n]...), nil
}

// MLClaim is what a party publishes: the machine it started from, the machine it ended at,
// and the answer that fell out.
//
// The prestate is what binds the claim to a model and an input; the poststate is what the
// dispute game settles; the answer is what everyone actually cares about.
type MLClaim struct {
	PreState  common.Hash   `json:"preState"`
	PostState common.Hash   `json:"postState"`
	Steps     uint64        `json:"steps"`
	ExitCode  uint8         `json:"exitCode"`
	Answer    hexutil.Bytes `json:"answer"`
}

// MakeMLClaim runs a loaded machine to completion and returns the claim it supports.
//
// maxSteps bounds the run so a guest that never halts fails here rather than hanging; pass 0
// for the default, which is generous enough for the largest model measured (6.1 billion).
func MakeMLClaim(state *VMState, maxSteps uint64) (*MLClaim, error) {
	if maxSteps == 0 {
		maxSteps = 20_000_000_000
	}
	pre, err := state.EncodeWitness().StateHash()
	if err != nil {
		return nil, fmt.Errorf("failed to hash the prestate: %w", err)
	}

	in := NewInstrumentedState(state, nil, discard{}, discard{})
	for !state.Exited {
		if state.Step >= maxSteps {
			return nil, fmt.Errorf("guest did not halt within %d steps", maxSteps)
		}
		if _, err := in.Step(false); err != nil {
			return nil, fmt.Errorf("guest failed at step %d, PC %#x: %w", state.Step, state.PC, err)
		}
	}

	post, err := state.EncodeWitness().StateHash()
	if err != nil {
		return nil, fmt.Errorf("failed to hash the poststate: %w", err)
	}
	answer, ok := MLResult(state)
	if !ok {
		return nil, fmt.Errorf("guest halted without committing a result; " +
			"it must call mlvm_commit_result (see examples/mlvm_result.h)")
	}
	return &MLClaim{
		PreState:  pre,
		PostState: post,
		Steps:     state.Step,
		ExitCode:  state.ExitCode,
		Answer:    answer,
	}, nil
}

// discard swallows guest stdout/stderr. Claims are about committed state, not about what the
// program printed.
type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
