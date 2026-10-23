#!/usr/bin/env python3
"""Fail when maintained Markdown points at a missing local path or heading."""

from __future__ import annotations

import re
import sys
from pathlib import Path
from urllib.parse import unquote


ROOT = Path(__file__).resolve().parent.parent
ROOT_DOCS = (
    "README.md",
    "CHANGELOG.md",
    ".github/CONTRIBUTING.md",
    ".github/SECURITY.md",
    ".github/GOVERNANCE.md",
    ".github/CODE_OF_CONDUCT.md",
)
LINK = re.compile(r"!?\[[^\]]*\]\(([^)]+)\)")
SCHEME = re.compile(r"^[a-zA-Z][a-zA-Z0-9+.-]*:")
HEADING = re.compile(r"^#{1,6}\s+(.+?)\s*#*\s*$")
EXPLICIT_ANCHOR = re.compile(r"<(?:a|span)\b[^>]*(?:id|name)=[\"']([^\"']+)[\"']", re.I)


def markdown_files() -> list[Path]:
    files = [ROOT / name for name in ROOT_DOCS]
    files.extend(sorted((ROOT / "docs").rglob("*.md")))
    files.extend((ROOT / "examples" / name for name in ("README.md", "rust/README.md")))
    return files


def local_target(raw: str) -> tuple[str, str] | None:
    target = raw.strip()
    if target.startswith("<") and ">" in target:
        target = target[1 : target.index(">")]
    else:
        target = target.split(maxsplit=1)[0]
    target = unquote(target)
    if SCHEME.match(target) or target.startswith("//"):
        return None
    path, separator, fragment = target.partition("#")
    return path, fragment if separator else ""


def heading_slug(label: str) -> str:
    label = re.sub(r"\[([^\]]+)\]\([^)]+\)", r"\1", label)
    label = re.sub(r"<[^>]+>", "", label)
    label = label.replace("`", "").lower()
    label = "".join(char for char in label if char.isalnum() or char in " _-")
    return re.sub(r"\s", "-", label.strip())


def anchors(path: Path) -> set[str]:
    found: set[str] = set()
    counts: dict[str, int] = {}
    for line in path.read_text(encoding="utf-8").splitlines():
        for anchor in EXPLICIT_ANCHOR.findall(line):
            found.add(anchor)
        match = HEADING.match(line)
        if not match:
            continue
        base = heading_slug(match.group(1))
        duplicate = counts.get(base, 0)
        counts[base] = duplicate + 1
        found.add(base if duplicate == 0 else f"{base}-{duplicate}")
    return found


def main() -> int:
    missing: list[str] = []
    anchor_cache: dict[Path, set[str]] = {}
    for source in markdown_files():
        if not source.is_file():
            missing.append(f"{source.relative_to(ROOT)}: document does not exist")
            continue
        for line_number, line in enumerate(source.read_text(encoding="utf-8").splitlines(), 1):
            for match in LINK.finditer(line):
                target = local_target(match.group(1))
                if target is None:
                    continue
                path, fragment = target
                resolved = source if not path else (source.parent / path).resolve()
                if not resolved.exists():
                    missing.append(
                        f"{source.relative_to(ROOT)}:{line_number}: missing {path}"
                    )
                    continue
                if fragment and resolved.is_file() and resolved.suffix.lower() == ".md":
                    available = anchor_cache.setdefault(resolved, anchors(resolved))
                    if fragment not in available:
                        missing.append(
                            f"{source.relative_to(ROOT)}:{line_number}: "
                            f"missing heading #{fragment} in {resolved.relative_to(ROOT)}"
                        )
    if missing:
        print("broken local Markdown links:", file=sys.stderr)
        print("\n".join(f"  {item}" for item in missing), file=sys.stderr)
        return 1
    print("Markdown links: clean")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
