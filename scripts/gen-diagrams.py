#!/usr/bin/env python3
"""gen-diagrams.py - regenerate the SVG figures in fig/.

    ./scripts/gen-diagrams.py

Five figures are generated:

  docs/fig/architecture.svg         the whole system: guest, execution, state, dispute, verdict
  docs/fig/address-space.svg        the 64-bit guest address space and what lives where
  docs/fig/isa-coverage.svg         per-extension instruction coverage, read from the VM source
  docs/fig/overview.svg             the README's story: the problem, then fingerprint, find, check
  docs/fig/bisection-questions.svg  Figure 4 of the write-up: questions per dispute against length

All are plain SVG with no external references, and all carry a `prefers-color-scheme`
stylesheet so they stay legible in GitHub's light and dark themes.

The address-space layout is not invented here: the constants come from
rvgo/fast/elf.go (heap, stack, file and randomness bases) and
rvgo/fast/file.go (the per-file slot size). If you change them there, update
REGIONS below and re-run this script.
"""

import pathlib
import re
import subprocess
import sys
import textwrap

ROOT = pathlib.Path(__file__).resolve().parent.parent
FIG = ROOT / "docs/fig"

# Shared stylesheet. Colours are chosen to sit on either a white or a near-black page:
# mid-tone fills with a light/dark text pair swapped by the media query.
STYLE = """
  <style>
    .bg    { fill: #ffffff; }
    .title { fill: #14161a; font: 600 17px ui-sans-serif, -apple-system, "Segoe UI", Roboto, sans-serif; }
    .sub   { fill: #5b6270; font: 400 12px ui-sans-serif, -apple-system, "Segoe UI", Roboto, sans-serif; }
    .lbl   { fill: #14161a; font: 500 13px ui-sans-serif, -apple-system, "Segoe UI", Roboto, sans-serif; }
    .note  { fill: #5b6270; font: 400 11.5px ui-sans-serif, -apple-system, "Segoe UI", Roboto, sans-serif; }
    .addr  { fill: #5b6270; font: 400 11.5px ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
    .axis  { stroke: #c9ced6; stroke-width: 1; }
    .brk   { stroke: #c9ced6; stroke-width: 1; stroke-dasharray: 3 3; }
    .box   { stroke: #ffffff; stroke-width: 1.5; }
    .ontop { fill: #ffffff; font: 600 12.5px ui-sans-serif, -apple-system, "Segoe UI", Roboto, sans-serif; }
    @media (prefers-color-scheme: dark) {
      .bg    { fill: #0d1117; }
      .title { fill: #e6edf3; }
      .sub   { fill: #9198a1; }
      .lbl   { fill: #e6edf3; }
      .note  { fill: #9198a1; }
      .addr  { fill: #9198a1; }
      .axis  { stroke: #30363d; }
      .brk   { stroke: #30363d; }
      .box   { stroke: #0d1117; }
      .ontop { fill: #0d1117; }
    }
  </style>
"""

# Region colours: distinguishable in both themes and for the common colour-vision
# deficiencies (no red/green pair carrying meaning on its own - the labels do that).
C_CODE = "#3b7dd8"
C_HEAP = "#2ea08c"
C_STACK = "#c9743a"
C_RAND = "#8b6cc9"
C_FILE = "#c04a6e"
C_FREE = "#7d8590"

# (base, label, size-note, colour, detail)
REGIONS = [
    (0x0000000000000000, "ELF image",
     "text, rodata, data, bss", C_CODE,
     "mapped from PT_LOAD segments; entry is typically 0x10000"),
    (0x00007F0000000000, "heap",
     "grows up, mmap serves from here", C_HEAP,
     "brk() reports a 1 GiB break; mmap hands out anonymous pages from this cursor"),
    (0x1000000000000000, "stack",
     "grows down from 2^60", C_STACK,
     "argc/argv/envp/auxv are written here at load time, including AT_RANDOM"),
    (0x1500000000000000, "randomness",
     "5 MiB pre-generated blob", C_RAND,
     "getrandom(2) walks a cursor through it; committed, not sampled from the host"),
    (0x2000000000000000, "staged files",
     "one 64 GiB slot per file", C_FILE,
     "openat/read/lseek are served from here; no host I/O ever happens"),
]

W, H = 900, 560


def esc(s: str) -> str:
    return s.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")


def wrap(text: str, width: int) -> list[str]:
    """Greedy word wrap. SVG has no text flow, so lines are laid out by hand."""
    lines, cur = [], ""
    for word in text.split():
        candidate = f"{cur} {word}".strip()
        if len(candidate) <= width:
            cur = candidate
        else:
            if cur:
                lines.append(cur)
            cur = word
    if cur:
        lines.append(cur)
    return lines


