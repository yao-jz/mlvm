#!/usr/bin/env python3
"""isa-coverage.py - report which RISC-V instructions MLVM actually decodes.

The instruction decoder in rvgo/fast/vm.go is one big switch on the opcode with
nested switches on funct3/funct5/funct7. Rather than maintain a hand-written coverage
table that drifts out of date, this script reads the source and reports what is there,
cross-referenced against the RV64IMAFD instruction list.

    ./scripts/isa-coverage.py              human-readable summary
    ./scripts/isa-coverage.py --markdown   the table used in docs/ISA.md
    ./scripts/isa-coverage.py --check      exit 1 if anything expected is missing

The detection is deliberately simple: an instruction counts as implemented if its mnemonic
appears in a `// ... = MNEMONIC` comment in the decoder, which is the convention the file
already uses throughout. That keeps the script honest - it reports what the code says it
does, and if someone adds an instruction without the comment, coverage under-reports
rather than over-reports.
"""

import argparse
import pathlib
import re
import sys

VM_GO = pathlib.Path(__file__).resolve().parent.parent / "rvgo/fast/vm.go"

# The RV64IMAFD instruction set, grouped the way the RISC-V unprivileged spec groups it.
EXTENSIONS = {
    "RV32I": [
        "LUI", "AUIPC", "JAL", "JALR",
        "BEQ", "BNE", "BLT", "BGE", "BLTU", "BGEU",
        "LB", "LH", "LW", "LBU", "LHU",
        "SB", "SH", "SW",
        "ADDI", "SLTI", "SLTIU", "XORI", "ORI", "ANDI", "SLLI", "SRLI", "SRAI",
        "ADD", "SUB", "SLL", "SLT", "SLTU", "XOR", "SRL", "SRA", "OR", "AND",
        "FENCE", "ECALL", "EBREAK",
    ],
    "RV64I": [
        "LWU", "LD", "SD", "ADDIW", "SLLIW", "SRLIW", "SRAIW",
        "ADDW", "SUBW", "SLLW", "SRLW", "SRAW",
    ],
    "RV32M": ["MUL", "MULH", "MULHSU", "MULHU", "DIV", "DIVU", "REM", "REMU"],
    "RV64M": ["MULW", "DIVW", "DIVUW", "REMW", "REMUW"],
    "RV32A": [
        "LR.W", "SC.W", "AMOSWAP.W", "AMOADD.W", "AMOXOR.W", "AMOAND.W", "AMOOR.W",
        "AMOMIN.W", "AMOMAX.W", "AMOMINU.W", "AMOMAXU.W",
    ],
    "RV64A": [
        "LR.D", "SC.D", "AMOSWAP.D", "AMOADD.D", "AMOXOR.D", "AMOAND.D", "AMOOR.D",
        "AMOMIN.D", "AMOMAX.D", "AMOMINU.D", "AMOMAXU.D",
    ],
    "RV32F": [
        "FLW", "FSW", "FMADD.S", "FMSUB.S", "FNMSUB.S", "FNMADD.S",
        "FADD.S", "FSUB.S", "FMUL.S", "FDIV.S", "FSQRT.S",
        "FSGNJ.S", "FSGNJN.S", "FSGNJX.S", "FMIN.S", "FMAX.S",
        "FCVT.W.S", "FCVT.WU.S", "FMV.X.W", "FEQ.S", "FLT.S", "FLE.S", "FCLASS.S",
        "FCVT.S.W", "FCVT.S.WU", "FMV.W.X",
    ],
    "RV64F": ["FCVT.L.S", "FCVT.LU.S", "FCVT.S.L", "FCVT.S.LU"],
    "RV32D": [
        "FLD", "FSD", "FMADD.D", "FMSUB.D", "FNMSUB.D", "FNMADD.D",
        "FADD.D", "FSUB.D", "FMUL.D", "FDIV.D", "FSQRT.D",
        "FSGNJ.D", "FSGNJN.D", "FSGNJX.D", "FMIN.D", "FMAX.D",
        "FCVT.S.D", "FCVT.D.S", "FEQ.D", "FLT.D", "FLE.D", "FCLASS.D",
        "FCVT.W.D", "FCVT.WU.D", "FCVT.D.W", "FCVT.D.WU",
    ],
    "RV64D": ["FCVT.L.D", "FCVT.LU.D", "FMV.X.D", "FCVT.D.L", "FCVT.D.LU", "FMV.D.X"],
    "Zicsr": ["CSRRW", "CSRRS", "CSRRC", "CSRRWI", "CSRRSI", "CSRRCI"],
    "Zifencei": ["FENCE.I"],
}

