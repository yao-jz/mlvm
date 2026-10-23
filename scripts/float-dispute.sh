#!/usr/bin/env bash
#
# float-dispute.sh - exercise the experimental terminal path for a disputed f64 instruction.
#
#   ./scripts/float-dispute.sh
#
# A neural network runs in the VM, a routable f64 instruction is selected, and the terminal
# components are exercised in an EVM using RISCV.sol and FloatSubGame.sol, with no IEEE-754
# arithmetic implemented in Solidity.
#
# Seven acts, all of them running rather than asserted:
#
#   1  an MLP runs in the VM and the test selects a routable f64 instruction
#   2  local bisection isolates a synthetic divergent hash
#   3  that instruction is FMUL.D, and RISCV.sol reverts rather than mis-execute it
#   4  the disputed operation becomes a nested RISC-V machine, derived on chain
#   5  local inner bisection isolates one instruction of the nested machine
#   6  that instruction is RV64I, so RISCV.sol checks it - unmodified
#   7  the result comes back on chain, bit-identical to the VM's own float unit
#
# This integrates route C's terminal components. It is not an outer game and does not bind
# caller-supplied operands or the nested result to the outer transition; see docs/STATUS.md.

set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

CRATE=examples/rust/float-coprocessor
COPROCESSOR=$CRATE/target/riscv64ima-unknown-linux-gnu/release/float-coprocessor

# The three inputs the dispute needs. Each one produces an unhelpful "skipped" if missing,
# so build them here rather than explain the skip.

if [[ ! -f examples/build/mlp ]]; then
    echo "==> building the guest program"
    make -s examples
fi

if [[ ! -f "$COPROCESSOR" ]]; then
    echo "==> building the float co-processor (rv64ima - no float instructions)"
    (cd "$CRATE" && cargo build --target riscv64ima-unknown-linux-gnu.json \
        -Zbuild-std=core,compiler_builtins \
        -Zbuild-std-features=compiler-builtins-mem --release) >/dev/null 2>&1
fi

if [[ ! -f rvsol/out/FloatSubGame.sol/FloatSubGame.json ]]; then
    echo "==> building contracts"
    (cd rvsol && forge build) >/dev/null
fi

echo
# -v so the narrative reaches stdout. The test prints it directly rather than through
# t.Logf, whose file:line prefix would sit on every line.
(go test ./rvgo/test/ -run TestFloatTerminalRoute -count=1 -v -timeout 20m) \
    | grep -v -E '^(=== RUN|--- PASS|--- SKIP|PASS$|ok  )'

cat <<'EOF'

==> related component checks

    make float-subgame     1,522 differential vectors through the nested machine
    make float-nesting     marginal RV64I cost of one f64 operation
    make float-placement   what excluding floats costs instead (25.3x)
EOF
