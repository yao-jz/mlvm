#!/usr/bin/env bash
#
# run-elf.sh - load a RISC-V ELF into MLVM and execute it.
#
#   ./scripts/run-elf.sh <elf> [file-to-stage ...]
#
# Any extra arguments are files staged into the emulated address space before the program
# starts, so the guest can openat()/read() them out of merkleized memory. Paths are
# repo-relative and are passed through verbatim, which means the guest must open a file
# under exactly the same string.
#
# Environment:
#   OUT_DIR      where state/meta/output JSON goes         (default: a new directory in .mlvm-run)
#   INFO_AT      progress pattern, as mlvm run --info-at   (default: %1000000)
#   RANDOM_FILE  blob that getrandom(2) serves from        (default: rvgo/random)
#   TRACE        set to 1 to show the per-syscall trace
#   KEEP         set to 1 to keep OUT_DIR after the run
#
# Examples:
#   ./scripts/run-elf.sh examples/build/hello
#   ./scripts/run-elf.sh examples/build/fileio examples/data/sample.txt
#   TRACE=1 ./scripts/run-elf.sh examples/build/mlp

set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

ELF="${1:-}"
if [[ -z "$ELF" ]]; then
    sed -n '2,22p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
    exit 2
fi
shift
require_elf "$ELF"
ensure_mlvm

INFO_AT="${INFO_AT:-%1000000}"
if [[ -n "${OUT_DIR:-}" ]]; then
    mkdir -p "$OUT_DIR"
else
    # A directory of its own, so two runs at once do not delete each other's files.
    mkdir -p "$REPO_ROOT/.mlvm-run"
    OUT_DIR="$(mktemp -d "$REPO_ROOT/.mlvm-run/run.XXXXXX")"
fi

STAGE_ARGS=()
for f in "$@"; do
    # Support guestPath=hostPath aliases so model packs can use stable guest names.
    if [[ "$f" == *=* ]]; then
        host="${f#*=}"
        if [[ ! -f "$host" ]]; then
            echo "error: no such file to stage: $host" >&2
            exit 1
        fi
        STAGE_ARGS+=(--load-file "$f")
        continue
    fi
    if [[ ! -f "$f" ]]; then
        echo "error: no such file to stage: $f" >&2
        exit 1
    fi
    STAGE_ARGS+=(--load-file "$f")
done

name="$(basename "$ELF")"
echo "==> loading $name into a fresh VM state"
"$MLVM_BIN" load-elf \
    --path "$ELF" \
    --out "$OUT_DIR/$name.state.json" \
    --meta "$OUT_DIR/$name.meta.json" \
    --random-file "$RANDOM_FILE" \
    "${STAGE_ARGS[@]}" >"$OUT_DIR/$name.load.log" 2>&1 || {
    echo "load-elf failed; see $OUT_DIR/$name.load.log" >&2
    tail -20 "$OUT_DIR/$name.load.log" >&2
    exit 1
}
echo "    $(grep -m1 'program entry' "$OUT_DIR/$name.load.log" || echo loaded)"

echo "==> running (guest output below)"
echo "-------------------------------------------------"
TRACE_ARG=()
[[ "${TRACE:-0}" == "1" ]] && TRACE_ARG=(--trace)

set +e
"$MLVM_BIN" run \
    --input "$OUT_DIR/$name.state.json" \
    --output "$OUT_DIR/$name.out.json" \
	--meta "$OUT_DIR/$name.meta.json" \
	--info-at "$INFO_AT" \
	--raw-output \
	"${TRACE_ARG[@]}"
rc=$?
set -e
echo "-------------------------------------------------"

if [[ "${KEEP:-0}" != "1" ]]; then
    rm -rf "$OUT_DIR"
    rmdir "$REPO_ROOT/.mlvm-run" 2>/dev/null || true
else
    echo "==> artifacts kept in $OUT_DIR"
fi

exit "$rc"
