#!/usr/bin/env bash
#
# float-placement.sh - is floating point cheaper to emulate in the interpreter, or in the
# compiler?
#
#   ./scripts/float-placement.sh
#
# Every verifiable virtual machine faces this choice. MLVM implements RV64F/D and executes
# FADD.D directly through the software IEEE-754 implementation in rvgo/comp. The
# alternative, which most fault-proof and zero-knowledge VMs take, is to omit F/D entirely
# and let the compiler expand each floating-point operation into a soft-float helper made
# of integer instructions - which the VM then emulates one by one.
#
# examples/rust/float-placement builds one no_std kernel for both targets:
#
#   riscv64imafd (lp64d)  float instructions, executed by the VM's SoftFloat
#   riscv64ima   (lp64)   no float instructions; compiler_builtins expands them
#
# The metric is emulated step count. It is hardware-independent, it determines replay cost
# during bisection, and for a zkVM it determines proving cost linearly.

set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

CRATE=examples/rust/float-placement
BUILD_FLAGS=(-Zbuild-std=core,compiler_builtins -Zbuild-std-features=compiler-builtins-mem --release)

command -v cargo >/dev/null || { echo "error: cargo not found; see docs/INSTALL.md" >&2; exit 1; }
ensure_mlvm

echo "==> building the same kernel for both targets"
for t in riscv64imafd riscv64ima; do
    (cd "$CRATE" && cargo build --target "$t-unknown-linux-gnu.json" "${BUILD_FLAGS[@]}") \
        >/dev/null 2>&1 || {
        echo "error: build failed for $t" >&2
        echo "hint:  needs nightly Rust with rust-src (rustup component add rust-src)" >&2
        exit 1
    }
done

echo
printf '%-14s %10s %10s  %s\n' TARGET BYTES FLOAT-INSNS "note"
for t in riscv64imafd riscv64ima; do
    bin="$CRATE/target/$t-unknown-linux-gnu/release/float-placement"
    n=$(riscv64-unknown-linux-gnu-objdump -d "$bin" 2>/dev/null \
        | grep -cE '\bf(add|sub|mul|div|sqrt|mv|cvt|l[wd]|s[wd]|madd|le|lt|eq)\.' || true)
    if [[ "$t" == riscv64imafd ]]; then
        note="VM executes these through SoftFloat"
    else
        note="compiler expanded them into integer code"
    fi
    printf '%-14s %10s %10s  %s\n' "$t" "$(wc -c <"$bin" | tr -d ' ')" "$n" "$note"
done

declare -A STEPS FALU FLS OUT
for t in riscv64imafd riscv64ima; do
    echo
    echo "==> running $t in the VM"
    log=$(mktemp)
    ./scripts/run-elf.sh "$CRATE/target/$t-unknown-linux-gnu/release/float-placement" >"$log" 2>&1
    STEPS[$t]=$(grep -m1 '^  steps' "$log" | awk '{print $2}')
    FALU[$t]=$(grep -m1 'float ALU' "$log" | awk '{print $3}')
    FLS[$t]=$(grep -m1 'float load/store' "$log" | awk '{print $3}')
    OUT[$t]=$(grep -E '^(trace|frobenius|C\[0\]\[0\])' "$log" | tr '\n' ';')
    grep -E '^  (steps|integer ALU|memory load|float ALU|float load|throughput)' "$log" | sed 's/^/  /'
    rm -f "$log"
done

echo
echo "======================================================================"
if [[ "${OUT[riscv64imafd]}" == "${OUT[riscv64ima]}" ]]; then
    echo " Both builds produced BIT-IDENTICAL results:"
    printf '%s\n' "${OUT[riscv64imafd]}" | tr ';' '\n' | sed '/^$/d;s/^/   /'
    echo
    echo " Two independent IEEE-754 implementations - MLVM's SoftFloat port and Rust's"
    echo " compiler_builtins - agree to the bit."
else
    echo " !! The two builds DISAGREE. That is a bug worth reporting."
    printf '   imafd: %s\n   ima:   %s\n' "${OUT[riscv64imafd]}" "${OUT[riscv64ima]}"
fi

echo
python3 - "${STEPS[riscv64imafd]}" "${STEPS[riscv64ima]}" "${FALU[riscv64imafd]}" "${FLS[riscv64imafd]}" <<'PY'
import sys
hard, soft, falu, fls = (int(x) for x in sys.argv[1:5])
nonfloat = hard - falu - fls
per_op = (soft - nonfloat - fls) / falu if falu else 0
print(f" interpreter-level float (rv64imafd) : {hard:>12,} steps")
print(f" compiler-level float   (rv64ima)    : {soft:>12,} steps")
print()
print(f" RATIO                               : {soft/hard:>11.1f}x more steps")
print(f" per double-precision arithmetic op  : {per_op:>11.0f} integer instructions")
print()
print(" Implementing F/D in the interpreter is not a cost - it is a large saving. For a")
print(" zero-knowledge VM, whose proving cost is linear in cycles, this ratio carries")
print(" through directly.")
PY
