#!/usr/bin/env python3
"""Write a committed MLVM decoder request.bin."""

from __future__ import annotations

import argparse
import struct
from pathlib import Path


def main() -> None:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("prompt", help="UTF-8 prompt text")
    p.add_argument("-o", "--output", type=Path, required=True)
    p.add_argument("--max-new-tokens", type=int, default=16)
    p.add_argument("--temperature-milli", type=int, default=0)
    args = p.parse_args()

    prompt = args.prompt.encode("utf-8")
    if len(prompt) > 256:
        raise SystemExit("prompt exceeds 256 bytes")
    if not 1 <= args.max_new_tokens <= 16:
        raise SystemExit("max-new-tokens must be in 1..16 for the demo profile")

    blob = struct.pack(
        "<IIII",
        1,
        args.max_new_tokens,
        args.temperature_milli,
        0,
    )
    blob += struct.pack("<I", len(prompt))
    blob += prompt.ljust(256, b"\0")
    args.output.write_bytes(blob)
    print(f"wrote {args.output} ({len(blob)} bytes)")


if __name__ == "__main__":
    main()
