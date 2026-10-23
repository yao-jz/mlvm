// Package e2edemo implements the one-command MLVM launch demos.
package e2edemo

import (
	"bytes"
	"crypto/sha256"
	"debug/elf"
	_ "embed"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/yao-jz/mlvm/rvgo/fast"
	"github.com/yao-jz/mlvm/rvgo/internal/dispute"
	"github.com/yao-jz/mlvm/rvgo/internal/receipt"
	"github.com/yao-jz/mlvm/rvgo/slow"
)

//go:embed assets/mlp-rv64imafd.elf
var mlpELF []byte

const (
	DefaultPrompt       = "Once upon a time"
	DefaultMaxNewTokens = 16
	MaxNewTokens        = 16
)

var ErrDecoderAssetsNotFound = errors.New("TinyStories decoder assets not found")

type Options struct {
	Prompt       string
	ModelPackDir string
	Demo         string // "decoder", "mlp", or empty for auto
	MaxNewTokens int
	JSON         bool
	VerifySlow   bool
	MaxSteps     uint64
}

type Result struct {
	Receipt *receipt.Receipt
	Text    string
}

func Run(opts Options) (*Result, error) {
	if opts.Demo == "mlp" {
		return runMLP(opts)
	}
	if opts.Demo != "" && opts.Demo != "decoder" {
		return nil, fmt.Errorf("unknown demo %q", opts.Demo)
	}
	assets, err := resolveDecoderAssets(opts.ModelPackDir)
	if err == nil {
		if err := assets.validatePinnedWeights(); err != nil {
			return nil, err
		}
		return runDecoder(opts, assets)
	}
	// A source build installed without the optional launch assets still has a useful
	// zero-dependency MLP demo. Explicit decoder intent must never silently fall back.
	if opts.Demo == "decoder" || opts.ModelPackDir != "" || opts.Prompt != "" || !errors.Is(err, ErrDecoderAssetsNotFound) {
		return nil, err
	}
	return runMLP(opts)
}

func runMLP(opts Options) (*Result, error) {
	if opts.Prompt != "" {
		return nil, fmt.Errorf("mlp demo has no prompt; omit the argument or run the default TinyStories demo")
	}
	state, guestDigest, err := loadEmbeddedELF(mlpELF, "mlp-rv64imafd.elf")
	if err != nil {
		return nil, err
	}
	pre, err := state.EncodeWitness().StateHash()
	if err != nil {
		return nil, err
	}
	claim, err := fast.MakeMLClaim(state, opts.MaxSteps)
	if err != nil {
		return nil, err
	}
	rec := &receipt.Receipt{
		ReceiptVersion: receipt.Version,
		Profile:        "mlvm-1",
		Demo:           "mlp",
		PreState:       pre.Hex(),
		PostState:      claim.PostState.Hex(),
		Steps:          claim.Steps,
		ExitCode:       claim.ExitCode,
		AnswerHex:      "0x" + hex.EncodeToString(claim.Answer),
		Guest:          guestDigest,
		CreatedAt:      time.Now().UTC(),
	}
	if err := attachResultCommitment(rec, state); err != nil {
		return nil, err
	}
	rec.Verifier.Mode = "result-proof"
	rec.Verifier.Passed = true
	rec.Verifier.Detail = "answer Merkle proof matches the halted poststate"
	if opts.VerifySlow {
		verifyState, _, err := loadEmbeddedELF(mlpELF, "mlp")
		if err != nil {
			return nil, err
		}
		if err := verifyCommitStep(verifyState); err != nil {
			rec.Verifier.Passed = false
			rec.Verifier.Mode = "result-proof+slow-step"
			rec.Verifier.Detail = err.Error()
		} else {
			rec.Verifier.Mode = "result-proof+slow-step"
			rec.Verifier.Passed = true
			rec.Verifier.Detail = "answer proof valid; MLVM_OUT commit step matched slow.Step"
		}
	}
	if err := rec.SealCanonical(); err != nil {
		return nil, err
	}
	if err := rec.Verify(); err != nil {
		return nil, fmt.Errorf("verify generated receipt: %w", err)
	}
	text := fmt.Sprintf(
		"MLVM deterministic inference demo (MLP)\n\n"+
			"  guest      %s (%d bytes)\n"+
			"  steps      %d\n"+
			"  prestate   %s\n"+
			"  poststate  %s\n"+
			"  answer     %s\n"+
			"  claim      %s\n"+
			"  verifier   %s passed=%v %s\n",
		guestDigest.SHA256, guestDigest.Size, claim.Steps, pre.Hex(), claim.PostState.Hex(),
		rec.AnswerHex, rec.Commitment.ClaimID, rec.Verifier.Mode, rec.Verifier.Passed, rec.Verifier.Detail,
	)
	return &Result{Receipt: rec, Text: text}, nil
}

type decoderAssets struct {
	ELF           []byte
	Model         []byte
	Tokenizer     []byte
	ModelPath     string
	TokenizerPath string
	ELFPath       string
	Source        string
}