# Instructions the decoder handles without naming individually, because one branch covers
# a whole family. Mapping each to the marker that proves the branch exists keeps the
# report accurate without pretending the source says something it does not.
FAMILY_MARKERS = {
    # The load/store opcodes decode size and signedness arithmetically from funct3 rather
    # than switching on each mnemonic.
    **{m: "000_0011: memory loading" for m in ("LB", "LH", "LW", "LBU", "LHU", "LWU", "LD")},
    **{m: "010_0011: memory storing" for m in ("SB", "SH", "SW", "SD")},
    # CSR instructions are dispatched by mode bits, not by mnemonic.
    **{m: "CSR instructions" for m in
       ("CSRRW", "CSRRS", "CSRRC", "CSRRWI", "CSRRSI", "CSRRCI")},
    # FENCE / FENCE.TSO / FENCE.I share one no-op branch.
    "FENCE.I": "FENCE / FENCE.TSO / FENCE.I all no-op",
    # The atomics dispatch on the 5-bit op field with a shared W/D size, so LR/SC/AMO*
    # appear once each rather than twice.
    **{f"{m}.D": f"= {m}" for m in
       ("LR", "SC", "AMOSWAP", "AMOADD", "AMOXOR", "AMOAND", "AMOOR",
        "AMOMIN", "AMOMAX", "AMOMINU", "AMOMAXU")},
    **{f"{m}.W": f"= {m}" for m in
       ("LR", "SC", "AMOSWAP", "AMOADD", "AMOXOR", "AMOAND", "AMOOR",
        "AMOMIN", "AMOMAX", "AMOMINU", "AMOMAXU")},
}

# Instructions that decode but whose semantics are knowingly incomplete. Reported as
# partial so the table never overstates what the VM guarantees. Keep in sync with
# fdKnownFailures in rvgo/test/vm_test.go - which is now empty, so what is left here is only
# the machinery this VM deliberately does not have.
PARTIAL = {
    # The multiply-add family, FMIN/FMAX and the float CSRs used to live here. They are all
    # conformant now, which is why fdKnownFailures is empty; the entries were removed rather
    # than left behind, so this table keeps meaning what it says.
    "CSRRW": "only fcsr/fflags/frm exist; every other CSR reads as 0 and ignores writes",
    "CSRRS": "only fcsr/fflags/frm exist; every other CSR reads as 0 and ignores writes",
    "CSRRC": "only fcsr/fflags/frm exist; every other CSR reads as 0 and ignores writes",
    "CSRRWI": "only fcsr/fflags/frm exist; every other CSR reads as 0 and ignores writes",
    "CSRRSI": "only fcsr/fflags/frm exist; every other CSR reads as 0 and ignores writes",
    "CSRRCI": "only fcsr/fflags/frm exist; every other CSR reads as 0 and ignores writes",
    "FENCE": "no-op: single hart, no store buffer to drain",
    "FENCE.I": "no-op: no instruction cache to invalidate",
    "EBREAK": "no-op: no debugger to trap to",
}


def load_source() -> str:
    if not VM_GO.exists():
        sys.exit(f"error: cannot find {VM_GO}")
    return VM_GO.read_text()


