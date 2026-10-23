#!/usr/bin/env bash
#
# float-nesting.sh - measure the nested RV64I expansion of an f64 operation.
#
#   ./scripts/float-nesting.sh
#
# RISCV.sol cannot execute FADD.D. But the same operation compiled for RV64IMA - where the
# compiler has no float instructions to emit - becomes integer instructions that RISCV.sol
# *can* execute and has been cross-checked on 84 conformance suites.
#
# A future dispute game could nest an integer trace and execute one terminal step. This is
# Cartesi's micro-architecture idea applied selectively: the measured expansion is ~10^2
# steps rather than a fixed 2^20 span.
#
# This script measures the marginal cost per operation and verifies every expanded step
# through the EVM stepper. `float-dispute.sh` integrates the other terminal components; it
# is also not a complete outer game.

set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

CRATE=examples/rust/float-coprocessor
BUILD=(-Zbuild-std=core,compiler_builtins -Zbuild-std-features=compiler-builtins-mem --release)
BIN=$CRATE/target/riscv64ima-unknown-linux-gnu/release/float-coprocessor

set_ops() {
    python3 - "$1" <<'PY'
import pathlib, re, sys
p = pathlib.Path('examples/rust/float-coprocessor/src/main.rs')
p.write_text(re.sub(r'const OPS: usize = \d+;', f'const OPS: usize = {sys.argv[1]};', p.read_text(), count=1))
PY
}

echo "==> marginal cost of one f64 addition, as RV64I instructions"
printf '  %-8s %10s\n' OPS STEPS
declare -a xs ys
for n in 1 2 5 10 20; do
    set_ops "$n"
    (cd "$CRATE" && cargo build --target riscv64ima-unknown-linux-gnu.json "${BUILD[@]}") >/dev/null 2>&1
    # Capture first: piping into `grep -m1` SIGPIPEs the producer, and under
    # `set -o pipefail` that aborts the script with a code that looks nothing like the cause.
    ./scripts/run-elf.sh "$BIN" >/tmp/mlvm-fc-run.log 2>&1 || true
    steps=$(grep -m1 '^  steps' /tmp/mlvm-fc-run.log | awk '{print $2}')
    printf '  %-8s %10s\n' "$n" "$steps"
    xs+=("$n"); ys+=("$steps")
done
python3 - "${xs[*]}" "${ys[*]}" <<'PY'
import sys
xs = [int(v) for v in sys.argv[1].split()]
ys = [int(v) for v in sys.argv[2].split()]
n = len(xs); sx = sum(xs); sy = sum(ys)
sxx = sum(x * x for x in xs); sxy = sum(x * y for x, y in zip(xs, ys))
slope = (n * sxy - sx * sy) / (n * sxx - sx * sx)
print(f"\n  {slope:.1f} RV64I instructions per f64 addition")
print(f"  fixed overhead (start-up + write syscall): {(sy - slope * sx) / n:.0f} steps")
PY

set_ops 1
(cd "$CRATE" && cargo build --target riscv64ima-unknown-linux-gnu.json "${BUILD[@]}") >/dev/null 2>&1

echo
echo "==> verifying every step through the on-chain verifier"
(cd rvsol && forge build >/dev/null 2>&1)
(go test ./rvgo/test/ -run TestFloatNesting -count=1 -v 2>&1 \
    | grep -E 'verifier-compatible|worst-case gas|matches rvgo|^--- ') | sed 's/.*float_nesting_test.go:[0-9]*: //;s/^/  /'