def address_space_svg() -> str:
    """A segmented map of the 64-bit guest address space.

    A linear scale is useless here: the ELF image is a few hundred KiB and the stack
    starts at 2^60. So each region gets an equal band, with break marks making the
    discontinuity explicit rather than hiding it.
    """
    top, bottom = 96, H - 74
    band = (bottom - top) / len(REGIONS)
    x0, x1 = 250, W - 40

    parts = [
        f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {W} {H}" width="{W}" height="{H}" '
        f'role="img" aria-label="MLVM guest address space">',
        STYLE,
        f'<rect class="bg" width="{W}" height="{H}"/>',
        f'<text class="title" x="40" y="42">MLVM guest address space</text>',
        f'<text class="sub" x="40" y="64">64-bit, merkleized as a binary tree of 4 KiB pages. '
        f'Bands are equal-height, not to scale - the gaps between them are enormous.</text>',
    ]

    # Draw from the highest address at the top down to zero, the way a memory map is
    # conventionally read.
    for i, (base, label, size_note, colour, detail) in enumerate(reversed(REGIONS)):
        y = top + i * band
        h = band - 16
        parts.append(
            f'<rect class="box" x="{x0}" y="{y:.1f}" width="{x1 - x0}" height="{h:.1f}" '
            f'rx="5" fill="{colour}"/>'
        )
        parts.append(f'<text class="ontop" x="{x0 + 14}" y="{y + 24:.1f}">{esc(label)}</text>')
        parts.append(
            f'<text class="ontop" x="{x0 + 14}" y="{y + 42:.1f}" opacity="0.85" '
            f'style="font-weight:400">{esc(size_note)}</text>'
        )
        parts.append(f'<text class="addr" x="40" y="{y + 20:.1f}">0x{base:016X}</text>')
        for n, line in enumerate(wrap(detail, 32)[:3]):
            parts.append(f'<text class="note" x="40" y="{y + 38 + n * 15:.1f}">{esc(line)}</text>')
        # Break marks between bands, to signal the elided range.
        if i < len(REGIONS) - 1:
            yb = y + h + 8
            parts.append(f'<line class="brk" x1="{x0}" y1="{yb:.1f}" x2="{x1}" y2="{yb:.1f}"/>')

    parts.append(
        f'<text class="note" x="40" y="{H - 40}">'
        f'Every byte above is covered by the memory root in the state witness. A proof for one '
        f'instruction carries only the 60-node</text>'
    )
    parts.append(
        f'<text class="note" x="40" y="{H - 24}">'
        f'merkle branch to the 32-byte words it touched - never a memory dump.</text>'
    )
    parts.append("</svg>")
    return "\n".join(parts)


def isa_coverage_svg() -> str:
    """Stacked bars of full / partial / missing instructions per extension.

    Numbers come from scripts/isa-coverage.py so the figure cannot drift from the code.
    """
    out = subprocess.run(
        [sys.executable, str(ROOT / "scripts/isa-coverage.py")],
        capture_output=True, text=True, check=True,
    ).stdout

    rows = []
    for line in out.splitlines():
        f = line.split()
        if len(f) == 5 and f[0].startswith(("RV", "Zi")) and f[1].isdigit():
            rows.append((f[0], int(f[1]), int(f[2]), int(f[3]), int(f[4])))
    if not rows:
        sys.exit("could not parse isa-coverage.py output")

    c_full, c_part, c_miss = "#2ea08c", "#d9a441", "#b04a5a"
    w, h = 900, 120 + len(rows) * 34
    x0, x1 = 130, w - 190
    maxtot = max(r[4] for r in rows)

    parts = [
        f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {w} {h}" width="{w}" height="{h}" '
        f'role="img" aria-label="MLVM RISC-V instruction coverage by extension">',
        STYLE,
        f'<rect class="bg" width="{w}" height="{h}"/>',
        '<text class="title" x="40" y="40">RV64IMAFD instruction coverage</text>',
        '<text class="sub" x="40" y="61">Generated from rvgo/fast/vm.go by '
        'scripts/isa-coverage.py. Partial means it decodes and executes, with a documented '
        'semantic gap.</text>',
    ]

    y = 88
    for name, full, part, miss, total in rows:
        scale = (x1 - x0) / maxtot
        x = x0
        parts.append(f'<text class="lbl" x="40" y="{y + 15}">{esc(name)}</text>')
        for n, colour in ((full, c_full), (part, c_part), (miss, c_miss)):
            if n == 0:
                continue
            bw = n * scale
            parts.append(
                f'<rect class="box" x="{x:.1f}" y="{y}" width="{bw:.1f}" height="21" '
                f'rx="3" fill="{colour}"/>'
            )
            if bw > 22:
                parts.append(
                    f'<text class="ontop" x="{x + bw / 2:.1f}" y="{y + 15}" '
                    f'text-anchor="middle">{n}</text>'
                )
            x += bw + 1.5
        parts.append(f'<text class="note" x="{x1 + 14}" y="{y + 15}">{full + part} / {total}</text>')
        y += 34

    lx = 40
    for label, colour in (("full", c_full), ("partial", c_part), ("missing", c_miss)):
        parts.append(f'<rect x="{lx}" y="{h - 36}" width="12" height="12" rx="2" fill="{colour}"/>')
        parts.append(f'<text class="note" x="{lx + 18}" y="{h - 26}">{label}</text>')
        lx += 24 + len(label) * 7
    parts.append("</svg>")
    return "\n".join(parts)


