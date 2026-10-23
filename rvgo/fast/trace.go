package fast

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Trace controls the VM's per-step diagnostic output: the syscall log, mmap/mremap
// decisions, file-descriptor bookkeeping and the random-number cursor.
//
// It is off by default. The trace is genuinely useful when porting a new program (it is
// usually how you discover the one syscall you are missing), but a program of any size
// issues tens of thousands of them, and writing that to a terminal costs more wall clock
// than the emulation itself.
//
// Enable it with `mlvm run --trace`, or set Trace directly when embedding the VM.
var Trace = false

// TraceOut is where trace lines go. It is stderr rather than stdout so that a traced run
// still pipes the guest program's own output cleanly.
var TraceOut io.Writer = os.Stderr

func tracef(format string, a ...any) {
	if !Trace {
		return
	}
	fmt.Fprintf(TraceOut, format, a...)
}

// kernelPseudoPrefixes are the mount points whose contents describe the host machine
// rather than the program: process introspection, kernel tunables and device nodes. A
// deterministic VM must not expose any of them, so openat on these is refused with
// EACCES regardless of what is staged.
var kernelPseudoPrefixes = []string{"/proc/", "/sys/", "/dev/", "/run/"}

// isKernelPseudoFile reports whether a path names host state rather than program data.
// The bare mount points themselves are included so that opendir("/proc") is refused too.
func isKernelPseudoFile(path string) bool {
	switch path {
	case "/proc", "/sys", "/dev", "/run":
		return true
	}
	for _, p := range kernelPseudoPrefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}
