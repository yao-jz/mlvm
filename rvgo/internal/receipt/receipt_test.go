package receipt

import (
	"encoding/binary"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/stretchr/testify/require"

	"github.com/yao-jz/mlvm/rvgo/fast"
)

func TestVerifyResultCommitmentAndTamper(t *testing.T) {
	state := fast.NewVMState()
	state.Exited = true
	answer := []byte("yes")
	var window [64]byte
	binary.LittleEndian.PutUint64(window[0:8], fast.MLResultMagic)
	binary.LittleEndian.PutUint64(window[8:16], uint64(len(answer)))
	copy(window[16:], answer)
	state.Memory.SetUnaligned(fast.MLResultBase, window[:32])
	state.Memory.SetUnaligned(fast.MLResultBase+32, window[32:])
	witness := state.EncodeWitness()
	post, err := witness.StateHash()
	require.NoError(t, err)
	pre := common.HexToHash("0x1234")
	proof := fast.MLResultProof(state)

	r := &Receipt{
		ReceiptVersion: Version,
		Profile:        "mlvm-1",
		Demo:           "test",
		PreState:       pre.Hex(),
		PostState:      post.Hex(),
		AnswerHex:      hexutil.Encode(answer),
		Guest: ArtifactDigest{
			SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			Size:   1,
		},
		Commitment: ResultCommitment{
			StateWitness: hexutil.Encode(witness),
			ResultProof:  hexutil.Encode(proof[:]),
			ClaimID:      ClaimID(pre, post, answer).Hex(),
		},
	}
	require.NoError(t, r.SealCanonical())
	require.NoError(t, r.Verify())
	first := r.CanonicalDigest
	require.NoError(t, r.SealCanonical())
	require.Equal(t, first, r.CanonicalDigest)

	r.AnswerHex = hexutil.Encode([]byte("no"))
	require.ErrorContains(t, r.Verify(), "does not match")
}
