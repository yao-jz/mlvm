//go:build !js

package evmstep

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// stripMetadata drops solc's CBOR metadata tail (its length is the last two bytes). The
// tail hashes compiler settings and source paths, so it can differ between machines that
// compile identical code.
func stripMetadata(code []byte) []byte {
	if len(code) < 2 {
		return code
	}
	n := int(code[len(code)-2])<<8 | int(code[len(code)-1])
	if n+2 > len(code) {
		return code
	}
	return code[:len(code)-n-2]
}

// TestEmbeddedMatchesArtifact fails when RISCV.sol was rebuilt with the same compiler but
// the embedded copy was not refreshed with `make evm-artifact`.
func TestEmbeddedMatchesArtifact(t *testing.T) {
	data, err := os.ReadFile("../../../rvsol/out/RISCV.sol/RISCV.json")
	if err != nil {
		t.Skip("no forge artifact; run `make test-sol` to build one")
	}
	var artifact struct {
		DeployedBytecode struct {
			Object string `json:"object"`
		} `json:"deployedBytecode"`
		RawMetadata string `json:"rawMetadata"`
	}
	require.NoError(t, json.Unmarshal(data, &artifact))
	var meta struct {
		Compiler struct {
			Version string `json:"version"`
		} `json:"compiler"`
	}
	require.NoError(t, json.Unmarshal([]byte(artifact.RawMetadata), &meta))
	embeddedSolc, err := os.ReadFile("RISCV.solc")
	require.NoError(t, err)
	if want := strings.TrimSpace(string(embeddedSolc)); meta.Compiler.Version != want {
		t.Skipf("artifact built by solc %s, embedded bytecode by %s; not comparable", meta.Compiler.Version, want)
	}
	built, err := hex.DecodeString(strings.TrimPrefix(artifact.DeployedBytecode.Object, "0x"))
	require.NoError(t, err)
	embedded, err := Code()
	require.NoError(t, err)
	require.Equal(t, hex.EncodeToString(stripMetadata(built)), hex.EncodeToString(stripMetadata(embedded)),
		"embedded RISCV.sol bytecode is stale: run `make evm-artifact`")
}

func TestCodeDecodes(t *testing.T) {
	code, err := Code()
	require.NoError(t, err)
	require.Greater(t, len(code), 8000)
	require.Less(t, len(stripMetadata(code)), len(code), "the embedded code carries a metadata tail")
}
