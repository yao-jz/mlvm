package xray

import (
	"fmt"
	"sort"
	"strings"

	"github.com/yao-jz/mlvm/rvgo/internal/rvdis"
)

// Count is one row of a breakdown.
type Count struct {
	Name  string  `json:"name"`
	Count uint64  `json:"count"`
	Share float64 `json:"share"`
}

// Function is a per-function row: self counts instructions inside the function, total
// also counts the functions it called.
type Function struct {
	Name  string  `json:"name"`
	Self  uint64  `json:"self"`
	Total uint64  `json:"total"`
	Share float64 `json:"share"` // self / all steps
}

// Node is one frame of the call tree; Value includes children.
type Node struct {
	Name     string  `json:"name"`
	Value    uint64  `json:"value"`
	Self     uint64  `json:"self"`
	Float    uint64  `json:"float"` // F/D instructions among Self
	Children []*Node `json:"children,omitempty"`
}

// Report is a finished profile.
type Report struct {
	Steps       uint64            `json:"steps"`
	FloatShare  float64           `json:"floatShare"` // all F/D instructions
	FloatArith  uint64            `json:"floatArith"` // instructions that round: add, mul, div, sqrt, fma, conversions
	Precision   map[string]uint64 `json:"floatArithByPrecision"`
	Classes     []Count           `json:"classes"`
	Mnemonics   []Count           `json:"mnemonics"`
	Functions   []Function        `json:"functions"`
	Tree        *Node             `json:"tree"`
	Marks       []Mark            `json:"marks,omitempty"`
	UniquePCs   int               `json:"uniquePCs"`
	FrameCount  int               `json:"frames"`
	Description string            `json:"description,omitempty"`
}

// Report builds the profile accumulated so far.
func (p *Profiler) Report() *Report {
	total := p.steps
	share := func(n uint64) float64 {
		if total == 0 {
			return 0
		}
		return float64(n) / float64(total)
	}
	r := &Report{Steps: total, Precision: map[string]uint64{}, Marks: append([]Mark(nil), p.marks...), UniquePCs: len(p.cache), FrameCount: len(p.frames) - 1}
	var float uint64
	for i, c := range rvdis.Classes {
		n := p.classes[i]
		if n == 0 {
			continue
		}
		if c.IsFloat() {
			float += n
		}
		if c == rvdis.ClassFloatArith {
			r.FloatArith = n
		}
		r.Classes = append(r.Classes, Count{Name: string(c), Count: n, Share: share(n)})
	}
	sort.SliceStable(r.Classes, func(i, j int) bool { return r.Classes[i].Count > r.Classes[j].Count })
	r.FloatShare = share(float)
	if n := p.precision[1]; n > 0 {
		r.Precision["s"] = n
	}
	if n := p.precision[2]; n > 0 {
		r.Precision["d"] = n
	}
	for i, m := range p.mnemonics {
		r.Mnemonics = append(r.Mnemonics, Count{Name: m, Count: p.mnemCount[i], Share: share(p.mnemCount[i])})
	}
	sort.Slice(r.Mnemonics, func(i, j int) bool {
		if r.Mnemonics[i].Count != r.Mnemonics[j].Count {
			return r.Mnemonics[i].Count > r.Mnemonics[j].Count
		}
		return r.Mnemonics[i].Name < r.Mnemonics[j].Name
	})

	// Build the call tree bottom-up so every node knows its inclusive value.
	nodes := make([]*Node, len(p.frames))
	for i := range p.frames {
		name := "all"
		if i != 0 {
			name = p.sym.Name(p.frames[i].sym)
		}
		nodes[i] = &Node{Name: name, Self: p.frames[i].self, Float: p.frames[i].float, Value: p.frames[i].self}
	}
	for i := len(p.frames) - 1; i > 0; i-- {
		parent := p.frames[i].parent
		nodes[parent].Value += nodes[i].Value
		nodes[parent].Children = append(nodes[parent].Children, nodes[i])
	}
	for _, n := range nodes {
		sort.Slice(n.Children, func(i, j int) bool {
			if n.Children[i].Value != n.Children[j].Value {
				return n.Children[i].Value > n.Children[j].Value
			}
			return n.Children[i].Name < n.Children[j].Name
		})
	}
	r.Tree = nodes[0]

	// Per-function self and inclusive totals. A recursive function counts once per stack.
	self := map[string]uint64{}
	incl := map[string]uint64{}
	var walk func(n *Node, onStack map[string]bool)
	walk = func(n *Node, onStack map[string]bool) {
		self[n.Name] += n.Self
		if !onStack[n.Name] {
			incl[n.Name] += n.Value
			onStack[n.Name] = true
			defer delete(onStack, n.Name)
		}
		for _, c := range n.Children {
			walk(c, onStack)
		}
	}
	for _, c := range r.Tree.Children {
		walk(c, map[string]bool{})
	}
	for name, s := range self {
		r.Functions = append(r.Functions, Function{Name: name, Self: s, Total: incl[name], Share: share(s)})
	}
	sort.Slice(r.Functions, func(i, j int) bool {
		if r.Functions[i].Self != r.Functions[j].Self {
			return r.Functions[i].Self > r.Functions[j].Self
		}
		return r.Functions[i].Name < r.Functions[j].Name
	})
	return r
}

func (n *Node) subtreeFloat() uint64 {
	total := n.Float
	for _, c := range n.Children {
		total += c.subtreeFloat()
	}
	return total
}

// Folded renders the call tree in the folded-stack format read by flamegraph.pl,
// inferno and speedscope: one "a;b;c count" line per stack with self instructions.
func (r *Report) Folded() string {
	var b strings.Builder
	var walk func(n *Node, prefix string)
	walk = func(n *Node, prefix string) {
		path := n.Name
		if prefix != "" {
			path = prefix + ";" + n.Name
		}
		if n.Self > 0 {
			fmt.Fprintf(&b, "%s %d\n", path, n.Self)
		}
		for _, c := range n.Children {
			walk(c, path)
		}
	}
	for _, c := range r.Tree.Children {
		walk(c, "")
	}
	return b.String()
}

// Trim returns a copy of the tree without frames below minShare of all steps; their
// instructions are kept in the parent's self count so totals still add up.
func (n *Node) Trim(total uint64, minShare float64) *Node {
	out := &Node{Name: n.Name, Value: n.Value, Self: n.Self, Float: n.Float}
	for _, c := range n.Children {
		if total > 0 && float64(c.Value)/float64(total) < minShare {
			out.Self += c.Value
			out.Float += c.subtreeFloat()
			continue
		}
		out.Children = append(out.Children, c.Trim(total, minShare))
	}
	return out
}
