package xray

import (
	"fmt"
	"html"
	"math"
	"strings"
)

// FlameOptions controls SVG rendering.
type FlameOptions struct {
	Title    string
	Subtitle string
	Width    int // pixels; default 1200
}

const (
	flameRowHeight = 18
	flamePad       = 10
	flameHeader    = 58
	flameFooter    = 34
	flameCharWidth = 6.6
)

// floatRamp is a single-hue sequential ramp (light to dark blue) for the share of a
// frame's own instructions that are F/D. Frames that mostly call other functions are
// drawn in a neutral gray instead, so color always means "this code does float work".
var floatRamp = [][3]float64{
	{0xb7, 0xd3, 0xf6}, {0x86, 0xb6, 0xef}, {0x55, 0x98, 0xe7},
	{0x2a, 0x78, 0xd6}, {0x1c, 0x5c, 0xab}, {0x10, 0x42, 0x81},
}

const callerFill = "#e1e0d9"

func floatColor(share float64) string {
	share = math.Max(0, math.Min(1, share))
	x := share * float64(len(floatRamp)-1)
	i := int(x)
	if i >= len(floatRamp)-1 {
		i = len(floatRamp) - 2
	}
	t := x - float64(i)
	a, b := floatRamp[i], floatRamp[i+1]
	c := func(k int) int { return int(a[k] + (b[k]-a[k])*t + 0.5) }
	return fmt.Sprintf("rgb(%d,%d,%d)", c(0), c(1), c(2))
}

// floatInk picks label ink that stays readable on the ramp.
func floatInk(share float64) string {
	if share > 0.55 {
		return "#ffffff"
	}
	return "#0b0b0b"
}

func depth(n *Node) int {
	d := 0
	for _, c := range n.Children {
		if cd := depth(c); cd > d {
			d = cd
		}
	}
	return d + 1
}

// FlameSVG renders the call tree as a standalone flame graph: the root at the bottom,
// callees stacked above callers, frame width proportional to instructions executed.
func (r *Report) FlameSVG(opts FlameOptions) []byte {
	width := opts.Width
	if width == 0 {
		width = 1200
	}
	tree := r.Tree.Trim(r.Steps, 0.0005)
	rows := depth(tree)
	height := flameHeader + rows*flameRowHeight + flameFooter
	plot := float64(width - 2*flamePad)

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" font-family="ui-monospace, SFMono-Regular, Menlo, monospace">`+"\n", width, height, width, height)
	fmt.Fprintf(&b, `<rect width="100%%" height="100%%" fill="#fcfcfb"/>`+"\n")
	fmt.Fprintf(&b, `<text x="%d" y="24" font-size="16" font-weight="600" fill="#1d1d1f">%s</text>`+"\n", flamePad, html.EscapeString(opts.Title))
	fmt.Fprintf(&b, `<text x="%d" y="44" font-size="12" fill="#5c5c60">%s</text>`+"\n", flamePad, html.EscapeString(opts.Subtitle))

	var draw func(n *Node, x float64, level int)
	draw = func(n *Node, x float64, level int) {
		w := float64(n.Value) / float64(r.Steps) * plot
		if w < 0.4 {
			return
		}
		y := height - flameFooter - (level+1)*flameRowHeight
		floatShare := 0.0
		if n.Self > 0 {
			floatShare = float64(n.Float) / float64(n.Self)
		}
		fill, ink := floatColor(floatShare), floatInk(floatShare)
		if level == 0 || n.Self*50 < n.Value {
			// The root and frames that mostly just call other functions are containers;
			// coloring them by a handful of their own instructions would be noise.
			fill, ink = callerFill, "#0b0b0b"
		}
		pct := 100 * float64(n.Value) / float64(r.Steps)
		tip := fmt.Sprintf("%s — %s instructions (%.2f%%), %.0f%% of its own instructions are F/D", n.Name, groupDigits(n.Value), pct, 100*floatShare)
		fmt.Fprintf(&b, `<g><title>%s</title><rect x="%.2f" y="%d" width="%.2f" height="%d" rx="2" fill="%s" stroke="#fcfcfb" stroke-width="2"/>`,
			html.EscapeString(tip), x+flamePad, y, w, flameRowHeight-1, fill)
		if w > 28 {
			label := Fit(ShortName(n.Name), int((w-6)/flameCharWidth))
			if label != "" {
				fmt.Fprintf(&b, `<text x="%.2f" y="%d" font-size="11" fill="%s">%s</text>`, x+flamePad+3, y+13, ink, html.EscapeString(label))
			}
		}
		b.WriteString("</g>\n")
		cx := x
		for _, c := range n.Children {
			draw(c, cx, level+1)
			cx += float64(c.Value) / float64(r.Steps) * plot
		}
	}
	draw(tree, 0, 0)

	// Legend: the color scale.
	ly := height - 16
	legend := "width = instructions executed · color = share of a frame's own instructions that are F/D:"
	fmt.Fprintf(&b, `<text x="%d" y="%d" font-size="11" fill="#5c5c60">%s</text>`, flamePad, ly, html.EscapeString(legend))
	lx := flamePad + int(float64(len([]rune(legend)))*flameCharWidth) + 12
	fmt.Fprintf(&b, `<text x="%d" y="%d" font-size="11" fill="#5c5c60">0%%</text>`, lx, ly)
	lx += 22
	for i := 0; i <= 10; i++ {
		fmt.Fprintf(&b, `<rect x="%d" y="%d" width="14" height="10" fill="%s"/>`, lx+i*14, ly-9, floatColor(float64(i)/10))
	}
	fmt.Fprintf(&b, `<text x="%d" y="%d" font-size="11" fill="#5c5c60">100%%</text>`, lx+11*14+4, ly)
	fmt.Fprintf(&b, `<rect x="%d" y="%d" width="14" height="10" fill="#e1e0d9"/><text x="%d" y="%d" font-size="11" fill="#5c5c60">caller frame</text>`, lx+11*14+44, ly-9, lx+11*14+62, ly)
	b.WriteString("</svg>\n")
	return []byte(b.String())
}

func groupDigits(n uint64) string {
	s := fmt.Sprintf("%d", n)
	var out []byte
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	return string(out)
}
