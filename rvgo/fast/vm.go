package fast

import (
	"encoding/binary"
	"fmt"
	"io"

	. "github.com/yao-jz/mlvm/rvgo/comp"
	. "github.com/yao-jz/mlvm/rvgo/include"
)

const (
	fdStdin         = 0
	fdStdout        = 1
	fdStderr        = 2
	fdHintRead      = 3
	fdHintWrite     = 4
	fdPreimageRead  = 5
	fdPreimageWrite = 6
)

// riscvStep runs a single instruction
// Note: errors are only returned in debugging/tooling modes, not in production use.
func (inst *InstrumentedState) riscvStep() (outErr error) {
	var revertCode uint64
	defer func() {
		if err := recover(); err != nil {
			outErr = fmt.Errorf("revert: %v", err)
		}
		if revertCode != 0 {
			outErr = fmt.Errorf("revert %x: %w", revertCode, outErr)
		}
	}()

	revertWithCode := func(code uint64, err error) {
		revertCode = code
		panic(err)
	}

	//
	// Yul64 - functions to do 64 bit math - see yul64.go
	//

	//
	// Bit hacking util
	//

	//
	// State layout
	// N/A

	//
	// State loading
	//
	s := inst.state

	//
	// State output
	// N/A

	//
	// State access
	//

	// getMemRoot
	// setMemRoot

	getPreimageKey := func() [32]byte {
		return s.PreimageKey
	}
	setPreimageKey := func(k [32]byte) {
		s.PreimageKey = k
	}

	getPreimageOffset := func() U64 {
		return s.PreimageOffset
	}
	setPreimageOffset := func(v U64) {
		s.PreimageOffset = v
	}

	getPC := func() U64 {
		return s.PC
	}
	setPC := func(pc U64) {
		s.PC = pc
	}

	getExited := func() (exited bool) {
		return s.Exited
	}
	setExited := func() {
		s.Exited = true
	}

	// no getExitCode necessary
	setExitCode := func(v uint8) {
		s.ExitCode = v
	}

	getStep := func() U64 {
		return s.Step
	}
	setStep := func(v U64) {
		s.Step = v
	}

	getHeap := func() U64 {
		return s.Heap
	}
	setHeap := func(v U64) {
		s.Heap = v
	}

	getLoadReservation := func() U64 {
		return s.LoadReservation
	}
	setLoadReservation := func(addr U64) {
		s.LoadReservation = addr
	}

	getRegister := func(reg U64) U64 {
		if reg > 31 {
			revertWithCode(0xbad4e9, fmt.Errorf("cannot load invalid register: %d", reg))
		}
		//tracef("load reg %2d: %016x\n", reg, state.Registers[reg])
		return s.Registers[reg]
	}
	setRegister := func(reg U64, v U64) {
		//tracef("write reg %2d: %016x   value: %016x\n", reg, state.Registers[reg], v)
		if reg == 0 { // reg 0 must stay 0
			// v is a HINT, but no hints are specified by standard spec, or used by us.
			return
		}
		if reg >= 32 {
			panic(fmt.Errorf("unknown register %d, cannot write %x", reg, v))
		}
		s.Registers[reg] = v
	}

	getFRegister := func(reg U64) U64 {
		if reg > 31 {
			revertWithCode(0xbad4e9, fmt.Errorf("cannot load invalid Fregister: %d", reg))
		}
		//tracef("load freg %2d: %016x\n", reg, state.FRegisters[reg])
		return s.FRegisters[reg]
	}

	setFRegister := func(reg U64, v U64) {
		//tracef("write freg %2d: %016x   value: %016x\n", reg, state.FRegisters[reg], v)
		if reg >= 32 {
			panic(fmt.Errorf("unknown Fregister %d, cannot write %x", reg, v))
		}
		s.FRegisters[reg] = v
	}

	//
	// Parse - functions to parse RISC-V instructions - see parse.go
	//

	//
	// Memory functions
	//

	getMemoryB32 := func(addr U64, proofIndex uint8) (out [32]byte) {
		if addr&31 != 0 { // quick addr alignment check
			revertWithCode(0xbad10ad0, fmt.Errorf("addr %d not aligned with 32 bytes", addr))
		}
		inst.trackMemAccess(addr, proofIndex)
		s.Memory.GetUnaligned(addr, out[:])
		return
	}

	setMemoryB32 := func(addr U64, v [32]byte, proofIndex uint8) {
		if addr&31 != 0 {
			panic(fmt.Errorf("addr %d not aligned with 32 bytes", addr))
		}
		inst.verifyMemChange(addr, proofIndex)
		s.Memory.SetUnaligned(addr, v[:])
	}

	// load unaligned, optionally signed, little-endian, integer of 1 ... 8 bytes from memory
	// addr: the address to load from
	// size: the number of bytes to load
	// signed: if true, then the integer is sign-extended
	// proofIndexL: the index of the left-side proof
	// proofIndexR: the index of the right-side proof, or 0xff if no right-side proof is expected
	loadMem := func(addr U64, size U64, signed bool, proofIndexL uint8, proofIndexR uint8) (out U64) {
		if size > 8 {
			revertWithCode(0xbad512e0, fmt.Errorf("cannot load more than 8 bytes: %d", size))
		}
		inst.trackMemAccess(addr&^31, proofIndexL) // track the left-side proof
		if (addr+size-1)&^31 != addr&^31 {         // the integer crosses a 32-byte boundary
			if proofIndexR == 0xff { // if no right-side proof is expected, then this is an error
				revertWithCode(0xbad22220, fmt.Errorf("unexpected need for right-side proof %d in loadMem", proofIndexR))
			}
			inst.trackMemAccess((addr+size-1)&^31, proofIndexR) // track the right-side proof
		}
		var v [8]byte
		s.Memory.GetUnaligned(addr, v[:size])    // use size to specify how many bytes to load (length of array)
		out = binary.LittleEndian.Uint64(v[:])   // convert byte to uint64
		bitSize := size << 3                     // size * 8
		if signed && out&(1<<(bitSize-1)) != 0 { // if the last bit is set, then extend it to the full 64 bits
			out |= 0xFFFF_FFFF_FFFF_FFFF << bitSize
		} // otherwise just leave it zeroed
		//tracef("load mem: %016x  size: %d  value: %016x  signed: %v\n", addr, size, v, signed)
		return out
	}

	// store unaligned, little-endian, integer of at most 32 bytes to memory
	storeMemUnaligned := func(addr U64, size U64, value U256, proofIndexL uint8, proofIndexR uint8, verifyL bool, verifyR bool) {
		if addr >= 6229920 {
			if addr-6229920 <= 64 {
				tracef("storeMem: addr %d, value %d\n", addr, value)
				revertWithCode(0xbad512e7, fmt.Errorf("cannot store to memory address %d", addr))
			}
		} else {
			if 6229920-addr <= 64 {
				tracef("storeMem: addr %d, value %d\n", addr, value)
				revertWithCode(0xbad512e7, fmt.Errorf("cannot store to memory address %d", addr))
			}
		}
		if size > 32 {
			revertWithCode(0xbad512e1, fmt.Errorf("cannot store more than 32 bytes: %d", size))
		}
		var bytez [32]byte
		binary.LittleEndian.PutUint64(bytez[:8], value[0])
		binary.LittleEndian.PutUint64(bytez[8:16], value[1])
		binary.LittleEndian.PutUint64(bytez[16:24], value[2])
		binary.LittleEndian.PutUint64(bytez[24:], value[3])

		leftAddr := addr &^ 31
		if verifyL {
			inst.trackMemAccess(leftAddr, proofIndexL) // It stores previous memory's proof
		}
		inst.verifyMemChange(leftAddr, proofIndexL)
		if (addr+size-1)&^31 == addr&^31 { // if aligned
			s.Memory.SetUnaligned(addr, bytez[:size])
			return
		}
		if proofIndexR == 0xff {
			revertWithCode(0xbad22221, fmt.Errorf("unexpected need for right-side proof %d in storeMemUnaligned", proofIndexR))
		}
		// if not aligned
		rightAddr := leftAddr + 32
		leftSize := rightAddr - addr
		s.Memory.SetUnaligned(addr, bytez[:leftSize])
		if verifyR {
			inst.trackMemAccess(rightAddr, proofIndexR)
		}
		inst.verifyMemChange(rightAddr, proofIndexR)
		s.Memory.SetUnaligned(rightAddr, bytez[leftSize:size])
	}

	storeMem := func(addr U64, size U64, value U64, proofIndexL uint8, proofIndexR uint8, verifyL bool, verifyR bool) {
		if addr >= 6229920 {
			if addr-6229920 <= 64 {
				tracef("storeMem: addr %d, value %d\n", addr, value)
				revertWithCode(0xbad512e7, fmt.Errorf("cannot store to memory address %d", addr))
			}
		} else {
			if 6229920-addr <= 64 {
				tracef("storeMem: addr %d, value %d\n", addr, value)
				revertWithCode(0xbad512e7, fmt.Errorf("cannot store to memory address %d", addr))
			}
		}
		if size > 8 {
			revertWithCode(0xbad512e8, fmt.Errorf("cannot store more than 8 bytes: %d", size))
		}
		var bytez [8]byte
		binary.LittleEndian.PutUint64(bytez[:], value)
		leftAddr := addr &^ 31
		if verifyL {
			inst.trackMemAccess(leftAddr, proofIndexL)
		}
		inst.verifyMemChange(leftAddr, proofIndexL)
		if (addr+size-1)&^31 == addr&^31 { // if aligned
			s.Memory.SetUnaligned(addr, bytez[:size])
			return
		}
		// if not aligned
		if proofIndexR == 0xff {
			revertWithCode(0xbad2222f, fmt.Errorf("unexpected need for right-side proof %d in storeMem", proofIndexR))
		}
		rightAddr := leftAddr + 32
		leftSize := rightAddr - addr
		s.Memory.SetUnaligned(addr, bytez[:leftSize])
		if verifyR {
			inst.trackMemAccess(rightAddr, proofIndexR)
		}
		inst.verifyMemChange(rightAddr, proofIndexR)
		s.Memory.SetUnaligned(rightAddr, bytez[leftSize:size])
	}

	//
	// CSR (control and status registers) functions
	//
	// Only the floating-point CSRs exist. RISC-V numbers them 0x001 fflags (the five
	// exception bits), 0x002 frm (the three rounding-mode bits) and 0x003 fcsr (both, with
	// frm in bits [7:5]). They are three views of one register, which is why they share
	// storage rather than being separate fields.
	//
	// Every other CSR reads as zero and ignores writes. That is a real limitation rather
	// than a safe default - a guest reading an unimplemented CSR gets a plausible answer
	// instead of a fault - but it cannot be tightened here: MLVM has no privilege levels and
	// no trap machinery, and guest startup code legitimately touches machine CSRs. The
	// riscv-tests preamble alone reads mhartid (0xF14) and writes mtvec (0x305), so making
	// unknown CSRs revert makes every conformance suite unrunnable.
	//
	// What matters for soundness is that all three implementations agree, and they do:
	// slow/vm.go and RISCV.sol mirror exactly this. See docs/ISA.md#known-gaps.
	const (
		csrFFlags = 0x001
		csrFRM    = 0x002
		csrFCSR   = 0x003
	)
	readCSR := func(num U64) U64 {
		switch num {
		case csrFFlags:
			return and64(s.FCSR, toU64(0x1F))
		case csrFRM:
			return and64(shr64(toU64(5), s.FCSR), toU64(0x7))
		case csrFCSR:
			return and64(s.FCSR, toU64(0xFF))
		}
		return toU64(0)
	}

	writeCSR := func(num U64, v U64) {
		switch num {
		case csrFFlags:
			s.FCSR = or64(and64(s.FCSR, toU64(0xE0)), and64(v, toU64(0x1F)))
		case csrFRM:
			s.FCSR = or64(and64(s.FCSR, toU64(0x1F)), shl64(toU64(5), and64(v, toU64(0x7))))
		case csrFCSR:
			s.FCSR = and64(v, toU64(0xFF))
		}
	}

	updateCSR := func(num U64, v U64, mode U64) (out U64) {
		out = readCSR(num)
		switch mode {
		case 1: // ?01 = CSRRW(I) - write v as-is
		case 2: // ?10 = CSRRS(I) - set the bits in v
			v = or64(out, v)
		case 3: // ?11 = CSRRC(I) - clear the bits in v
			v = and64(out, not64(v))
		default:
			revertWithCode(0xbadc0de0, fmt.Errorf("unkwown CSR mode: %d", mode))
		}
		writeCSR(num, v)
		return
	}

	//
	// Floating-point exception flags and rounding mode
	//
	// SoftFloat reports exceptions through a package-level word, as the C reference does.
	// beginFloatOp resets it and selects the rounding mode; endFloatOp folds whatever was
	// raised into fcsr. Between the two, nothing else may run - which holds because a step
	// executes exactly one instruction.
	//
	// The rounding mode comes from the instruction's rm field, except that rm == 7 means
	// "dynamic": use frm from the CSR. RISC-V's rm encoding and Berkeley's rounding-mode
	// numbering agree on 0..4 (RNE, RTZ, RDN, RUP, RMM), so no translation is needed.
	beginFloatOp := func(rm U64) {
		mode := rm
		if mode == 7 {
			mode = and64(shr64(toU64(5), s.FCSR), toU64(0x7))
		}
		if gt64(mode, toU64(4)) != 0 {
			revertWithCode(0xbadf1041, fmt.Errorf("reserved rounding mode: %d", mode))
		}
		Softfloat_exceptionFlags = 0
		Softfloat_roundingMode = uint8(mode)
	}
	// For OP-FP, funct3 carries the rounding mode only on the operations that round. On
	// FSGNJ, FMIN/FMAX, the comparisons, FCLASS and the register moves it selects the variant
	// instead, and feeding it to beginFloatOp would reject a perfectly legal instruction as a
	// reserved rounding mode.
	opFPRoundingMode := func(funct5, funct3 U64) U64 {
		switch funct5 {
		case 0x00, 0x01, 0x02, 0x03, 0x0B, 0x08, 0x18, 0x1A:
			return funct3
		}
		return toU64(0)
	}

	// RISC-V NaN-boxes single-precision values in the 64-bit float registers: a register
	// holds a valid single only if its upper 32 bits are all ones. Anything else - a double
	// that was never a single, a raw FMV.D.X - reads as the canonical single NaN.
	//
	// Without this, a single-precision instruction reading a register that happens to hold a
	// double would silently compute on the double's low half, which is neither what the
	// hardware does nor what a guest expects. rv64ud-p-move test_40 checks exactly that.
	unboxF32 := func(v U64) uint32 {
		if v&0xFFFF_FFFF_0000_0000 != 0xFFFF_FFFF_0000_0000 {
			return 0x7FC00000 // canonical single-precision NaN
		}
		return uint32(v)
	}

	endFloatOp := func() {
		s.FCSR = or64(s.FCSR, toU64(Softfloat_exceptionFlags))
	}

	//
	// Preimage oracle interactions
	//
	writePreimageKey := func(addr U64, count U64) U64 {
		// adjust count down, so we only have to read a single 32 byte leaf of memory
		alignment := and64(addr, toU64(31))
		maxData := sub64(toU64(32), alignment)
		if gt64(count, maxData) != 0 {
			count = maxData
		}

		dat := b32asBEWord(getMemoryB32(sub64(addr, alignment), 1))
		// shift out leading bits
		dat = shl(u64ToU256(shl64(toU64(3), alignment)), dat)
		// shift to right end, remove trailing bits
		dat = shr(u64ToU256(shl64(toU64(3), sub64(toU64(32), count))), dat)

		bits := shl(toU256(3), u64ToU256(count))

		preImageKey := getPreimageKey()

		// Append to key content by bit-shifting
		key := b32asBEWord(preImageKey)
		key = shl(bits, key)
		key = or(key, dat)

		// We reset the pre-image value offset back to 0 (the right part of the merkle pair)
		setPreimageKey(beWordAsB32(key))
		setPreimageOffset(toU64(0))
		return count
	}

	readPreimageValue := func(addr U64, count U64) U64 {
		preImageKey := getPreimageKey()
		offset := getPreimageOffset()

		pdatB32, pdatlen, err := inst.readPreimage(preImageKey, offset) // pdat is left-aligned
		if err != nil {
			revertWithCode(0xbadf00d0, err)
		}
		if iszero64(pdatlen) { // EOF
			return toU64(0)
		}
		alignment := and64(addr, toU64(31))    // how many bytes addr is offset from being left-aligned
		maxData := sub64(toU64(32), alignment) // higher alignment leaves less room for data this step
		if gt64(count, maxData) != 0 {
			count = maxData
		}
		if gt64(count, pdatlen) != 0 { // cannot read more than pdatlen
			count = pdatlen
		}

		bits := shl64(toU64(3), sub64(toU64(32), count))             // 32-count, in bits
		mask := not(sub(shl(u64ToU256(bits), toU256(1)), toU256(1))) // left-aligned mask for count bytes
		alignmentBits := u64ToU256(shl64(toU64(3), alignment))
		mask = shr(alignmentBits, mask)                  // mask of count bytes, shifted by alignment
		pdat := shr(alignmentBits, b32asBEWord(pdatB32)) // pdat, shifted by alignment

		// update pre-image reader with updated offset
		newOffset := add64(offset, count)
		setPreimageOffset(newOffset)

		node := getMemoryB32(sub64(addr, alignment), 1)
		dat := and(b32asBEWord(node), not(mask)) // keep old bytes outside of mask
		dat = or(dat, and(pdat, mask))           // fill with bytes from pdat
		setMemoryB32(sub64(addr, alignment), beWordAsB32(dat), 1)
		return count
	}

	// readStagedFileName reads a NUL-terminated openat path through getMemoryB32 so every
	// byte that can affect the file-system commitment is present in a step proof. The bound
	// keeps the proof surface finite: at most nine leaves, even at the worst alignment.
	readStagedFileName := func(addr U64) (string, error) {
		out := make([]byte, 0, MaxStagedFileNameLength)
		leafAddr := and64(addr, not64(toU64(31)))
		leafOffset := int(and64(addr, toU64(31)))
		proofIndex := uint8(1) // proof 0 is the instruction fetch

		for len(out) <= MaxStagedFileNameLength {
			leaf := getMemoryB32(leafAddr, proofIndex)
			proofIndex++
			for i := leafOffset; i < len(leaf); i++ {
				if leaf[i] == 0 {
					return string(out), nil
				}
				if len(out) == MaxStagedFileNameLength {
					return "", fmt.Errorf("pathname exceeds %d bytes", MaxStagedFileNameLength)
				}
				out = append(out, leaf[i])
			}
			leafAddr = add64(leafAddr, toU64(32))
			leafOffset = 0
		}
		panic("unreachable")
	}

	// copyStagedFileBytes performs one bounded short read. Source and destination ranges
	// each span at most two 32-byte leaves. Source proofs precede destination proofs, and a
	// destination's second proof is generated after its first leaf has been updated.
	copyStagedFileBytes := func(file *FileInfo, fileOffset, addr, count U64) U64 {
		if fileOffset > file.Size {
			revertWithCode(0xf001ca11, fmt.Errorf("file %q offset %d exceeds size %d", file.Name, fileOffset, file.Size))
		}
		n := min(count, min(toU64(MaxStagedFileReadSize), sub64(file.Size, fileOffset)))
		if n == 0 {
			return 0
		}

		sourceAddr := add64(file.Addr, fileOffset)
		sourceLeft := and64(sourceAddr, not64(toU64(31)))
		_ = getMemoryB32(sourceLeft, 1)
		nextProofIndex := uint8(2)
		if and64(add64(sourceAddr, sub64(n, toU64(1))), not64(toU64(31))) != sourceLeft {
			_ = getMemoryB32(add64(sourceLeft, toU64(32)), 2)
			nextProofIndex = 3
		}

		var data [MaxStagedFileReadSize]byte
		s.Memory.GetUnaligned(sourceAddr, data[:n])
		var value U256
		for i := range value {
			value[i] = binary.LittleEndian.Uint64(data[i*8 : (i+1)*8])
		}
		storeMemUnaligned(addr, n, value, nextProofIndex, nextProofIndex+1, true, true)
		return n
	}

	//
	// Syscall handling
	//
	sysCall := func() {
		a7 := getRegister(toU64(17))
		tracef("syscall: %d\n", a7)
		switch a7 {
		case 93: // exit the calling thread. No multi-thread support yet, so just exit.
			a0 := getRegister(toU64(10))
			tracef("exit: %d\n", a0)
			setExitCode(uint8(a0))
			setExited()
			// program stops here, no need to change registers.
		case 94: // exit-group
			a0 := getRegister(toU64(10))
			setExitCode(uint8(a0))
			setExited()
		case 214: // brk
			// Go sys_linux_riscv64 runtime will only ever call brk(NULL), i.e. first argument (register a0) set to 0.

			// brk(0) changes nothing about the memory, and returns the current page break
			v := shl64(toU64(30), toU64(1)) // set program break at 1 GiB
			setRegister(toU64(10), v)
			setRegister(toU64(11), toU64(0)) // no error
		case 215: // munmap
			addr := getRegister(toU64(10)) // A0 = addr
			length := getRegister(toU64(11))
			tracef("munmap: requested to unmap memory from address 0x%016x with length 0x%x\n", addr, length)
			// ignore the request, as we don't support any memory un-mapping
			setRegister(toU64(10), toU64(0)) // no error
			setRegister(toU64(11), toU64(0)) // no error
		case 222: // mmap
			// A0 = addr (hint)
			addr := getRegister(toU64(10))
			// A1 = n (length)
			length := getRegister(toU64(11))
			tracef("mmap: requested to map memory at address 0x%x with length 0x%x, but actually allocate with length 0x%x\n", addr, length, uint64(1<<36))
			length = uint64(1 << 36)
			// A2 = prot (memory protection type, can ignore)
			// A3 = flags (shared with other process and or written back to file, can ignore)  // TODO maybe assert the MAP_ANONYMOUS flag is set
			// A4 = fd (file descriptor, can ignore because we support anon memory only)
			// A5 = offset (offset in file, we don't support any non-anon memory, so we can ignore this)

			// ignore: prot, flags, fd, offset
			switch addr {
			case 0:
				// No hint, allocate it ourselves, by as much as the requested length.
				// Increase the length to align it with desired page size if necessary.
				align := and64(length, shortToU64(4095))
				if align != 0 {
					length = add64(length, sub64(shortToU64(4096), align))
				}
				prevHeap := getHeap()
				setRegister(toU64(10), prevHeap) // return the previous heap
				setHeap(add64(prevHeap, length)) // increment heap with length
				//tracef("mmap: 0x%016x (+ 0x%x increase)\n", s.Heap, length)
			default:
				// allow hinted memory address (leave it in A0 as return argument)
				//tracef("mmap: 0x%016x (0x%x allowed)\n", addr, length)
			}
			setRegister(toU64(11), toU64(0)) // no error
		case 216: // mremap
			// A0 = old_addr
			oldAddr := getRegister(toU64(10))
			// A1 = old_size
			oldSize := getRegister(toU64(11))
			// A2 = new_size
			newSize := getRegister(toU64(12))
			// A3 = flags
			// A4 = new_addr
			// A5 = new_size
			tracef("mremap: requested to remap memory from address 0x%x with length 0x%x to new size 0x%x\n", oldAddr, oldSize, newSize)
			if newSize >= uint64(1<<36) {
				revertWithCode(0xf001ca11, fmt.Errorf("mremap: too much memory request (max is 64GB)"))
			}

			setRegister(toU64(10), oldAddr)  // return the old address
			setRegister(toU64(11), toU64(0)) // no error
			// revertWithCode(0xf001ca11, fmt.Errorf("unrecognized system call: %d", a7))
		case 63: // read
			fd := getRegister(toU64(10))    // A0 = fd
			addr := getRegister(toU64(11))  // A1 = *buf addr
			count := getRegister(toU64(12)) // A2 = count
			var n U64
			var errCode U64
			switch fd {
			case fdStdin: // stdin
				n = toU64(0) // never read anything from stdin
				errCode = toU64(0)
			case fdHintRead: // hint-read
				// say we read it all, to continue execution after reading the hint-write ack response
				n = count
				tracef("hint-read: %d bytes\n", count)
				errCode = toU64(0)
			case fdPreimageRead: // preimage read
				n = readPreimageValue(addr, count)
				tracef("preimage-read: %d bytes\n", n)
				errCode = toU64(0)
			default:
				filename, fileOffset, exists := s.FileSystem.Descriptor(fd)
				if fd <= MaxStagedFD {
					inst.trackFileSystemAccess("", fd)
				}
				if !exists {
					tracef("file descriptor %d does not exist, default handling", fd)
					n = u64Mask()         //  -1 (reading error)
					errCode = toU64(0x4d) // EBADF
				} else {
					tracef("want to read file %d for %d bytes\n", fd, count)
					// read from filename in the file system
					file, exists := s.FileSystem.File(filename)
					if !exists {
						revertWithCode(0xf001ca11, fmt.Errorf("file %q does not exist", filename))
					}
					// POSIX permits short reads. Bound this transition to one 32-byte value
					// so its source and destination each require at most two Merkle proofs.
					tracef("file.Addr = 0x%x, fileOffset = %d\n", file.Addr, fileOffset)
					n = copyStagedFileBytes(file, fileOffset, addr, count)
					if err := s.FileSystem.SetDescriptorOffset(fd, add64(fileOffset, n)); err != nil {
						revertWithCode(0xf001ca11, err)
					}
					errCode = toU64(0)
					tracef("file read: actually read %d bytes from %s\n", n, filename)
				}
			}
			setRegister(toU64(10), n)
			setRegister(toU64(11), errCode)
		case 64: // write
			fd := getRegister(toU64(10))    // A0 = fd
			addr := getRegister(toU64(11))  // A1 = *buf addr
			count := getRegister(toU64(12)) // A2 = count
			var n U64
			var errCode U64
			switch fd {
			case fdStdout: // stdout
				_, err := io.Copy(inst.stdOut, s.Memory.ReadMemoryRange(addr, count))
				if err != nil {
					panic(fmt.Errorf("stdout writing err: %w", err))
				}
				n = count // write completes fully in single instruction step
				errCode = toU64(0)
			case fdStderr: // stderr
				_, err := io.Copy(inst.stdErr, s.Memory.ReadMemoryRange(addr, count))
				if err != nil {
					panic(fmt.Errorf("stderr writing err: %w", err))
				}
				n = count // write completes fully in single instruction step
				errCode = toU64(0)
			case fdHintWrite: // hint-write
				hintData, _ := io.ReadAll(s.Memory.ReadMemoryRange(addr, count))
				s.LastHint = append(inst.state.LastHint, hintData...)
				for len(s.LastHint) >= 4 { // process while there is enough data to check if there are any hints
					hintLen := binary.BigEndian.Uint32(s.LastHint[:4])
					if hintLen >= uint32(len(s.LastHint[4:])) {
						hint := s.LastHint[4 : 4+hintLen] // without the length prefix
						s.LastHint = s.LastHint[4+hintLen:]
						inst.preimageOracle.Hint(hint)
					} else {
						break // stop processing hints if there is incomplete data buffered
					}
				}
				n = count
				errCode = toU64(0)
			case fdPreimageWrite: // pre-image key write
				n = writePreimageKey(addr, count)
				errCode = toU64(0) // no error
			default: // any other file, including (3) hint read (5) preimage read
				n = u64Mask()         //  -1 (writing error)
				errCode = toU64(0x4d) // EBADF
			}
			setRegister(toU64(10), n)
			setRegister(toU64(11), errCode)
		case 25: // fcntl - file descriptor manipulation / info lookup
			fd := getRegister(toU64(10))  // A0 = fd
			cmd := getRegister(toU64(11)) // A1 = cmd
			var out U64
			var errCode U64
			switch cmd {
			case 0x1: // F_GETFD: get the close-on-exec flag
				// Go 1.21+ calls this on fds 0, 1 and 2 during runtime.checkfds, before
				// main() runs. Returning EINVAL makes it conclude the descriptors are
				// unusable and die with "fatal error: cannot open standard fds", which is
				// one of two independent reasons Go guests do not currently boot.
				//
				// 0 means "not close-on-exec", which is the truth: there is no exec here.
				switch fd {
				case fdStdin, fdStdout, fdStderr, fdHintRead, fdHintWrite, fdPreimageRead, fdPreimageWrite:
					out = toU64(0)
				default:
					_, _, exists := s.FileSystem.Descriptor(fd)
					if fd <= MaxStagedFD {
						inst.trackFileSystemAccess("", fd)
					}
					if exists {
						out = toU64(0)
					} else {
						out = u64Mask()
						errCode = toU64(0x4d) // EBADF
					}
				}
			case 0x3: // F_GETFL: get file descriptor flags
				switch fd {
				case 0: // stdin
					out = toU64(0) // O_RDONLY
				case 1: // stdout
					out = toU64(1) // O_WRONLY
				case 2: // stderr
					out = toU64(1) // O_WRONLY
				case 3: // hint-read
					out = toU64(0) // O_RDONLY
				case 4: // hint-write
					out = toU64(1) // O_WRONLY
				case 5: // pre-image read
					out = toU64(0) // O_RDONLY
				case 6: // pre-image write
					out = toU64(1) // O_WRONLY
				default:
					_, _, exists := s.FileSystem.Descriptor(fd)
					if fd <= MaxStagedFD {
						inst.trackFileSystemAccess("", fd)
					}
					if exists {
						out = toU64(0) // staged files are O_RDONLY
					} else {
						out = u64Mask()
						errCode = toU64(0x4d) // EBADF
					}
				}
			default: // no other commands: don't allow changing flags, duplicating FDs, etc.
				out = u64Mask()
				errCode = toU64(0x16) // EINVAL (cmd not recognized by this kernel)
			}
			setRegister(toU64(10), out)
			setRegister(toU64(11), errCode) // EBADF
		case 278: // getrandom - get random bytes from go runtime's random generator
			// in this case, only return 0 as random number, as we don't have a real random generator in solidity
			addr := getRegister(toU64(10))  // A0 = *buf addr
			count := getRegister(toU64(11)) // A1 = count
			randomNumberFileAddr := s.RandomFileAddr
			randomNumberFileOffset := s.RandomFileOffset
			tracef("getrandom: %d bytes\n", count)
			if count == 0 {
				setRegister(toU64(10), toU64(0))
				setRegister(toU64(11), toU64(0)) // no error
			} else {
				// max load 8 bytes for now
				if count > 8 {
					// warning
					tracef("[warning]: getrandom: count > 8, only 8 bytes will be loaded\n")
					count = 8
				}
				randomValue := loadMem(randomNumberFileAddr+randomNumberFileOffset, count, false, 1, 2)
				// imitate Atomic Memory Operation to generate proof
				storeMem(addr, count, randomValue, 1, 3, false, true) // after overwriting 1, proof 2 is no longer valid
				randomNumberFileOffset = add64(randomNumberFileOffset, count)
				if randomNumberFileOffset >= RandomFileSize {
					tracef("[warning]: getrandom: random number file offset reached 5MB, reset to 0\n")
					randomNumberFileOffset = 0
				}
				s.RandomFileOffset = randomNumberFileOffset
				tracef("getrandom: %d bytes of value %d, now randomNumberOffset = %d\n", count, randomValue, randomNumberFileOffset)
				setRegister(toU64(10), count)
				setRegister(toU64(11), toU64(0)) // no error
			}
		case 56: // openat - the Go linux runtime will try to open optional /sys/kernel files for performance hints
			pathname := getRegister(toU64(11)) // A1 = *pathname
			// flags := getRegister(toU64(12))    // A2 = flags
			// mode := getRegister(toU64(13))     // A3 = mode
			filename, error := readStagedFileName(pathname)
			if error != nil {
				revertWithCode(0xf001ca11, fmt.Errorf("in openat syscall: failed to read pathname: %w", error))
			}
			tracef("openat: %s\n", filename)
			switch {
			case s.FileSystem.Has(filename):
				// The file was staged into emulated memory at load time. Hand out a
				// descriptor; read/lseek will serve it straight out of the merkleized
				// address space. Descriptors start at 100 to stay clear of the reserved
				// 0-6 (stdio, hint and pre-image channels).
				fd := s.FileSystem.NextFD()
				inst.trackFileSystemAccess(filename, fd)
				if _, err := s.FileSystem.Open(filename); err != nil {
					revertWithCode(0xf001ca11, err)
				}
				setRegister(toU64(10), fd)
				setRegister(toU64(11), toU64(0)) // no error
				file, _ := s.FileSystem.File(filename)
				tracef("openat succeed: %s -> %d, at memory %x\n", filename, fd, file.Addr)

			case isKernelPseudoFile(filename):
				// Kernel-provided pseudo-files. Both the Go runtime and glibc probe a
				// long tail of these for optional performance hints - hugepage size,
				// CPU topology, cgroup limits, /proc/self/maps - and every one of them
				// is a host detail that must not leak into a deterministic execution.
				//
				// Refusing with EACCES is what makes that safe: every libc and runtime
				// treats it as "this system does not expose that", and falls back to a
				// built-in default. Reverting instead would make the VM unable to host
				// any real runtime, because the exact set of paths probed varies by libc
				// version.
				tracef("openat denied (host detail): %s\n", filename)
				setRegister(toU64(10), u64Mask())  // -1
				setRegister(toU64(11), toU64(0xd)) // EACCES

			default:
				// A real file the program expects but nobody staged. This is almost
				// always a missing --load-file rather than a program that can cope, so
				// fail loudly instead of returning ENOENT and letting it fail later in a
				// harder-to-diagnose way.
				revertWithCode(0xf001ca11, fmt.Errorf("file %q was not staged into the VM; pass --load-file %s", filename, filename))
			}
		case 57: // close
			fd := getRegister(toU64(10)) // A0 = fd
			tracef("close: %d\n", fd)
			filename, _, exists := s.FileSystem.Descriptor(fd)
			if !exists {
				revertWithCode(0xf001ca11, fmt.Errorf("file descriptor %d does not exist", fd))
			}
			inst.trackFileSystemAccess(filename, fd)
			if err := s.FileSystem.CloseFile(fd); err != nil {
				revertWithCode(0xf001ca11, fmt.Errorf("failed to close file descriptor %d: %w", fd, err))
			}
			setRegister(toU64(10), toU64(0)) // no error
			setRegister(toU64(11), toU64(0)) // no error
		case 435: // clone3 - ignore any clone request
			setRegister(toU64(10), u64Mask())
			setRegister(toU64(11), toU64(0))
		case 62: // lsseek - lseek
			fd := getRegister(toU64(10))     // A0 = fd
			offset := getRegister(toU64(11)) // A1 = offset
			whence := getRegister(toU64(12)) // A2 = whence
			tracef("lseek: fd: %d, offset: %d, whence: %d\n", fd, offset, whence)
			var errCode U64
			var out U64
			filename, currentOffset, exists := s.FileSystem.Descriptor(fd)
			if !exists {
				revertWithCode(0xf001ca11, fmt.Errorf("file descriptor %d does not exist", fd))
			}
			inst.trackFileSystemAccess(filename, fd)
			file, exists := s.FileSystem.File(filename)
			if !exists {
				revertWithCode(0xf001ca11, fmt.Errorf("file %q does not exist", filename))
			}
			fileSize := file.Size
			switch whence {
			case 0: // SEEK_SET
				if offset > fileSize {
					errCode = toU64(0x22) // EINVAL
				} else {
					if err := s.FileSystem.SetDescriptorOffset(fd, offset); err != nil {
						revertWithCode(0xf001ca11, err)
					}
					out = offset
				}
			case 1: // SEEK_CUR
				if offset > fileSize-currentOffset {
					errCode = toU64(0x22) // EINVAL
				} else {
					out = add64(currentOffset, offset)
					if err := s.FileSystem.SetDescriptorOffset(fd, out); err != nil {
						revertWithCode(0xf001ca11, err)
					}
				}
			case 2: // SEEK_END
				if offset > fileSize {
					errCode = toU64(0x22) // EINVAL
				} else {
					out = sub64(fileSize, offset)
					if err := s.FileSystem.SetDescriptorOffset(fd, out); err != nil {
						revertWithCode(0xf001ca11, err)
					}
				}
			default:
				revertWithCode(0xf001ca11, fmt.Errorf("unrecognized whence: %d", whence))
			}
			setRegister(toU64(10), out)
			setRegister(toU64(11), errCode)
		case 291: // statx
			// int statx(int dirfd, const char *restrict pathname, int flags, unsigned int mask, struct statx *restrict statxbuf);
			// dirfd := getRegister(toU64(10))    // A0 = dirfd
			// flags := getRegister(toU64(12))    // A2 = flags
			// mask := getRegister(toU64(13))     // A3 = mask
			// statxbuf := getRegister(toU64(14)) // A4 = *statxbuf
			// tracef("statx: dirfd: %d, filename: %v, flags: %d, mask: %d, statxbuf: %d\n", dirfd, filename, flags, mask, statxbuf)
			// is that ok to return 0 directly?
			setRegister(toU64(10), toU64(0)) // no error
			setRegister(toU64(11), toU64(0)) // no error
		case 258: // riscv_hwprobe - query which RISC-V extensions the CPU implements
			// Refused, deliberately. Reporting the host's extensions would make the trace
			// depend on the machine that produced it, and reporting our own would let a
			// guest branch on them - either way two parties could disagree. ENOSYS is what
			// a kernel without this call returns, and every caller has a fallback for it.
			//
			// Newer Go and glibc probe this; without the case the VM reverts on an
			// unrecognised syscall.
			setRegister(toU64(10), u64Mask()) // -1
			setRegister(toU64(11), toU64(38)) // ENOSYS
		case 123: // sched_getaffinity - hardcode to indicate affinity with any cpu-set mask
			setRegister(toU64(10), toU64(0))
			setRegister(toU64(11), toU64(0))
		case 226: // mprotect - ignore any memory protection requests
			setRegister(toU64(10), toU64(0))
			setRegister(toU64(11), toU64(0))
		case 124: // sched_yield - nothing to yield, synchronous execution only, for now
			setRegister(toU64(10), toU64(0))
			setRegister(toU64(11), toU64(0))
		case 73: // ppoll_time32 - ignore any poll request
			setRegister(toU64(10), toU64(0))
			setRegister(toU64(11), toU64(0))
		case 113: // clock_gettime
			addr := getRegister(toU64(11)) // addr of timespec struct
			// write 1337s + 42ns as time
			storeMemUnaligned(addr, toU64(8), shortToU256(1337), 1, 0xff, true, false)
			storeMemUnaligned(add64(addr, toU64(8)), toU64(8), toU256(42), 2, 0xff, true, false)
			setRegister(toU64(10), toU64(0))
			setRegister(toU64(11), toU64(0))
		case 135: // rt_sigprocmask - ignore any sigset changes
			setRegister(toU64(10), toU64(0))
			setRegister(toU64(11), toU64(0))
		case 132: // sigaltstack - ignore any hints of an alternative signal receiving stack addr
			setRegister(toU64(10), toU64(0))
			setRegister(toU64(11), toU64(0))
		case 178: // gettid - hardcode to 0
			setRegister(toU64(10), toU64(0))
			setRegister(toU64(11), toU64(0))
		case 179: // sysinfo - return a fixed, committed view of "the machine"
			// glibc probes this during startup. Host values would poison the trace, so
			// every field is a protocol constant: 1337s uptime, 1 GiB total/free RAM,
			// mem_unit=1 at Linux uapi offset 104, everything else zero.
			// Layout matches include/uapi/linux/sysinfo.h on RV64 (112-byte padded).
			addr := getRegister(toU64(10))
			nextProof := uint8(1) // proof 0 is the ECALL instruction
			storeChunk := func(offset U64, value U256) {
				chunkAddr := add64(addr, offset)
				leftAddr := chunkAddr &^ 31
				rightAddr := (chunkAddr + 15) &^ 31
				storeMemUnaligned(chunkAddr, toU64(16), value, nextProof, nextProof+1, true, true)
				nextProof++
				if leftAddr != rightAddr {
					nextProof++
				}
			}
			storeChunk(toU64(0), shortToU256(1337))
			storeChunk(toU64(16), toU256(0))
			storeChunk(toU64(32), or(longToU256(1<<30), shl(toU256(64), longToU256(1<<30))))
			storeChunk(toU64(48), toU256(0))
			storeChunk(toU64(64), toU256(0))
			storeChunk(toU64(80), toU256(0))
			storeChunk(toU64(96), shl(toU256(64), toU256(1)))
			setRegister(toU64(10), toU64(0))
			setRegister(toU64(11), toU64(0))
		case 134: // rt_sigaction - no-op, we never send signals, and thus need no sig handler info
			setRegister(toU64(10), toU64(0))
			setRegister(toU64(11), toU64(0))
		case 220: // clone - not supported
			setRegister(toU64(10), toU64(1))
			setRegister(toU64(11), toU64(0))
		case 163: // getrlimit
			res := getRegister(toU64(10))
			addr := getRegister(toU64(11))
			switch res {
			case 0x7: // RLIMIT_NOFILE
				// first 8 bytes: soft limit. 1024 file handles max open
				// second 8 bytes: hard limit
				storeMemUnaligned(addr, toU64(16), or(shortToU256(1024), shl(toU256(64), shortToU256(1024))), 1, 2, true, true)
			default:
				revertWithCode(0xf0012, fmt.Errorf("unrecognized resource limit lookup: %d", res))
			}
		case 233: // madvise - ignored
			setRegister(toU64(10), toU64(0))
			setRegister(toU64(11), toU64(0))
		case 79: // stat - ignored
			setRegister(toU64(10), toU64(0))
			setRegister(toU64(11), toU64(0)) // EBADF
		case 96: // set_tid_address - ignored
			setRegister(toU64(10), toU64(0))
			setRegister(toU64(11), toU64(0))
		case 98: // futex - ignored
			setRegister(toU64(10), toU64(0))
			setRegister(toU64(11), toU64(0))
		case 99: // get_robust_list - ignored
			setRegister(toU64(10), toU64(0))
			setRegister(toU64(11), toU64(0))
		case 261: // prlimit64 - ignored
			setRegister(toU64(10), toU64(0))
			setRegister(toU64(11), toU64(0))
		case 78: // readlinkat - ignored
			setRegister(toU64(10), toU64(0))
			setRegister(toU64(11), toU64(0)) // EBADF
		// case 261: // prlimit64 -- unsupported, we have getrlimit, is prlimit64 even called?
		// 	revertWithCode(0xf001ca11, fmt.Errorf("unsupported system call: %d", a7))
		case 422: // futex - not supported, for now
			revertWithCode(0xf001ca11, fmt.Errorf("unsupported system call: %d", a7))
		case 101: // nanosleep - not supported, for now
			revertWithCode(0xf001ca11, fmt.Errorf("unsupported system call: %d", a7))
		default:
			revertWithCode(0xf001ca11, fmt.Errorf("unrecognized system call: %d", a7))
		}
	}

	//
	// Instruction execution
	//

	if getExited() { // early exit if we can
		return nil
	}
	setStep(add64(getStep(), toU64(1)))

	pc := getPC()
	instr := loadMem(pc, toU64(4), false, 0, 0xff) // raw instruction

	// these fields are ignored if not applicable to the instruction type / opcode
	opcode := parseOpcode(instr)
	rd := parseRd(instr) // destination register index
	funct3 := parseFunct3(instr)
	rs1 := parseRs1(instr) // source register 1 index
	rs2 := parseRs2(instr) // source register 2 index
	rs3 := parseRs3(instr) // source register 3 index
	funct7 := parseFunct7(instr)
	funct5 := parseFunct5(instr)

	// printReg := func() {
	// 	tracef("pc: 0x%x\n", pc)
	// 	for i := 0; i < 32; i++ {
	// 		tracef("x%d: 0x%x\n", i, getRegister(uint64(i)))
	// 	}
	// 	for i := 0; i < 32; i++ {
	// 		tracef("f%d: 0x%x\n", i, getFRegister(uint64(i)))
	// 	}
	// }

	// check32NaN := func(v uint32) {
	// 	v = v & 0xFFFFFFFF
	// 	exp := (v >> 23) & 0xFF
	// 	frac := v & 0x7FFFFF
	// 	if exp == 0xFF && frac != 0 {
	// 		// tracef("NaN detected: %x\n", v)
	// 		// tracef("pc: 0x%x\n", pc)
	// 		// printReg()
	// 		// revertWithCode(0xf001ca11, fmt.Errorf("NaN detected"))
	// 	}
	// }

	// check64NaN := func(v uint64) {
	// 	exp := (v >> 52) & 0x7FF
	// 	frac := v & 0xFFFFFFFFFFFFF
	// 	if exp == 0x7FF && frac != 0 {
	// 		// tracef("NaN detected: %x\n", v)
	// 		// tracef("pc: 0x%x\n", pc)
	// 		// printReg()
	// 		// revertWithCode(0xf001ca11, fmt.Errorf("NaN detected"))
	// 	}
	// }

	switch opcode {
	case 0x03: // 000_0011: memory loading
		// LB, LH, LW, LD, LBU, LHU, LWU
		s.OpCount.Mem++
		imm := parseImmTypeI(instr)
		signed := iszero64(and64(funct3, toU64(4)))      // 4 = 100 -> bitflag
		size := shl64(and64(funct3, toU64(3)), toU64(1)) // 3 = 11 -> 1, 2, 4, 8 bytes size
		rs1Value := getRegister(rs1)
		memIndex := add64(rs1Value, signExtend64(imm, toU64(11)))
		rdValue := loadMem(memIndex, size, signed, 1, 2)
		setRegister(rd, rdValue)
		setPC(add64(pc, toU64(4)))
	case 0x23: // 010_0011: memory storing
		// SB, SH, SW, SD
		imm := parseImmTypeS(instr)
		s.OpCount.Mem++
		size := shl64(funct3, toU64(1))
		value := getRegister(rs2)
		rs1Value := getRegister(rs1)
		memIndex := add64(rs1Value, signExtend64(imm, toU64(11)))
		storeMem(memIndex, size, value, 1, 2, true, true)
		setPC(add64(pc, toU64(4)))
	case 0x63: // 110_0011: branching
		rs1Value := getRegister(rs1)
		rs2Value := getRegister(rs2)
		branchHit := toU64(0)
		switch funct3 {
		case 0: // 000 = BEQ
			branchHit = eq64(rs1Value, rs2Value)
		case 1: // 001 = BNE
			branchHit = and64(not64(eq64(rs1Value, rs2Value)), toU64(1))
		case 4: // 100 = BLT
			branchHit = slt64(rs1Value, rs2Value)
		case 5: // 101 = BGE
			branchHit = and64(not64(slt64(rs1Value, rs2Value)), toU64(1))
		case 6: // 110 = BLTU
			branchHit = lt64(rs1Value, rs2Value)
		case 7: // 111 = BGEU
			branchHit = and64(not64(lt64(rs1Value, rs2Value)), toU64(1))
		}
		switch branchHit {
		case 0:
			pc = add64(pc, toU64(4))
		default:
			imm := parseImmTypeB(instr)
			// imm is a signed offset, in multiples of 2 bytes.
			// So it's really 13 bits with a hardcoded 0 bit.
			pc = add64(pc, imm)
		}
		// not like the other opcodes: nothing to write to rd register, and PC has already changed
		setPC(pc)
	case 0x13: // 001_0011: immediate arithmetic and logic
		rs1Value := getRegister(rs1)
		imm := parseImmTypeI(instr)
		s.OpCount.Arith++
		var rdValue U64
		switch funct3 {
		case 0: // 000 = ADDI
			rdValue = add64(rs1Value, imm)
		case 1: // 001 = SLLI
			rdValue = shl64(and64(imm, toU64(0x3F)), rs1Value) // lower 6 bits in 64 bit mode
		case 2: // 010 = SLTI
			rdValue = slt64(rs1Value, imm)
		case 3: // 011 = SLTIU
			rdValue = lt64(rs1Value, imm)
		case 4: // 100 = XORI
			rdValue = xor64(rs1Value, imm)
		case 5: // 101 = SR~
			switch shr64(toU64(6), imm) { // in rv64i the top 6 bits select the shift type
			case 0x00: // 000000 = SRLI
				rdValue = shr64(and64(imm, toU64(0x3F)), rs1Value) // lower 6 bits in 64 bit mode
			case 0x10: // 010000 = SRAI
				rdValue = sar64(and64(imm, toU64(0x3F)), rs1Value) // lower 6 bits in 64 bit mode
			}
		case 6: // 110 = ORI
			rdValue = or64(rs1Value, imm)
		case 7: // 111 = ANDI
			rdValue = and64(rs1Value, imm)
		}
		setRegister(rd, rdValue)
		setPC(add64(pc, toU64(4)))
	case 0x1B: // 001_1011: immediate arithmetic and logic signed 32 bit
		rs1Value := getRegister(rs1)
		s.OpCount.Arith++
		imm := parseImmTypeI(instr)
		var rdValue U64
		switch funct3 {
		case 0: // 000 = ADDIW
			rdValue = mask32Signed64(add64(rs1Value, imm))
		case 1: // 001 = SLLIW
			rdValue = mask32Signed64(shl64(and64(imm, toU64(0x1F)), rs1Value))
		case 5: // 101 = SR~
			shamt := and64(imm, toU64(0x1F))
			switch shr64(toU64(6), imm) { // in rv64i the top 6 bits select the shift type
			case 0x00: // 000000 = SRLIW
				rdValue = signExtend64(shr64(shamt, and64(rs1Value, u32Mask())), toU64(31))
			case 0x10: // 010000 = SRAIW
				rdValue = signExtend64(shr64(shamt, and64(rs1Value, u32Mask())), sub64(toU64(31), shamt))
			}
		}
		setRegister(rd, rdValue)
		setPC(add64(pc, toU64(4)))
	case 0x33: // 011_0011: register arithmetic and logic
		rs1Value := getRegister(rs1)
		s.OpCount.Arith++
		rs2Value := getRegister(rs2)
		var rdValue U64
		switch funct7 {
		case 1: // RV M extension
			switch funct3 {
			case 0: // 000 = MUL: signed x signed
				rdValue = mul64(rs1Value, rs2Value)
			case 1: // 001 = MULH: upper bits of signed x signed
				rdValue = u256ToU64(shr(toU256(64), mul(signExtend64To256(rs1Value), signExtend64To256(rs2Value))))
			case 2: // 010 = MULHSU: upper bits of signed x unsigned
				rdValue = u256ToU64(shr(toU256(64), mul(signExtend64To256(rs1Value), u64ToU256(rs2Value))))
			case 3: // 011 = MULHU: upper bits of unsigned x unsigned
				rdValue = u256ToU64(shr(toU256(64), mul(u64ToU256(rs1Value), u64ToU256(rs2Value))))
			case 4: // 100 = DIV
				switch rs2Value {
				case 0:
					rdValue = u64Mask()
				default:
					rdValue = sdiv64(rs1Value, rs2Value)
				}
			case 5: // 101 = DIVU
				switch rs2Value {
				case 0:
					rdValue = u64Mask()
				default:
					rdValue = div64(rs1Value, rs2Value)
				}
			case 6: // 110 = REM
				switch rs2Value {
				case 0:
					rdValue = rs1Value
				default:
					rdValue = smod64(rs1Value, rs2Value)
				}
			case 7: // 111 = REMU
				switch rs2Value {
				case 0:
					rdValue = rs1Value
				default:
					rdValue = mod64(rs1Value, rs2Value)
				}
			}
		default:
			switch funct3 {
			case 0: // 000 = ADD/SUB
				switch funct7 {
				case 0x00: // 0000000 = ADD
					rdValue = add64(rs1Value, rs2Value)
				case 0x20: // 0100000 = SUB
					rdValue = sub64(rs1Value, rs2Value)
				}
			case 1: // 001 = SLL
				rdValue = shl64(and64(rs2Value, toU64(0x3F)), rs1Value) // only the low 6 bits are consider in RV6VI
			case 2: // 010 = SLT
				rdValue = slt64(rs1Value, rs2Value)
			case 3: // 011 = SLTU
				rdValue = lt64(rs1Value, rs2Value)
			case 4: // 100 = XOR
				rdValue = xor64(rs1Value, rs2Value)
			case 5: // 101 = SR~
				switch funct7 {
				case 0x00: // 0000000 = SRL
					rdValue = shr64(and64(rs2Value, toU64(0x3F)), rs1Value) // logical: fill with zeroes
				case 0x20: // 0100000 = SRA
					rdValue = sar64(and64(rs2Value, toU64(0x3F)), rs1Value) // arithmetic: sign bit is extended
				}
			case 6: // 110 = OR
				rdValue = or64(rs1Value, rs2Value)
			case 7: // 111 = AND
				rdValue = and64(rs1Value, rs2Value)
			}
		}
		setRegister(rd, rdValue)
		setPC(add64(pc, toU64(4)))
	case 0x3B: // 011_1011: register arithmetic and logic in 32 bits
		rs1Value := getRegister(rs1)
		s.OpCount.Arith++
		rs2Value := getRegister(rs2)
		var rdValue U64
		switch funct7 {
		case 1: // RV M extension
			switch funct3 {
			case 0: // 000 = MULW
				rdValue = mask32Signed64(mul64(and64(rs1Value, u32Mask()), and64(rs2Value, u32Mask())))
			case 4: // 100 = DIVW
				switch rs2Value {
				case 0:
					rdValue = u64Mask()
				default:
					rdValue = mask32Signed64(sdiv64(mask32Signed64(rs1Value), mask32Signed64(rs2Value)))
				}
			case 5: // 101 = DIVUW
				switch rs2Value {
				case 0:
					rdValue = u64Mask()
				default:
					rdValue = mask32Signed64(div64(and64(rs1Value, u32Mask()), and64(rs2Value, u32Mask())))
				}
			case 6: // 110 = REMW
				switch rs2Value {
				case 0:
					rdValue = mask32Signed64(rs1Value)
				default:
					rdValue = mask32Signed64(smod64(mask32Signed64(rs1Value), mask32Signed64(rs2Value)))
				}
			case 7: // 111 = REMUW
				switch rs2Value {
				case 0:
					rdValue = mask32Signed64(rs1Value)
				default:
					rdValue = mask32Signed64(mod64(and64(rs1Value, u32Mask()), and64(rs2Value, u32Mask())))
				}
			}
		default:
			switch funct3 {
			case 0: // 000 = ADDW/SUBW
				switch funct7 {
				case 0x00: // 0000000 = ADDW
					rdValue = mask32Signed64(add64(and64(rs1Value, u32Mask()), and64(rs2Value, u32Mask())))
				case 0x20: // 0100000 = SUBW
					rdValue = mask32Signed64(sub64(and64(rs1Value, u32Mask()), and64(rs2Value, u32Mask())))
				}
			case 1: // 001 = SLLW
				rdValue = mask32Signed64(shl64(and64(rs2Value, toU64(0x1F)), rs1Value))
			case 5: // 101 = SR~
				shamt := and64(rs2Value, toU64(0x1F))
				switch funct7 {
				case 0x00: // 0000000 = SRLW
					rdValue = signExtend64(shr64(shamt, and64(rs1Value, u32Mask())), toU64(31))
				case 0x20: // 0100000 = SRAW
					rdValue = signExtend64(shr64(shamt, and64(rs1Value, u32Mask())), sub64(toU64(31), shamt))
				}
			}
		}
		setRegister(rd, rdValue)
		setPC(add64(pc, toU64(4)))
	case 0x37: // 011_0111: LUI = Load upper immediate
		imm := parseImmTypeU(instr)
		rdValue := shl64(toU64(12), imm)
		setRegister(rd, rdValue)
		setPC(add64(pc, toU64(4)))
	case 0x17: // 001_0111: AUIPC = Add upper immediate to PC
		imm := parseImmTypeU(instr)
		rdValue := add64(pc, signExtend64(shl64(toU64(12), imm), toU64(31)))
		s.OpCount.Arith++
		setRegister(rd, rdValue)
		setPC(add64(pc, toU64(4)))
	case 0x6F: // 110_1111: JAL = Jump and link
		imm := parseImmTypeJ(instr)
		rdValue := add64(pc, toU64(4))
		setRegister(rd, rdValue)
		setPC(add64(pc, signExtend64(shl64(toU64(1), imm), toU64(20)))) // signed offset in multiples of 2 bytes (last bit is there, but ignored)
	case 0x67: // 110_0111: JALR = Jump and link register
		rs1Value := getRegister(rs1)
		imm := parseImmTypeI(instr)
		rdValue := add64(pc, toU64(4))
		setRegister(rd, rdValue)
		setPC(and64(add64(rs1Value, signExtend64(imm, toU64(11))), xor64(u64Mask(), toU64(1)))) // least significant bit is set to 0
	case 0x73: // 111_0011: environment things
		switch funct3 {
		case 0: // 000 = ECALL/EBREAK
			switch shr64(toU64(20), instr) { // I-type, top 12 bits
			case 0: // imm12 = 000000000000 ECALL
				sysCall()
				setPC(add64(pc, toU64(4)))
			default: // imm12 = 000000000001 EBREAK
				setPC(add64(pc, toU64(4))) // ignore breakpoint
			}
		default: // CSR instructions
			imm := parseCSSR(instr)
			value := rs1
			// rs1Value := getRegister(rs1)
			if iszero64(and64(funct3, toU64(4))) {
				value = getRegister(rs1)
			}
			mode := and64(funct3, toU64(3))
			rdValue := updateCSR(imm, value, mode)
			setRegister(rd, rdValue)
			setPC(add64(pc, toU64(4)))
		}
	case 0x2F: // 010_1111: RV32A and RV32A atomic operations extension
		// acquire and release bits:
		//   aq := and64(shr64(toU64(1), funct7), toU64(1))
		//   rl := and64(funct7, toU64(1))
		// if none set: unordered
		// if aq is set: no following mem ops observed before acquire mem op
		// if rl is set: release mem op not observed before earlier mem ops
		// if both set: sequentially consistent
		// These are no-op here because there is no pipeline of mem ops to acquire/release.
		s.OpCount.Arith++
		// 0b010 == RV32A W variants
		// 0b011 == RV64A D variants
		size := shl64(funct3, toU64(1))
		if lt64(size, toU64(4)) != 0 {
			revertWithCode(0xbada70, fmt.Errorf("bad AMO size: %d", size))
		}
		addr := getRegister(rs1)
		// TODO check if addr is aligned

		op := shr64(toU64(2), funct7)
		switch op {
		case 0x2: // 00010 = LR = Load Reserved
			v := loadMem(addr, size, true, 1, 2)
			setRegister(rd, v)
			setLoadReservation(addr)
		case 0x3: // 00011 = SC = Store Conditional
			rdValue := toU64(1)
			if eq64(addr, getLoadReservation()) != 0 {
				rs2Value := getRegister(rs2)
				storeMem(addr, size, rs2Value, 1, 2, true, true)
				rdValue = toU64(0)
			}
			setRegister(rd, rdValue)
			setLoadReservation(toU64(0))
		default: // AMO: Atomic Memory Operation
			rs2Value := getRegister(rs2)
			if eq64(size, toU64(4)) != 0 {
				rs2Value = mask32Signed64(rs2Value)
			}
			value := rs2Value
			v := loadMem(addr, size, true, 1, 2)
			rdValue := v
			switch op {
			case 0x0: // 00000 = AMOADD = add
				v = add64(v, value)
			case 0x1: // 00001 = AMOSWAP
				v = value
			case 0x4: // 00100 = AMOXOR = xor
				v = xor64(v, value)
			case 0x8: // 01000 = AMOOR = or
				v = or64(v, value)
			case 0xc: // 01100 = AMOAND = and
				v = and64(v, value)
			case 0x10: // 10000 = AMOMIN = min signed
				if slt64(value, v) != 0 {
					v = value
				}
			case 0x14: // 10100 = AMOMAX = max signed
				if sgt64(value, v) != 0 {
					v = value
				}
			case 0x18: // 11000 = AMOMINU = min unsigned
				if lt64(value, v) != 0 {
					v = value
				}
			case 0x1c: // 11100 = AMOMAXU = max unsigned
				if gt64(value, v) != 0 {
					v = value
				}
			default:
				revertWithCode(0xf001a70, fmt.Errorf("unknown atomic operation %d", op))
			}
			storeMem(addr, size, v, 1, 3, false, true) // after overwriting 1, proof 2 is no longer valid
			setRegister(rd, rdValue)
		}
		setPC(add64(pc, toU64(4)))
	case 0x0F: // 000_1111: fence
		// Used to impose additional ordering constraints; flushing the mem operation pipeline.
		// This VM doesn't have a pipeline, nor additional harts, so this is a no-op.
		// FENCE / FENCE.TSO / FENCE.I all no-op: there's nothing to synchronize.
		setPC(add64(pc, toU64(4)))
	case 0x07: // FLW/FLD: floating point load word/double
		width := parseWidth(instr)
		imm := parseImmTypeI(instr)
		s.OpCount.FloatLS++
		rs1Value := getRegister(rs1)
		memIndex := add64(rs1Value, signExtend64(imm, toU64(11)))
		rdValue := U64(0)
		switch width {
		case 2: // 010 = FLW 32 bit = 4 bytes
			rdValue = loadMem(memIndex, toU64(4), false, 1, 2) // signed
			rdValue |= 0xFFFF_FFFF_0000_0000                   // sign extend to 64 bits
			// check32NaN(uint32(rdValue & 0xFFFFFFFF))
		case 3: // 011 = FLD 64 bit = 8 bytes
			rdValue = loadMem(memIndex, toU64(8), false, 1, 2)
			// check64NaN(rdValue)
		}
		setFRegister(rd, rdValue)
		setPC(add64(pc, toU64(4)))
	case 0x27: // FSW/FSD: floating point store word/double
		width := parseWidth(instr)
		s.OpCount.FloatLS++
		imm := parseImmTypeS(instr)
		value := getFRegister(rs2)
		rs1Value := getRegister(rs1)
		memIndex := add64(rs1Value, signExtend64(imm, toU64(11)))
		switch width {
		case 2: // 010 = FSW 32 bit = 4 bytes
			// check32NaN(uint32(value & 0xFFFFFFFF))
			storeMem(memIndex, toU64(4), value, 1, 2, true, true)
		case 3: // 011 = FSD 64 bit = 8 bytes
			// check64NaN(value)
			storeMem(memIndex, toU64(8), value, 1, 2, true, true)
		}
		setPC(add64(pc, toU64(4)))
	case 0x53: // 1010011 FADD etc.
		s.OpCount.Float++
		beginFloatOp(opFPRoundingMode(funct5, funct3))
		format := parsefmt(instr)
		width := parseWidth(instr)
		rs1ValueD := getFRegister(rs1)
		rs2ValueD := getFRegister(rs2)
		// fmt.Println(rs1ValueD, rs2ValueD)
		rdValue := uint64(0)
		if format == 0 {
			// single precision, unboxed per the RISC-V NaN-boxing rule
			// rs1ValueS / rs2ValueS are the operands as single-precision *values*, so they
			// are unboxed. rs1ValueD / rs2ValueD keep the raw 64-bit register contents, for
			// the instructions that move bits rather than interpret them - FMV.X.W is
			// specified as a transfer in which "the bits are not modified".
			rs1ValueS := unboxF32(rs1ValueD)
			rs2ValueS := unboxF32(rs2ValueD)
			switch funct5 {
			case 0x00: // 00000 = FADD.S
				rdValue = uint64(uint32(F32_add(Float32_t(rs1ValueS), Float32_t(rs2ValueS))))
				// check32NaN(rs1ValueS)
				// check32NaN(rs2ValueS)
				// check32NaN(uint32(rdValue))
				rdValue |= 0xFFFF_FFFF_0000_0000 // extend to 64 bits with 1
				setFRegister(rd, rdValue)
			case 0x01: // 00001 = FSUB.S
				rdValue = uint64(uint32(F32_sub(Float32_t(rs1ValueS), Float32_t(rs2ValueS))))
				// check32NaN(rs1ValueS)
				// check32NaN(rs2ValueS)
				// check32NaN(uint32(rdValue))
				rdValue |= 0xFFFF_FFFF_0000_0000 // extend to 64 bits with 1
				setFRegister(rd, rdValue)
			case 0x02: // 00010 = FMUL.S
				rdValue = uint64(uint32(F32_mul(Float32_t(rs1ValueS), Float32_t(rs2ValueS))))
				// check32NaN(rs1ValueS)
				// check32NaN(rs2ValueS)
				// check32NaN(uint32(rdValue))
				rdValue |= 0xFFFF_FFFF_0000_0000 // extend to 64 bits with 1
				setFRegister(rd, rdValue)
			case 0x03: // 00011 = FDIV.S
				rdValue = uint64(uint32(F32_div(Float32_t(rs1ValueS), Float32_t(rs2ValueS))))
				// check32NaN(rs1ValueS)
				// check32NaN(rs2ValueS)
				// check32NaN(uint32(rdValue))
				rdValue |= 0xFFFF_FFFF_0000_0000 // extend to 64 bits with 1
				setFRegister(rd, rdValue)
			case 0x0B: // 01011 = FSQRT.S
				rdValue = uint64(uint32(F32_sqrt(Float32_t(rs1ValueS))))
				// check32NaN(rs1ValueS)
				// check32NaN(uint32(rdValue))
				rdValue |= 0xFFFF_FFFF_0000_0000 // extend to 64 bits with 1
				setFRegister(rd, rdValue)
			case 0x04: // 00100 = FSGNJ.S or FSGNJN.S or FSGNJX.S
				if width == 0x0 { // 0 = FSGNJ.S
					FirstBit := rs2ValueS & 0x80000000
					RemainingBits := rs1ValueS & 0x7FFFFFFF
					rdValue = uint64(uint32(FirstBit | RemainingBits))
				} else if width == 0x1 { // 1 = FSGNJN.S
					FirstBit := (rs2ValueS & 0x80000000) >> 31
					if FirstBit == 0 {
						FirstBit = 1
					} else {
						FirstBit = 0
					}
					FirstBit <<= 31
					RemainingBits := rs1ValueS & 0x7FFFFFFF
					rdValue = uint64(uint32(FirstBit | RemainingBits))
				} else if width == 0x2 { // 10 = FSGNJX.S
					FirstBitrs1 := rs1ValueS & 0x80000000
					FirstBitrs2 := rs2ValueS & 0x80000000
					FirstBit := FirstBitrs1 ^ FirstBitrs2
					RemainingBits := rs1ValueS & 0x7FFFFFFF
					rdValue = uint64(uint32(FirstBit | RemainingBits))
				}
				// check32NaN(uint32(rdValue & 0xFFFF_FFFF))
				rdValue |= 0xFFFF_FFFF_0000_0000 // extend to 64 bits with 1
				setFRegister(rd, rdValue)
			case 0x05: // 00101 = FMIN.S or FMAX.S
				if width == 0x0 { // 0 = FMIN.S
					rdValue = uint64(uint32(F32_min(Float32_t(rs1ValueS), Float32_t(rs2ValueS))))
				} else if width == 0x1 { // 1 = FMAX.S
					rdValue = uint64(uint32(F32_max(Float32_t(rs1ValueS), Float32_t(rs2ValueS))))
				}
				rdValue |= 0xFFFF_FFFF_0000_0000 // NaN-box the single-precision result
				setFRegister(rd, rdValue)
			case 0x08: // 01000 FCVT.S.D
				rs1ValueD = getFRegister(rs1)
				rdValue = uint64(F64_to_f32(Float64_t(rs1ValueD))) & 0xFFFF_FFFF
				rdValue |= 0xFFFF_FFFF_0000_0000 // NaN-box the single-precision result
				// rdValue |= 0xFFFF_FFFF_0000_0000 // extend to 64 bits with 1
				setFRegister(rd, rdValue)
			case 0x18: // 11000 = FCVT.W.S or FCVT.WU.S, FCVT.L.S, FCVT.LU.S
				newrdValue := uint64(0)
				switch rs2 {
				case 0x0: // 0 = FCVT.W.S
					// convert to I32
					rdValue = uint64(signExtend64(uint64(F32_to_i32(Float32_t(rs1ValueS), true)), toU64(31)))
					// check32NaN(rs1ValueS)
					newrdValue = rdValue
				case 0x1: // 1 = FCVT.WU.S
					// convert to U32
					rdValue = signExtend64(uint64(F32_to_ui32(Float32_t(rs1ValueS), true)), toU64(31))
					// check32NaN(rs1ValueS)
					newrdValue = rdValue
				case 0x2: // 10 = FCVT.L.S
					rdValue = uint64(F32_to_i64(Float32_t(rs1ValueS), true))
					// check32NaN(rs1ValueS)
					newrdValue = rdValue
				case 0x3: // 11 = FCVT.LU.S
					rdValue = uint64(F32_to_ui64(Float32_t(rs1ValueS), true))
					// check32NaN(rs1ValueS)
					newrdValue = rdValue
				}
				setRegister(rd, newrdValue)
			case 0x1C: // 11100 = FMV.X.W or FCLASS.S
				if width == 0x0 { // 0 = FMV.X.W - a raw transfer, so no NaN-boxing check
					rdValue |= (signExtend64(uint64(uint32(rs1ValueD)), toU64(31)))
					// check32NaN(rs1ValueS)
					setRegister(rd, rdValue)
				} else if width == 0x1 { // 1 = FCLASS.S
					rdValue |= uint64(F32_classS(Float32_t(rs1ValueS))) & 0x3FF
					// check32NaN(rs1ValueS)
					setRegister(rd, rdValue)
				}
			case 0x14: // 10100 = FEQ.S or FLT.S or FLE.S
				// check32NaN(rs1ValueS)
				// check32NaN(rs2ValueS)
				switch width {
				case 0x02: // 10 = FEQ.S
					if F32_eq(Float32_t(rs1ValueS), Float32_t(rs2ValueS)) {
						rdValue = 1
					} else {
						rdValue = 0
					}
				case 0x01: // 1 = FLT.S
					if F32_lt(Float32_t(rs1ValueS), Float32_t(rs2ValueS)) {
						rdValue = 1
					} else {
						rdValue = 0
					}
				case 0x00: // 0 = FLE.S
					if F32_le(Float32_t(rs1ValueS), Float32_t(rs2ValueS)) {
						rdValue = 1
					} else {
						rdValue = 0
					}
				}
				setRegister(rd, rdValue)
			case 0x1A: // 11010 = FCVT.S.W or FCVT.S.WU or FCVT.S.L or FCVT.S.LU
				switch rs2 {
				case 0x0: // 0 = FCVT.S.W
					iValue := int32(getRegister(rs1) & 0xFFFF_FFFF) // clear top 32 bits
					rdValue = uint64(I32_to_f32(iValue))
					rdValue |= 0xFFFF_FFFF_0000_0000 // extend to 64 bits with 1
				case 0x1: // 1 = FCVT.S.WU
					uiValue := uint32(getRegister(rs1) & 0xFFFF_FFFF) // clear top 32 bits
					rdValue = uint64(UI32ToF32(uiValue))
					rdValue |= 0xFFFF_FFFF_0000_0000 // extend to 64 bits with 1
				case 0x2: // 10 = FCVT.S.L
					iValue := int64(getRegister(rs1))
					rdValue = uint64(I64_to_f32(iValue)) & 0xFFFF_FFFF
					rdValue |= 0xFFFF_FFFF_0000_0000 // extend to 64 bits with 1
				case 0x3: // 11 = FCVT.S.LU
					uiValue := uint64(getRegister(rs1))
					rdValue = uint64(UI64ToF32(uiValue)) & 0xFFFF_FFFF
					rdValue |= 0xFFFF_FFFF_0000_0000 // extend to 64 bits with 1
				}
				// check32NaN(uint32(rdValue & 0xFFFF_FFFF))
				setFRegister(rd, rdValue)
			case 0x1E: // 11110 = FMV.W.X
				rdValue = getRegister(rs1) & 0x0000_0000_FFFF_FFFF // the low 32 bits, raw
				rdValue |= 0xFFFF_FFFF_0000_0000                   // NaN-box them
				setFRegister(rd, rdValue)
			}
		} else if format == 1 {
			// double precision
			switch funct5 {
			case 0x00: // 00000 = FADD.D
				rdValue = uint64(F64_add(Float64_t(rs1ValueD), Float64_t(rs2ValueD)))
				// check64NaN(rs1ValueD)
				// check64NaN(rs2ValueD)
				// check64NaN(rdValue)
				setFRegister(rd, rdValue)
			case 0x01: // 00001 = FSUB.D
				rdValue = uint64(F64_sub(Float64_t(rs1ValueD), Float64_t(rs2ValueD)))
				// check64NaN(rs1ValueD)
				// check64NaN(rs2ValueD)
				// check64NaN(rdValue)
				setFRegister(rd, rdValue)
			case 0x02: // 00010 = FMUL.D
				rdValue = uint64(F64_mul(Float64_t(rs1ValueD), Float64_t(rs2ValueD)))
				// check64NaN(rs1ValueD)
				// check64NaN(rs2ValueD)
				// check64NaN(rdValue)
				setFRegister(rd, rdValue)
			case 0x03: // 00011 = FDIV.D
				rdValue = uint64(F64_div(Float64_t(rs1ValueD), Float64_t(rs2ValueD)))
				// check64NaN(rs1ValueD)
				// check64NaN(rs2ValueD)
				// check64NaN(rdValue)
				setFRegister(rd, rdValue)
			case 0x0B: // 01011 = FSQRT.D
				rdValue = uint64(F64_sqrt(Float64_t(rs1ValueD)))
				// check64NaN(rs1ValueD)
				// check64NaN(rdValue)
				setFRegister(rd, rdValue)
			case 0x04: // 00100 = FSGNJ.D or FSGNJN.D or FSGNJX.D
				if width == 0x0 { // 0 = FSGNJ.D
					FirstBit := rs2ValueD & 0x80000000_00000000
					RemainingBits := rs1ValueD & 0x7FFF_FFFF_FFFF_FFFF
					rdValue = uint64(FirstBit | RemainingBits)
				} else if width == 0x1 { // 1 = FSGNJN.D
					FirstBit := (rs2ValueD & 0x80000000_00000000) >> 63
					if FirstBit == 0 {
						FirstBit = 1
					} else {
						FirstBit = 0
					}
					FirstBit = FirstBit << 63
					RemainingBits := rs1ValueD & 0x7FFF_FFFF_FFFF_FFFF
					rdValue = uint64(FirstBit | RemainingBits)
				} else if width == 0x2 { // 10 = FSGNJX.D
					FirstBitrs1 := rs1ValueD & 0x80000000_00000000
					FirstBitrs2 := rs2ValueD & 0x80000000_00000000
					FirstBit := FirstBitrs1 ^ FirstBitrs2
					RemainingBits := rs1ValueD & 0x7FFF_FFFF_FFFF_FFFF
					rdValue = uint64(FirstBit | RemainingBits)
				}
				// check64NaN(rdValue)
				setFRegister(rd, rdValue)
			case 0x05: // 00101 = FMIN.D or FMAX.D
				if width == 0x0 { // 0 = FMIN.D
					rdValue = uint64(F64_min(Float64_t(rs1ValueD), Float64_t(rs2ValueD)))
				} else if width == 0x1 { // 1 = FMAX.D
					rdValue = uint64(F64_max(Float64_t(rs1ValueD), Float64_t(rs2ValueD)))
				}
				setFRegister(rd, rdValue)
			case 0x08: // 01000 = FCVT.D.S
				rs1ValueS := unboxF32(rs1ValueD)
				rdValue = uint64(F32_to_f64(Float32_t(rs1ValueS)))
				// check32NaN(rs1ValueS)
				setFRegister(rd, rdValue)
			case 0x14: // 10100 = FEQ.D or FLT.D or FLE.D
				// check64NaN(rs1ValueD)
				// check64NaN(rs2ValueD)
				switch width {
				case 0x02: // 10 = FEQ.D
					if F64_eq(Float64_t(rs1ValueD), Float64_t(rs2ValueD)) {
						rdValue = 1
					} else {
						rdValue = 0
					}
				case 0x01: // 1 = FLT.D
					if F64_lt(Float64_t(rs1ValueD), Float64_t(rs2ValueD)) {
						rdValue = 1
					} else {
						rdValue = 0
					}
				case 0x00: // 0 = FLE.D
					if F64_le(Float64_t(rs1ValueD), Float64_t(rs2ValueD)) {
						rdValue = 1
					} else {
						rdValue = 0
					}
				}
				setRegister(rd, rdValue)
			case 0x1C: // 11100 = FMV.X.D or FCLASS.D
				// check64NaN(rs1ValueD)
				if width == 0x0 { // 0 = FMV.X.D
					rdValue = rs1ValueD
					setRegister(rd, rdValue)
				} else if width == 0x1 { // 1 = FCLASS.D
					rdValue = uint64(F64_classS(Float64_t(rs1ValueD))) & 0x3FF
					setRegister(rd, rdValue)
				}
			case 0x18: // 11000 = FCVT.W.D or FCVT.WU.D, FCVT.L.D, FCVT.LU.D
				switch rs2 {
				case 0x0: // 0 = FCVT.W.D
					// oldrdValue := getRegister(rd) & 0xFFFF_FFFF_0000_0000 // clear bottom 32 bits
					// fmt.Println("oldrdValue", oldrdValue)
					// check64NaN(rs1ValueD)
					rdValue = signExtend64(uint64(F64_to_i32(Float64_t(rs1ValueD), true)), toU64(31))
					// rdValue |= oldrdValue
					setRegister(rd, rdValue)
				case 0x1: // 1 = FCVT.WU.D
					// oldrdValue := getRegister(rd) & 0xFFFF_FFFF_0000_0000 // clear bottom 32 bits
					rdValue = signExtend64(uint64(F64_to_ui32(Float64_t(rs1ValueD), true)), toU64(31)) // clear top 32 bits
					// check64NaN(rs1ValueD)
					// rdValue |= oldrdValue
					setRegister(rd, rdValue)
				case 0x2: // 10 = FCVT.L.D
					rdValue = uint64(F64_to_i64(Float64_t(rs1ValueD), true))
					// check64NaN(rs1ValueD)
					setRegister(rd, rdValue)
				case 0x3: // 11 = FCVT.LU.D
					rdValue = uint64(F64_to_ui64(Float64_t(rs1ValueD), true))
					// check64NaN(rs1ValueD)
					setRegister(rd, rdValue)
				}
			case 0x1A: // 11010 = FCVT.D.W or FCVT.D.WU or FCVT.D.L or FCVT.D.LU
				switch rs2 {
				case 0x0: // 0 = FCVT.D.W
					iValue := int32(getRegister(rs1) & 0xFFFF_FFFF) // clear top 32 bits
					// check32NaN(uint32(iValue))
					rdValue = uint64(I32_to_f64(iValue))
					// check64NaN(rdValue)
					setFRegister(rd, rdValue)
				case 0x1: // 1 = FCVT.D.WU
					uiValue := uint32(getRegister(rs1) & 0xFFFF_FFFF) // clear top 32 bits
					// check32NaN(uiValue)
					rdValue = uint64(UI32ToF64(uiValue))
					// check64NaN(rdValue)
					setFRegister(rd, rdValue)
				case 0x2: // 10 = FCVT.D.L
					rdValue = uint64(I64_to_f64(int64(getRegister(rs1))))
					// check64NaN(rdValue)
					setFRegister(rd, rdValue)
				case 0x3: // 11 = FCVT.D.LU
					rdValue = uint64(UI64ToF64(uint64(getRegister(rs1))))
					// check64NaN(rdValue)
					setFRegister(rd, rdValue)
				}
			case 0x1E: // 11110 = FMV.D.X
				rdValue = getRegister(rs1)
				// check64NaN(rdValue)
				setFRegister(rd, rdValue)
			}
		}
		endFloatOp()
		setPC(add64(pc, toU64(4)))
	case 0x43: // 1000011 FMADD.S FMADD.D
		format := parsefmt(instr)
		s.OpCount.Float++
		beginFloatOp(funct3)
		rs1ValueD := getFRegister(rs1)
		rs2ValueD := getFRegister(rs2)
		rs3ValueD := getFRegister(rs3)
		rdValue := uint64(0)
		if format == 0 {
			// single precision
			// single precision, unboxed per the RISC-V NaN-boxing rule
			rs1ValueS := unboxF32(rs1ValueD)
			rs2ValueS := unboxF32(rs2ValueD)
			rs3ValueS := unboxF32(rs3ValueD)
			rdValue = uint64(uint32(Softfloat_mulAddF32(rs1ValueS, rs2ValueS, rs3ValueS, 0)))
			rdValue |= 0xFFFF_FFFF_0000_0000 // extend to 64 bits with 1
		} else if format == 1 {
			// double precision
			rdValue = uint64(Softfloat_mulAddF64(rs1ValueD, rs2ValueD, rs3ValueD, 0))
		}
		setFRegister(rd, rdValue)
		endFloatOp()
		setPC(add64(pc, toU64(4)))
	case 0x47: // 1000111 FMSUB.S FMSUB.D
		format := parsefmt(instr)
		s.OpCount.Float++
		beginFloatOp(funct3)
		rs1ValueD := getFRegister(rs1)
		rs2ValueD := getFRegister(rs2)
		rs3ValueD := getFRegister(rs3)
		rdValue := uint64(0)
		if format == 0 {
			// single precision
			// single precision, unboxed per the RISC-V NaN-boxing rule
			rs1ValueS := unboxF32(rs1ValueD)
			rs2ValueS := unboxF32(rs2ValueD)
			rs3ValueS := unboxF32(rs3ValueD)
			rdValue = uint64(uint32(Softfloat_mulAddF32(rs1ValueS, rs2ValueS, rs3ValueS^0x8000_0000, 0)))
			rdValue |= 0xFFFF_FFFF_0000_0000 // extend to 64 bits with 1
		} else if format == 1 {
			// double precision
			rdValue = uint64(Softfloat_mulAddF64(rs1ValueD, rs2ValueD, rs3ValueD^0x8000_0000_0000_0000, 0))
		}
		setFRegister(rd, rdValue)
		endFloatOp()
		setPC(add64(pc, toU64(4))) // no-op this.
	case 0x4B: // 1001011 FNMSUB.S FNMSUB.D
		format := parsefmt(instr)
		s.OpCount.Float++
		beginFloatOp(funct3)
		rs1ValueD := getFRegister(rs1)
		rs2ValueD := getFRegister(rs2)
		rs3ValueD := getFRegister(rs3)
		rdValue := uint64(0)
		if format == 0 {
			// single precision
			// single precision, unboxed per the RISC-V NaN-boxing rule
			rs1ValueS := unboxF32(rs1ValueD)
			rs2ValueS := unboxF32(rs2ValueD)
			rs3ValueS := unboxF32(rs3ValueD)
			rdValue = uint64(uint32(Softfloat_mulAddF32(rs1ValueS^0x8000_0000, rs2ValueS, rs3ValueS, 0)))
			rdValue |= 0xFFFF_FFFF_0000_0000 // extend to 64 bits with 1
		} else if format == 1 {
			// double precision
			rdValue = uint64(Softfloat_mulAddF64(rs1ValueD^0x8000_0000_0000_0000, rs2ValueD, rs3ValueD, 0))
		}
		setFRegister(rd, rdValue)
		endFloatOp()
		setPC(add64(pc, toU64(4)))
	case 0x4F: // 1001111 FNMADD.S FNMADD.D
		format := parsefmt(instr)
		rs1ValueD := getFRegister(rs1)
		rs2ValueD := getFRegister(rs2)
		s.OpCount.Float++
		beginFloatOp(funct3)
		rs3ValueD := getFRegister(rs3)
		rdValue := uint64(0)
		if format == 0 {
			// single precision
			// single precision, unboxed per the RISC-V NaN-boxing rule
			rs1ValueS := unboxF32(rs1ValueD)
			rs2ValueS := unboxF32(rs2ValueD)
			rs3ValueS := unboxF32(rs3ValueD)
			rdValue = uint64(uint32(Softfloat_mulAddF32(rs1ValueS^0x8000_0000, rs2ValueS, rs3ValueS^0x8000_0000, 0)))
			rdValue |= 0xFFFF_FFFF_0000_0000 // extend to 64 bits with 1
		} else if format == 1 {
			// double precision
			rdValue = uint64(Softfloat_mulAddF64(rs1ValueD^0x8000_0000_0000_0000, rs2ValueD, rs3ValueD^0x8000_0000_0000_0000, 0))
		}
		setFRegister(rd, rdValue)
		endFloatOp()
		setPC(add64(pc, toU64(4)))
	default:
		revertWithCode(0xf001c0de, fmt.Errorf("unknown instruction opcode: %d", opcode))
	}
	return nil
}
