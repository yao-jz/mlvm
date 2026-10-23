package e2edemo

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPinnedWeightsRejectTamper(t *testing.T) {
	assets, err := resolveDecoderAssets("")
	require.NoError(t, err)
	require.NoError(t, assets.validatePinnedWeights())
	assets.Model = append([]byte(nil), assets.Model...)
	assets.Model[0] ^= 0xff
	require.Error(t, assets.validatePinnedWeights())
}

func TestBuildRequest(t *testing.T) {
	buf, err := buildRequest("Once upon a time", 16)
	require.NoError(t, err)
	require.Len(t, buf, 276)
	_, err = buildRequest("hi", 99)
	require.Error(t, err)
}

func TestRunMLP(t *testing.T) {
	res, err := Run(Options{Demo: "mlp"})
	require.NoError(t, err)
	require.Equal(t, "mlp", res.Receipt.Demo)
	require.Greater(t, res.Receipt.Steps, uint64(1000))
	require.NotEmpty(t, res.Receipt.PreState)
	require.NotEmpty(t, res.Receipt.PostState)
	require.NotEmpty(t, res.Receipt.AnswerHex)
	require.NoError(t, res.Receipt.Verify())
}

func TestRunMLPVerify(t *testing.T) {
	if testing.Short() {
		t.Skip("slow-step verify")
	}
	res, err := Run(Options{Demo: "mlp", VerifySlow: true})
	require.NoError(t, err)
	require.True(t, res.Receipt.Verifier.Passed)
	require.Contains(t, res.Receipt.Verifier.Mode, "slow-step")
}

func TestRunDecoder(t *testing.T) {
	if testing.Short() {
		t.Skip("decoder e2e")
	}
	res, err := Run(Options{Demo: "decoder", Prompt: DefaultPrompt})
	require.NoError(t, err)
	require.Equal(t, "decoder", res.Receipt.Demo)
	require.Equal(t, DefaultPrompt, res.Receipt.Prompt)
	require.Contains(t, res.Receipt.GeneratedText, "little girl")
	require.Equal(t, []uint16{432, 383, 286, 261, 376, 298, 315, 421, 395, 317, 426, 338, 401, 396, 267, 337}, res.Receipt.Tokens)
	require.NoError(t, res.Receipt.Verify())
	require.True(t, res.Receipt.Verifier.Passed)
}

func TestExtractGeneratedKeepsLeadingSpace(t *testing.T) {
	stdout := "MLVM decoder demo\nprompt: The dragon\ngenerated:  was very small\ntokens: 1 2\n"
	require.Equal(t, " was very small", extractGenerated(stdout))
	require.Equal(t, ", there", extractGenerated("generated: , there\r\n"))
}

func TestGuestMatchesDemoPrestate(t *testing.T) {
	if testing.Short() {
		t.Skip("decoder e2e")
	}
	res, err := Run(Options{Demo: "decoder", Prompt: "The dragon", MaxNewTokens: 4})
	require.NoError(t, err)
	g, err := NewDecoderGuest("The dragon", 4)
	require.NoError(t, err)
	pre, err := g.State.EncodeWitness().StateHash()
	require.NoError(t, err)
	require.Equal(t, res.Receipt.PreState, pre.Hex())
}

func TestVocabRoundTrip(t *testing.T) {
	v, err := LoadVocab()
	require.NoError(t, err)
	tokens := []uint16{432, 383, 286, 261, 376, 298, 315, 421, 395, 317, 426, 338, 401, 396, 267, 337}
	require.Equal(t, ", there was a little girl named Lily. She loved to play", v.Text(tokens))
}

func TestTokenWatcher(t *testing.T) {
	run := func(chunks ...string) []string {
		var got []string
		started := 0
		w := &TokenWatcher{OnStart: func(uint64) { started++ }, OnToken: func(s string, _ uint64) { got = append(got, s) }}
		for _, c := range chunks {
			_, _ = w.Write([]byte(c))
		}
		require.Equal(t, 1, started)
		return got
	}
	// The end marker split across writes, a newline token, a split start marker.
	require.Equal(t, []string{",", " there", "\n", " was"},
		run("MLVM decoder demo", "\nprompt: x\ngenera", "ted: ", ",", " there", "\n", " was", "\n", "tokens:", " 1 2", "\n"))
	// The end marker in one write with the token list, as the guest really prints it.
	require.Equal(t, []string{",", " there"},
		run("generated: ", ",", " there", "\ntokens: 432 383\n"))
	// A token that ends with what looks like the start of the marker.
	require.Equal(t, []string{" a", "\n", "b"},
		run("generated: ", " a\n", "b", "\ntok", "ens: 1\n"))
}