ARCH_STYLE = """
  <style>
    .bg    { fill: #ffffff; }
    .panel { fill: #f6f8fa; stroke: #d0d7de; stroke-width: 1; }
    .card  { fill: #ffffff; stroke: #d0d7de; stroke-width: 1; }
    .void  { fill: none; stroke: #8c959f; stroke-width: 1.2; stroke-dasharray: 5 4; }
    .title { fill: #1f2328; font: 600 19px ui-sans-serif, -apple-system, "Segoe UI", Roboto, sans-serif; }
    .sub   { fill: #59636e; font: 400 13px ui-sans-serif, -apple-system, "Segoe UI", Roboto, sans-serif; }
    .lbl   { fill: #1f2328; font: 500 13.5px ui-sans-serif, -apple-system, "Segoe UI", Roboto, sans-serif; }
    .hdr   { fill: #59636e; font: 700 12px ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; letter-spacing: 0.06em; }
    .m     { fill: #1f2328; font: 400 12px ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
    .mm    { fill: #59636e; font: 400 12px ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
    .ms    { fill: #59636e; font: 400 11px ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
    .tag   { font: 700 12px ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
    .stage { fill: #1f2328; font: 700 12px ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
    .wire  { stroke: #8c959f; stroke-width: 1.3; fill: none; }
    .head  { fill: #8c959f; }
    .seg   { fill: #d0d7de; stroke: #f6f8fa; stroke-width: 1.5; }
    .segn  { fill: #3b7dd8; fill-opacity: 0.38; stroke: #f6f8fa; stroke-width: 1.5; }
    .mt    { fill: #1f2328; font: 600 14px ui-sans-serif, -apple-system, "Segoe UI", Roboto, sans-serif; }
    .d     { fill: #424a53; font: 400 12.5px ui-sans-serif, -apple-system, "Segoe UI", Roboto, sans-serif; }
    .c     { fill: #6e7781; font: 400 11px ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
    .ok    { fill: #1a7f37; font: 600 12.5px ui-sans-serif, -apple-system, "Segoe UI", Roboto, sans-serif; }
    @media (prefers-color-scheme: dark) {
      .bg    { fill: #0d1117; }
      .panel { fill: #161b22; stroke: #30363d; }
      .card  { fill: #0d1117; stroke: #30363d; }
      .void  { stroke: #6e7681; }
      .title, .lbl, .m, .stage { fill: #e6edf3; }
      .sub, .hdr, .mm, .ms { fill: #9198a1; }
      .wire  { stroke: #6e7681; }
      .head  { fill: #6e7681; }
      .seg   { fill: #30363d; stroke: #161b22; }
      .segn  { fill-opacity: 0.55; stroke: #161b22; }
      .mt    { fill: #e6edf3; }
      .d     { fill: #b7bdc8; }
      .c     { fill: #7d8590; }
      .ok    { fill: #3fb950; }
    }
  </style>
"""

C_EXEC = "#3b7dd8"
C_FP = "#8b6cc9"
C_STATE = "#2ea08c"
C_LIAR = "#c9743a"
C_GREY = "#7d8590"