func runDecoder(opts Options, assets decoderAssets) (*Result, error) {
	prompt := opts.Prompt
	if prompt == "" {
		prompt = DefaultPrompt
	}
	maxNewTokens := opts.MaxNewTokens
	if maxNewTokens == 0 {
		maxNewTokens = DefaultMaxNewTokens
	}
	requestBytes, err := buildRequest(prompt, maxNewTokens)
	if err != nil {
		return nil, err
	}

	elfBytes, modelBytes, tokenizerBytes := assets.ELF, assets.Model, assets.Tokenizer
	if len(elfBytes) == 0 || len(modelBytes) == 0 || len(tokenizerBytes) == 0 {
		return nil, fmt.Errorf("decoder assets from %s are incomplete", assets.Source)
	}

	newState := func() (*fast.VMState, receipt.ArtifactDigest, error) {
		state, guestDigest, err := loadEmbeddedELF(elfBytes, "decoder.elf")
		if err != nil {
			return nil, guestDigest, err
		}
		guestDigest.GuestPath = "decoder.elf"
		for _, file := range []struct {
			name string
			data []byte
		}{
			{name: "model.bin", data: modelBytes},
			{name: "tokenizer.bin", data: tokenizerBytes},
			{name: "request.bin", data: requestBytes},
		} {
			if err := fast.StageFileBytes(file.name, file.data, state); err != nil {
				return nil, guestDigest, err
			}
		}
		return state, guestDigest, nil
	}

	state, guestDigest, err := newState()
	if err != nil {
		return nil, err
	}
	inputDigests := []receipt.ArtifactDigest{
		digestBytes("model.bin", modelBytes),
		digestBytes("tokenizer.bin", tokenizerBytes),
	}
	requestDigest := digestBytes("request.bin", requestBytes)

	pre, err := state.EncodeWitness().StateHash()
	if err != nil {
		return nil, err
	}
	maxSteps := opts.MaxSteps
	if maxSteps == 0 {
		maxSteps = 250_000_000
	}
	var stdout bytes.Buffer
	inst := fast.NewInstrumentedState(state, nil, &stdout, discard{})
	for !state.Exited {
		if state.Step >= maxSteps {
			return nil, fmt.Errorf("decoder did not halt within %d steps", maxSteps)
		}
		if _, err := inst.Step(false); err != nil {
			return nil, err
		}
	}
	post, err := state.EncodeWitness().StateHash()
	if err != nil {
		return nil, err
	}
	answer, ok := fast.MLResult(state)
	if !ok {
		return nil, fmt.Errorf("decoder halted without committing an MLResult")
	}
	tokens, err := decodeResult(answer)
	if err != nil {
		return nil, err
	}
	text := extractGenerated(stdout.String())
	rec := &receipt.Receipt{
		ReceiptVersion: receipt.Version,
		Profile:        "mlvm-1",
		Demo:           "decoder",
		Prompt:         prompt,
		GeneratedText:  text,
		Tokens:         tokens,
		PreState:       pre.Hex(),
		PostState:      post.Hex(),
		Steps:          state.Step,
		ExitCode:       state.ExitCode,
		AnswerHex:      "0x" + hex.EncodeToString(answer),
		Guest:          guestDigest,
		Inputs:         inputDigests,
		Request:        &requestDigest,
		CreatedAt:      time.Now().UTC(),
	}
	if err := attachResultCommitment(rec, state); err != nil {
		return nil, err
	}
	rec.Verifier.Mode = "result-proof"
	rec.Verifier.Passed = true
	rec.Verifier.Detail = "answer Merkle proof matches the halted poststate"
	if opts.VerifySlow {
		verifyState, _, err := newState()
		if err != nil {
			return nil, err
		}
		if err := verifyCommitStep(verifyState); err != nil {
			rec.Verifier.Passed = false
			rec.Verifier.Mode = "result-proof+slow-step"
			rec.Verifier.Detail = err.Error()
		} else {
			rec.Verifier.Mode = "result-proof+slow-step"
			rec.Verifier.Detail = "answer proof valid; MLVM_OUT commit step matched slow.Step"
		}
	}
	if err := rec.SealCanonical(); err != nil {
		return nil, err
	}
	if err := rec.Verify(); err != nil {
		return nil, fmt.Errorf("verify generated receipt: %w", err)
	}

	story := prompt + text
	out := fmt.Sprintf(
		"MLVM  ·  TinyStories (greedy, %d tokens)\n\n"+
			"  %s\n\n"+
			"  prompt     %q\n"+
			"  generated  %q\n"+
			"  tokens     %v\n"+
			"  steps      %d\n"+
			"  prestate   %s\n"+
			"  poststate  %s\n"+
			"  claim      %s\n"+
			"  verifier   %s passed=%v  %s\n",
		len(tokens), story, prompt, text, tokens, state.Step, pre.Hex(), post.Hex(), rec.Commitment.ClaimID,
		rec.Verifier.Mode, rec.Verifier.Passed, rec.Verifier.Detail,
	)
	return &Result{Receipt: rec, Text: out}, nil
}

