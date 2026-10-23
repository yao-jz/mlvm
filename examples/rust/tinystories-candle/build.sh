#!/usr/bin/env bash
#
# Cross-compile the Candle TinyStories guest to RV64IMAFD.
#
#   ./build.sh          incremental
#   ./build.sh clean    cargo clean first
#
# Same toolchain as ../bert-riscv: nightly Rust with rust-src and a riscv64 glibc cross
# linker. See docs/INSTALL.md#5-rust--only-to-rebuild-the-ml-guests.
set -euo pipefail
cd "$(dirname "$0")"
[[ "${1:-}" == "clean" ]] && cargo clean

cargo build --target riscv64imafd-unknown-linux-gnu.json \
            -Zbuild-std=std,panic_abort --release

OUT=target/riscv64imafd-unknown-linux-gnu/release/tinystories-candle
echo
echo "built $OUT"
echo "run it with the decoder's model, tokenizer and a request (see README.md)"
