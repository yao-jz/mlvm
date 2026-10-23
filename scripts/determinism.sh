#!/usr/bin/env bash
#
# determinism.sh - prove that MLVM's software floating point matches your hardware FPU.
#
#   ./scripts/determinism.sh [example ...]      (default: ieee matmul mlp)
#
# For each example it builds two binaries from the same C source - one for RV64IMAFD, one
# for the host - runs the first inside MLVM and the second natively, and diffs the output
# byte for byte.
#
# This is the property the whole project depends on. A fraud proof is only meaningful if
# honest parties agree on the answer, so the emulator must not merely be "close" to IEEE
# 754: every FADD.D, FMUL.D, FDIV.D and FSQRT.D has to produce the same 64 bits an x86 or
# ARM FPU would, including subnormals and ties-to-even. MLVM gets this by never touching
# the host FPU at all - rvgo/comp is a Go port of Berkeley SoftFloat 3, called from
# fast/vm.go with a fixed rounding mode and a fixed NaN policy.
#
# mlp is in the default set as a deliberate counter-example: it calls tanh() and exp(),
# which are libm routines rather than ISA instructions, and glibc's last-ULP results
# differ from Apple's or musl's. Its divergence is a property of the C library, not of the
# emulator, so it is reported but does not fail the run.

set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

# Examples whose output depends on libm transcendentals, where a last-ULP difference
# between two libm implementations is expected and is not an emulator bug.
LIBM_DEPENDENT=" mlp "

EXAMPLES=("$@")
if [[ ${#EXAMPLES[@]} -eq 0 ]]; then
    EXAMPLES=(ieee matmul mlp)
fi

echo "==> building RISC-V and host binaries from identical sources"
make -s -C examples all native >/dev/null

identical=0
expected=0
failed=0

for ex in "${EXAMPLES[@]}"; do
    rv="examples/build/$ex"
    host="examples/build/native/$ex"
    if [[ ! -x "$rv" || ! -x "$host" ]]; then
        echo "!! $ex: missing binaries ($rv / $host)"
        failed=$((failed + 1))
        continue
    fi

    vm_out="$(./scripts/run-elf.sh "$rv" 2>/dev/null \
        | sed -n '/^----/,/^----/p' | sed '1d;$d')"
    host_out="$("$host")"

    echo
    echo "=== $ex ==============================================="
    if [[ "$vm_out" == "$host_out" ]]; then
        echo "IDENTICAL - MLVM softfloat == host FPU, byte for byte"
        printf '%s\n' "$host_out" | sed 's/^/    /'
        identical=$((identical + 1))
    elif [[ "$LIBM_DEPENDENT" == *" $ex "* ]]; then
        echo "DIVERGED (expected: this example calls libm transcendentals)"
        diff <(printf '%s\n' "$host_out") <(printf '%s\n' "$vm_out") \
            --label "host libm ($(uname -m))" --label "glibc in mlvm (rv64imafd)" -u \
            | sed 's/^/    /' || true
        expected=$((expected + 1))
    else
        echo "DIVERGED - this is an emulator bug, please report it"
        diff <(printf '%s\n' "$host_out") <(printf '%s\n' "$vm_out") \
            --label "host ($(uname -m))" --label "mlvm (rv64imafd)" -u | sed 's/^/    /' || true
        failed=$((failed + 1))
    fi
done

echo
echo "======================================================="
printf "bit-identical: %d    expected libm divergence: %d    unexplained: %d\n" \
    "$identical" "$expected" "$failed"
[[ $failed -eq 0 ]]
