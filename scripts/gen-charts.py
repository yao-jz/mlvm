#!/usr/bin/env python3
"""gen-charts.py - turn results/model-bench.tsv into the figures in docs/BENCHMARKS.md.

    ./scripts/model-bench.sh --fetch     # produce the data
    ./scripts/gen-charts.py              # produce the figures

Three figures are written to docs/fig/:

  model-instruction-mix.svg   what fraction of each model's trace is floating point
  model-scaling.svg           steps and memory against model size
  model-throughput.svg        emulation rate, and how it moves with the float fraction

Plain SVG, no dependencies, no external references, with a `prefers-color-scheme`
stylesheet so they stay legible in GitHub's light and dark themes.
"""

import csv
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
DATA = ROOT / "results/model-bench.tsv"
FIG = ROOT / "docs/fig"

STYLE = """
  <style>
    .bg    { fill: #ffffff; }
    .title { fill: #14161a; font: 600 17px ui-sans-serif, -apple-system, "Segoe UI", Roboto, sans-serif; }
    .sub   { fill: #5b6270; font: 400 12px ui-sans-serif, -apple-system, "Segoe UI", Roboto, sans-serif; }
    .lbl   { fill: #14161a; font: 500 13px ui-sans-serif, -apple-system, "Segoe UI", Roboto, sans-serif; }
    .note  { fill: #5b6270; font: 400 11.5px ui-sans-serif, -apple-system, "Segoe UI", Roboto, sans-serif; }
    .axis  { stroke: #c9ced6; stroke-width: 1; }
    .grid  { stroke: #e6e9ee; stroke-width: 1; }
    .box   { stroke: #ffffff; stroke-width: 1.5; }
    .ontop { fill: #ffffff; font: 600 11.5px ui-sans-serif, -apple-system, "Segoe UI", Roboto, sans-serif; }
    @media (prefers-color-scheme: dark) {
      .bg    { fill: #0d1117; }
      .title { fill: #e6edf3; }
      .sub   { fill: #9198a1; }
      .lbl   { fill: #e6edf3; }
      .note  { fill: #9198a1; }
      .axis  { stroke: #30363d; }
      .grid  { stroke: #21262d; }
      .box   { stroke: #0d1117; }
      .ontop { fill: #0d1117; }
    }
  </style>
"""

C_INT = "#3b7dd8"    # integer ALU
C_MEM = "#7d8590"    # integer load/store
C_FALU = "#2ea08c"   # float ALU
C_FLS = "#8b6cc9"    # float load/store
C_OTHER = "#c9ced6"  # branches, jumps, system - counted by neither
C_ACCENT = "#c9743a"


def esc(s):
    return str(s).replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")


def load():
    if not DATA.exists():
        sys.exit(f"no data at {DATA}; run ./scripts/model-bench.sh first")
    rows = []
    with DATA.open() as f:
        for r in csv.DictReader(f, delimiter="\t"):
            try:
                rows.append({
                    "model": r["model"],
                    "params": float(r["params_M"]),
                    "layers": int(r["layers"]),
                    "hidden": int(r["hidden"]),
                    "steps": int(r["steps"]),
                    "arith": int(r["arith"]),
                    "memops": int(r["memops"]),
                    "falu": int(r["float_alu"]),
                    "fls": int(r["float_ls"]),
                    "pages": int(r["pages"]),
                    "ckpt": int(r["ckpt_bytes"]),
                    "secs": float(r["seconds"]),
                    "score": r["score"],
                })
            except (ValueError, KeyError):
                continue
    if not rows:
        sys.exit(f"{DATA} has no usable rows")
    rows.sort(key=lambda r: r["params"])
    return rows


def svg_open(w, h, title, sub, label):
    return [
        f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {w} {h}" width="{w}" '
        f'height="{h}" role="img" aria-label="{esc(label)}">',
        STYLE,
        f'<rect class="bg" width="{w}" height="{h}"/>',
        f'<text class="title" x="40" y="40">{esc(title)}</text>',
        f'<text class="sub" x="40" y="61">{esc(sub)}</text>',
    ]


