package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const testSHA = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func validManifest() *Manifest {
	m := New()
	m.MLVM = Tool{Version: "0.1.0", Commit: "abc123"}
	m.Guest = Artifact{SHA256: testSHA, Size: 123, SourcePath: "/host/guest.elf"}
	m.Inputs = []Artifact{{
		GuestPath:  "model.bin",
		SHA256:     testSHA,
		Size:       456,
		SourcePath: "/host/model.bin",
	}}
	return m
}

func TestValidateFrozenProfile(t *testing.T) {
	m := validManifest()
	require.NoError(t, m.Validate())

	m.ExecutionProfile = "other"
	require.ErrorContains(t, m.Validate(), "must both be")
	m.ExecutionProfile = Profile
	m.Formats.Snapshot = "mlvm.snapshot/4"
	require.ErrorContains(t, m.Validate(), "frozen")
}

func TestCanonicalDigestIgnoresHostPaths(t *testing.T) {
	m := validManifest()
	first, err := m.Digest()
	require.NoError(t, err)
	m.Guest.SourcePath = "/different/guest"
	m.Inputs[0].SourcePath = `C:\different\model`
	second, err := m.Digest()
	require.NoError(t, err)
	require.Equal(t, first, second)

	m.Inputs[0].GuestPath = "renamed.bin"
	third, err := m.Digest()
	require.NoError(t, err)
	require.NotEqual(t, first, third)
}

func TestLoadRejectsUnknownAndTrailingJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	require.NoError(t, validManifest().Write(path))
	_, err := Load(path)
	require.NoError(t, err)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	raw = []byte(strings.Replace(string(raw), `"manifestVersion": 1,`, `"manifestVersion": 1, "unknown": true,`, 1))
	require.NoError(t, os.WriteFile(path, raw, 0o644))
	_, err = Load(path)
	require.ErrorContains(t, err, "unknown field")

	require.NoError(t, validManifest().Write(path))
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = file.WriteString("\n{}\n")
	require.NoError(t, err)
	require.NoError(t, file.Close())
	_, err = Load(path)
	require.ErrorContains(t, err, "multiple JSON values")
}
