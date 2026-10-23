#!/usr/bin/env python3
"""Classify decoder-relevant transitions for the moonshot settleability report.

Reads an instruction-profile JSON (optional) or emits the static classification
used by docs/STATUS.md and the spike notes.
"""

from __future__ import annotations

import json
import sys
from pathlib import Path

# Static classification for the TinyStories-260K C decoder guest.
# "direct"  = RISCV.sol / slow can execute today
# "nested"  = needs bound f32/f64 sub-game
# "stub"    = deterministic compatibility stub (must match across implementations)
# "gap"     = executable in fast only / not yet settleable

ROWS = [
    {"kind": "alu_int", "examples": "ADD/SUB/MUL/DIV/shift/branch", "route": "direct"},
    {"kind": "load_store", "examples": "LB/LH/LW/LD/SB/SH/SW/SD", "route": "direct"},
    {"kind": "float_load_store", "examples": "FLW/FSW/FLD/FSD", "route": "direct"},
    {"kind": "float_bitmanip", "examples": "FSGNJ/FMV", "route": "direct"},
    {"kind": "float_arith_f32", "examples": "FADD.S/FMUL.S/FMADD.S/FSQRT.S", "route": "nested"},
    {"kind": "float_arith_f64", "examples": "FADD.D/… (MLP path)", "route": "nested"},
    {"kind": "syscall_exit", "examples": "exit/exit_group", "route": "direct"},
    {"kind": "syscall_file", "examples": "openat/read/close/lseek", "route": "direct"},
    {"kind": "syscall_mmap", "examples": "mmap/munmap/mprotect/brk", "route": "direct"},
    {"kind": "syscall_sysinfo", "examples": "sysinfo(179)", "route": "stub"},
    {"kind": "syscall_getrandom", "examples": "getrandom(278)", "route": "gap"},
]


def main() -> int:
    out = {
        "profile": "mlvm-1",
        "guest": "examples/decoder (TinyStories-260K)",
        "rows": ROWS,
        "summary": {
            "direct": sum(1 for r in ROWS if r["route"] == "direct"),
            "nested": sum(1 for r in ROWS if r["route"] == "nested"),
            "stub": sum(1 for r in ROWS if r["route"] == "stub"),
            "gap": sum(1 for r in ROWS if r["route"] == "gap"),
        },
        "launchClaim": (
            "Public demo claims deterministic fast execution + selected integer "
            "settlement preview. Full decoder-trace settlement requires closing "
            "nested f32 and getrandom equivalence."
        ),
    }
    text = json.dumps(out, indent=2) + "\n"
    if len(sys.argv) > 1:
        path = Path(sys.argv[1])
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)
        print(f"wrote {path}")
    else:
        sys.stdout.write(text)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
