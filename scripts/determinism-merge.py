#!/usr/bin/env python3
"""Merge per-platform determinism records into the files the playground serves.

    python3 scripts/determinism-merge.py --out-dir web/dist --commit SHA --source ci rec1.json rec2.json ...

Writes determinism.json (every prompt, every platform's poststate) and badge.json (a
shields.io endpoint: "bit-identical | 6/6 platforms"). Exits non-zero if any platform
disagrees, so CI fails instead of publishing a mismatch.
"""
import argparse
import datetime
import json
import pathlib
import sys


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("records", nargs="+")
    ap.add_argument("--out-dir", required=True)
    ap.add_argument("--commit", default="unknown")
    ap.add_argument("--source", default="local", choices=["ci", "local"])
    args = ap.parse_args()

    records = [json.loads(pathlib.Path(p).read_text()) for p in args.records]
    records.sort(key=lambda r: r["platform"])
    reference = records[0]
    runs, mismatches = [], []
    for i, base in enumerate(reference["runs"]):
        platforms = []
        for rec in records:
            run = rec["runs"][i]
            if run["prompt"] != base["prompt"]:
                print(f"error: {rec['platform']} ran prompts in a different order", file=sys.stderr)
                return 2
            platforms.append({"platform": rec["platform"], "poststate": run["poststate"], "steps": run["steps"]})
            if (run["poststate"], run["prestate"], run["steps"]) != (base["poststate"], base["prestate"], base["steps"]):
                mismatches.append((base["prompt"], rec["platform"], run["poststate"]))
        runs.append({
            "prompt": base["prompt"], "maxNewTokens": base["maxNewTokens"], "steps": base["steps"],
            "prestate": base["prestate"], "poststate": base["poststate"], "text": base.get("text", ""),
            "platforms": platforms,
        })

    identical = not mismatches
    n = len(records)
    out = {
        "generated": datetime.datetime.now(datetime.timezone.utc).isoformat(timespec="seconds"),
        "commit": args.commit,
        "source": args.source,
        "summary": {"platforms": n, "prompts": len(runs), "identical": identical,
                    "platformNames": [r["platform"] for r in records]},
        "runs": runs,
    }
    badge = {
        "schemaVersion": 1,
        "label": "bit-identical",
        "message": f"{n}/{n} platforms" if identical else f"MISMATCH on {len({m[1] for m in mismatches})} of {n}",
        "color": "2a78d6" if identical else "d03b3b",
    }
    d = pathlib.Path(args.out_dir)
    d.mkdir(parents=True, exist_ok=True)
    (d / "determinism.json").write_text(json.dumps(out, indent=2) + "\n")
    (d / "badge.json").write_text(json.dumps(badge) + "\n")
    for prompt, plat, post in mismatches:
        print(f"MISMATCH {plat}: {prompt!r} -> {post}", file=sys.stderr)
    print(f"{len(runs)} prompts x {n} platforms: {'bit-identical' if identical else 'MISMATCH'}")
    return 0 if identical else 1


if __name__ == "__main__":
    sys.exit(main())
