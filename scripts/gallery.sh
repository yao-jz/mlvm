#!/usr/bin/env bash
#
# gallery.sh - X-ray and Catch the Liar on every kind of guest MLVM runs: C programs of a
# few hundred thousand instructions, the same model in C and in Candle, a GEMM with and
# without float instructions, and Candle BERT models of billions.
#
#   ./scripts/gallery.sh                 everything but SmolLM2 and the four larger BERTs
#   ./scripts/gallery.sh --all           also those (slow: about two hours of CPU, 10 GB)
#   ./scripts/gallery.sh decoder mlp     just these entries
#
# Writes flame graphs to docs/gallery/<entry>.svg, every report as JSON to
# results/gallery/, and one row per run to results/gallery.tsv, rebuilt from the JSON
# after each run. docs/GALLERY.md quotes those rows. Time is CPU seconds, user plus sys:
# wall clock depends on what else the machine is doing, and on whether it naps.
#
# Entries other than decoder and mlp need guests built outside Go: the C examples
# (`make examples`), the Rust crates in examples/rust (see its README), and for BERT the
# checkpoints in .models/ (`./scripts/model-bench.sh --fetch`). Entries whose guest or
# inputs are missing are skipped with a note.

set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

BERT=examples/rust/bert-riscv/target/riscv64imafd-unknown-linux-gnu/release/myriscvcandleapp
CANDLE_TS=examples/rust/tinystories-candle/target/riscv64imafd-unknown-linux-gnu/release/tinystories-candle
LLM=examples/rust/llm-candle/target/riscv64imafd-unknown-linux-gnu/release/llm-candle
GEMM=examples/rust/float-placement/target/%s-unknown-linux-gnu/release/float-placement
SVG_DIR=docs/gallery
OUT=results/gallery
TSV=results/gallery.tsv

DEFAULT=(decoder tinystories-candle mlp matmul ieee gemm-float gemm-softfloat minilm-l6)
ALL=("${DEFAULT[@]}" smollm2 minilm-l12 bge-small bge-base bert-base)

want=()
for arg in "$@"; do
    case "$arg" in
        --all) want=("${ALL[@]}") ;;
        *) want+=("$arg") ;;
    esac
