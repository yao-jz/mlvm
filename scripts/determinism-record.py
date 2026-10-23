#!/usr/bin/env python3
"""Record the demo's state roots for the determinism matrix on this machine.

Runs `mlvm demo --json` for every prompt in web/determinism-prompts.json and writes one
JSON record: the platform label plus prestate, poststate and step count per prompt.
scripts/determinism-merge.py combines records from every platform.

    python3 scripts/determinism-record.py --mlvm bin/mlvm --platform linux/amd64 --out rec.json
"""
import argparse
import json
import pathlib
import platform
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--mlvm", default=str(ROOT / "bin" / "mlvm"))
    ap.add_argument("--platform", required=True, help="label such as linux/amd64")
    ap.add_argument("--runner", default=platform.platform())
    ap.add_argument("--out", required=True)
    args = ap.parse_args()

    spec = json.loads((ROOT / "web" / "determinism-prompts.json").read_text())
    runs = []
    for prompt in spec["prompts"]:
        out = subprocess.run(
            [args.mlvm, "demo", "--json", "--max-new-tokens", str(spec["maxNewTokens"]), "--", prompt],
            check=True, capture_output=True, text=True, encoding="utf-8",
        ).stdout
        r = json.loads(out)
        runs.append({
            "prompt": prompt,
            "maxNewTokens": spec["maxNewTokens"],
            "steps": r["steps"],
            "prestate": r["preState"],
            "poststate": r["postState"],
            "text": r.get("generatedText", ""),
        })
        print(f"{args.platform:16} {r['postState']}  {prompt!r}", file=sys.stderr)
    version = subprocess.run([args.mlvm, "--version"], capture_output=True, text=True, encoding="utf-8").stdout.strip()
    record = {"platform": args.platform, "runner": args.runner, "mlvm": version, "runs": runs}
    pathlib.Path(args.out).write_text(json.dumps(record, indent=2) + "\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
