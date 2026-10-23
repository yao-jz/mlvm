#!/usr/bin/env bash
#
# Cross-compile the BERT inference guest to RV64IMAFD.
#
#   ./build.sh          incremental
#   ./build.sh clean    cargo clean first
#
# Requires nightly Rust with rust-src, and the tokenizers crate vendored at
# third_party/tokenizers - see docs/INSTALL.md#5-rust--only-to-rebuild-the-ml-guests.
set -euo pipefail
cd "$(dirname "$0")"
[[ "${1:-}" == "clean" ]] && cargo clean

cargo build --target riscv64imafd-unknown-linux-gnu.json \
            -Zbuild-std=std,panic_abort --release

OUT=target/riscv64imafd-unknown-linux-gnu/release/myriscvcandleapp
echo
echo "built $OUT"
echo "run it with:"
echo "  ./scripts/run-elf.sh examples/rust/bert-riscv/$OUT \\"
echo "      config.json tokenizer.json model.safetensors"