done
[[ ${#want[@]} -eq 0 ]] && want=("${DEFAULT[@]}")

ensure_mlvm
mkdir -p "$SVG_DIR" "$OUT"

# Peak resident memory of a command goes in a .time file next to its JSON report, from
# /usr/bin/time (BSD -l or GNU -v).
timed() {
    local log="$1"; shift
    if /usr/bin/time -l true >/dev/null 2>&1; then
        /usr/bin/time -l "$@" 2> "$log"
    else
        /usr/bin/time -v "$@" 2> "$log"
    fi
}

# tabulate rebuilds the TSV from every report in $OUT, so entries run in separate
# invocations, even at the same time, all end up in it.
tabulate() {
    python3 - "$OUT" "$TSV" "${ALL[@]}" <<'PY'
import json, os, re, sys
out, tsv, order = sys.argv[1], sys.argv[2], sys.argv[3:]
runs = ["xray", "liar-ulp", "liar-load", "liar-int", "liar-output", "liar-commit", "liar-token"]

def usage(path):
    """CPU seconds (user + sys) and peak RSS in MB. CPU time rather than wall clock: a
    laptop that sleeps or throttles mid-run stretches the one and not the other."""
    try:
        text = open(path).read()
    except OSError:
        return "", ""
    m = re.search(r"([\d.]+) real\s+([\d.]+) user\s+([\d.]+) sys", text)  # BSD
    cpu = round(float(m.group(2)) + float(m.group(3)), 1) if m else ""
    m = re.search(r"(\d+)\s+maximum resident set size", text)  # BSD: bytes
    if m:
        return cpu, int(m.group(1)) >> 20
    u = re.search(r"User time \(seconds\): ([\d.]+)", text)  # GNU
    k = re.search(r"System time \(seconds\): ([\d.]+)", text)
    if u and k:
        cpu = round(float(u.group(1)) + float(k.group(1)), 1)
    m = re.search(r"Maximum resident set size \(kbytes\): (\d+)", text)  # GNU: KiB
    return cpu, (int(m.group(1)) >> 10 if m else "")

rows = []
for name in os.listdir(out):
    if not name.endswith(".json"):
        continue
    stem = name[:-5]
    entry, run = (stem[:-5], "xray") if stem.endswith("-xray") else stem.rsplit("-liar-", 1)
    if run != "xray":
        run = "liar-" + run
    try:
        r = json.load(open(os.path.join(out, name)))
    except ValueError:
        continue  # still being written
    cpu, rss = usage(os.path.join(out, stem + ".time"))
    if run == "xray":
        row = [entry, run, r["steps"], round(100 * r["floatShare"], 1), "", "", "", "", "", cpu, rss]
    elif r.get("noDispute"):
        # The lie left no trace in the final state: both machines halted in the same state.
        row = [entry, run, max(r["honestSteps"], r["liarSteps"]), "", "",
               "no dispute (same poststate)", "", "", "", cpu, rss]
    else:
        v = r.get("verdict") or {}
        evm = (v.get("evm") or {}).get("gasUsed", "")
        judge = "RISCV.sol (EVM) + slow.Step" if v.get("evm") else "SoftFloat re-execution"
        row = [entry, run, max(r["honestSteps"], r["liarSteps"]), "", len(r["rounds"]),
               v.get("instruction", ""), v.get("function", ""), judge, evm, cpu, rss]
    rows.append(row)
rank = lambda r: (order.index(r[0]) if r[0] in order else len(order), runs.index(r[1]) if r[1] in runs else 99)
rows.sort(key=rank)
header = "entry run instructions float_pct rounds disputed function checked_by evm_gas cpu_seconds peak_rss_mb".split()
tmp = tsv + ".tmp.%d" % os.getpid()
with open(tmp, "w") as f:
    for row in [header] + rows:
        f.write("\t".join(str(x) for x in row) + "\n")
os.replace(tmp, tsv)
PY
}

# xray ENTRY TITLE ARGS...: profile, write the flame graph and the JSON report.
xray() {
    local entry="$1" title="$2"; shift 2
    echo "==> $entry: X-ray"
    timed "$OUT/$entry-xray.time" "$MLVM_BIN" xray "$@" --label "$title" \
        --svg "$SVG_DIR/$entry.svg" --json --min-share 0.002 > "$OUT/$entry-xray.json.tmp"
    mv "$OUT/$entry-xray.json.tmp" "$OUT/$entry-xray.json"
    tabulate
}

# liar ENTRY RUN ARGS...: one Catch the Liar dispute.
liar() {
    local entry="$1" run="$2"; shift 2
    echo "==> $entry: Catch the Liar ($run)"
    timed "$OUT/$entry-liar-$run.time" "$MLVM_BIN" liar "$@" --json > "$OUT/$entry-liar-$run.json.tmp"
    mv "$OUT/$entry-liar-$run.json.tmp" "$OUT/$entry-liar-$run.json"
    tabulate
}

# need FILE WHAT: skip the entry (return 1) with a note when FILE is missing.
need() {
    [[ -e "$1" ]] && return 0
    echo "    skipped: no $1 ($2)" >&2
    return 1
}

# The Hugging Face name of a checkpoint in .models/.
checkpoint() {
    case "$1" in
        minilm-l6) echo all-MiniLM-L6-v2 ;;
        minilm-l12) echo all-MiniLM-L12-v2 ;;
        bge-small) echo bge-small-en-v1.5 ;;
        bge-base) echo bge-base-en-v1.5 ;;
        bert-base) echo bert-base-uncased ;;
    esac
}

# Arguments that run the Candle guest on one checkpoint.
candle() {
    local dir=".models/$1"
    if [[ ! -x "$BERT" ]]; then
        echo "    skipped: the Candle guest is not built (examples/rust/README.md)" >&2
        return 1
    fi
    if [[ ! -s "$dir/model.safetensors" ]]; then
        echo "    skipped: no checkpoint in $dir (./scripts/model-bench.sh --fetch)" >&2
        return 1
    fi
    # The same randomness blob as scripts/run-elf.sh, so step counts match docs/EXAMPLES.md:
    # getrandom(2) seeds Rust's hash maps, and different seeds take different paths.
    CANDLE_ARGS=(--elf "$BERT" --random-file "$RANDOM_FILE"
        --load-file "config.json=$dir/config.json"
        --load-file "tokenizer.json=$dir/tokenizer.json"
        --load-file "model.safetensors=$dir/model.safetensors")
}

