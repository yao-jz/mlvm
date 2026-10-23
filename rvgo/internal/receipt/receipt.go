// Package receipt defines the machine-readable execution receipt emitted by `mlvm demo`.
package receipt

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/yao-jz/mlvm/rvgo/fast"
)

const Version = 1

type ArtifactDigest struct {
	GuestPath string `json:"guestPath,omitempty"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
}

type ResultCommitment struct {
	// StateWitness is the final 674-byte mlvm.state-witness/1 value.
	StateWitness string `json:"stateWitness"`
	// ResultProof is the 1,920-byte Merkle branch for MLResultBase.
	ResultProof string `json:"resultProof"`
	// ClaimID matches MLClaim.sol claimId(prestate, poststate, answer).
	ClaimID string `json:"claimId"`
}

type Receipt struct {
	ReceiptVersion int      `json:"receiptVersion"`
	Profile        string   `json:"profile"`
	Demo           string   `json:"demo"` // "mlp" or "decoder"
	Prompt         string   `json:"prompt,omitempty"`
	GeneratedText  string   `json:"generatedText,omitempty"`
	Tokens         []uint16 `json:"tokens,omitempty"`

	PreState  string `json:"preState"`
	PostState string `json:"postState"`
	Steps     uint64 `json:"steps"`
	ExitCode  uint8  `json:"exitCode"`
	AnswerHex string `json:"answerHex"`

	Guest   ArtifactDigest   `json:"guest"`
	Inputs  []ArtifactDigest `json:"inputs,omitempty"`
	Request *ArtifactDigest  `json:"request,omitempty"`

	Commitment ResultCommitment `json:"commitment"`

	Verifier struct {
		Mode   string `json:"mode"` // "result-proof" | "result-proof+slow-step" | "none"
		Passed bool   `json:"passed"`
		Detail string `json:"detail,omitempty"`
	} `json:"verifier"`

	// CanonicalDigest is sha256 of CanonicalJSON. It excludes CreatedAt, verifier
	// detail, and generatedText (stdout display only; committed output is Tokens).
	CanonicalDigest string `json:"canonicalDigest,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
}

func (r *Receipt) JSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

type canonicalBody struct {
	ReceiptVersion int              `json:"receiptVersion"`
	Profile        string           `json:"profile"`
	Demo           string           `json:"demo"`
	Prompt         string           `json:"prompt,omitempty"`
	Tokens         []uint16         `json:"tokens,omitempty"`
	PreState       string           `json:"preState"`
	PostState      string           `json:"postState"`
	Steps          uint64           `json:"steps"`
	ExitCode       uint8            `json:"exitCode"`
	AnswerHex      string           `json:"answerHex"`
	Guest          ArtifactDigest   `json:"guest"`
	Inputs         []ArtifactDigest `json:"inputs,omitempty"`
	Request        *ArtifactDigest  `json:"request,omitempty"`
	ClaimID        string           `json:"claimId"`
}

// SealCanonical fills CanonicalDigest from the consensus fields.
func (r *Receipt) SealCanonical() error {
	digest, err := r.computeCanonicalDigest()
	if err != nil {
		return err
	}
	r.CanonicalDigest = digest
	return nil
}