def is_implemented(mnemonic: str, src: str) -> bool:
    # Primary signal: the mnemonic appears as a token in a decoder comment. The file
    # labels every branch this way - "// 000 = BEQ", "// 011_0111: LUI = Load upper
    # immediate", "// 1000011 FMADD.S FMADD.D" - so a token match tracks the code closely
    # without depending on one particular comment style.
    pattern = r"(?<![A-Za-z0-9_.])" + re.escape(mnemonic) + r"(?![A-Za-z0-9_.])"
    if re.search(pattern, src):
        return True
    marker = FAMILY_MARKERS.get(mnemonic)
    return bool(marker and marker in src)


def analyse(src: str):
    rows = []
    for ext, mnemonics in EXTENSIONS.items():
        for m in mnemonics:
            if not is_implemented(m, src):
                status, note = "missing", ""
            elif m in PARTIAL:
                status, note = "partial", PARTIAL[m]
            else:
                status, note = "full", ""
            rows.append((ext, m, status, note))
    return rows


def summarise(rows):
    per_ext = {}
    for ext, _m, status, _n in rows:
        d = per_ext.setdefault(ext, {"full": 0, "partial": 0, "missing": 0})
        d[status] += 1
    return per_ext


def print_text(rows):
    per_ext = summarise(rows)
    width = max(len(e) for e in per_ext)
    print(f"MLVM instruction coverage  (source of truth: {VM_GO.relative_to(VM_GO.parents[3])})")
    print()
    print(f"  {'extension'.ljust(width)}   full  partial  missing   total")
    print(f"  {'-' * width}   ----  -------  -------   -----")
    tot = {"full": 0, "partial": 0, "missing": 0}
    for ext, d in per_ext.items():
        total = sum(d.values())
        for k in tot:
            tot[k] += d[k]
        print(f"  {ext.ljust(width)}   {d['full']:>4}  {d['partial']:>7}  {d['missing']:>7}   {total:>5}")
    grand = sum(tot.values())
    print(f"  {'-' * width}   ----  -------  -------   -----")
    print(f"  {'TOTAL'.ljust(width)}   {tot['full']:>4}  {tot['partial']:>7}  {tot['missing']:>7}   {grand:>5}")

    partial = [(m, n) for _e, m, s, n in rows if s == "partial"]
    missing = [m for _e, m, s, _n in rows if s == "missing"]
    if partial:
        print("\n  partial:")
        for m, n in partial:
            print(f"    {m:<12} {n}")
    if missing:
        print("\n  missing:")
        print("    " + ", ".join(missing))
    print()
    print("  Not covered at all, by design: RVC (compressed 16-bit instructions), the")
    print("  privileged ISA, RV32/64Q (quad float), and the vector extension. Guest code")
    print("  must therefore be built with -march=rv64imafd, without 'c'.")


def print_markdown(rows):
    per_ext = summarise(rows)
    icon = {"full": "yes", "partial": "partial", "missing": "no"}
    print("| Extension | Implemented | Partial | Missing | Coverage |")
    print("| --- | ---: | ---: | ---: | ---: |")
    for ext, d in per_ext.items():
        total = sum(d.values())
        pct = 100.0 * (d["full"] + d["partial"]) / total if total else 0
        print(f"| `{ext}` | {d['full']} | {d['partial']} | {d['missing']} | {pct:.0f}% |")
    print()
    print("| Instruction | Status | Note |")
    print("| --- | --- | --- |")
    for _ext, m, status, note in rows:
        if status != "full":
            print(f"| `{m}` | {icon[status]} | {note or '-'} |")


def main():
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--markdown", action="store_true", help="emit the docs/ISA.md table")
    ap.add_argument("--check", action="store_true", help="exit 1 if anything is missing")
    args = ap.parse_args()

    rows = analyse(load_source())
    if args.markdown:
        print_markdown(rows)
    else:
        print_text(rows)

    if args.check:
        missing = [m for _e, m, s, _n in rows if s == "missing"]
        if missing:
            print(f"\nFAIL: {len(missing)} instruction(s) not decoded", file=sys.stderr)
            return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
