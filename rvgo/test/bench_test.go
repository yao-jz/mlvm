package test

import (
	"debug/elf"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yao-jz/mlvm/rvgo/fast"
)

// The benchmarks in this file measure three execution and witness-building costs relevant
// to a future interactive protocol:
//
//	BenchmarkFastStep      raw emulation throughput  (bisection replays millions of steps)
//	BenchmarkProvingStep   throughput with merkle-proof capture (the one disputed step)
//	BenchmarkMerkleRoot    cost of re-merkleizing memory (per snapshot / commitment)
//
// Run them with:
//
//	make bench            # from the repo root
//	go test ./rvgo/test/ -run '^$' -bench . -benchtime 10x
//
// Corpus: every RISC-V ELF under examples/build (built by `make examples`) plus the
// checked-in glibc binary rvgo/hello-libc. Note that tests/riscv-tests/benchmarks/*.riscv
// is deliberately NOT used: those ELFs are built with the compressed (RVC) extension,
// which this VM does not decode - they revert at step 0 with "unknown instruction opcode:
// 1". Build the corpus with -march=rv64imafd (no 'c') instead.
var benchCorpusDirs = []string{
	"../hello-libc",        // checked in: static glibc "Hello, world!", ~2.1M steps
	"../../examples/build", // built by `make examples`
}

type benchProgram struct {
	name string
	path string
}

// needsStagedInput lists example programs that read a file out of emulated memory. They
// need `--load-file` staging that the benchmark harness does not do, so they are excluded
// from the corpus rather than reported as failures. Run them with scripts/run-elf.sh.
var needsStagedInput = map[string]bool{"fileio": true}

// discoverBenchPrograms collects RISC-V ELFs from benchCorpusDirs. Entries may be a single
// file or a directory; missing entries are silently ignored so the benchmarks still run on
// a fresh clone with nothing built.
func discoverBenchPrograms(tb testing.TB) []benchProgram {
	tb.Helper()
	var out []benchProgram
	for _, entry := range benchCorpusDirs {
		p := filepath.FromSlash(entry)
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			out = append(out, benchProgram{name: filepath.Base(p), path: p})
			continue
		}
		items, err := os.ReadDir(p)
		if err != nil {
			continue
		}
		for _, item := range items {
			if item.IsDir() || filepath.Ext(item.Name()) != "" {
				continue // skip .c, .dump, .json and friends
			}
			if needsStagedInput[item.Name()] {
				continue
			}
			out = append(out, benchProgram{name: item.Name(), path: filepath.Join(p, item.Name())})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// loadProgram prepares a program the same way `mlvm load-elf` does, minus the
// randomness blob: PatchVM is what writes the initial stack pointer and the
// argc/argv/envp/auxv block, and without it the guest starts with sp=0 and runs a
// materially different execution.
//
// The 5 MiB randomness blob is deliberately not mapped here. getrandom then reads zeros
// from unallocated pages, which is fine for measurement, and it keeps the reported page
// counts equal to the program's own working set rather than working set + 1280 pages of
// entropy.
func loadProgram(tb testing.TB, path string) *fast.VMState {
	tb.Helper()
	f, err := elf.Open(path)
	require.NoError(tb, err)
	defer f.Close()
	state, err := fast.LoadELF(f)
	require.NoError(tb, err, "must load ELF")
	require.NoError(tb, fast.PatchVM(f, state), "must set up the initial stack")
	return state
}

// runToCompletion steps the VM until it exits or maxSteps is reached, and reports how far
// it got. It returns rather than fails: a program that trips an unimplemented instruction
// is still a useful throughput sample up to that point.
func runToCompletion(state *fast.VMState, maxSteps uint64) (steps uint64, err error) {
	in := fast.NewInstrumentedState(state, nil, io.Discard, io.Discard)
	for steps = 0; steps < maxSteps; steps++ {
		if _, err = in.Step(false); err != nil {
			return steps, err
		}
		if state.Exited {
			return steps + 1, nil
		}
	}
	return steps, nil
}

// BenchmarkFastStep reports emulated instructions per second. Each iteration runs one
// whole program from a fresh state, so instr/s is the number that matters, not ns/op.
func BenchmarkFastStep(b *testing.B) {
	progs := discoverBenchPrograms(b)
	if len(progs) == 0 {
		b.Skip("no benchmark programs found; run `make examples` first")
	}
	for _, p := range progs {
		b.Run(p.name, func(b *testing.B) {
			var totalSteps uint64
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				state := loadProgram(b, p.path)
				b.StartTimer()
				steps, err := runToCompletion(state, 500_000_000)
				totalSteps += steps
				if err != nil {
					b.StopTimer()
					b.Skipf("unsupported instruction after %d steps: %v", steps, err)
				}
			}
			b.StopTimer()
			if elapsed := b.Elapsed().Seconds(); elapsed > 0 {
				b.ReportMetric(float64(totalSteps)/elapsed/1e6, "Minstr/s")
				b.ReportMetric(float64(totalSteps)/float64(b.N), "steps/op")
			}
		})
	}
}

// BenchmarkProvingStep measures emulation with memory-proof capture enabled. Each step
// merkleizes the touched branch, so this is far slower than fast stepping. The reported
// witness-B/step is the raw state-plus-branch payload and excludes ABI and protocol data.
func BenchmarkProvingStep(b *testing.B) {
	progs := discoverBenchPrograms(b)
	if len(progs) == 0 {
		b.Skip("no benchmark programs found; run `make examples` first")
	}
	for _, p := range progs {
		b.Run(p.name, func(b *testing.B) {
			state := loadProgram(b, p.path)
			in := fast.NewInstrumentedState(state, nil, io.Discard, io.Discard)
			// Warm past process start-up so we measure steady-state code, then take a few
			// proving steps so the merkle node cache is populated. Without this the first
			// measured step pays for merkleizing the entire freshly-loaded memory image
			// and dominates a short benchmark run.
			for i := 0; i < 2000 && !state.Exited; i++ {
				if _, err := in.Step(false); err != nil {
					b.Skipf("unsupported instruction during warmup: %v", err)
				}
			}
			for i := 0; i < 100 && !state.Exited; i++ {
				if _, err := in.Step(true); err != nil {
					b.Skipf("unsupported instruction during warmup: %v", err)
				}
			}
			b.ResetTimer()
			var witnessBytes, done int
			for i := 0; i < b.N && !state.Exited; i++ {
				wit, err := in.Step(true)
				if err != nil {
					b.StopTimer()
					b.Skipf("unsupported instruction: %v", err)
				}
				witnessBytes += len(wit.ProofData()) + len(wit.State)
				done++
			}
			b.StopTimer()
			if done > 0 {
				b.ReportMetric(float64(witnessBytes)/float64(done), "witness-B/step")
			}
		})
	}
}

// BenchmarkMerkleRoot measures a full memory merkleization from a dirty root, which is
// what a snapshot or a fresh state commitment costs. Page count is reported so numbers
// from different programs are comparable.
func BenchmarkMerkleRoot(b *testing.B) {
	progs := discoverBenchPrograms(b)
	if len(progs) == 0 {
		b.Skip("no benchmark programs found; run `make examples` first")
	}
	for _, p := range progs {
		b.Run(p.name, func(b *testing.B) {
			state := loadProgram(b, p.path)
			if _, err := runToCompletion(state, 200_000); err != nil {
				b.Skipf("unsupported instruction: %v", err)
			}
			pages := state.Memory.PageCount()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Dirty one leaf so the root is genuinely recomputed rather than served
				// from the node cache.
				state.Memory.Invalidate(state.PC &^ 31)
				_ = state.Memory.MerkleRoot()
			}
			b.StopTimer()
			b.ReportMetric(float64(pages), "pages")
			b.ReportMetric(float64(pages)*fast.PageSize/1024, "KiB")
		})
	}
}

