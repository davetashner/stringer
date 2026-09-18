#!/usr/bin/env python3
"""Aggregate eval/bench.sh summaries into Markdown tables.

Usage: python3 eval/bench-table.py <results-dir> [--order a/b,c/d,...]
"""
import glob
import json
import os
import re
import sys


def parse_duration(text: str) -> float:
    """Parse Go duration strings such as 1h2m3.4s, 350ms, 12µs."""
    if text.endswith("ms"):
        return float(text[:-2]) / 1000
    if text.endswith("µs") or text.endswith("ns"):
        return 0.0
    m = re.match(r"(?:(\d+)h)?(?:([\d.]+)m)?(?:([\d.]+)s)?$", text)
    if not m:
        return 0.0
    return int(m.group(1) or 0) * 3600 + float(m.group(2) or 0) * 60 + float(m.group(3) or 0)


def report_internal_seconds(report_path: str) -> float:
    """The pipeline duration printed in the report header (monotonic clock, excludes host sleep)."""
    try:
        for line in open(report_path, errors="replace"):
            m = re.match(r"Duration:\s+(\S+)", line)
            if m:
                return parse_duration(m.group(1))
    except FileNotFoundError:
        pass
    return 0.0


def scan_collector_seconds(stderr_path: str, ncollectors: int = 14) -> float:
    """Sum over workspaces of the slowest collector: the parallel collector phase (monotonic)."""
    ds = []
    try:
        for line in open(stderr_path, errors="replace"):
            m = re.search(r'msg="collector complete" name=\S+ signals=\d+ duration=(\S+)', line)
            if m:
                ds.append(parse_duration(m.group(1)))
    except FileNotFoundError:
        return 0.0
    blocks = [ds[i:i + ncollectors] for i in range(0, len(ds), ncollectors)]
    return sum(max(b) for b in blocks if b)


def collector_times(stderr_path: str) -> dict:
    """Sum each collector's duration across all workspaces from scan stderr."""
    out: dict = {}
    try:
        for line in open(stderr_path, errors="replace"):
            m = re.search(r'msg="collector complete" name=(\S+) signals=\d+ duration=(\S+)', line)
            if m:
                out[m.group(1)] = out.get(m.group(1), 0.0) + parse_duration(m.group(2))
    except FileNotFoundError:
        pass
    return out


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
    print("Scan and report are the collector/pipeline phases on the monotonic clock (host sleep excluded); wall columns are `date`-based.\n")
    print("| Repository | Files | Signals | Scan | Report | Scan wall | Report wall | Peak RSS | High-conf % |")
    print("|------------|------:|--------:|-----:|-------:|----------:|------------:|---------:|------------:|")
    for r in rows:
        total = r.get("total_signals", 0) or 0
        hi = r.get("confidence", {}).get("high_ge_0.8", 0)
        pct = f"{100 * hi / total:.0f}%" if total else "-"
        d = os.path.join(root, r["repo"].replace("/", "-"))
        scan_int = scan_collector_seconds(os.path.join(d, "scan.stderr"))
        rep_int = report_internal_seconds(os.path.join(d, "report.txt"))
        print(
            f"| {r['repo']} | {r['files']:,} | {total:,} | {fmt_time(scan_int)} | {fmt_time(rep_int)} "
            f"| {fmt_time(r['scan_seconds'])} | {fmt_time(r['report_seconds'])} "
            f"| {max(r['scan_max_rss_mb'], r['report_max_rss_mb']):,} MB | {pct} |"
        )

    sources = sorted({s for r in rows for s in r.get("by_source", {})})
    print("\n## Signals by collector\n")
    print("| Repository | " + " | ".join(sources) + " |")
    print("|------------|" + "|".join("---:" for _ in sources) + "|")
    for r in rows:
        bs = r.get("by_source", {})
        print(f"| {r['repo']} | " + " | ".join(str(bs.get(s, 0)) for s in sources) + " |")

    print("\n## Slowest collector per repo (summed across workspaces, from scan stderr)\n")
    print("| Repository | Collector | Time | Share of scan |")
    print("|------------|-----------|-----:|--------------:|")
    for r in rows:
        per = collector_times(os.path.join(root, r["repo"].replace("/", "-"), "scan.stderr"))
        if not per:
            continue
        name, secs = max(per.items(), key=lambda kv: kv[1])
        share = 100 * secs / r["scan_seconds"] if r["scan_seconds"] else 0
        print(f"| {r['repo']} | {name} | {fmt_time(secs)} | {share:.0f}% |")

    print("\n## Pinned commits\n")
    print("| Repository | Commit | Date | Errors/Warnings (scan stderr) |")
    print("|------------|--------|------|------------------------------|")
    for r in rows:
        e = r.get("scan_stderr", {})
        print(f"| {r['repo']} | `{r['sha'][:10]}` | {r['sha_date']} | {e.get('errors', 0)}/{e.get('warnings', 0)} |")


if __name__ == "__main__":
    main()
