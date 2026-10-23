#!/usr/bin/env bash
#
# Cross-compile the playground guest to RV64IMA (integer only, soft-float).
#
#   ./build.sh          incremental
#   ./build.sh clean    cargo clean first
#
# This target deliberately omits F and D so the compiler emits soft-float calls, which is
# useful for comparing compiler-level against VM-level float emulation.
set -euo pipefail
cd "$(dirname "$0")"
[[ "${1:-}" == "clean" ]] && cargo clean

cargo build --target riscv64ima-unknown-linux-gnu.json -Zbuild-std=std,panic_abort

OUT=target/riscv64ima-unknown-linux-gnu/debug/playground
echo
echo "built $OUT"
echo "run it with:  ./scripts/run-elf.sh examples/rust/playground/$OUT"
