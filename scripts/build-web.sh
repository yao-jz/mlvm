#!/usr/bin/env bash
# build-web.sh - assemble the browser playground into a static directory.
#
#   ./scripts/build-web.sh [out-dir]        # default web/dist
#
# Builds mlvm.wasm from rvgo/wasm, copies Go's wasm_exec.js and the two pages (the
# playground and the technical write-up, blog.html), and, unless the directory already has
# one from CI, records a local determinism.json from the native binary and the WebAssembly
# build. Serve the result with any static server:
#   python3 -m http.server -d web/dist 8000
set -euo pipefail
source "$(dirname "$0")/lib.sh"
cd "$REPO_ROOT"
OUT="${1:-web/dist}"
mkdir -p "$OUT"

COMMIT="$(git rev-parse HEAD 2>/dev/null || echo unknown)"
echo "==> building $OUT/mlvm.wasm" >&2
GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" -o "$OUT/mlvm.wasm" ./rvgo/wasm

GOROOT="$(go env GOROOT)"
WASM_EXEC="$GOROOT/lib/wasm/wasm_exec.js"
[[ -f "$WASM_EXEC" ]] || WASM_EXEC="$GOROOT/misc/wasm/wasm_exec.js"
# install, not cp: the toolchain's copy is read-only, and cp cannot overwrite the copy a
# previous build left in $OUT.
install -m 644 "$WASM_EXEC" "$OUT/wasm_exec.js"
cp web/index.html web/blog.html "$OUT/"
# Both pages show the repository's diagrams and benchmark figures.
mkdir -p "$OUT/figures"
cp docs/fig/*.svg "$OUT/figures/"
cp web/src/*.js web/src/*.css "$OUT/"
[[ -f web/social-card.png ]] && cp web/social-card.png "$OUT/"
# The "Beyond the toy model" section: recorded gallery runs (scripts/gallery.sh).
python3 scripts/gallery-json.py "$OUT"

# Re-record a local determinism.json made at another commit: the VM or the guest may have
# changed, and stale roots would show up as mismatches in the page. CI writes its own
# (source "ci") right before calling this script.
stale=1
if [[ -f "$OUT/determinism.json" ]]; then
    stale="$(python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); print(0 if d.get("source") == "ci" or d.get("commit") == sys.argv[2] else 1)' "$OUT/determinism.json" "$COMMIT")"
fi
if [[ "$stale" == 1 || "${MLVM_REFRESH_DETERMINISM:-0}" == 1 ]]; then
    ensure_mlvm
    host="$(go env GOOS)/$(go env GOARCH)"
    tmp="$(mktemp -d)"
    python3 scripts/determinism-record.py --mlvm "$MLVM_BIN" --platform "$host" --out "$tmp/native.json"
    if command -v node >/dev/null 2>&1; then
        node web/test/record.mjs "$OUT/mlvm.wasm" "$WASM_EXEC" "$tmp/wasm.json"
    fi
    python3 scripts/determinism-merge.py --out-dir "$OUT" --commit "$COMMIT" --source local "$tmp"/*.json
    rm -rf "$tmp"
fi
echo "==> $OUT ready: python3 -m http.server -d $OUT 8000" >&2