for entry in "${want[@]}"; do
    case "$entry" in
        decoder)
            xray decoder "TinyStories-260K decoder (C, llama2.c-style), 16 tokens"
            liar decoder ulp
            liar decoder token --lie token
            ;;
        tinystories-candle)
            echo "==> $entry"
            need "$CANDLE_TS" "examples/rust/tinystories-candle/build.sh" || continue
            request="$(mktemp)"
            python3 examples/decoder/mkrequest.py "Once upon a time" -o "$request" >/dev/null
            args=(--elf "$CANDLE_TS" --load-file model.bin=models/stories260K.bin
                --load-file tokenizer.bin=models/tok512.bin --load-file "request.bin=$request")
            title="TinyStories-260K through Candle (Rust), 16 tokens"
            xray "$entry" "$title" "${args[@]}"
            liar "$entry" ulp "${args[@]}" --label "$title"
            liar "$entry" token "${args[@]}" --label "$title" --lie token
            rm -f "$request"
            ;;
        smollm2)
            echo "==> $entry"
            need "$LLM" "examples/rust/llm-candle/build.sh" || continue
            m=.models/smollm2-135m
            need "$m/model.safetensors" "examples/rust/llm-candle/README.md says how to fetch it" || continue
            request="$(mktemp)"
            python3 examples/decoder/mkrequest.py "Once upon a time" -o "$request" >/dev/null
            args=(--elf "$LLM" --random-file "$RANDOM_FILE" --load-file "config.json=$m/config.json"
                --load-file "tokenizer.json=$m/tokenizer.json" --load-file "model.safetensors=$m/model.safetensors"
                --load-file "request.bin=$request")
            title="SmolLM2-135M through Candle (Rust), 16 tokens"
            xray "$entry" "$title" "${args[@]}"
            liar "$entry" ulp "${args[@]}" --label "$title"
            # Its 8th token is " Lily" (ID 14176): commit " Tim" (6362) instead.
            liar "$entry" token "${args[@]}" --label "$title" --lie token --token-index 7 --token-id 6362
            rm -f "$request"
            ;;
        mlp)
            xray mlp "Embedded MLP (C, f64)" --demo mlp
            liar mlp ulp --demo mlp
            liar mlp load --demo mlp --lie load
            liar mlp int --demo mlp --lie int --function dense --at 0
            liar mlp output --demo mlp --lie output --forge "67.66=97.66"
            # The committed answer is four f64 probabilities and the argmax: make it 3.
            liar mlp commit --demo mlp --lie commit --forge 0x0000000000000000=0x0300000000000000
            ;;
        matmul)
            echo "==> $entry"
            need examples/build/matmul "make examples" || continue
            xray matmul "32x32 matrix multiply (C, f64)" --elf examples/build/matmul
            # Its float work is the multiply, in the first half; the rest is printf.
            liar matmul ulp --elf examples/build/matmul --label matmul --at 10%
            liar matmul load --elf examples/build/matmul --label matmul --lie load --at 10%
            ;;
        ieee)
            echo "==> $entry"
            need examples/build/ieee "make examples" || continue
            xray ieee "IEEE-754 regression (C, f64)" --elf examples/build/ieee
            liar ieee ulp --elf examples/build/ieee --label ieee --mnemonic fsqrt.d --at 0
            ;;
        gemm-float | gemm-softfloat)
            # One no_std GEMM, built with float instructions and without them.
            echo "==> $entry"
            target=riscv64imafd title="GEMM, float instructions (Rust no_std)"
            [[ "$entry" == gemm-softfloat ]] && target=riscv64ima title="GEMM, compiler soft-float (Rust no_std)"
            # shellcheck disable=SC2059
            elf="$(printf "$GEMM" "$target")"
            need "$elf" "./scripts/float-placement.sh" || continue
            xray "$entry" "$title" --elf "$elf"
            if [[ "$entry" == gemm-float ]]; then
                liar "$entry" ulp --elf "$elf" --label "$title" --mnemonic fmul.d
            else
                liar "$entry" int --elf "$elf" --label "$title" --lie int --function __muldf3 --mnemonic mulhu
            fi
            ;;
        minilm-l6 | minilm-l12 | bge-small | bge-base | bert-base)
            echo "==> $entry"
            candle "$entry" || continue
            title="Candle BERT (Rust), $(checkpoint "$entry")"
            xray "$entry" "$title" "${CANDLE_ARGS[@]}"
            liar "$entry" ulp "${CANDLE_ARGS[@]}" --label "$title"
            if [[ "$entry" == minilm-l6 ]]; then
                liar "$entry" load "${CANDLE_ARGS[@]}" --label "$title" --lie load
                # The same forged score, printed and committed: only the second is disputable.
                liar "$entry" output "${CANDLE_ARGS[@]}" --label "$title" --lie output --forge "score: 0.61=score: 0.91"
                liar "$entry" commit "${CANDLE_ARGS[@]}" --label "$title" --lie commit --forge "score: 0.61=score: 0.91"
            fi
            ;;
        *)
            echo "error: unknown entry $entry (want one of: ${ALL[*]})" >&2
            exit 1
            ;;
    esac
done

echo
column -t -s $'\t' "$TSV"
