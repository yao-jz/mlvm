// Package xray profiles a guest at instruction granularity: which functions the
// instructions belong to, which call stacks they ran under, and which classes and
// mnemonics they were. Because MLVM is deterministic, a profile is exact and repeatable:
// the same ELF and inputs give the same counts on every machine, with no sampling noise.
//
// Stacks come from a shadow call stack: a jal/jalr that links through ra or t0 pushes a
// frame, `jalr zero,0(ra)` pops one, and any other move into a different function
// replaces the top frame (a tail call). The guest runs exactly as it would unprofiled;
// the profiler only reads the program counter and the instruction word.
package xray

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"

	"github.com/yao-jz/mlvm/rvgo/fast"
	"github.com/yao-jz/mlvm/rvgo/internal/rvdis"
)

const unknownSymbol = "[unknown]"

// maxDepth bounds the shadow stack so deep recursion cannot grow the frame tree without
// limit; frames deeper than this are folded into their parent.
const maxDepth = 128

type symbol struct {
	name  string
	start uint64
	end   uint64
}

// Symbolizer maps program counters to function names using the ELF symbol table.
type Symbolizer struct {
	syms  []symbol
	names []string
}

// NewSymbolizer reads FUNC symbols from an ELF image.
func NewSymbolizer(elfBytes []byte) (*Symbolizer, error) {
	f, err := elf.NewFile(bytes.NewReader(elfBytes))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := f.Symbols()
	if err != nil {
		return nil, fmt.Errorf("read ELF symbols: %w", err)
	}
	var syms []symbol
	for _, s := range raw {
		if elf.ST_TYPE(s.Info) != elf.STT_FUNC || s.Value == 0 {
			continue
		}
		syms = append(syms, symbol{name: cleanName(s.Name), start: s.Value, end: s.Value + s.Size})
	}
	sort.Slice(syms, func(i, j int) bool {
		if syms[i].start != syms[j].start {
			return syms[i].start < syms[j].start
		}
		return syms[i].end > syms[j].end // prefer the widest alias
	})
	// Drop aliases that share a start address; keep the first (widest) name.
	dedup := syms[:0]
	for _, s := range syms {
		if len(dedup) > 0 && dedup[len(dedup)-1].start == s.start {
			continue
		}
		dedup = append(dedup, s)
	}
	out := &Symbolizer{syms: dedup, names: make([]string, 0, len(dedup)+1)}
	for _, s := range dedup {
		out.names = append(out.names, s.name)
	}
	out.names = append(out.names, unknownSymbol)
	return out, nil
}

// cleanName demangles Rust symbols and drops the clone suffixes GCC adds to specialized
// copies of a function (encode.constprop.0, msort_with_tmp.part.0), so they report under
// the source name.
func cleanName(name string) string {
	if rust, ok := demangleRust(name); ok {
		return rust
	}
	for _, marker := range []string{".constprop.", ".part.", ".isra.", ".cold", ".lto_priv."} {
		if i := strings.Index(name, marker); i > 0 {
			name = name[:i]
		}
	}
	return name
}

// Lookup returns the index of the function containing pc, or the unknown index.
func (s *Symbolizer) Lookup(pc uint64) int {
	i := sort.Search(len(s.syms), func(i int) bool { return s.syms[i].start > pc }) - 1
	if i >= 0 {
		sym := s.syms[i]
		if pc < sym.end || sym.end == sym.start {
			return i
		}
	}
	return len(s.syms)
}

// Name returns a function name by index.
func (s *Symbolizer) Name(i int) string { return s.names[i] }

// Len is the number of functions; indexes 0..Len()-1 name them, and Len() is the unknown
// function Lookup returns for a pc outside all of them.
func (s *Symbolizer) Len() int { return len(s.syms) }

type decoded struct {
	inst  rvdis.Inst
	class int
	mnem  int
	sym   int
	float bool // an F/D instruction
	prec  int  // 1 single, 2 double, 0 not float arithmetic
	call  bool // links through ra/t0: push a frame
	ret   bool // jalr zero,0(ra): pop a frame
}

type frame struct {
	parent   int
	sym      int
	self     uint64
	float    uint64      // F/D instructions among self
	children map[int]int // sym -> frame index
}

// Profiler accumulates an instruction-level profile one step at a time.
type Profiler struct {
	sym       *Symbolizer
	cache     map[uint64]*decoded
	fastTag   []uint64 // direct-mapped front for cache, indexed by pc>>2
	fastEnt   []*decoded
	mnemonics []string
	mnemIndex map[string]int
	mnemCount []uint64
	classIdx  map[rvdis.Class]int
	classes   []uint64
	precision [3]uint64
	frames    []frame
	current   int
	depth     int
	overflow  int // calls deeper than maxDepth, folded into the deepest frame
	steps     uint64
	marks     []Mark
}