func (r *Receipt) computeCanonicalDigest() (string, error) {
	if r == nil {
		return "", fmt.Errorf("receipt is null")
	}
	body := canonicalBody{
		ReceiptVersion: r.ReceiptVersion,
		Profile:        r.Profile,
		Demo:           r.Demo,
		Prompt:         r.Prompt,
		Tokens:         r.Tokens,
		PreState:       r.PreState,
		PostState:      r.PostState,
		Steps:          r.Steps,
		ExitCode:       r.ExitCode,
		AnswerHex:      r.AnswerHex,
		Guest:          r.Guest,
		Inputs:         r.Inputs,
		Request:        r.Request,
		ClaimID:        r.Commitment.ClaimID,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// ClaimID matches MLClaim.sol:
// keccak256(abi.encode(preState, postState, keccak256(answer))).
func ClaimID(preState, postState common.Hash, answer []byte) common.Hash {
	answerHash := crypto.Keccak256Hash(answer)
	return crypto.Keccak256Hash(preState[:], postState[:], answerHash[:])
}

// Verify checks the receipt's self-contained poststate/result commitment. It proves that
// AnswerHex is in the claimed halted poststate. It does not prove the whole execution from
// PreState to PostState; that remains the dispute protocol's job.
func (r *Receipt) Verify() error {
	if r == nil {
		return fmt.Errorf("receipt is null")
	}
	if r.ReceiptVersion != Version {
		return fmt.Errorf("unsupported receiptVersion %d", r.ReceiptVersion)
	}
	if r.Profile != "mlvm-1" {
		return fmt.Errorf("unsupported profile %q", r.Profile)
	}
	pre, err := parseHash("preState", r.PreState)
	if err != nil {
		return err
	}
	post, err := parseHash("postState", r.PostState)
	if err != nil {
		return err
	}
	answer, err := hexutil.Decode(r.AnswerHex)
	if err != nil {
		return fmt.Errorf("answerHex: %w", err)
	}
	witnessBytes, err := hexutil.Decode(r.Commitment.StateWitness)
	if err != nil {
		return fmt.Errorf("commitment.stateWitness: %w", err)
	}
	witness := fast.StateWitness(witnessBytes)
	witnessHash, err := witness.StateHash()
	if err != nil {
		return fmt.Errorf("commitment.stateWitness: %w", err)
	}
	if witnessHash != post {
		return fmt.Errorf("postState does not match committed state witness")
	}
	proof, err := hexutil.Decode(r.Commitment.ResultProof)
	if err != nil {
		return fmt.Errorf("commitment.resultProof: %w", err)
	}
	committedAnswer, err := fast.VerifyMLResultProof(witness, proof)
	if err != nil {
		return fmt.Errorf("commitment.resultProof: %w", err)
	}
	if !bytes.Equal(committedAnswer, answer) {
		return fmt.Errorf("answerHex does not match bytes committed in postState")
	}
	wantClaim := ClaimID(pre, post, answer)
	if r.Commitment.ClaimID != wantClaim.Hex() {
		return fmt.Errorf("claimId does not bind this prestate, poststate, and answer")
	}
	wantCanon, err := r.computeCanonicalDigest()
	if err != nil {
		return err
	}
	if r.CanonicalDigest != "" && r.CanonicalDigest != wantCanon {
		return fmt.Errorf("canonicalDigest does not match consensus fields")
	}
	if err := validateArtifact("guest", r.Guest); err != nil {
		return err
	}
	for i, input := range r.Inputs {
		if err := validateArtifact(fmt.Sprintf("inputs[%d]", i), input); err != nil {
			return err
		}
	}
	if r.Request != nil {
		if err := validateArtifact("request", *r.Request); err != nil {
			return err
		}
	}
	return nil
}

func parseHash(label, value string) (common.Hash, error) {
	if len(value) != 66 || !strings.HasPrefix(value, "0x") {
		return common.Hash{}, fmt.Errorf("%s must be a 0x-prefixed 32-byte hash", label)
	}
	raw, err := hexutil.Decode(value)
	if err != nil {
		return common.Hash{}, fmt.Errorf("%s: %w", label, err)
	}
	return common.BytesToHash(raw), nil
}

func validateArtifact(label string, artifact ArtifactDigest) error {
	if artifact.Size <= 0 {
		return fmt.Errorf("%s size must be positive", label)
	}
	if len(artifact.SHA256) != sha256.Size*2 || artifact.SHA256 != strings.ToLower(artifact.SHA256) {
		return fmt.Errorf("%s sha256 must be 64 lowercase hexadecimal characters", label)
	}
	if _, err := hex.DecodeString(artifact.SHA256); err != nil {
		return fmt.Errorf("%s sha256: %w", label, err)
	}
	return nil
}
