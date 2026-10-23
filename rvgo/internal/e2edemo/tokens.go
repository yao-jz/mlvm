package e2edemo

import "strings"

// TokenWatcher is an io.Writer for the decoder guest's stdout that reports each generated
// token as the guest prints it. The guest prints "generated: ", one write per token, and
// then "\ntokens: ...", which may arrive in one write or several. Text that could be the
// start of that end marker is held until the next write decides; a token whose text is
// itself "\n" is still reported, at the step it was printed.
type TokenWatcher struct {
	// Now reports the current instruction count, recorded with each event (optional).
	Now     func() uint64
	OnStart func(at uint64)
	OnToken func(text string, at uint64)

	tail    string // last bytes before the start marker, so a split "generated: " is found
	started bool
	done    bool
	pending string // held text that may begin the end marker
	pendAt  uint64
}

const (
	generatedMarker = "generated: "
	endMarker       = "\ntokens:"
)

func (w *TokenWatcher) now() uint64 {
	if w.Now == nil {
		return 0
	}
	return w.Now()
}

func (w *TokenWatcher) Write(p []byte) (int, error) {
	if w.done {
		return len(p), nil
	}
	text := string(p)
	if !w.started {
		buf := w.tail + text
		i := strings.Index(buf, generatedMarker)
		if i < 0 {
			if len(buf) > len(generatedMarker) {
				buf = buf[len(buf)-len(generatedMarker):]
			}
			w.tail = buf
			return len(p), nil
		}
		w.started = true
		w.tail = ""
		if w.OnStart != nil {
			w.OnStart(w.now())
		}
		text = buf[i+len(generatedMarker):]
	}
	w.feed(text, w.now())
	return len(p), nil
}

func (w *TokenWatcher) feed(text string, at uint64) {
	if text == "" {
		return
	}
	if w.pending != "" {
		joined := w.pending + text
		switch {
		case strings.HasPrefix(joined, endMarker):
			w.done = true
			return
		case strings.HasPrefix(endMarker, joined):
			w.pending = joined // still could be the marker
			return
		}
		w.emit(w.pending, w.pendAt) // it was a real token after all
		w.pending = ""
	}
	if i := strings.Index(text, endMarker); i >= 0 {
		if i > 0 {
			w.emit(text[:i], at)
		}
		w.done = true
		return
	}
	for k := min(len(text), len(endMarker)-1); k > 0; k-- {
		if strings.HasSuffix(text, endMarker[:k]) {
			if k < len(text) {
				w.emit(text[:len(text)-k], at)
			}
			w.pending, w.pendAt = text[len(text)-k:], at
			return
		}
	}
	w.emit(text, at)
}

func (w *TokenWatcher) emit(text string, at uint64) {
	if w.OnToken != nil {
		w.OnToken(text, at)
	}
}
