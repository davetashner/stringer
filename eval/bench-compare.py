#!/usr/bin/env python3
"""Compare a bench.sh results directory with the v1.10.0 tables in docs/research/benchmark-2026-09.md.

Usage: python3 eval/bench-compare.py <results-dir> [--doc docs/research/benchmark-2026-09.md]
Prints Markdown: per-repo signals / high-confidence share / scan / report before and after,
and per-collector deltas.
"""
import glob
import json
import os
import re
import sys

import importlib.util

_spec = importlib.util.spec_from_file_location(
    "bench_table", os.path.join(os.path.dirname(os.path.abspath(__file__)), "bench-table.py")
)
_bt = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_bt)
fmt_time = _bt.fmt_time
report_internal_seconds = _bt.report_internal_seconds
scan_collector_seconds = _bt.scan_collector_seconds
collector_times = _bt.collector_times


def parse_doc_tables(doc_path: str):
    text = open(doc_path, encoding="utf-8").read()
    before = {}
    # Overview table rows: | repo | lang | files | signals | high | scan | report | wall | rss | feb signals | feb time |
    for m in re.finditer(r"^\| ([\w./-]+)(?: \(new\))? \| [^|]+ \| ([\d,]+) \| ([\d,]+) \| (\d+)% \| ([^|]+) \| ([^|]+) \| [^|]+ \| [^|]+ \| [^|]+ \| [^|]+ \|$", text, re.M):
        repo = m.group(1)
        before[repo] = {"signals": int(m.group(3).replace(",", "")), "high_pct": int(m.group(4)),
                        "scan": m.group(5).strip(), "report": m.group(6).strip()}
    # Signals-by-collector table
    sec = text.split("### Where the signals come from")[1].split("Observations:")[0]
    header = None
    for line in sec.splitlines():
        if line.startswith("| Repository |"):
            header = [h.strip() for h in line.strip("|").split("|")][1:]
        elif header and line.startswith("| ") and not line.startswith("|---"):
            cells = [c.strip() for c in line.strip("|").split("|")]
            repo = cells[0]
            vals = cells[1:]
            if repo in before and len(vals) == len(header):
                before[repo]["by_source"] = {h: int(v.replace(",", "")) for h, v in zip(header, vals)}
    return before


def main():
    if len(sys.argv) < 2:
        sys.exit(__doc__)
    root = sys.argv[1]
    doc = sys.argv[sys.argv.index("--doc") + 1] if "--doc" in sys.argv else "docs/research/benchmark-2026-09.md"
    before = parse_doc_tables(doc)
    rows = []
    for path in glob.glob(os.path.join(root, "*", "summary.json")):
        rows.append(json.load(open(path)))
    rows.sort(key=lambda r: r["files"])

    print("## Signals and timing: v1.10.0 vs main\n")
    print("| Repository | Signals before | Signals after | Change | High before | High after | Scan before | Scan after | Report before | Report after |")
    print("|------------|---------------:|--------------:|-------:|------------:|-----------:|------------:|-----------:|--------------:|-------------:|")
    for r in rows:
        b = before.get(r["repo"])
        d = os.path.join(root, r["repo"].replace("/", "-"))
        total = r.get("total_signals", 0)
        hi = r.get("confidence", {}).get("high_ge_0.8", 0)
        pct = f"{100 * hi / total:.0f}%" if total else "-"
        scan_after = fmt_time(scan_collector_seconds(os.path.join(d, "scan.stderr")))
        rep_after = fmt_time(report_internal_seconds(os.path.join(d, "report.txt")))
        if b:
            change = f"{100 * (total - b['signals']) / b['signals']:+.0f}%"
            print(f"| {r['repo']} | {b['signals']:,} | {total:,} | {change} | {b['high_pct']}% | {pct} | {b['scan']} | {scan_after} | {b['report']} | {rep_after} |")
        else:
            print(f"| {r['repo']} | - | {total:,} | - | - | {pct} | - | {scan_after} | - | {rep_after} |")

    sources = sorted({s for r in rows for s in r.get("by_source", {})} | {s for b in before.values() for s in b.get("by_source", {})})
    print("\n## Signals by collector: before -> after\n")
    print("| Repository | " + " | ".join(sources) + " |")
    print("|------------|" + "|".join("---:" for _ in sources) + "|")
    for r in rows:
        b = before.get(r["repo"], {}).get("by_source", {})
        a = r.get("by_source", {})
        cells = []
        for s in sources:
            bv, av = b.get(s), a.get(s, 0)
            cells.append(f"{bv:,} -> {av:,}" if bv is not None else f"{av:,}")
        print(f"| {r['repo']} | " + " | ".join(cells) + " |")

    print("\n## Slowest collector after\n")
    print("| Repository | Collector | Time | Share of scan |")
    print("|------------|-----------|-----:|--------------:|")
    for r in rows:
        per = collector_times(os.path.join(root, r["repo"].replace("/", "-"), "scan.stderr"))
        if not per:
            continue
        name, secs = max(per.items(), key=lambda kv: kv[1])
        sc = scan_collector_seconds(os.path.join(root, r["repo"].replace("/", "-"), "scan.stderr"))
        print(f"| {r['repo']} | {name} | {fmt_time(secs)} | {100 * secs / sc if sc else 0:.0f}% |")


if __name__ == "__main__":
    main()
