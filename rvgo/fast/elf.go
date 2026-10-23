package fast

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

func LoadELF(f *elf.File) (*VMState, error) {
	out := &VMState{
		SchemaVersion: StateSchemaVersion,
		PC:            0,
		Memory:        NewMemory(),
		Registers:     [32]uint64{},
		FRegisters:    [32]uint64{},
		ExitCode:      0,
		Exited:        false,
		// Note: Go heap arenas in 64 bit riscv start at 0xc0_00_00_00_00
		// (c0 << 32) and range to 7f_00_00_00_00_00  (7f << 40) and specifies these with mmap hints.
		// Go imposes no address space limits on riscv64 however (based on malloc.go heapAddrBits).
		// So we grow the heap starting from this address, to not overlap with any hinted data
		// But we are running Rust, so we don't need to worry about this.
		Heap:             0x7f_00_00_00_00_00,
		FilePointer:      DefaultFileBase,
		FileSystem:       NewFileSystem(),
		RandomFileAddr:   DefaultRandomFileAddr,
		RandomFileOffset: 0,
	}

	// statically prepare VM state:
	out.PC = f.Entry
	tracef("program entry: %x\n", f.Entry)
	tracef("program count: %d\n", len(f.Progs))

	for i, prog := range f.Progs {
		tracef("prog %d: paddr: %x range %016x - %016x  (mem %016x)  type: %s\n", i, prog.Paddr, prog.Vaddr, prog.Vaddr+prog.Memsz, prog.Memsz, prog.Type.String())
		if prog.Type == 0x70000003 {
			// RISC-V reuses the MIPS_ABIFLAGS program type to type its segment with the `.riscv.attributes` section.
			// See: https://github.com/riscv-non-isa/riscv-elf-psabi-doc/blob/master/riscv-elf.adoc#attributes
			// This section has 0 mem size because it is not loaded into memory.
			// TODO: maybe try to parse this section to see what metadata Go outputs? (if any?)
			continue
		}

		r := io.Reader(io.NewSectionReader(prog, 0, int64(prog.Filesz)))
		if prog.Filesz != prog.Memsz {
			if prog.Type == elf.PT_LOAD {
				if prog.Filesz < prog.Memsz {
					r = io.MultiReader(r, bytes.NewReader(make([]byte, prog.Memsz-prog.Filesz)))
				} else {
					return nil, fmt.Errorf("invalid PT_LOAD program segment %d, file size (%d) > mem size (%d)", i, prog.Filesz, prog.Memsz)
				}
			} else {
				// A non-PT_LOAD segment whose memsz differs from its filesz is not
				// something we can zero-fill meaningfully. In practice this is PT_TLS,
				// whose .tbss tail is allocated per-thread rather than mapped, so
				// skipping it is correct for our single-threaded guests.
				tracef("skip segment %d (%s): filesz %d != memsz %d\n", i, prog.Type, prog.Filesz, prog.Memsz)
				continue
			}
		}

		if err := out.Memory.SetMemoryRange(prog.Vaddr, r); err != nil {
			return nil, fmt.Errorf("failed to read program segment %d: %w", i, err)
		}
	}
	return out, nil
}

// PatchVM prepares a freshly loaded ELF for execution. It does two unrelated things:
//
//  1. Neutralises the parts of the Go runtime that cannot work here - the garbage
//     collector, the sysmon thread, memory profiling. This is symbol-driven and only
//     applies to Go guests.
//  2. Sets up the initial stack: the stack pointer, and the argc/argv/envp/auxv block the
//     C runtime reads before main(). Every guest needs this.
//
// The two are separated deliberately. A stripped binary has no symbol table, so step 1 is
// impossible - but step 1 is a no-op for C and Rust guests anyway, while skipping step 2
// leaves the guest with sp=0 and no auxv, which fails in confusing ways much later.
// So a missing symbol table is not an error; it just means there is nothing to patch.
func PatchVM(f *elf.File, vmState *VMState) error {
	symbols, err := f.Symbols()
	if err != nil {
		// No symbol table: a stripped binary, or one built without one. Nothing to patch
		// out, so carry on to the stack setup below.
		tracef("no symbol table (%v); skipping Go-runtime patching\n", err)
		symbols = nil
	}
	for _, s := range symbols {
		// Disable Golang GC by patching the functions that enable the GC to a no-op function.
		switch s.Name {
		case "runtime.gcenable",
			"runtime.init.5",            // patch out: init() { go forcegchelper() }
			"runtime.main.func1",        // patch out: main.func() { newm(sysmon, ....) }
			"runtime.deductSweepCredit", // uses floating point nums and interacts with gc we disabled
			"runtime.(*gcControllerState).commit",
			// We need to patch this out, we don't pass float64nan because we don't support floats
			"runtime.check":
			// RISCV patch: ret (pseudo instruction)
			// 00008067 = jalr zero, ra, 0
			// Jump And Link Register, but rd=zero so no linking, and thus only jumping to the return address.
			// (return address is in register $ra based on RISCV call convention)
			tracef("patching out function %s\n", s.Name)
			if err := vmState.Memory.SetMemoryRange(s.Value, bytes.NewReader([]byte{
				0x67, 0x80, 0x00, 0x00,
			})); err != nil {
				return fmt.Errorf("failed to patch Go runtime.gcenable: %w", err)
			}
		case "runtime.MemProfileRate":
			if err := vmState.Memory.SetMemoryRange(s.Value, bytes.NewReader(make([]byte, 8))); err != nil { // disable mem profiling, to avoid a lot of unnecessary floating point ops
				return err
			}
		}
	}

	// To no-op an instruction:
	//vmState.SetMemRange(addr, 4, bytes.NewReader([]byte{0x13, 0x00, 0x00, 0x00}))

	// now insert the initial stack

	// setup stack pointer
	sp := uint64(0x10_00_00_00_00_00_00_00)
	vmState.Registers[2] = sp

	storeMem := func(addr uint64, v uint64) {
		var dat [8]byte
		binary.LittleEndian.PutUint64(dat[:], v)
		_ = vmState.Memory.SetMemoryRange(addr, bytes.NewReader(dat[:]))
	}

	// init argc, argv, aux on stack
	storeMem(sp+8*1, 0x42)                // argc = 0 (argument count)
	storeMem(sp+8*2, 0x35)                // argv[n] = 0 (terminating argv)
	storeMem(sp+8*3, 0)                   // envp[term] = 0 (no env vars)
	storeMem(sp+8*4, 6)                   // auxv[0] = _AT_PAGESZ = 6 (key)
	storeMem(sp+8*5, 4096)                // auxv[1] = page size of 4 KiB (value) - (== minPhysPageSize)
	storeMem(sp+8*6, 25)                  // auxv[2] = AT_RANDOM
	storeMem(sp+8*7, sp+8*9)              // auxv[3] = address of 16 bytes containing random value
	storeMem(sp+8*8, 0)                   // auxv[term] = 0
	storeMem(sp+8*9, 0x6f727020646e6172)  // randomness 8/16
	storeMem(sp+8*10, 0x6164626d616c6f74) // randomness 16/16

	// entrypoint is set as part of elf load function
	return nil
}

