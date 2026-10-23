package fast

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVerifyMLResultProof(t *testing.T) {
	state := NewVMState()
	state.Exited = true
	payload := []byte("verified answer")
	var window [64]byte
	binary.LittleEndian.PutUint64(window[0:8], MLResultMagic)
	binary.LittleEndian.PutUint64(window[8:16], uint64(len(payload)))
	copy(window[16:], payload)
	state.Memory.SetUnaligned(MLResultBase, window[:32])
	state.Memory.SetUnaligned(MLResultBase+32, window[32:])

	witness := state.EncodeWitness()
	proof := MLResultProof(state)
	got, err := VerifyMLResultProof(witness, proof[:])
	require.NoError(t, err)
	require.Equal(t, payload, got)

	tampered := proof
	tampered[16] ^= 1
	_, err = VerifyMLResultProof(witness, tampered[:])
	require.ErrorContains(t, err, "does not match")
}