def architecture_svg() -> str:
    """The whole system on one page, in the order a reader meets it: what a run starts from,
    how one instruction executes, how the machine is fingerprinted, how two runs find their
    first disagreement, and how that one instruction is checked.

    Each module has a plain title, one plain sentence, and a grey line naming the code and
    the numbers behind it. The numbers come from the code and the measurements: the state
    record from fast/state.go, proof sizes from fast/memory.go and fast/file.go, the
    checkpoint policy from internal/liar, gas from the EVM tests and the gallery.
    """
    # Rows: inputs/run 300, fingerprint 272, find/check 286, where it runs 136, plus gaps.
    w, h = 960, 74 + 300 + 28 + 272 + 28 + 286 + 26 + 136
    p = [
        f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {w} {h}" width="{w}" height="{h}" '
        f'role="img" aria-label="How MLVM works: the inputs of a run, executing one instruction '
        f'at a time, fingerprinting the whole machine, finding the first disagreement between two '
        f'runs, and checking that one instruction">',
        ARCH_STYLE,
        '<defs><marker id="ah" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="8" '
        'markerHeight="8" markerUnits="userSpaceOnUse" orient="auto-start-reverse">'
        '<path class="head" d="M0,0 L10,5 L0,10 z"/></marker></defs>',
        f'<rect class="bg" width="{w}" height="{h}"/>',
    ]

    def t(x, y, s, cls="d", anchor=None, fill=None):
        a = f' text-anchor="{anchor}"' if anchor else ""
        f = f' fill="{fill}"' if fill else ""
        p.append(f'<text class="{cls}" x="{x:g}" y="{y:g}"{a}{f}>{esc(s)}</text>')

    def rect(x, y, rw, rh, cls, rx=8):
        p.append(f'<rect class="{cls}" x="{x:g}" y="{y:g}" width="{rw:g}" height="{rh:g}" rx="{rx}"/>')

    def arrow(x1, y1, x2, y2):
        p.append(f'<line class="wire" x1="{x1:g}" y1="{y1:g}" x2="{x2:g}" y2="{y2:g}" marker-end="url(#ah)"/>')

    def section(x, y, num, name, plain):
        t(x, y, f"{num} · {name}", "hdr")
        t(x, y + 20, plain, "lbl")

    def module(x, y, mw, mh, title, lines, code, colour=None):
        """A card: a plain title, one or two plain lines, and a grey line of code."""
        rect(x, y, mw, mh, "card", rx=6)
        if colour:
            p.append(f'<rect x="{x:g}" y="{y + 8:g}" width="3" height="{mh - 16:g}" rx="1.5" fill="{colour}"/>')
        t(x + 14, y + 21, title, "mt")
        for n, line in enumerate(lines):
            t(x + 14, y + 40 + n * 17, line)
        t(x + 14, y + mh - 11, code, "c")

    t(24, 34, "How MLVM works", "title")
    t(24, 56, "A model run you can check one instruction at a time. Each module: what it does, "
              "and in grey, the code behind it.", "sub")

    # 01 inputs ------------------------------------------------------------------------
    y0, ph = 74, 300
    rect(24, y0, 284, ph, "panel")
    section(38, y0 + 22, "01", "INPUTS", "What a run starts from")
    for i, (title, line, code) in enumerate((
            ("The program", "The model's code, compiled for RISC-V", "static RV64IMAFD ELF, C or Rust"),
            ("Input files", "Weights, tokenizer and prompt", "copied into memory before the run"),
            ("Fixed random bytes", "Every run gets the same “random” bytes", "5 MiB blob served by getrandom"))):
        module(38, y0 + 62 + i * 76, 256, 68, title, (line,), code, C_STATE)
    arrow(308, y0 + ph / 2, 330, y0 + ph / 2)

    # 02 run ---------------------------------------------------------------------------
    rect(332, y0, 604, ph, "panel")
    section(346, y0 + 22, "02", "RUN", "Execute the program one instruction at a time")
    stages = (("Fetch", "read the instruction"), ("Decode", "find what it asks"),
              ("Execute", "compute the result"), ("Write back", "update the state"))
    sw = (576 - 3 * 16) / 4
    for i, (name, sub) in enumerate(stages):
        x = 346 + i * (sw + 16)
        rect(x, y0 + 62, sw, 50, "card", rx=6)
        t(x + sw / 2, y0 + 82, name, "mt", "middle")
        t(x + sw / 2, y0 + 100, sub, "d", "middle")
        if i:
            arrow(x - 15, y0 + 87, x - 2, y0 + 87)
    t(346, y0 + 132, "Each instruction is one of these kinds of work:", "d")
    uw = (576 - 2 * 12) / 3
    for i, (title, lines, code, colour) in enumerate((
            ("Integer math", ("Add, multiply, compare,", "read and write memory"), "rvgo/fast · yul64.go", C_EXEC),
            ("Floating point", ("Done in software, so every", "computer gets the same bits"), "rvgo/comp · SoftFloat 3e", C_FP),
            ("System calls", ("Files, memory and time come", "from the VM, never the host"), "37 Linux syscalls", C_GREY))):
        module(346 + i * (uw + 12), y0 + 144, uw, 100, title, lines, code, colour)
    t(346, y0 + 276, "About 20 million instructions per second natively; the same code runs in a browser.", "c")

    # 03 fingerprint -------------------------------------------------------------------
    arrow(480, y0 + ph, 480, y0 + ph + 26)
    t(490, y0 + ph + 18, "after any instruction", "c")
    y1, ph1 = y0 + ph + 28, 272
    rect(24, y1, 912, ph1, "panel")
    section(38, y1 + 22, "03", "FINGERPRINT", "The whole machine hashes to one 32-byte fingerprint")
    t(38, y1 + 64, "A 674-byte state record holds the registers and the roots of memory and files. "
                   "Its hash is the fingerprint, also called the state root.", "d")
    x0, scale, by = 40, 880 / 674, y1 + 102
    split = x0 + 362 * scale
    for a, b, lab in ((x0, split - 2, "inherited from Asterisc · 362 B"), (split + 2, x0 + 674 * scale, "added by MLVM · 312 B")):
        p.append(f'<line class="wire" x1="{a:.1f}" y1="{by - 8}" x2="{b:.1f}" y2="{by - 8}"/>')
        p.append(f'<line class="wire" x1="{a:.1f}" y1="{by - 12}" x2="{a:.1f}" y2="{by - 4}"/>')
        p.append(f'<line class="wire" x1="{b:.1f}" y1="{by - 12}" x2="{b:.1f}" y2="{by - 4}"/>')
        t((a + b) / 2, by - 15, lab, "c", "middle")
    fields = ((0, 32, "", False, "memory root"), (32, 72, "", False, "pre-image key"),
              (72, 106, "", False, "pc, step, heap"), (106, 362, "32 integer registers", False, ""),
              (362, 618, "32 float registers", True, ""), (618, 634, "", True, "randomness"),
              (634, 642, "", True, "fcsr"), (642, 674, "", True, "files root"))
    for a, b, lab, new, below in fields:
        xa, xb = x0 + a * scale, x0 + b * scale
        p.append(f'<rect class="{"segn" if new else "seg"}" x="{xa:.1f}" y="{by}" '
                 f'width="{xb - xa:.1f}" height="28"><title>state record bytes {a}–{b}</title></rect>')
        if lab:
            t((xa + xb) / 2, by + 18, lab, "d", "middle")
    # The narrow fields are named in order on one line under each end of the bar.
    for a, b, lab, new, below in fields:
        if below:
            xm = x0 + (a + b) / 2 * scale
            p.append(f'<line class="wire" x1="{xm:.1f}" y1="{by + 28}" x2="{xm:.1f}" y2="{by + 34}"/>')
    t(x0, by + 47, "memory root · pre-image key · pc, step, heap", "c")
    t(x0 + 880, by + 47, "randomness · fcsr · files root", "c", "end")
    cw = (880 - 2 * 16) / 3
    for i, (title, lines, code) in enumerate((
            ("Memory", ("A Merkle tree over all 2⁶⁴ bytes: one word", "is proven with a 1,920-byte proof"), "4 KiB pages · rvgo/fast/memory.go"),
            ("Files", ("Staged files and open file handles", "have their own trees, proven the same way"), "992-byte proof · rvgo/fast/file.go"),
            ("The answer", ("The model writes its answer to a fixed", "address, so the fingerprint covers it"), "MLVM_OUT · 0x3100_0000_0000_0000"))):
        module(40 + i * (cw + 16), y1 + 166, cw, 92, title, lines, code, C_STATE if i < 2 else C_EXEC)

    # 04 find --------------------------------------------------------------------------
    y2, ph2 = y1 + ph1 + 28, 286
    arrow(264, y1 + ph1, 264, y2 - 2)
    t(274, y1 + ph1 + 18, "fingerprints of two runs", "c")
    arrow(732, y1 + ph1, 732, y2 - 2)
    t(742, y1 + ph1 + 18, "state record + memory proofs", "c")
    rect(24, y2, 480, ph2, "panel")
    section(38, y2 + 22, "04", "FIND", "Find the first instruction where two runs differ")
    lanes = (("original run", y2 + 66, C_EXEC), ("changed run", y2 + 94, C_LIAR))
    lie_x = 352
    for name, y, colour in lanes:
        t(38, y + 4, name, "d", fill=colour)
        x_start = 136
        if colour == C_LIAR:
            p.append(f'<line x1="{x_start}" y1="{y}" x2="{lie_x}" y2="{y}" stroke="{C_GREY}" stroke-width="2"/>')
            p.append(f'<line x1="{lie_x}" y1="{y}" x2="486" y2="{y}" stroke="{colour}" stroke-width="2"/>')
        else:
            p.append(f'<line x1="{x_start}" y1="{y}" x2="486" y2="{y}" stroke="{colour}" stroke-width="2"/>')
        for k in range(8):
            p.append(f'<circle class="card" cx="{x_start + k * 50}" cy="{y}" r="3.5"/>')
    p.append(f'<path d="M{lie_x - 4},{y2 + 90} l8,8 m0,-8 l-8,8" stroke="{C_LIAR}" stroke-width="2"/>')
    p.append(f'<rect x="340" y="{y2 + 56}" width="24" height="48" fill="{C_EXEC}" fill-opacity="0.12"/>')
    t(352, y2 + 120, "the search ends here", "c", "middle")
    for n, line in enumerate((
            "1. The two runs end with different fingerprints.",
            "2. Ask both for their fingerprint halfway through.",
            "3. Keep the half where they first differ, and repeat.",
            "About log₂ N rounds: 26 for 38 million instructions,",
            "34 for 14.8 billion. Saved checkpoints let each side answer",
            "without running from the start.")):
        t(38, y2 + 150 + n * 19, line, "d")
    t(38, y2 + ph2 - 14, "rvgo/internal/liar · checkpoint every 2²² instructions", "c")
    arrow(504, y2 + ph2 / 2, 526, y2 + ph2 / 2)

    # 05 check -------------------------------------------------------------------------
    rect(528, y2, 408, ph2, "panel")
    section(542, y2 + 22, "05", "CHECK", "Re-execute only that one instruction")
    t(542, y2 + 64, "✓ The state record hashes to the fingerprint both runs agreed on.", "d")
    t(542, y2 + 83, "✓ The memory proofs match its memory root.", "d")
    for i, (title, line, code, colour) in enumerate((
            ("RISCV.sol", "A smart contract runs the step in an EVM", "integer and memory · ≈84K gas", C_EXEC),
            ("slow.Step", "The same rules, written in Go", "integer, memory and file steps", C_STATE),
            ("SoftFloat", "Recomputes a floating-point result", "float arithmetic · a local check", C_FP))):
        module(542, y2 + 98 + i * 54, 380, 48, title, (), code, colour)
        t(642, y2 + 98 + i * 54 + 21, line, "d")
    t(542, y2 + ph2 - 14, "→ a few-kilobyte record that anyone can check again", "d")

    # 06 where it runs -----------------------------------------------------------------
    y3 = y2 + ph2 + 26
    t(24, y3 + 12, "06 · WHERE IT RUNS", "hdr")
    bw = (912 - 3 * 12) / 4
    for i, (title, lines, code, dashed) in enumerate((
            ("Command line", ("Native builds for Linux,", "macOS and Windows"), "cmd/mlvm · brew · install.sh", False),
            ("Browser", ("The same VM, compiled to", "WebAssembly"), "rvgo/wasm · the playground", False),
            ("Every push", ("Six platforms must produce", "identical fingerprints"), "workflows/determinism.yml", False),
            ("Not yet", ("An on-chain dispute game with", "bonds and clocks; an audit"), "see docs/STATUS.md", True))):
        x = 24 + i * (bw + 12)
        if dashed:
            rect(x, y3 + 22, bw, 92, "void")
            t(x + 14, y3 + 43, title, "mt")
            for n, line in enumerate(lines):
                t(x + 14, y3 + 62 + n * 17, line)
            t(x + 14, y3 + 103, code, "c")
        else:
            module(x, y3 + 22, bw, 92, title, lines, code, C_GREY)
    p.append("</svg>")
    return "\n".join(p)


