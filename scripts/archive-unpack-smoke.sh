#!/usr/bin/env bash
# Unpack a GoReleaser archive (or pick one from dist/) and check that
# `mlvm demo` matches results/archive-smoke-canon.sha256.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BASELINE="$ROOT/results/archive-smoke-canon.sha256"
TARGET="${1:-}"

if [[ ! -f "$BASELINE" ]]; then
  echo "error: missing $BASELINE" >&2
  exit 1
fi
if [[ -z "$TARGET" ]]; then
  echo "usage: $0 <archive-or-dist-dir>" >&2
  exit 1
fi

python3 - "$ROOT" "$BASELINE" "$TARGET" <<'PY'
import json, os, platform, shutil, subprocess, sys, tarfile, tempfile, zipfile
from pathlib import Path

root, baseline_path, target = Path(sys.argv[1]), Path(sys.argv[2]), Path(sys.argv[3])
want = baseline_path.read_text().strip()

def score(name: str, system: str, machine: str) -> int:
    n = name.lower()
    s = 0
    if system == "linux" and "linux" in n:
        s += 10
    if system == "darwin" and "darwin" in n:
        s += 10
    if system == "windows" and "windows" in n:
        s += 10
    if machine in ("x86_64", "amd64") and ("x86_64" in n or "amd64" in n):
        s += 5
    if machine in ("arm64", "aarch64") and "arm64" in n:
        s += 5
    if n.endswith(".tar.gz") or n.endswith(".zip"):
        s += 1
    return s

def pick_archive(path: Path) -> Path:
    if path.is_file():
        return path
    system = platform.system().lower()
    machine = platform.machine().lower()
    cands = [p for p in path.iterdir() if p.suffix in {".gz", ".zip"} or p.name.endswith(".tar.gz")]
    if not cands:
        raise SystemExit(f"no archives in {path}")
    cands.sort(key=lambda p: score(p.name, system, machine), reverse=True)
    chosen = cands[0]
    if score(chosen.name, system, machine) < 11:
        raise SystemExit(f"no archive in {path} matches {system}/{machine}; have {[p.name for p in cands]}")
    return chosen

archive = pick_archive(target)
print(f"unpacking {archive.name}")
tmp = Path(tempfile.mkdtemp(prefix="mlvm-unpack-"))
try:
    if archive.suffix == ".zip":
        with zipfile.ZipFile(archive) as zf:
            zf.extractall(tmp)
    else:
        with tarfile.open(archive) as tf:
            tf.extractall(tmp)
    bins = list(tmp.rglob("mlvm")) + list(tmp.rglob("mlvm.exe"))
    if not bins:
        raise SystemExit(f"archive {archive.name} has no mlvm binary")
    binary = bins[0]
    binary.chmod(binary.stat().st_mode | 0o111)
    out = subprocess.check_output([str(binary), "demo", "--json"], cwd=tmp)
    rec = json.loads(out)
    assert rec["demo"] == "decoder", rec.get("demo")
    assert rec["verifier"]["passed"] is True
    digest = rec.get("canonicalDigest")
    print("canonical demo digest:", digest)
    if digest != want:
        raise SystemExit(f"digest mismatch\n  want {want}\n  got  {digest}")
    print("archive unpack smoke ok")
finally:
    shutil.rmtree(tmp, ignore_errors=True)
PY
