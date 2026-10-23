#!/usr/bin/env bash
# Run the TinyStories decoder inside MLVM and record a receipt.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=lib.sh
source "$ROOT/scripts/lib.sh"
ensure_mlvm

PACK="${1:-$ROOT/models/pack260k}"
PROMPT="${2:-Once upon a time}"
OUT="${OUT:-$ROOT/results/decoder-receipt.json}"
mkdir -p "$(dirname "$OUT")"

if [[ ! -f "$PACK/model.bin" ]]; then
  "$ROOT/scripts/decoder-fetch.sh"
fi
if [[ ! -f "$ROOT/examples/decoder/build/decoder" ]]; then
  make -C "$ROOT/examples/decoder" CROSS="${CROSS:-riscv64-unknown-linux-gnu-}"
fi
cp "$ROOT/examples/decoder/build/decoder" "$PACK/decoder.elf"

"$MLVM_BIN" demo --model-pack "$PACK" --json --receipt "$OUT" "$PROMPT"
echo "wrote $OUT"
