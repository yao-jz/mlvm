#!/usr/bin/env bash
# Fetch the pinned TinyStories-260K weights from the model-pack manifest.
# Downloads into a temp file, checks size + SHA-256, then atomically moves.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST="$ROOT/models/pack260k"
MANIFEST="$ROOT/models/tinystories-260k.manifest.json"
mkdir -p "$DEST"

python3 - "$MANIFEST" "$DEST" <<'PY'
import hashlib, json, os, sys, tempfile, urllib.request
from pathlib import Path

manifest_path, dest = Path(sys.argv[1]), Path(sys.argv[2])
spec = json.loads(manifest_path.read_text())
dest.mkdir(parents=True, exist_ok=True)

def fetch(url: str, out: Path, want_sha: str, want_size: int) -> None:
    if out.exists():
        data = out.read_bytes()
        if len(data) == want_size and hashlib.sha256(data).hexdigest() == want_sha:
            print(f"ok  {out}")
            return
    print(f"==> fetching {out.name}")
    req = urllib.request.Request(url, headers={"User-Agent": "mlvm-decoder-fetch"})
    with urllib.request.urlopen(req, timeout=120) as resp:
        data = resp.read()
    if len(data) != want_size:
        raise SystemExit(f"size mismatch for {out.name}: got {len(data)} want {want_size}")
    got = hashlib.sha256(data).hexdigest()
    if got != want_sha:
        raise SystemExit(f"sha256 mismatch for {out.name}: got {got} want {want_sha}")
    fd, tmp = tempfile.mkstemp(dir=out.parent, prefix=".fetch-")
    try:
        os.write(fd, data)
        os.close(fd)
        os.replace(tmp, out)
    except Exception:
        try:
            os.remove(tmp)
        except OSError:
            pass
        raise
    print(f"ok  {out}")

for art in spec["artifacts"]:
    fetch(art["url"], dest / art["guestPath"], art["sha256"], art["size"])
PY

if [[ -f "$ROOT/examples/decoder/build/decoder" ]]; then
  cp "$ROOT/examples/decoder/build/decoder" "$DEST/decoder.elf"
  echo "ok  decoder.elf"
else
  echo "note: decoder.elf not copied (build with make -C examples/decoder); mlvm demo uses the embedded guest"
fi

cp "$MANIFEST" "$DEST/manifest.json"
echo "pack ready at $DEST"
