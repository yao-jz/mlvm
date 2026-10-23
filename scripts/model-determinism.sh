#!/usr/bin/env bash
#
# model-determinism.sh - does a real transformer produce the same answer inside MLVM as it
# does on your hardware?
#
#   ./scripts/model-determinism.sh              every model in .models/
#   ./scripts/model-determinism.sh bge-base     one of them
#
# examples/rust/bert-host and examples/rust/bert-riscv share a byte-identical src/main.rs
# and pinned dependency versions, so the only difference between the two runs is the
# machine underneath: one uses your CPU's FPU, the other executes every FADD.D and FMUL.D
# through the software IEEE-754 implementation in rvgo/comp.
#
# This is a portability measurement, not a proof of inference. VM self-consistency is the
# protocol-relevant property; comparison with native hardware shows where a guest port or
# its math libraries produce different results.

set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

GUEST=examples/rust/bert-riscv/target/riscv64imafd-unknown-linux-gnu/release/myriscvcandleapp
HOST=examples/rust/bert-host/target/release/bert-host
RESULTS=results/model-determinism.tsv

for bin in "$GUEST" "$HOST"; do
    if [[ ! -x "$bin" ]]; then
        echo "error: $bin not built" >&2
        echo "hint:  see examples/rust/README.md" >&2
        exit 1
    fi
done
ensure_mlvm
mkdir -p results

cleanup() { rm -f config.json tokenizer.json model.safetensors; }
trap cleanup EXIT

WANT=("$@")
if [[ ${#WANT[@]} -eq 0 ]]; then
    WANT=()
    for d in .models/*/; do
        [[ -s "$d/model.safetensors" ]] && WANT+=("$(basename "$d")")
    done
fi
[[ ${#WANT[@]} -eq 0 ]] && { echo "no checkpoints in .models/ (run ./scripts/model-bench.sh --fetch)" >&2; exit 1; }

printf 'model\tvm_score\thost_score\tidentical\tabs_diff\trel_ulp\n' > "$RESULTS"

same=0; differ=0
for name in "${WANT[@]}"; do
    dir=".models/$name"
    echo
    echo "=== $name ==============================================="

    cleanup
    ln -s "$dir/config.json" config.json
    ln -s "$dir/tokenizer.json" tokenizer.json
    ln -s "$dir/model.safetensors" model.safetensors

    # Capture to files first. Piping straight into `grep -m1` makes grep exit on the
    # first match, which SIGPIPEs the producer and, under `set -o pipefail`, aborts the
    # script - with an exit code (141) that looks nothing like the actual cause.
    "$HOST" >"results/$name.host.log" 2>/dev/null || true
    ./scripts/run-elf.sh "$GUEST" config.json tokenizer.json model.safetensors \
        >"results/$name.vm.log" 2>/dev/null || true
    host_score=$(grep -m1 '^score:' "results/$name.host.log" | awk '{print $2}' || true)
    vm_score=$(grep -m1 '^score:' "results/$name.vm.log" | awk '{print $2}' || true)

    if [[ -z "$host_score" || -z "$vm_score" ]]; then
        echo "  !! run failed (host='$host_score' vm='$vm_score')"
        continue
    fi

    read -r ident absdiff ulps <<<"$(python3 -c "
import struct, sys
h, v = float('$host_score'), float('$vm_score')
same = 'yes' if h == v else 'no'
# distance in units-in-the-last-place, computed on the f32 the model actually produced
hb = struct.unpack('<i', struct.pack('<f', h))[0]
vb = struct.unpack('<i', struct.pack('<f', v))[0]
print(same, abs(h - v), abs(hb - vb))")"

    printf '  host (%s FPU)   %s\n' "$(uname -m)" "$host_score"
    printf '  mlvm (softfloat) %s\n' "$vm_score"
    if [[ "$ident" == "yes" ]]; then
        echo "  IDENTICAL"
        same=$((same + 1))
    else
        printf '  differs by %s  (%s ULP in float32)\n' "$absdiff" "$ulps"
        differ=$((differ + 1))
    fi
    printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$name" "$vm_score" "$host_score" "$ident" "$absdiff" "$ulps" >> "$RESULTS"
done

cleanup
echo
echo "======================================================================"
column -t -s$'\t' "$RESULTS" 2>/dev/null || cat "$RESULTS"
echo
echo "identical: $same    differing: $differ"
echo
echo "Any divergence here is not the arithmetic: ISA-level operations are bit-exact (see"
echo "'make determinism'). It comes from FMADD being emulated unfused and from libm"
echo "transcendentals differing between implementations. See"
echo "docs/BENCHMARKS.md#floating-point-determinism."