# Figures drawn inline in the write-up, exported so the README can show them. They keep the
# page's class names from web/src/blog.css; the tokens below give them the palette of the
# figures above.
BLOG_FIGURES = (("bisection-questions.svg", "fig4"),)
BLOG_STYLE = """
    svg {
      --surface: #ffffff; --surface-2: #f6f8fa; --ink: #1f2328; --ink-2: #59636e;
      --muted: #8c959f; --grid: #eaeef2; --axis: #d0d7de; --caller: #d0d7de;
      --accent: #3b7dd8; --honest: #3b7dd8; --liar: #c9743a;
      --sans: ui-sans-serif, -apple-system, "Segoe UI", Roboto, sans-serif;
      --mono: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    }
    @media (prefers-color-scheme: dark) {
      svg {
        --surface: #0d1117; --surface-2: #161b22; --ink: #e6edf3; --ink-2: #9198a1;
        --muted: #6e7681; --grid: #21262d; --axis: #30363d; --caller: #30363d;
      }
    }
    .bg { fill: var(--surface); stroke: var(--axis); stroke-width: 1; }
    text { font-family: var(--sans); }
"""


def blog_figure_svg(fig_id: str, pad: int = 16) -> str:
    html = (ROOT / "web/blog.html").read_text()
    css = (ROOT / "web/src/blog.css").read_text()
    m = re.search(rf'<figure class="fig" id="{fig_id}">.*?<svg viewBox="0 0 (\d+) (\d+)"'
                  r' (role="img" aria-labelledby="[^"]+")>(.*?)</svg>', html, re.S)
    if not m:
        raise SystemExit(f"error: no <svg> in figure #{fig_id} of web/blog.html")
    w, h = int(m.group(1)) + 2 * pad, int(m.group(2)) + 2 * pad
    title, body = textwrap.dedent(m.group(4)).strip().split("\n", 1)
    rules = re.findall(r"^\.f-[\w-]+ \{[^}\n]*\}$", css, re.M)
    style = BLOG_STYLE + "".join(f"    {r}\n" for r in rules)
    return (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="{-pad} {-pad} {w} {h}" '
            f'width="{w}" height="{h}" {m.group(3)}>\n{title}\n<style>{style}</style>\n'
            f'<rect class="bg" x="{0.5 - pad}" y="{0.5 - pad}" width="{w - 1}" height="{h - 1}" rx="12"/>\n'
            f"{body}\n</svg>\n")


