#!/usr/bin/env bash
#
# model-bench.sh - run real ML models inside MLVM and record what they cost.
#
#   ./scripts/model-bench.sh --fetch         download the checkpoints (hundreds of MB)
#   ./scripts/model-bench.sh                 run every model in .models/
#   ./scripts/model-bench.sh minilm          run just one
#
# For each model this records steps, instruction mix, memory footprint, wall clock and the
# similarity score the model produced, into results/model-bench.tsv - which
# scripts/gen-charts.py turns into the figures in docs/BENCHMARKS.md.
#
# Checkpoints live in .models/<name>/{config.json,tokenizer.json,model.safetensors} and are
# gitignored.
#
# Staging note: MLVM's guest argv is a stub, so the guest cannot be told where its files
# are - it opens the literal paths `config.json`, `tokenizer.json` and `model.safetensors`.
# Files are staged under exactly the name the guest opens, so this script symlinks the
# selected checkpoint to those three names at the repo root for the duration of the run.
# Symlinks rather than copies: bert-base's checkpoint is 420 MiB.

set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

GUEST=examples/rust/bert-riscv/target/riscv64imafd-unknown-linux-gnu/release/myriscvcandleapp
MODELS_DIR=.models
RESULTS=results/model-bench.tsv

# A deliberate spread of BERT-architecture encoders: same code path, four model sizes,
# so the cost curve is about the model rather than about the guest program.
MODEL_NAMES=(minilm-l6 bge-small minilm-l12 bge-base bert-base)
MODEL_REPOS=(
    "sentence-transformers/all-MiniLM-L6-v2"        # 22.7M   6 layers, hidden 384
    "BAAI/bge-small-en-v1.5"                        # 33.4M  12 layers, hidden 384
    "sentence-transformers/all-MiniLM-L12-v2"       # 33.4M  12 layers, hidden 384
    "BAAI/bge-base-en-v1.5"                         # 109M   12 layers, hidden 768
    "google-bert/bert-base-uncased"                 # 110M   12 layers, hidden 768 (bert. prefix)
)

fetch() {
    for i in "${!MODEL_NAMES[@]}"; do
        name=${MODEL_NAMES[$i]}; repo=${MODEL_REPOS[$i]}
        mkdir -p "$MODELS_DIR/$name"
        echo "==> $name  ($repo)"
        for f in config.json tokenizer.json model.safetensors; do
            if [[ -s "$MODELS_DIR/$name/$f" ]]; then
                printf "    %-20s cached\n" "$f"
                continue
            fi
            curl -sL --fail -o "$MODELS_DIR/$name/$f" \
                "https://huggingface.co/$repo/resolve/main/$f"
            printf "    %-20s %s bytes\n" "$f" "$(wc -c <"$MODELS_DIR/$name/$f" | tr -d ' ')"
        done
    done
}

if [[ "${1:-}" == "--fetch" ]]; then fetch; shift; fi

if [[ ! -x "$GUEST" ]]; then
    echo "error: the BERT guest is not built." >&2
    echo "hint:  cd examples/rust/bert-riscv && ./build.sh" >&2
    echo "       see examples/rust/README.md for the toolchain requirements" >&2
    exit 1
fi
ensure_mlvm
mkdir -p results

cleanup() { rm -f config.json tokenizer.json model.safetensors; }
trap cleanup EXIT

WANT=("$@")
[[ ${#WANT[@]} -eq 0 ]] && WANT=("${MODEL_NAMES[@]}")

# Results accumulate across invocations, so that running one model does not discard the
# others. A row is replaced if that model is re-run.
HEADER='model\tparams_M\tlayers\thidden\tsteps\tarith\tmemops\tfloat_alu\tfloat_ls\tpages\tckpt_bytes\tseconds\tscore'
[[ -s "$RESULTS" ]] || printf '%b\n' "$HEADER" > "$RESULTS"
for name in "${WANT[@]}"; do
    grep -v "^$name	" "$RESULTS" > "$RESULTS.tmp" 2>/dev/null || true
    mv "$RESULTS.tmp" "$RESULTS"
done

for name in "${WANT[@]}"; do
    dir="$MODELS_DIR/$name"
    if [[ ! -s "$dir/model.safetensors" ]]; then
        echo "!! $name: no checkpoint in $dir (run with --fetch)" >&2
        continue
    fi

    echo
    echo "======================================================================"
    echo " $name"
    echo "======================================================================"

    read -r layers hidden heads <<<"$(python3 -c "
import json
c = json.load(open('$dir/config.json'))
print(c.get('num_hidden_layers','?'), c.get('hidden_size','?'), c.get('num_attention_heads','?'))")"
    bytes=$(wc -c <"$dir/model.safetensors" | tr -d ' ')
    params=$(python3 -c "print(f'{$bytes/4/1e6:.1f}')")
    printf "  %s layers, hidden %s, %s heads, %s MiB checkpoint, ~%sM params (fp32)\n" \
        "$layers" "$hidden" "$heads" "$(python3 -c "print(f'{$bytes/1048576:.0f}')")" "$params"

    cleanup
    ln -s "$dir/config.json"        config.json
    ln -s "$dir/tokenizer.json"     tokenizer.json
    ln -s "$dir/model.safetensors"  model.safetensors

    log=results/$name.log
    start=$(python3 -c 'import time;print(time.time())')
    ./scripts/run-elf.sh "$GUEST" config.json tokenizer.json model.safetensors \
        >"$log" 2>&1 || true
    end=$(python3 -c 'import time;print(time.time())')
    secs=$(python3 -c "print(f'{$end-$start:.1f}')")

    field() { grep -m1 "$1" "$log" | awk "{print \$$2}" | tr -d ','; }
    steps=$(field '^  steps' 2)
    arith=$(field 'integer ALU' 3)
    memops=$(field 'memory load/store' 3)
    falu=$(field 'float ALU' 3)
    fls=$(field 'float load/store' 3)
    pages=$(field 'pages touched' 3)
    score=$(grep -m1 '^score:' "$log" | awk '{print $2}')

    if [[ -z "${steps:-}" ]]; then
        echo "  !! run failed; last lines of $log:" >&2
        tail -5 "$log" | sed 's/^/     /' >&2
        continue
    fi

    printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
        "$name" "$params" "$layers" "$hidden" \
        "$steps" "$arith" "$memops" "$falu" "$fls" "$pages" "$bytes" "$secs" \
        "${score:-n/a}" >> "$RESULTS"

    grep -E '^  (steps|integer ALU|memory load|float ALU|float load|pages touched|wall clock|throughput)' "$log" \
        | sed 's/^/  /' || true
    echo "  score            $score"
done

cleanup
echo
echo "======================================================================"
command -v column >/dev/null && column -t -s$'\t' "$RESULTS" || cat "$RESULTS"
echo
echo "written to $RESULTS"
