package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnswerLine(t *testing.T) {
	out := []byte("loading\nscore: 0.6110916734 'a' 'b'\n\n")
	require.Equal(t, "score: 0.6110916734 'a' 'b'", answerLine(out, -1), "last non-empty line")
	require.Equal(t, "score: 0.6110916734 'a' 'b'", answerLine(out, 15), "the line holding a byte")
	require.Equal(t, "loading", answerLine(out, 0))
	require.Equal(t, "", answerLine(nil, -1))
	require.Equal(t, "no newline", answerLine([]byte("no newline"), -1))

	// A guest that follows the decoder's output protocol answers with what it generated,
	// newlines included.
	gen := []byte("prompt: def f(n):\ngenerated: \n    return n\ntokens: 1 2 3\n")
	require.Equal(t, "\n    return n", answerLine(gen, -1))
}