def overview_svg() -> str:
    """The story the README and the playground tell: someone runs a model and returns an
    answer you cannot check; MLVM fingerprints every step, finds the first instruction where
    two runs differ, and has a referee re-execute only that one."""
    w, h = 960, 464
    p = [
        f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {w} {h}" width="{w}" height="{h}" '
        f'role="img" aria-label="From an answer to one checked instruction: someone runs a model and '
        f'returns an answer that cannot be checked by itself; MLVM fingerprints every step, finds the '
        f'first instruction where two runs differ in about log2 N rounds, and a referee re-executes '
        f'only that instruction">',
        ARCH_STYLE,
        '<defs><marker id="ah" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="8" '
        'markerHeight="8" markerUnits="userSpaceOnUse" orient="auto-start-reverse">'
        '<path class="head" d="M0,0 L10,5 L0,10 z"/></marker></defs>',
        f'<rect class="bg" width="{w}" height="{h}"/>',
    ]

    def t(x, y, s, cls="d", anchor=None, fill=None):
        a = f' text-anchor="{anchor}"' if anchor else ""
        f = f' fill="{fill}"' if fill else ""
        p.append(f'<text class="{cls}" x="{x:g}" y="{y:g}"{a}{f}>{esc(s)}</text>')

    def rect(x, y, rw, rh, cls, rx=8):
        p.append(f'<rect class="{cls}" x="{x:g}" y="{y:g}" width="{rw:g}" height="{rh:g}" rx="{rx}"/>')

    def arrow(x1, y1, x2, y2):
        p.append(f'<line class="wire" x1="{x1:g}" y1="{y1:g}" x2="{x2:g}" y2="{y2:g}" marker-end="url(#ah)"/>')

    def line(x1, y1, x2, y2, colour, width=2):
        p.append(f'<line x1="{x1:g}" y1="{y1:g}" x2="{x2:g}" y2="{y2:g}" stroke="{colour}" stroke-width="{width}"/>')

    t(24, 34, "From an answer to one checked instruction", "title")
    t(24, 56, "How MLVM lets you verify a model run without running it again.", "sub")

    # The problem ------------------------------------------------------------------------
    rect(24, 72, 912, 104, "panel")
    t(38, 94, "THE PROBLEM", "hdr")
    for i, (title, line_) in enumerate((("You agree on a model", "its weights and a prompt"),
                                        ("Someone else runs it", "on their own computer"),
                                        ("They return an answer", "\u201cOnce upon a time, \u2026\u201d"))):
        x = 38 + i * 216
        rect(x, 106, 190, 54, "card", rx=6)
        t(x + 14, 127, title, "mt")
        t(x + 14, 146, line_)
        if i:
            arrow(x - 24, 133, x - 3, 133)
    t(686, 124, "Is the answer right?", "mt")
    t(686, 143, "The answer alone cannot show it,")
    t(686, 160, "and running the model again is expensive.")

    arrow(480, 176, 480, 206)
    t(492, 197, "MLVM makes the run checkable in three steps", "c")

    # The three steps ------------------------------------------------------------------
    y, ph, pw = 210, 240, (912 - 2 * 14) / 3
    steps = (
        ("STEP 1", "Fingerprint every step",
         ("After any instruction, the machine's",
          "memory and registers hash to one",
          "32-byte fingerprint. The same run gives",
          "the same fingerprints on any computer.")),
        ("STEP 2", "Find the first disagreement",
         ("If two runs end with different",
          "fingerprints, compare them halfway and",
          "keep the half where they differ. About",
          "log\u2082 N rounds: 34 for 14.8 billion.")),
        ("STEP 3", "Check that one instruction",
         ("A referee re-executes only that",
          "instruction. It never runs the model,",
          "and the evidence is a few kilobytes",
          "that anyone can check again.")),
    )
    for i, (num, title, caption) in enumerate(steps):
        px = 24 + i * (pw + 14)
        rect(px, y, pw, ph, "panel")
        t(px + 14, y + 24, num, "hdr")
        t(px + 14, y + 45, title, "mt")
        for n, s_ in enumerate(caption):
            t(px + 14, y + 172 + n * 17, s_)
        if i:
            arrow(px - 13, y + 104, px - 2, y + 104)

    # Step 1: a run, and the fingerprint after each instruction.
    px = 24
    xs = [px + 36 + k * 56 for k in range(5)]
    line(xs[0], y + 88, xs[-1], y + 88, C_EXEC)
    for k, (x, lab, fp) in enumerate(zip(xs, ("1", "2", "3", "\u2026", "N"),
                                          ("0x3a\u2026", "0x91\u2026", "0x07\u2026", "", "0xbe\u2026"))):
        t(x, y + 76, lab, "c", "middle")
        if fp:
            p.append(f'<circle class="card" cx="{x}" cy="{y + 88}" r="4"/>')
            rect(x - 25, y + 100, 50, 20, "card", rx=4)
            t(x, y + 114, fp, "c", "middle")
    t(px + 14, y + 140, "a fingerprint after every instruction", "c")

    # Step 2: two runs that differ from one instruction on, and the halving search.
    px = 24 + pw + 14
    x0, x1, xc = px + 22, px + 236, px + 158
    t(px + 14, y + 66, "original", "c", fill=C_EXEC)
    line(x0 + 50, y + 70, x1, y + 70, C_EXEC)
    t(px + 14, y + 90, "changed", "c", fill=C_LIAR)
    line(x0 + 50, y + 94, xc, y + 94, C_GREY)
    line(xc, y + 94, x1, y + 94, C_LIAR)
    p.append(f'<path d="M{xc - 4},{y + 90} l8,8 m0,-8 l-8,8" stroke="{C_LIAR}" stroke-width="2"/>')
    t(x1 + 8, y + 74, "0xbe\u2026", "c", fill=C_EXEC)
    t(x1 + 8, y + 98, "0x89\u2026", "c", fill=C_LIAR)
    lo, hi = x0 + 50, x1
    for r in range(4):
        p.append(f'<rect x="{lo:.1f}" y="{y + 110 + r * 9}" width="{hi - lo:.1f}" height="5" rx="2" '
                 f'fill="{C_EXEC}" fill-opacity="{0.25 + 0.15 * r:.2f}"/>')
        mid = (lo + hi) / 2
        lo, hi = (mid, hi) if xc > mid else (lo, mid)
    t(px + 14, y + 116, "round 1", "c")
    t(px + 14, y + 143, "round 4", "c")

    # Step 3: the evidence goes in, one instruction is re-executed, the verdict comes out.
    px = 24 + 2 * (pw + 14)
    for k, lab in enumerate(("674-byte state", "memory proof")):
        rect(px + 14 + k * 136, y + 58, 128, 24, "card", rx=4)
        t(px + 14 + k * 136 + 64, y + 74, lab, "c", "middle")
    arrow(px + 147, y + 82, px + 147, y + 98)
    rect(px + 14, y + 100, 266, 28, "card", rx=4)
    p.append(f'<rect x="{px + 14}" y="{y + 108}" width="3" height="12" rx="1.5" fill="{C_FP}"/>')
    t(px + 28, y + 118, "re-execute", "c")
    t(px + 108, y + 119, "fmul.s ft3, ft4, ft5", "m")
    t(px + 14, y + 148, "\u2713 the original run is right", "ok")
    p.append("</svg>")
    return "\n".join(p)


def main() -> int:
    FIG.mkdir(exist_ok=True)
    for name, svg in (("address-space.svg", address_space_svg()),
                      ("isa-coverage.svg", isa_coverage_svg()),
                      ("architecture.svg", architecture_svg()),
                      ("overview.svg", overview_svg()),
                      *((name, blog_figure_svg(fig)) for name, fig in BLOG_FIGURES)):
        path = FIG / name
        path.write_text(svg)
        print(f"wrote {path.relative_to(ROOT)}  ({len(svg):,} bytes)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
