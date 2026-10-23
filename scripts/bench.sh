#!/usr/bin/env bash
#
# bench.sh - measure MLVM and print the tables that go in docs/BENCHMARKS.md.
#
#   ./scripts/bench.sh              run everything
#   ./scripts/bench.sh --markdown   emit markdown tables only
#
# Three numbers characterize the execution and single-step proof components:
#
#   emulation throughput   how fast a host can replay the trace
#   proving throughput     how fast it can produce a selected step's witness
#   witness size           the raw state-and-branch payload
#
# It also records the instruction mix per program, which is what tells you whether a
# workload is float-bound (and therefore paying the SoftFloat tax) or integer-bound.

set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

MARKDOWN=0
[[ "${1:-}" == "--markdown" ]] && MARKDOWN=1

if [[ ! -d examples/build ]]; then
    echo "==> building examples first"
    make -s -C examples all
fi

echo "==> host"
if [[ "$(uname -s)" == "Darwin" ]]; then
    CPU="$(sysctl -n machdep.cpu.brand_string 2>/dev/null || uname -m)"
    CORES="$(sysctl -n hw.ncpu 2>/dev/null || getconf _NPROCESSORS_ONLN 2>/dev/null || echo unknown)"
    mem_bytes="$(sysctl -n hw.memsize 2>/dev/null || true)"
    if [[ "$mem_bytes" =~ ^[0-9]+$ ]]; then
        MEM="$((mem_bytes / 1024 / 1024 / 1024)) GiB"
    else
        MEM="unknown (sysctl unavailable)"
    fi
else
    CPU="$(grep -m1 'model name' /proc/cpuinfo | cut -d: -f2- | sed 's/^ *//' || echo unknown)"
    CORES="$(nproc)"
    MEM="$(( $(grep MemTotal /proc/meminfo | awk '{print $2}') / 1024 / 1024 )) GiB"
fi
echo "    cpu      $CPU"
echo "    cores    $CORES"
echo "    memory   $MEM"
echo "    go       $(go version | awk '{print $3}')"
echo "    os       $(uname -sr)"

echo
echo "==> per-program run (steps, memory, instruction mix)"
RESULTS="$(go test ./rvgo/test/ -run 'TestBenchmarkPrograms' -count=1 -v 2>/dev/null \
    | grep 'BENCHRESULT' | sed 's/.*BENCHRESULT\t//')"

if [[ $MARKDOWN -eq 1 ]]; then
    echo
    echo "| Program | Steps | Pages | Integer ALU | Memory | Float | Result |"
    echo "| --- | ---: | ---: | ---: | ---: | ---: | --- |"
    while IFS=$'\t' read -r name steps pages arith mem flt status; do
        printf "| \`%s\` | %'d | %'d | %'d | %'d | %'d | %s |\n" \
            "$name" "$steps" "$pages" "$arith" "$mem" "$flt" "$status"
    done <<< "$RESULTS"
else
    printf "    %-14s %12s %8s %12s %12s %10s  %s\n" \
        PROGRAM STEPS PAGES INT-ALU MEM-OPS FLOAT RESULT
    while IFS=$'\t' read -r name steps pages arith mem flt status; do
        printf "    %-14s %12s %8s %12s %12s %10s  %s\n" \
            "$name" "$steps" "$pages" "$arith" "$mem" "$flt" "$status"
    done <<< "$RESULTS"
fi

echo
echo "==> emulation throughput (whole program per iteration)"
(go test ./rvgo/test/ -run '^$' -bench 'BenchmarkFastStep' \
    -benchtime "${FAST_ITERS:-5}x" -count=1 2>/dev/null) | grep -E '^Benchmark' | sed 's/^/    /'

echo
echo "==> proving throughput and merkleization (per step, warm cache)"
(go test ./rvgo/test/ -run '^$' -bench 'BenchmarkProvingStep|BenchmarkMerkleRoot' \
    -benchtime "${PROOF_ITERS:-2000}x" -count=1 2>/dev/null) | grep -E '^Benchmark' | sed 's/^/    /'

echo
echo "==> witness size for one proven step"
ensure_mlvm
"$MLVM_BIN" load-elf --path examples/build/hello \
    --out /tmp/mlvm-bench-state.json --meta /tmp/mlvm-bench-meta.json \
    --random-file "$RANDOM_FILE" >/dev/null 2>&1
mkdir -p /tmp/mlvm-bench-proofs
"$MLVM_BIN" run --input /tmp/mlvm-bench-state.json \
    --output /tmp/mlvm-bench-out.json --meta /tmp/mlvm-bench-meta.json \
    --proof-at '=100000' --proof-fmt '/tmp/mlvm-bench-proofs/proof-%d.json' \
    --stop-at '=100001' >/dev/null 2>&1
python3 - <<'PY'
import json, glob
files = glob.glob('/tmp/mlvm-bench-proofs/proof-*.json')
if not files:
    print("    (no proof produced)")
else:
    d = json.load(open(files[0]))
    state = (len(d['state-data']) - 2) // 2
    proof = (len(d['proof-data']) - 2) // 2
    print(f"    state witness   {state} bytes  (memroot 32 + preimage 40 + core 34 + int regs 256 + float regs 256 + random cursor 16 + fcsr 8 + file-system root 32)")
    print(f"    memory proof    {proof} bytes  ({proof // 32} keccak nodes = {proof // 32 // 60} merkle branch(es) x 60 levels)")
    print(f"    raw payload     {state + proof} bytes for this selected step")
PY
rm -rf /tmp/mlvm-bench-proofs /tmp/mlvm-bench-*.json

echo
echo "==> instruction coverage"
"$REPO_ROOT/scripts/isa-coverage.py" | sed -n '3,20p' | sed 's/^/    /'
