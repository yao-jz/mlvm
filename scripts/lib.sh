# shellcheck shell=bash
#
# lib.sh - shared setup for the scripts in this directory. Source it, do not run it.
#
# Its one job is to hand back a runnable path to the MLVM binary in $MLVM_BIN,
# with the repo root as the working directory. Everything runs from the repo root so a
# staged file path means the same thing to you, to the host, and to the guest: the guest
# must open a file under exactly the string it was staged with, and repo-relative paths
# are the least surprising choice.

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RVGO="$REPO_ROOT/rvgo"
MLVM_BIN="$REPO_ROOT/bin/mlvm"

# Default randomness blob. getrandom(2) serves from this, and it is part of the committed
# initial state rather than a host resource - see docs/SYSCALLS.md#randomness.
RANDOM_FILE="${RANDOM_FILE:-$RVGO/random}"

ensure_mlvm() {
    # Rebuild if the binary is missing or older than any Go source in the CLI or rvgo. Checking a
    # single file is not enough: a change to elf.go or memory.go matters just as much as
    # one to vm.go, and a stale binary produces confusing results.
    if [[ -x "$MLVM_BIN" ]]; then
        newest=$(find "$REPO_ROOT/cmd" "$RVGO" -name '*.go' \
            -newer "$MLVM_BIN" -print -quit 2>/dev/null)
        [[ -z "$newest" ]] && return 0
    fi
    echo "==> building MLVM" >&2
    mkdir -p "$REPO_ROOT/bin"
    # Build and sign a private copy, then rename it into place: scripts run side by side
    # (scripts/gallery.sh) may rebuild at once, and must never run a half-written binary.
    local tmp="$MLVM_BIN.tmp.$$"
    (cd "$REPO_ROOT" && go build -trimpath -o "$tmp" ./cmd/mlvm) || { rm -f "$tmp"; return 1; }

    # macOS: the Go linker produces an ad-hoc "linker-signed" signature that some
    # security configurations refuse, and the binary dies with SIGKILL and no message.
    # Re-signing ad-hoc with codesign makes it runnable. Harmless if it is already fine.
    if [[ "$(uname -s)" == "Darwin" ]] && command -v codesign >/dev/null 2>&1; then
        codesign -s - -f "$tmp" >/dev/null 2>&1 || true
    fi
    mv -f "$tmp" "$MLVM_BIN"
}

require_elf() {
    if [[ ! -f "$1" ]]; then
        echo "error: no such ELF: $1" >&2
        echo "hint:  run 'make examples' to build the sample programs" >&2
        exit 1
    fi
}

cd "$REPO_ROOT"
