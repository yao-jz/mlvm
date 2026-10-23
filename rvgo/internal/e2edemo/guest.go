package e2edemo

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/yao-jz/mlvm/rvgo/fast"
)

// Guest is a ready-to-run demo machine: its prestate plus the ELF that produced it, so
// tools such as the profiler and the dispute explorer can name functions and
// instructions. Every call returns a fresh, independent prestate.
type Guest struct {
	Demo         string // "decoder" or "mlp"
	Prompt       string
	MaxNewTokens int
	ELF          []byte
	State        *fast.VMState
}

// NewGuest builds the prestate for an embedded demo. demo is "decoder" (or empty) or
// "mlp"; prompt and maxNewTokens apply to the decoder only.
func NewGuest(demo, prompt string, maxNewTokens int) (*Guest, error) {
	switch demo {
	case "", "decoder":
		return NewDecoderGuest(prompt, maxNewTokens)
	case "mlp":
		if prompt != "" {
			return nil, fmt.Errorf("mlp demo has no prompt")
		}
		state, _, err := loadEmbeddedELF(mlpELF, "mlp-rv64imafd.elf")
		if err != nil {
			return nil, err
		}
		return &Guest{Demo: "mlp", ELF: mlpELF, State: state}, nil
	}
	return nil, fmt.Errorf("unknown demo %q", demo)
}

// NewDecoderGuest stages the embedded TinyStories-260K checkpoint, tokenizer and a request
// for prompt into a fresh decoder prestate. It is the same prestate `mlvm demo` hashes.
func NewDecoderGuest(prompt string, maxNewTokens int) (*Guest, error) {
	if prompt == "" {
		prompt = DefaultPrompt
	}
	if maxNewTokens == 0 {
		maxNewTokens = DefaultMaxNewTokens
	}
	assets, err := resolveDecoderAssets("")
	if err != nil {
		return nil, err
	}
	if err := assets.validatePinnedWeights(); err != nil {
		return nil, err
	}
	request, err := buildRequest(prompt, maxNewTokens)
	if err != nil {
		return nil, err
	}
	state, _, err := loadEmbeddedELF(assets.ELF, "decoder.elf")
	if err != nil {
		return nil, err
	}
	for _, file := range []struct {
		name string
		data []byte
	}{
		{"model.bin", assets.Model},
		{"tokenizer.bin", assets.Tokenizer},
		{"request.bin", request},
	} {
		if err := fast.StageFileBytes(file.name, file.data, state); err != nil {
			return nil, err
		}
	}
	return &Guest{Demo: "decoder", Prompt: prompt, MaxNewTokens: maxNewTokens, ELF: assets.ELF, State: state}, nil
}

// ResultTokens decodes the token IDs a halted decoder committed at MLVM_OUT.
func ResultTokens(state *fast.VMState) ([]uint16, error) {
	answer, ok := fast.MLResult(state)
	if !ok {
		return nil, fmt.Errorf("guest has not committed an MLResult")
	}
	return decodeResult(answer)
}

// IsTinyStoriesTokenizer reports whether data is the embedded tok512.bin, so a guest that
// stages it (such as the Candle port in examples/rust/tinystories-candle) commits token IDs
// this package can turn back into text.
func IsTinyStoriesTokenizer(data []byte) bool { return bytes.Equal(data, embeddedTokenizer) }

// Vocab is the llama2.c tokenizer vocabulary, used to turn committed token IDs back into
// text outside the VM (for example, to show what a forged answer would say).
type Vocab struct {
	pieces []string
}

// LoadVocab parses the embedded tok512.bin: a max-token-length int32, then per token a
// float32 score, an int32 length and the piece bytes.
func LoadVocab() (*Vocab, error) {
	data := embeddedTokenizer
	if len(data) < 4 {
		return nil, fmt.Errorf("tokenizer too short")
	}
	off := 4
	var pieces []string
	for off < len(data) {
		if off+8 > len(data) {
			return nil, fmt.Errorf("truncated tokenizer at byte %d", off)
		}
		_ = math.Float32frombits(binary.LittleEndian.Uint32(data[off:])) // score, unused
		n := int(binary.LittleEndian.Uint32(data[off+4:]))
		off += 8
		if n < 0 || off+n > len(data) {
			return nil, fmt.Errorf("bad tokenizer piece length %d", n)
		}
		pieces = append(pieces, string(data[off:off+n]))
		off += n
	}
	return &Vocab{pieces: pieces}, nil
}

// Piece returns the text of one token, decoding <0xHH> byte tokens.
func (v *Vocab) Piece(token uint16) string {
	if int(token) >= len(v.pieces) {
		return fmt.Sprintf("<%d>", token)
	}
	p := v.pieces[token]
	if strings.HasPrefix(p, "<0x") && strings.HasSuffix(p, ">") && len(p) == 6 {
		if b, err := strconv.ParseUint(p[3:5], 16, 8); err == nil {
			return string([]byte{byte(b)})
		}
	}
	return p
}

// Text joins generated tokens. The first generated token follows the prompt, so its
// leading space is kept, exactly as the guest prints it.
func (v *Vocab) Text(tokens []uint16) string {
	var b strings.Builder
	for _, t := range tokens {
		b.WriteString(v.Piece(t))
	}
	return b.String()
}

// Lookup returns the token whose piece is exactly s.
func (v *Vocab) Lookup(s string) (uint16, bool) {
	for i, p := range v.pieces {
		if p == s {
			return uint16(i), true
		}
	}
	return 0, false
}