def instruction_mix(rows):
    """Stacked bars: what fraction of each model's trace is floating point.

    'other' is the remainder - branches, jumps and system instructions execute but the
    profiler does not count them, so showing it explicitly keeps the bars honest.
    """
    w = 900
    h = 130 + len(rows) * 42
    x0, x1 = 150, w - 150
    parts = svg_open(w, h, "Where the instructions go",
                     "Instruction mix per model, measured inside MLVM. Bars are 100% of the "
                     "executed trace.",
                     "instruction mix per model")
    y = 92
    for r in rows:
        total = r["steps"]
        other = total - r["arith"] - r["memops"] - r["falu"] - r["fls"]
        segs = [(r["arith"], C_INT), (r["memops"], C_MEM),
                (r["falu"], C_FALU), (r["fls"], C_FLS), (max(other, 0), C_OTHER)]
        parts.append(f'<text class="lbl" x="40" y="{y + 14}">{esc(r["model"])}</text>')
        parts.append(f'<text class="note" x="40" y="{y + 29}">{r["params"]:.0f}M params</text>')
        x = x0
        for n, colour in segs:
            bw = (x1 - x0) * n / total
            if bw <= 0:
                continue
            parts.append(f'<rect class="box" x="{x:.1f}" y="{y}" width="{bw:.1f}" '
                         f'height="26" rx="3" fill="{colour}"/>')
            pct = 100 * n / total
            if bw > 34:
                parts.append(f'<text class="ontop" x="{x + bw / 2:.1f}" y="{y + 18}" '
                             f'text-anchor="middle">{pct:.0f}%</text>')
            x += bw
        fp = 100 * (r["falu"] + r["fls"]) / total
        parts.append(f'<text class="note" x="{x1 + 12}" y="{y + 18}">{fp:.0f}% float</text>')
        y += 42

    lx = 40
    for label, colour in (("integer ALU", C_INT), ("int load/store", C_MEM),
                          ("float ALU", C_FALU), ("float load/store", C_FLS),
                          ("branch/jump/system", C_OTHER)):
        parts.append(f'<rect x="{lx}" y="{h - 38}" width="12" height="12" rx="2" fill="{colour}"/>')
        parts.append(f'<text class="note" x="{lx + 18}" y="{h - 28}">{label}</text>')
        lx += 30 + len(label) * 6.2
    parts.append("</svg>")
    return "\n".join(parts)


def scaling(rows):
    """Steps and emulated memory per model.

    Categorical rather than plotted against parameter count: several of these models have
    near-identical sizes (33.4M twice, ~110M twice), so a numeric x-axis stacks their
    labels on top of each other and hides the very comparison the chart is for.
    """
    w = 900
    h = 210 + len(rows) * 46
    x0, x1 = 190, w - 210
    parts = svg_open(w, h, "Cost scales with the model, not with the VM",
                     "Emulated instructions per full inference. Memory is dominated by the "
                     "checkpoint staged into the address space.",
                     "steps and memory per model")
    peak = max(r["steps"] for r in rows) * 1.16
    y = 100
    for r in rows:
        bw = (x1 - x0) * r["steps"] / peak
        mib = r["pages"] * 4096 / 1048576
        parts.append(f'<text class="lbl" x="40" y="{y + 16}">{esc(r["model"])}</text>')
        parts.append(f'<text class="note" x="40" y="{y + 31}">'
                     f'{r["params"]:.0f}M / {r["layers"]}L / h{r["hidden"]}</text>')
        parts.append(f'<rect class="box" x="{x0}" y="{y}" width="{bw:.1f}" height="28" '
                     f'rx="3" fill="{C_FALU}"/>')
        parts.append(f'<text class="note" x="{x0 + bw + 12:.1f}" y="{y + 19}">'
                     f'{r["steps"] / 1e9:.2f}B steps</text>')
        # memory as a thin under-bar on the same scale as the largest footprint
        max_mib = max(rr["pages"] * 4096 / 1048576 for rr in rows)
        mw = (x1 - x0) * mib / max_mib * 0.999
        parts.append(f'<rect x="{x0}" y="{y + 30}" width="{mw:.1f}" height="6" rx="2" '
                     f'fill="{C_ACCENT}" opacity="0.75"/>')
        parts.append(f'<text class="note" x="{x0 + mw + 12:.1f}" y="{y + 37}" '
                     f'opacity="0.8">{mib:.0f} MiB</text>')
        y += 46

    lx = 40
    for label, colour in (("emulated steps", C_FALU), ("memory touched", C_ACCENT)):
        parts.append(f'<rect x="{lx}" y="{h - 62}" width="12" height="12" rx="2" fill="{colour}"/>')
        parts.append(f'<text class="note" x="{lx + 18}" y="{h - 52}">{label}</text>')
        lx += 30 + len(label) * 6.4
    parts.append(f'<text class="note" x="40" y="{h - 28}">'
                 f'A proof for one instruction commits to all of that memory with a single '
                 f'32-byte root, whatever its size.</text>')
    parts.append("</svg>")
    return "\n".join(parts)


