#!/usr/bin/env python3
"""Compare output equivalence between Craftmake and Snakemake execution runs."""

from __future__ import annotations

import argparse
import hashlib
import json
import sys
from pathlib import Path


def hash_file(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--craftmake-dir", required=True, type=Path)
    parser.add_argument("--snakemake-dir", required=True, type=Path)
    parser.add_argument("--report", required=True, type=Path)
    args = parser.parse_args()

    required_artifacts = [
        "output/samples/sampleA.tsv",
        "output/samples/sampleB.tsv",
        "output/summary.tsv",
    ]

    report = {
        "schema_version": "otter.executor-comparison/v1",
        "craftmake_dir": str(args.craftmake_dir),
        "snakemake_dir": str(args.snakemake_dir),
        "equal": True,
        "artifacts": [],
    }

    for rel in required_artifacts:
        cm_path = args.craftmake_dir / rel
        sm_path = args.snakemake_dir / rel

        if not cm_path.exists():
            report["equal"] = False
            report["artifacts"].append({"path": rel, "status": "missing_in_craftmake"})
            continue
        if not sm_path.exists():
            report["equal"] = False
            report["artifacts"].append({"path": rel, "status": "missing_in_snakemake"})
            continue

        cm_hash = hash_file(cm_path)
        sm_hash = hash_file(sm_path)
        matches = cm_hash == sm_hash
        if not matches:
            report["equal"] = False

        report["artifacts"].append({
            "path": rel,
            "craftmake_sha256": cm_hash,
            "snakemake_sha256": sm_hash,
            "matches": matches,
            "bytes": cm_path.stat().st_size,
        })

    args.report.parent.mkdir(parents=True, exist_ok=True)
    args.report.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")

    if report["equal"]:
        print(f"Executor parity verified: all {len(required_artifacts)} artifacts match byte-for-byte!")
        return 0
    else:
        print("Executor parity failure: differences found between Craftmake and Snakemake!", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
