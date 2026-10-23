package xray

import (
	"strconv"
	"strings"
)

// demangleRust turns a legacy-mangled Rust symbol, the scheme rustc uses by default,
// into its path:
//
//	_ZN104_$LT$candle_core..cpu_backend..MatMul$u20$as$u20$candle_core..cpu_backend..utils..Map2$GT$1f17h2bd6a2c6c8a3e0b1E
//	<candle_core::cpu_backend::MatMul as candle_core::cpu_backend::utils::Map2>::f
//
// The trailing hash element is dropped. It reports false for anything else, including C++
// names, which share the _ZN prefix but carry parameter types after the closing E.
func demangleRust(sym string) (string, bool) {
	rest, ok := strings.CutPrefix(sym, "_ZN")
	if !ok {
		return "", false
	}
	var elems []string
	for {
		if rest == "" {
			return "", false
		}
		if rest[0] == 'E' {
			rest = rest[1:]
			break
		}
		n := 0
		for n < len(rest) && rest[n] >= '0' && rest[n] <= '9' {
			n++
		}
		size, err := strconv.Atoi(rest[:n])
		if n == 0 || err != nil || size == 0 || n+size > len(rest) {
			return "", false
		}
		elems = append(elems, rest[n:n+size])
		rest = rest[n+size:]
	}
	if rest != "" && rest[0] != '.' { // ".llvm.123" and similar suffixes are fine
		return "", false
	}
	if len(elems) > 1 && isRustHash(elems[len(elems)-1]) {
		elems = elems[:len(elems)-1]
	}
	if len(elems) == 0 {
		return "", false
	}
	var b strings.Builder
	for i, e := range elems {
		if i > 0 {
			b.WriteString("::")
		}
		if !unescapeRust(&b, e) {
			return "", false
		}
	}
	return b.String(), true
}

func isRustHash(e string) bool {
	if len(e) != 17 || e[0] != 'h' {
		return false
	}
	for _, c := range e[1:] {
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f') {
			return false
		}
	}
	return true
}

var rustEscapes = map[string]string{
	"SP": "@", "BP": "*", "RF": "&", "LT": "<", "GT": ">", "LP": "(", "RP": ")", "C": ",",
}

// unescapeRust writes one path element: ".." is "::" and $..$ spells a punctuation mark.
func unescapeRust(b *strings.Builder, e string) bool {
	if strings.HasPrefix(e, "_$") {
		e = e[1:]
	}
	for e != "" {
		switch {
		case strings.HasPrefix(e, ".."):
			b.WriteString("::")
			e = e[2:]
		case e[0] == '$':
			end := strings.IndexByte(e[1:], '$')
			if end < 0 {
				return false
			}
			code := e[1 : 1+end]
			if s, ok := rustEscapes[code]; ok {
				b.WriteString(s)
			} else if hex, ok := strings.CutPrefix(code, "u"); ok {
				r, err := strconv.ParseUint(hex, 16, 32)
				if err != nil {
					return false
				}
				b.WriteRune(rune(r))
			} else {
				return false
			}
			e = e[2+end:]
		default:
			n := strings.IndexAny(e, "$.")
			if n == 0 { // a single '.'
				n = 1
			} else if n < 0 {
				n = len(e)
			}
			b.WriteString(e[:n])
			e = e[n:]
		}
	}
	return true
}

// ShortName is how a function is labeled where space is short: paths inside generic
// arguments and qualified names are cut to their last segment, so
//
//	<candle_core::cpu_backend::MatMul as candle_core::cpu_backend::utils::Map2>::f
//
// reads <MatMul as Map2>::f. The leading path is kept; Fit trims it if needed.
func ShortName(name string) string {
	if !strings.Contains(name, "::") {
		return name
	}
	var b strings.Builder
	depth, mark := 0, 0 // mark: where the identifier being written began
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c == '<':
			depth++
			b.WriteByte(c)
			mark = b.Len()
		case c == '>':
			depth = max(0, depth-1)
			b.WriteByte(c)
			mark = b.Len()
		case c == ':' && i+1 < len(name) && name[i+1] == ':' && depth > 0:
			// drop the segment just written, and this separator
			s := b.String()[:mark]
			b.Reset()
			b.WriteString(s)
			i++
		case c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
			b.WriteByte(c)
		default:
			b.WriteByte(c)
			mark = b.Len()
		}
	}
	return b.String()
}

// Fit shortens a label to at most n bytes. Paths lose their front ("..f32::x4x2x4"),
// because the end of a path is the specific part; other names lose their end.
func Fit(name string, n int) string {
	if len(name) <= n {
		return name
	}
	if n < 3 {
		return ""
	}
	if strings.Contains(name, "::") {
		return ".." + name[len(name)-(n-2):]
	}
	return name[:n-2] + ".."
}