def throughput(rows):
    """Emulation rate against the float fraction of the trace."""
    w, h = 900, 152 + len(rows) * 40
    x0, x1 = 200, w - 210
    parts = svg_open(w, h, "Emulation rate holds as float intensity rises",
                     "Millions of emulated instructions per second, against the share of the "
                     "trace executed by the software FPU.",
                     "throughput per model")
    rates = [(r, r["steps"] / r["secs"] / 1e6) for r in rows]
    peak = max(v for _, v in rates) * 1.25
    y = 100
    for r, rate in rates:
        fp = 100 * (r["falu"] + r["fls"]) / r["steps"]
        bw = (x1 - x0) * rate / peak
        parts.append(f'<text class="lbl" x="40" y="{y + 15}">{esc(r["model"])}</text>')
        parts.append(f'<text class="note" x="40" y="{y + 30}">{fp:.0f}% float, '
                     f'{r["secs"]:.0f}s</text>')
        parts.append(f'<rect class="box" x="{x0}" y="{y}" width="{bw:.1f}" height="24" '
                     f'rx="3" fill="{C_INT}"/>')
        parts.append(f'<text class="note" x="{x0 + bw + 12:.1f}" y="{y + 17}">'
                     f'{rate:.1f} M instr/s</text>')
        y += 40
    parts.append(f'<text class="note" x="40" y="{h - 36}">'
                 f'A workload that is over half floating point runs at a comparable rate to one '
                 f'with none, because a software</text>')
    parts.append(f'<text class="note" x="40" y="{h - 20}">'
                 f'FADD.D costs little more than the fetch, decode and dispatch every instruction '
                 f'already pays.</text>')
    parts.append("</svg>")
    return "\n".join(parts)


def main():
    rows = load()
    FIG.mkdir(parents=True, exist_ok=True)
    for name, svg in (("model-instruction-mix.svg", instruction_mix(rows)),
                      ("model-scaling.svg", scaling(rows)),
                      ("model-throughput.svg", throughput(rows))):
        (FIG / name).write_text(svg)
        print(f"wrote docs/fig/{name}  ({len(svg):,} bytes)")

    print()
    print("| Model | Params | Layers | Hidden | Steps | Float % | Memory | Wall clock | Score |")
    print("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |")
    for r in rows:
        fp = 100 * (r["falu"] + r["fls"]) / r["steps"]
        print(f"| `{r['model']}` | {r['params']:.1f}M | {r['layers']} | {r['hidden']} | "
              f"{r['steps'] / 1e6:,.0f}M | {fp:.1f}% | "
              f"{r['pages'] * 4096 / 1048576:.0f} MiB | {r['secs']:.0f}s | `{r['score']}` |")
    return 0


if __name__ == "__main__":
    sys.exit(main())
