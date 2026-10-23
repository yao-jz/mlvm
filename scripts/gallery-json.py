#!/usr/bin/env python3
"""Summarize the gallery reports for the playground's "Beyond the toy model" section.

    scripts/gallery-json.py OUT_DIR

Reads results/gallery/*.json (written by scripts/gallery.sh) and results/gallery.tsv, and
writes OUT_DIR/gallery.json, a copy of each flame graph in OUT_DIR/gallery/, and a copy of
each dispute record in OUT_DIR/disputes/ for the page to replay and referee. Entries
without reports are left out, so a partial gallery still renders.
"""

import csv
import json
import shutil
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
REPORTS = ROOT / "results" / "gallery"
TSV = ROOT / "results" / "gallery.tsv"
FLAMES = ROOT / "docs" / "gallery"

# Order, title and description of each entry gallery.sh knows. The page shows flame graphs
# for FEATURED: the same model in C and in Candle, the Hugging Face LLM, the soft-float GEMM,
# and the smallest and largest BERT.
FEATURED = {"decoder", "tinystories-candle", "smollm2", "gemm-softfloat", "minilm-l6", "bert-base"}
ENTRIES = [
    ("decoder", "TinyStories-260K", "C, llama2.c-style decoder, f32, 16 tokens"),
    ("tinystories-candle", "TinyStories-260K through Candle", "Rust, the same model, tokens and answer layout as the C decoder"),
    ("mlp", "MLP", "C, f64, the embedded example"),
    ("matmul", "matmul", "C, f64, a 32x32 matrix multiply"),
    ("ieee", "ieee", "C, f64, an IEEE-754 rounding regression"),
    ("gemm-float", "GEMM with float instructions", "Rust no_std, rv64imafd"),
    ("gemm-softfloat", "GEMM in soft-float", "Rust no_std, rv64ima: floats compiled to integer code"),
    ("smollm2", "SmolLM2-135M", "Rust, Candle Llama: a Hugging Face LLM, 135M parameters"),
    ("minilm-l6", "all-MiniLM-L6-v2", "Rust, Candle BERT, 22.7M parameters"),
    ("minilm-l12", "all-MiniLM-L12-v2", "Rust, Candle BERT, 33.4M parameters"),
    ("bge-small", "bge-small-en-v1.5", "Rust, Candle BERT, 33.4M parameters"),
    ("bge-base", "bge-base-en-v1.5", "Rust, Candle BERT, 109.5M parameters"),
    ("bert-base", "bert-base-uncased", "Rust, Candle BERT, 110.1M parameters"),
]
LIES = {
    "ulp": "one float result off by 1 ULP",
    "load": "one bit of one loaded value",
    "int": "one bit of one integer result",
    "output": "a forged printed result",
    "commit": "a forged committed answer",
    "token": "a forged answer token",
}


def short_name(name):
    """Cut paths inside <...> to their last segment, as xray.ShortName does:
    <candle_core::cpu_backend::CpuStorage as candle_core::backend::BackendStorage>::affine
    becomes <CpuStorage as BackendStorage>::affine."""
    out, depth, mark, i = [], 0, 0, 0
    while i < len(name):
        c = name[i]
        if c in "<>":
            depth = depth + 1 if c == "<" else max(0, depth - 1)
            out.append(c)
            mark = len(out)
        elif c == ":" and name[i + 1 : i + 2] == ":" and depth > 0:
            del out[mark:]
            i += 1
        elif c.isalnum() or c == "_":
            out.append(c)
        else:
            out.append(c)
            mark = len(out)
        i += 1
    return "".join(out)


def usage():
    rows = {}
    if TSV.exists():
        with TSV.open() as f:
            for r in csv.DictReader(f, delimiter="\t"):
                rows[(r["entry"], r["run"])] = r
    return rows


def number(s):
    try:
        return float(s)
    except (TypeError, ValueError):
        return None


def main():
    if len(sys.argv) != 2:
        sys.exit(__doc__)
    out = Path(sys.argv[1])
    rows = usage()
    entries = []
    for key, title, about in ENTRIES:
        xray_path = REPORTS / f"{key}-xray.json"
        if not xray_path.exists():
            continue
        xray = json.loads(xray_path.read_text())
        entry = {
            "id": key,
            "title": title,
            "about": about,
            "instructions": xray["steps"],
            "floatShare": xray["floatShare"],
            "top": [{"name": short_name(f["name"]), "share": f["share"]} for f in xray["functions"][:3]],
            "cpuSeconds": number(rows.get((key, "xray"), {}).get("cpu_seconds")),
            "featured": key in FEATURED,
            "lies": [],
        }
        flame = FLAMES / f"{key}.svg"
        if flame.exists():
            (out / "gallery").mkdir(parents=True, exist_ok=True)
            shutil.copyfile(flame, out / "gallery" / flame.name)
            entry["flame"] = f"gallery/{flame.name}"
        for kind in LIES:
            path = REPORTS / f"{key}-liar-{kind}.json"
            if not path.exists():
                continue
            r = json.loads(path.read_text())
            v = r.get("verdict") or {}
            row = rows.get((key, f"liar-{kind}"), {})
            record = None
            if v:
                (out / "disputes").mkdir(parents=True, exist_ok=True)
                record = f"disputes/{key}-{kind}.json"
                shutil.copyfile(path, out / record)
            entry["lies"].append({
                "kind": kind,
                "lie": LIES[kind],
                "instructions": max(r["honestSteps"], r["liarSteps"]),
                "rounds": len(r.get("rounds") or []),
                # The lie left no trace in the final state, so there was nothing to dispute.
                "noDispute": bool(r.get("noDispute")),
                "instruction": v.get("instruction", ""),
                "function": short_name(v.get("function", "")),
                "judge": "RISCV.sol" if v.get("evm") else "SoftFloat",
                "gas": (v.get("evm") or {}).get("gasUsed"),
                "tries": len(r.get("attempts") or []) or 1,
                "honest": r.get("honestText", ""),
                "liar": r.get("liarText", ""),
                "cpuSeconds": number(row.get("cpu_seconds")),
                "peakMB": number(row.get("peak_rss_mb")),
                "record": record,
            })
        entries.append(entry)
    out.mkdir(parents=True, exist_ok=True)
    (out / "gallery.json").write_text(json.dumps({"entries": entries}, indent=1) + "\n")
    print(f"gallery.json: {len(entries)} entries", file=sys.stderr)


if __name__ == "__main__":
    main()
