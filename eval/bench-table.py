#!/usr/bin/env python3
"""Aggregate eval/bench.sh summaries into Markdown tables.

Usage: python3 eval/bench-table.py <results-dir> [--order a/b,c/d,...]
"""
import glob
import json
import os
import sys


def fmt_time(seconds: float) -> str:
    s = int(round(seconds))
    if s < 60:
        return f"{s}s"
    if s < 3600:
        return f"{s // 60}m {s % 60:02d}s"
    return f"{s // 3600}h {(s % 3600) // 60:02d}m"


def main() -> None:
    if len(sys.argv) < 2:
        sys.exit(__doc__)
    root = sys.argv[1]
    order = []
    if "--order" in sys.argv:
        order = sys.argv[sys.argv.index("--order") + 1].split(",")
    rows = []
    for path in glob.glob(os.path.join(root, "*", "summary.json")):
        rows.append(json.load(open(path)))
    if order:
        rank = {r: i for i, r in enumerate(order)}
        rows.sort(key=lambda r: rank.get(r["repo"], 999))
    else:
        rows.sort(key=lambda r: r["files"])

    print("## Overview\n")
    print("| Repository | Files | Signals | Scan | Report | Peak RSS | High-conf % |")
    print("|------------|------:|--------:|-----:|-------:|---------:|------------:|")
    for r in rows:
        total = r.get("total_signals", 0) or 0
        hi = r.get("confidence", {}).get("high_ge_0.8", 0)
        pct = f"{100 * hi / total:.0f}%" if total else "-"
        print(
            f"| {r['repo']} | {r['files']:,} | {total:,} | {fmt_time(r['scan_seconds'])} "
            f"| {fmt_time(r['report_seconds'])} | {max(r['scan_max_rss_mb'], r['report_max_rss_mb']):,} MB | {pct} |"
        )

    sources = sorted({s for r in rows for s in r.get("by_source", {})})
    print("\n## Signals by collector\n")
    print("| Repository | " + " | ".join(sources) + " |")
    print("|------------|" + "|".join("---:" for _ in sources) + "|")
    for r in rows:
        bs = r.get("by_source", {})
        print(f"| {r['repo']} | " + " | ".join(str(bs.get(s, 0)) for s in sources) + " |")

    print("\n## Slowest collector per repo\n")
    print("| Repository | Collector | Time | Share of report |")
    print("|------------|-----------|-----:|----------------:|")
    for r in rows:
        per = r.get("report_collectors", {})
        if not per:
            continue
        name, v = max(per.items(), key=lambda kv: kv[1]["seconds"])
        share = 100 * v["seconds"] / r["report_seconds"] if r["report_seconds"] else 0
        print(f"| {r['repo']} | {name} | {fmt_time(v['seconds'])} | {share:.0f}% |")

    print("\n## Pinned commits\n")
    print("| Repository | Commit | Date | Errors/Warnings (scan stderr) |")
    print("|------------|--------|------|------------------------------|")
    for r in rows:
        e = r.get("scan_stderr", {})
        print(f"| {r['repo']} | `{r['sha'][:10]}` | {r['sha_date']} | {e.get('errors', 0)}/{e.get('warnings', 0)} |")


if __name__ == "__main__":
    main()