// TestBenchmarkPrograms is a plain test that records, for every program in the corpus, how
// many steps it takes, how much memory it touches and what mix of instructions it runs.
// scripts/bench.sh parses the BENCHRESULT lines into the table in docs/BENCHMARKS.md.
func TestBenchmarkPrograms(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping full program runs in -short mode")
	}
	progs := discoverBenchPrograms(t)
	if len(progs) == 0 {
		t.Skip("no benchmark programs found; run `make examples` first")
	}
	for _, p := range progs {
		t.Run(p.name, func(t *testing.T) {
			state := loadProgram(t, p.path)
			steps, err := runToCompletion(state, 500_000_000)
			status := "ok"
			switch {
			case err != nil:
				status = fmt.Sprintf("stopped: %v", err)
			case !state.Exited:
				status = "step limit reached"
			case state.ExitCode != 0:
				status = fmt.Sprintf("exit code %d", state.ExitCode)
			}
			t.Logf("BENCHRESULT\t%s\t%d\t%d\t%d\t%d\t%d\t%s",
				p.name, steps, state.Memory.PageCount(),
				state.OpCount.Arith, state.OpCount.Mem,
				state.OpCount.Float+state.OpCount.FloatLS, status)
		})
	}
}

// TestMerkleTamperDetection is the property the whole fraud-proof design rests on: change
// a single byte of emulated memory and the state commitment must change. It runs a
// program, snapshots the root, flips one bit, and requires a different root - then puts
// the bit back and requires the original root returns.
func TestMerkleTamperDetection(t *testing.T) {
	progs := discoverBenchPrograms(t)
	if len(progs) == 0 {
		t.Skip("no benchmark programs found; run `make examples` first")
	}
	state := loadProgram(t, progs[0].path)
	if _, err := runToCompletion(state, 50_000); err != nil {
		t.Skipf("unsupported instruction: %v", err)
	}

	before := state.Memory.MerkleRoot()

	// Pick an address the program has actually written to, so we are not flipping a bit
	// in an unallocated (implicitly zero) page.
	addr := state.Registers[2] &^ 31 // stack pointer, 32-byte aligned
	var orig [32]byte
	state.Memory.GetUnaligned(addr, orig[:])

	tampered := orig
	tampered[0] ^= 0x01
	state.Memory.SetUnaligned(addr, tampered[:])
	after := state.Memory.MerkleRoot()
	require.NotEqual(t, before, after, "flipping one bit at %#x must change the memory root", addr)

	state.Memory.SetUnaligned(addr, orig[:])
	restored := state.Memory.MerkleRoot()
	require.Equal(t, before, restored, "restoring the bit must restore the memory root")

	t.Logf("root before   %x", before)
	t.Logf("root tampered %x  (one bit flipped at %#x)", after, addr)
	t.Logf("root restored %x", restored)
}