// ParseLoadFileSpec accepts either a bare host path (guest name = host path, for
// backwards compatibility) or an explicit alias "guestPath=hostPath". Aliases let a
// release pack stage model.bin from an arbitrary host location while the guest opens
// the stable committed name.
func ParseLoadFileSpec(spec string) (guestPath, hostPath string, err error) {
	if spec == "" {
		return "", "", fmt.Errorf("empty --load-file specification")
	}
	if i := strings.IndexByte(spec, '='); i >= 0 {
		guestPath = spec[:i]
		hostPath = spec[i+1:]
		if guestPath == "" || hostPath == "" {
			return "", "", fmt.Errorf("invalid --load-file %q; expected guestPath=hostPath", spec)
		}
		return guestPath, hostPath, nil
	}
	return spec, spec, nil
}

// StageFileBytes maps deterministic bytes into the guest under a stable path.
// It is used for generated inputs such as request.bin so callers do not need a
// mutable model-pack directory or a temporary host file.
func StageFileBytes(guestPath string, data []byte, state *VMState) error {
	if state == nil || state.Memory == nil || state.FileSystem == nil {
		return fmt.Errorf("cannot stage %q into an incomplete VM state", guestPath)
	}
	if err := state.FileSystem.AddFile(guestPath, uint64(len(data)), state); err != nil {
		return err
	}
	file, ok := state.FileSystem.files[guestPath]
	if !ok {
		return fmt.Errorf("staged file %q disappeared from manifest", guestPath)
	}
	if err := state.Memory.SetMemoryRange(file.Addr, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("failed to load generated file %q into memory: %w", guestPath, err)
	}
	return nil
}

func LoadFile(files []string, state *VMState) error {
	for _, spec := range files {
		guestPath, hostPath, err := ParseLoadFileSpec(spec)
		if err != nil {
			return err
		}
		tracef("loading file: guest %q from host %q\n", guestPath, hostPath)
		file, err := os.Open(hostPath)
		if err != nil {
			return fmt.Errorf("failed to open file %q: %w", hostPath, err)
		}
		fileInfo, err := file.Stat()
		if err != nil {
			file.Close()
			return fmt.Errorf("failed to stat file %q: %w", hostPath, err)
		}
		if err := state.FileSystem.AddFile(guestPath, uint64(fileInfo.Size()), state); err != nil {
			file.Close()
			return err
		}
		if err := state.Memory.SetMemoryRange(state.FileSystem.files[guestPath].Addr, file); err != nil {
			file.Close()
			return fmt.Errorf("failed to load file %q into memory as %q: %w", hostPath, guestPath, err)
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("failed to close file %q: %w", hostPath, err)
		}
	}
	return nil
}

type SortedSymbols []elf.Symbol

// FindSymbol finds the symbol that intersects with the given addr, or nil if none exists
func (s SortedSymbols) FindSymbol(addr uint64) elf.Symbol {
	// find first symbol with higher start. Or n if no such symbol exists
	i := sort.Search(len(s), func(i int) bool {
		return s[i].Value > addr
	})
	if i == 0 {
		return elf.Symbol{Name: "!start", Value: 0}
	}
	out := &s[i-1]
	if out.Value+out.Size < addr { // addr may be pointing to a gap between symbols
		return elf.Symbol{Name: "!gap", Value: addr}
	}
	return *out
}

func Symbols(f *elf.File) (SortedSymbols, error) {
	symbols, err := f.Symbols()
	if err != nil {
		return nil, fmt.Errorf("failed to read symbols data: %w", err)
	}
	// Go compiler supposedly already sorts them,
	// but it does not do so for some Go internals like internal/bytealg.IndexByteString),
	// and not every ELF has sorted symbols.
	out := make(SortedSymbols, len(symbols))
	for i := range out {
		out[i] = symbols[i]
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Value < out[j].Value
	})
	return out, nil
}