// Mark labels a step, for example the step at which a token was printed.
type Mark struct {
	Step  uint64 `json:"step"`
	Label string `json:"label"`
}

// NewProfiler prepares a profiler for a guest built from elfBytes.
func NewProfiler(elfBytes []byte) (*Profiler, error) {
	sym, err := NewSymbolizer(elfBytes)
	if err != nil {
		return nil, err
	}
	p := &Profiler{
		sym:       sym,
		cache:     make(map[uint64]*decoded, 1<<14),
		fastTag:   make([]uint64, fastCacheSize),
		fastEnt:   make([]*decoded, fastCacheSize),
		mnemIndex: map[string]int{},
		classIdx:  map[rvdis.Class]int{},
		classes:   make([]uint64, len(rvdis.Classes)),
		frames:    []frame{{parent: -1, sym: -1, children: map[int]int{}}},
	}
	for i, c := range rvdis.Classes {
		p.classIdx[c] = i
	}
	return p, nil
}

const fastCacheSize = 1 << 16

func (p *Profiler) decode(pc uint64, state *fast.VMState) *decoded {
	slot := (pc >> 2) & (fastCacheSize - 1)
	if e := p.fastEnt[slot]; e != nil && p.fastTag[slot] == pc {
		return e
	}
	d := p.decodeSlow(pc, state)
	p.fastTag[slot], p.fastEnt[slot] = pc, d
	return d
}

func (p *Profiler) decodeSlow(pc uint64, state *fast.VMState) *decoded {
	if d, ok := p.cache[pc]; ok {
		return d
	}
	var word [4]byte
	state.Memory.GetUnaligned(pc, word[:])
	inst := rvdis.Decode(binary.LittleEndian.Uint32(word[:]), pc)
	mnem, ok := p.mnemIndex[inst.Mnemonic]
	if !ok {
		mnem = len(p.mnemonics)
		p.mnemIndex[inst.Mnemonic] = mnem
		p.mnemonics = append(p.mnemonics, inst.Mnemonic)
		p.mnemCount = append(p.mnemCount, 0)
	}
	w := inst.Word
	op, rd, rs1 := w&0x7f, (w>>7)&31, (w>>15)&31
	d := &decoded{
		inst:  inst,
		class: p.classIdx[inst.Class],
		mnem:  mnem,
		sym:   p.sym.Lookup(pc),
		float: inst.Class.IsFloat(),
		prec:  map[string]int{"s": 1, "d": 2}[inst.Precision],
		call:  (op == 0x6f || op == 0x67) && (rd == 1 || rd == 5),
		ret:   op == 0x67 && rd == 0 && (rs1 == 1 || rs1 == 5) && w>>20 == 0,
	}
	p.cache[pc] = d
	return d
}

func (p *Profiler) child(parent, sym int) int {
	if idx, ok := p.frames[parent].children[sym]; ok {
		return idx
	}
	p.frames = append(p.frames, frame{parent: parent, sym: sym, children: map[int]int{}})
	idx := len(p.frames) - 1
	p.frames[parent].children[sym] = idx
	return idx
}

// Step executes one instruction of inst's state and accounts for it.
func (p *Profiler) Step(inst *fast.InstrumentedState, state *fast.VMState) error {
	d := p.decode(state.PC, state)
	if p.current == 0 || p.frames[p.current].sym != d.sym {
		// First instruction, a tail call or a return into a different function than the
		// frame we popped to: the running function replaces the top frame.
		parent := 0
		if p.current != 0 {
			parent = p.frames[p.current].parent
		}
		p.current = p.child(parent, d.sym)
	}
	p.frames[p.current].self++
	if d.float {
		p.frames[p.current].float++
	}
	p.classes[d.class]++
	p.mnemCount[d.mnem]++
	p.precision[d.prec]++
	p.steps++

	if _, err := inst.Step(false); err != nil {
		return err
	}

	switch {
	case d.call && !state.Exited:
		if p.depth < maxDepth {
			p.depth++
			p.current = p.child(p.current, p.sym.Lookup(state.PC))
		} else {
			p.overflow++ // too deep: count it so its return does not pop a real frame
		}
	case d.ret && p.overflow > 0:
		p.overflow--
	case d.ret && p.depth > 0:
		p.depth--
		p.current = p.frames[p.current].parent
	}
	return nil
}

// Mark records a labelled step, such as a token boundary.
func (p *Profiler) Mark(step uint64, label string) {
	p.marks = append(p.marks, Mark{Step: step, Label: label})
}

// Steps returns the number of instructions profiled so far.
func (p *Profiler) Steps() uint64 { return p.steps }
