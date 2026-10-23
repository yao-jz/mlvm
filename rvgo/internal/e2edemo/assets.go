package e2edemo

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/yao-jz/mlvm/rvgo/fast"
	"github.com/yao-jz/mlvm/rvgo/internal/receipt"
)

//go:embed assets/decoder-rv64imafd.elf
var embeddedDecoderELF []byte

//go:embed assets/stories260k.bin
var embeddedModel []byte

//go:embed assets/tok512.bin
var embeddedTokenizer []byte

func resolveDecoderAssets(packDir string) (decoderAssets, error) {
	if packDir != "" {
		return assetsFromDir(packDir)
	}
	if len(embeddedDecoderELF) > 0 && len(embeddedModel) > 0 && len(embeddedTokenizer) > 0 {
		return decoderAssets{
			ELF:           embeddedDecoderELF,
			Model:         embeddedModel,
			Tokenizer:     embeddedTokenizer,
			Source:        "embedded",
			ELFPath:       "decoder.elf",
			ModelPath:     "model.bin",
			TokenizerPath: "tokenizer.bin",
		}, nil
	}
	if a, err := assetsFromDir("models/pack260k"); err == nil {
		a.Source = "models/pack260k"
		return a, nil
	}
	return decoderAssets{}, fmt.Errorf("%w: run ./scripts/decoder-fetch.sh or pass --model-pack", ErrDecoderAssetsNotFound)
}

const (
	pinnedModelSHA256 = "b0a507e7ad0f626624f17112325e66691f9076d622e1d3274d103d00299f2696"
	pinnedModelSize   = 1056540
	pinnedTokSHA256   = "037cb335abb25d1fa9e8ecae30ed2a3a8ace9302862ebcdc05d51a6bbb10c312"
	pinnedTokSize     = 6227
)

func (a decoderAssets) validatePinnedWeights() error {
	if err := checkPinned("model.bin", a.Model, pinnedModelSHA256, pinnedModelSize); err != nil {
		return err
	}
	return checkPinned("tokenizer.bin", a.Tokenizer, pinnedTokSHA256, pinnedTokSize)
}

func checkPinned(name string, data []byte, wantSHA string, wantSize int) error {
	if len(data) != wantSize {
		return fmt.Errorf("%s size %d != pinned %d", name, len(data), wantSize)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != wantSHA {
		return fmt.Errorf("%s sha256 mismatch (not the pinned TinyStories-260K artifact)", name)
	}
	return nil
}

func assetsFromDir(dir string) (decoderAssets, error) {
	modelPath := filepath.Join(dir, "model.bin")
	tokPath := filepath.Join(dir, "tokenizer.bin")
	elfPath := filepath.Join(dir, "decoder.elf")
	if _, err := os.Stat(elfPath); err != nil {
		return decoderAssets{}, fmt.Errorf("%w: pack %s must contain decoder.elf (no cwd fallback)", ErrDecoderAssetsNotFound, dir)
	}
	for _, p := range []string{modelPath, tokPath} {
		if _, err := os.Stat(p); err != nil {
			return decoderAssets{}, fmt.Errorf("%w: missing %s", ErrDecoderAssetsNotFound, p)
		}
	}
	elfBytes, err := os.ReadFile(elfPath)
	if err != nil {
		return decoderAssets{}, err
	}
	modelBytes, err := os.ReadFile(modelPath)
	if err != nil {
		return decoderAssets{}, err
	}
	tokBytes, err := os.ReadFile(tokPath)
	if err != nil {
		return decoderAssets{}, err
	}
	return decoderAssets{
		ELF:           elfBytes,
		Model:         modelBytes,
		Tokenizer:     tokBytes,
		Source:        dir,
		ELFPath:       elfPath,
		ModelPath:     modelPath,
		TokenizerPath: tokPath,
	}, nil
}

func attachResultCommitment(rec *receipt.Receipt, state *fast.VMState) error {
	if rec == nil || state == nil {
		return fmt.Errorf("cannot attach result commitment")
	}
	wit := state.EncodeWitness()
	proof := fast.MLResultProof(state)
	pre := common.HexToHash(rec.PreState)
	post := common.HexToHash(rec.PostState)
	answer, err := hexutil.Decode(rec.AnswerHex)
	if err != nil {
		return fmt.Errorf("answerHex: %w", err)
	}
	rec.Commitment = receipt.ResultCommitment{
		StateWitness: hexutil.Encode(wit),
		ResultProof:  hexutil.Encode(proof[:]),
		ClaimID:      receipt.ClaimID(pre, post, answer).Hex(),
	}
	return nil
}

func buildRequest(prompt string, maxNew int) ([]byte, error) {
	if maxNew <= 0 {
		maxNew = DefaultMaxNewTokens
	}
	if maxNew > MaxNewTokens {
		return nil, fmt.Errorf("max-new-tokens %d exceeds demo profile cap %d", maxNew, MaxNewTokens)
	}
	p := []byte(prompt)
	if len(p) > 256 {
		return nil, fmt.Errorf("prompt exceeds 256 bytes")
	}
	buf := make([]byte, 276)
	// little-endian request.bin v1; see docs/DECODER-PROTOCOL.md
	putU32 := func(off int, v uint32) {
		buf[off] = byte(v)
		buf[off+1] = byte(v >> 8)
		buf[off+2] = byte(v >> 16)
		buf[off+3] = byte(v >> 24)
	}
	putU32(0, 1)
	putU32(4, uint32(maxNew))
	putU32(8, 0)
	putU32(12, 0)
	putU32(16, uint32(len(p)))
	copy(buf[20:], p)
	return buf, nil
}

func digestBytes(guestPath string, data []byte) receipt.ArtifactDigest {
	sum := sha256.Sum256(data)
	return receipt.ArtifactDigest{
		GuestPath: guestPath,
		SHA256:    hex.EncodeToString(sum[:]),
		Size:      int64(len(data)),
	}
}
