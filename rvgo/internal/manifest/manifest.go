// Package manifest implements the mlvm.manifest/1 execution manifest.
package manifest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

const Version = 1
const Profile = "mlvm-1"

type Formats struct {
	Snapshot        string `json:"snapshot"`
	StateWitness    string `json:"stateWitness"`
	FilesystemProof string `json:"filesystemProof"`
	RiscvStep       string `json:"riscvStep"`
}

type Artifact struct {
	GuestPath  string `json:"guestPath"`
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size"`
	SourcePath string `json:"sourcePath,omitempty"`
}

type Randomness struct {
	Mode   string `json:"mode"` // "zero" or "blob"
	Size   uint64 `json:"size"`
	SHA256 string `json:"sha256,omitempty"`
}

type Tool struct {
	Version string `json:"version"`
	Commit  string `json:"commit,omitempty"`
}

type Manifest struct {
	ManifestVersion  uint64     `json:"manifestVersion"`
	Profile          string     `json:"profile"`
	ExecutionProfile string     `json:"executionProfile"`
	Formats          Formats    `json:"formats"`
	MLVM             Tool       `json:"mlvm"`
	Guest            Artifact   `json:"guest"`
	Inputs           []Artifact `json:"inputs"`
	Randomness       Randomness `json:"randomness"`
	ExpectedPrestate string     `json:"expectedPrestate,omitempty"`
}

func DefaultFormats() Formats {
	return Formats{
		Snapshot:        "mlvm.snapshot/3",
		StateWitness:    "mlvm.state-witness/1",
		FilesystemProof: "mlvm.filesystem-proof/2",
		RiscvStep:       "mlvm.riscv-step/1",
	}
}

func New() *Manifest {
	return &Manifest{
		ManifestVersion:  Version,
		Profile:          Profile,
		ExecutionProfile: Profile,
		Formats:          DefaultFormats(),
		Inputs:           []Artifact{},
		Randomness:       Randomness{Mode: "zero", Size: 5 * 1024 * 1024},
	}
}

func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	if err := ensureJSONEOF(dec); err != nil {
		return nil, err
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

func (m *Manifest) Validate() error {
	if m.ManifestVersion != Version {
		return fmt.Errorf("unsupported manifestVersion %d", m.ManifestVersion)
	}
	if m.Profile != Profile || m.ExecutionProfile != Profile {
		return fmt.Errorf("profile and executionProfile must both be %q", Profile)
	}
	if m.Formats != DefaultFormats() {
		return fmt.Errorf("manifest formats do not match the frozen %s profile", Profile)
	}
	if err := validateArtifact("guest", m.Guest, false); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(m.Inputs))
	for i, in := range m.Inputs {
		if err := validateArtifact(fmt.Sprintf("input[%d]", i), in, true); err != nil {
			return err
		}
		if _, ok := seen[in.GuestPath]; ok {
			return fmt.Errorf("duplicate input guestPath %q", in.GuestPath)
		}
		seen[in.GuestPath] = struct{}{}
	}
	switch m.Randomness.Mode {
	case "zero":
		if m.Randomness.SHA256 != "" {
			return fmt.Errorf("zero randomness must not carry sha256")
		}
	case "blob":
		if err := validateSHA256(m.Randomness.SHA256); err != nil {
			return fmt.Errorf("randomness sha256: %w", err)
		}
	default:
		return fmt.Errorf("randomness mode must be %q or %q", "zero", "blob")
	}
	if m.Randomness.Size != 5*1024*1024 {
		return fmt.Errorf("randomness size %d does not match mlvm-1 size %d", m.Randomness.Size, 5*1024*1024)
	}
	if m.ExpectedPrestate != "" {
		if len(m.ExpectedPrestate) != 66 || !strings.HasPrefix(m.ExpectedPrestate, "0x") {
			return fmt.Errorf("expectedPrestate must be a 0x-prefixed 32-byte hash")
		}
		if _, err := hex.DecodeString(m.ExpectedPrestate[2:]); err != nil {
			return fmt.Errorf("expectedPrestate: %w", err)
		}
	}
	return nil
}

func ensureJSONEOF(dec *json.Decoder) error {
	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("manifest contains multiple JSON values")
		}
		return err
	}
	return nil
}

func validateArtifact(label string, artifact Artifact, requireGuestPath bool) error {
	if requireGuestPath && artifact.GuestPath == "" {
		return fmt.Errorf("%s requires guestPath", label)
	}
	if artifact.Size <= 0 {
		return fmt.Errorf("%s requires a positive size", label)
	}
	if err := validateSHA256(artifact.SHA256); err != nil {
		return fmt.Errorf("%s sha256: %w", label, err)
	}
	return nil
}

func validateSHA256(value string) error {
	if len(value) != sha256.Size*2 {
		return fmt.Errorf("must contain exactly %d lowercase hexadecimal characters", sha256.Size*2)
	}
	if value != strings.ToLower(value) {
		return fmt.Errorf("must be lowercase hexadecimal")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return err
	}
	return nil
}

// CanonicalJSON returns the portable manifest commitment. Host source paths are
// deliberately removed; input order remains significant.
func (m *Manifest) CanonicalJSON() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	copyManifest := *m
	copyManifest.Guest.SourcePath = ""
	copyManifest.Inputs = append([]Artifact(nil), m.Inputs...)
	for i := range copyManifest.Inputs {
		copyManifest.Inputs[i].SourcePath = ""
	}
	return json.Marshal(copyManifest)
}

func (m *Manifest) Digest() (string, error) {
	data, err := m.CanonicalJSON()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func HashFile(path string) (string, int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), int64(len(data)), nil
}

func VerifyFile(path, wantSHA string, wantSize int64) error {
	gotSHA, gotSize, err := HashFile(path)
	if err != nil {
		return err
	}
	if wantSize != 0 && gotSize != wantSize {
		return fmt.Errorf("%s size %d != %d", path, gotSize, wantSize)
	}
	if wantSHA != "" && gotSHA != wantSHA {
		return fmt.Errorf("%s sha256 %s != %s", path, gotSHA, wantSHA)
	}
	return nil
}

func (m *Manifest) Write(path string) error {
	blob, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	blob = append(blob, '\n')
	return os.WriteFile(path, blob, 0o644)
}