func loadEmbeddedELF(raw []byte, name string) (*fast.VMState, receipt.ArtifactDigest, error) {
	sum := sha256.Sum256(raw)
	digest := receipt.ArtifactDigest{
		SHA256: hex.EncodeToString(sum[:]),
		Size:   int64(len(raw)),
	}
	f, err := elf.NewFile(bytes.NewReader(raw))
	if err != nil {
		return nil, digest, fmt.Errorf("parse %s: %w", name, err)
	}
	defer f.Close()
	state, err := fast.LoadELF(f)
	if err != nil {
		return nil, digest, err
	}
	if err := fast.PatchVM(f, state); err != nil {
		return nil, digest, err
	}
	if err := state.LoadRandomNumberFile(""); err != nil {
		return nil, digest, err
	}
	return state, digest, nil
}

func writeRequest(path, prompt string, maxNew int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	p := []byte(prompt)
	if len(p) > 256 {
		return fmt.Errorf("prompt exceeds 256 bytes")
	}
	buf := make([]byte, 276)
	binary.LittleEndian.PutUint32(buf[0:], 1)
	binary.LittleEndian.PutUint32(buf[4:], uint32(maxNew))
	binary.LittleEndian.PutUint32(buf[8:], 0)
	binary.LittleEndian.PutUint32(buf[12:], 0)
	binary.LittleEndian.PutUint32(buf[16:], uint32(len(p)))
	copy(buf[20:], p)
	return os.WriteFile(path, buf, 0o644)
}

func decodeResult(answer []byte) ([]uint16, error) {
	if len(answer) < 4 {
		return nil, fmt.Errorf("MLResult too short")
	}
	if binary.LittleEndian.Uint16(answer[0:2]) != 1 {
		return nil, fmt.Errorf("unsupported decoder result version %d", binary.LittleEndian.Uint16(answer[0:2]))
	}
	count := int(binary.LittleEndian.Uint16(answer[2:4]))
	if count > MaxNewTokens {
		count = MaxNewTokens
	}
	tokens := make([]uint16, 0, count)
	for i := 0; i < count; i++ {
		off := 4 + 2*i
		if off+2 > len(answer) {
			break
		}
		tokens = append(tokens, binary.LittleEndian.Uint16(answer[off:off+2]))
	}
	return tokens, nil
}

func extractGenerated(stdout string) string {
	const marker = "generated: "
	i := strings.Index(stdout, marker)
	if i < 0 {
		return strings.TrimSpace(stdout)
	}
	rest := stdout[i+len(marker):]
	if j := strings.IndexByte(rest, '\n'); j >= 0 {
		rest = rest[:j]
	}
	// Keep leading spaces: the first generated token is often " was", and prompt+text
	// must read "The dragon was", not "The dragonwas".
	return strings.TrimRight(rest, "\r")
}

func hashFile(path string) (string, int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), int64(len(data)), nil
}

// verifyCommitStep replays a prepared prestate and cross-checks the integer store that
// writes the MLVM_OUT magic with slow.Step.
func verifyCommitStep(state *fast.VMState) error {
	if state == nil {
		return fmt.Errorf("nil state")
	}
	inst := fast.NewInstrumentedState(state, nil, discard{}, discard{})
	const magic = uint64(0x4D4C564D5F4F5554)
	const resultBase = fast.MLResultBase
	for !state.Exited {
		var instr [4]byte
		state.Memory.GetUnaligned(state.PC, instr[:])
		op := binary.LittleEndian.Uint32(instr[:])
		prove := false
		if op&0x7f == 0x23 && ((op>>12)&7) == 3 { // SD
			rs1 := (op >> 15) & 0x1f
			imm := int64(((op >> 7) & 0x1f) | ((op >> 25) << 5))
			imm = (imm << 52) >> 52 // sign-extend 12-bit
			addr := state.Registers[rs1] + uint64(imm)
			if addr >= resultBase && addr < resultBase+64 {
				prove = true
			}
		}
		wit, err := inst.Step(prove)
		if err != nil {
			return err
		}
		if !prove || wit == nil {
			continue
		}
		var hdr [8]byte
		state.Memory.GetUnaligned(resultBase, hdr[:])
		if binary.LittleEndian.Uint64(hdr[:]) != magic {
			continue
		}
		input := wit.EncodeStepInput(fast.LocalContext{})
		post, err := slow.Step(input, nil)
		if err != nil {
			return fmt.Errorf("slow.Step: %w", err)
		}
		fastPost, err := state.EncodeWitness().StateHash()
		if err != nil {
			return err
		}
		if common.Hash(post) != fastPost {
			return fmt.Errorf("slow/fast mismatch: slow=%x fast=%x", post, fastPost)
		}
		return nil
	}
	return fmt.Errorf("guest exited before MLVM_OUT store was observed")
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

// ReplayMLPPrefix records a real fast-VM prefix of the embedded MLP guest.
// Used by `mlvm demo --web --demo mlp` so bisection is over executed steps,
// not XOR-derived hashes. The decoder explorer stays a receipt simulation.
func ReplayMLPPrefix(maxSteps uint64) (*dispute.Game, error) {
	state, _, err := loadEmbeddedELF(mlpELF, "mlp-rv64imafd.elf")
	if err != nil {
		return nil, err
	}
	return dispute.BuildTrace(state, maxSteps)
}
