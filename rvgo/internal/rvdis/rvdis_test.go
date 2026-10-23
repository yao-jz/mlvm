package rvdis

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// objdumpLine is one instruction as GNU objdump -M no-aliases prints it.
type objdumpLine struct {
	pc   uint64
	word uint32
	text string // "mnemonic\toperands" without objdump's trailing comments
}

func parseObjdump(t *testing.T, out []byte) []objdumpLine {
	t.Helper()
	var lines []objdumpLine
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		parts := strings.SplitN(sc.Text(), "\t", 4)
		if len(parts) < 3 || !strings.HasSuffix(parts[0], ":") {
			continue
		}
		pc, err := strconv.ParseUint(strings.TrimSpace(strings.TrimSuffix(parts[0], ":")), 16, 64)
		if err != nil {
			continue
		}
		hexWord := strings.TrimSpace(parts[1])
		if len(hexWord) != 8 { // compressed or data
			continue
		}
		word, err := strconv.ParseUint(hexWord, 16, 32)
		if err != nil {
			continue
		}
		text := strings.TrimSpace(parts[2])
		if len(parts) == 4 {
			ops := parts[3]
			if i := strings.Index(ops, " #"); i >= 0 {
				ops = ops[:i]
			}
			if i := strings.Index(ops, " <"); i >= 0 {
				ops = ops[:i]
			}
			text += "\t" + strings.TrimSpace(ops)
		}
		lines = append(lines, objdumpLine{pc: pc, word: uint32(word), text: text})
	}
	return lines
}

const goldenPath = "testdata/objdump-golden.txt"

var guestELFs = []string{
	"../e2edemo/assets/decoder-rv64imafd.elf",
	"../e2edemo/assets/mlp-rv64imafd.elf",
}

// TestDecodeMatchesObjdump disassembles every instruction of the embedded demo guests
// and compares with GNU objdump, when a RISC-V objdump is available.
func TestDecodeMatchesObjdump(t *testing.T) {
	objdump := os.Getenv("RISCV_OBJDUMP")
	if objdump == "" {
		for _, c := range []string{"riscv64-unknown-linux-gnu-objdump", "/opt/riscv-lp64d/bin/riscv64-unknown-linux-gnu-objdump", "riscv64-linux-gnu-objdump"} {
			if p, err := exec.LookPath(c); err == nil {
				objdump = p
				break
			}
		}
	}
	if objdump == "" {
		t.Skip("no RISC-V objdump; TestDecodeGolden still runs")
	}

	examples := map[string][]objdumpLine{}
	for _, elf := range guestELFs {
		out, err := exec.Command(objdump, "-d", "-M", "no-aliases", elf).Output()
		require.NoError(t, err)
		lines := parseObjdump(t, out)
		require.NotEmpty(t, lines)
		mismatches := 0
		for _, l := range lines {
			got := Decode(l.word, l.pc).String()
			if got != l.text {
				mismatches++
				if mismatches <= 20 {
					t.Errorf("%s pc=%x word=%08x: got %q, objdump %q", filepath.Base(elf), l.pc, l.word, got, l.text)
				}
				continue
			}
			mnemonic := strings.SplitN(l.text, "\t", 2)[0]
			if len(examples[mnemonic]) < 3 {
				examples[mnemonic] = append(examples[mnemonic], l)
			}
		}
		require.Zero(t, mismatches, "%s: %d of %d instructions differ from objdump", elf, mismatches, len(lines))
	}

	if os.Getenv("UPDATE_GOLDEN") != "" {
		var keys []string
		for k := range examples {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		b.WriteString("# pc word text - generated from objdump -M no-aliases by TestDecodeMatchesObjdump (UPDATE_GOLDEN=1)\n")
		for _, k := range keys {
			for _, l := range examples[k] {
				fmt.Fprintf(&b, "%x %08x %s\n", l.pc, l.word, l.text)
			}
		}
		require.NoError(t, os.MkdirAll("testdata", 0o755))
		require.NoError(t, os.WriteFile(goldenPath, []byte(b.String()), 0o644))
	}
}

// TestDecodeGolden checks a few examples of every mnemonic the demo guests use, so CI
// without a RISC-V toolchain still covers the decoder.
func TestDecodeGolden(t *testing.T) {
	data, err := os.ReadFile(goldenPath)
	require.NoError(t, err)
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.SplitN(line, " ", 3)
		require.Len(t, fields, 3, line)
		pc, err := strconv.ParseUint(fields[0], 16, 64)
		require.NoError(t, err)
		word, err := strconv.ParseUint(fields[1], 16, 32)
		require.NoError(t, err)
		require.Equal(t, fields[2], Decode(uint32(word), pc).String(), line)
		n++
	}
	require.Greater(t, n, 100)
}

func TestClasses(t *testing.T) {
	cases := []struct {
		word  uint32
		class Class
		prec  string
		rd    int
		frd   bool
	}{
		{0x10d7f7d3, ClassFloatArith, "s", 15, true}, // fmul.s fa5,fa5,fa3
		{0x000e2787, ClassFloatMem, "", 15, true},    // flw fa5,0(t3)
		{0xf0000753, ClassFloatMove, "", 14, true},   // fmv.w.x fa4,zero
		{0x00108093, ClassIntALU, "", 1, false},      // addi ra,ra,1
		{0x00113023, ClassStore, "", -1, false},      // sd ra,0(sp)
		{0x00000073, ClassSystem, "", -1, false},     // ecall
	}
	for _, c := range cases {
		inst := Decode(c.word, 0)
		require.Equal(t, c.class, inst.Class, inst.String())
		require.Equal(t, c.prec, inst.Precision, inst.String())
		require.Equal(t, c.rd, inst.Rd, inst.String())
		require.Equal(t, c.frd, inst.FloatRd, inst.String())
	}
}
