#!/usr/bin/env bash
# Fail-closed check that the embedded TinyStories demo still produces the
# checked-in canonical receipt digest. Does not rewrite the baseline.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

BASELINE="$ROOT/results/archive-smoke-canon.sha256"
if [[ ! -f "$BASELINE" ]]; then
  echo "error: missing $BASELINE (check it into git)" >&2
  exit 1
fi

make build
# The receipt carries a creation time, so it goes to a temporary file; the checked-in
# results/archive-smoke-receipt.json is an example, refreshed by hand with the baseline.
RECEIPT="$(mktemp)"
trap 'rm -f "$RECEIPT"' EXIT
bin/mlvm demo --json > "$RECEIPT"
RECEIPT="$RECEIPT" python3 - <<'PY'
import json, os, sys
from pathlib import Path
r = json.loads(Path(os.environ["RECEIPT"]).read_text())
assert r["profile"] == "mlvm-1"
assert r["demo"] == "decoder"
assert r["verifier"]["passed"] is True
assert "little girl" in r.get("generatedText", "")
digest = r.get("canonicalDigest")
assert digest and len(digest) == 64
want = Path("results/archive-smoke-canon.sha256").read_text().strip()
print("canonical demo digest:", digest)
if digest != want:
    print("canonical digest mismatch", file=sys.stderr)
    print("  want", want, file=sys.stderr)
    print("  got ", digest, file=sys.stderr)
    print("update the baseline only after a deliberate guest/protocol change", file=sys.stderr)
    sys.exit(1)
print("archive smoke ok")
PY
