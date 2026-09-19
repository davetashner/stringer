#!/usr/bin/env python3
"""Benchmark regression check: compare an eval/bench.sh results dir with eval/baseline/.

Usage:
  python3 eval/bench-check.py <results-dir> [--markdown FILE]   # check, exit 1 on drift
  python3 eval/bench-check.py --update <results-dir>            # rewrite the baselines
  python3 eval/bench-check.py --targets                         # print owner/repo@sha targets

Signal counts must match the baseline exactly (the inputs are pinned commits),
unless a collector entry in the baseline carries a "tolerance" (absolute count).
Kinds listed under "ignored_kinds" depend on the wall clock rather than on the
pinned inputs and are left out of the comparison. Durations are machine-dependent,
so a collector only fails the check when it exceeds perf.max_seconds, or when it
exceeds both perf.max_ratio x its baseline and perf.ratio_floor_seconds.
Standard library only.
"""
import argparse
import datetime
import glob
import importlib.util
import json
import os
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
BASELINE_DIR = os.path.join(HERE, "baseline")
# Network- and advisory-database-dependent collectors: bench.sh must run with these excluded.
NETWORK_COLLECTORS = ["github", "vuln", "dephealth"]
# Signal kinds whose count depends on today's date, not on the pinned commit:
#   gitlog churn: "modified N times in the last 90 days" relative to now;
#   gitlog stale-branch: branch tip older than 30 days relative to now;
#   docstale doc-code-drift: co-change window is `git log --since=1y` relative to now.
DEFAULT_IGNORED_KINDS = {"gitlog": ["churn", "stale-branch"], "docstale": ["doc-code-drift"]}
DEFAULT_PERF = {"max_seconds": 60, "max_ratio": 10, "ratio_floor_seconds": 5}

_spec = importlib.util.spec_from_file_location("bench_table", os.path.join(HERE, "bench-table.py"))
_bt = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_bt)


def load_results(results_dir: str) -> dict:
    """Map repo -> {"summary": ..., "collectors": {name: {"signals", "kinds", "seconds"}}}."""
    out = {}
    for path in sorted(glob.glob(os.path.join(results_dir, "*", "summary.json"))):
        summary = json.load(open(path))
        by_kind = summary.get("by_source_kind", {})
        times = _bt.collector_times(os.path.join(os.path.dirname(path), "scan.stderr"))
        collectors = {}
        for name in sorted(set(times) | set(by_kind)):
            kinds = by_kind.get(name, {})
            collectors[name] = {"signals": sum(kinds.values()), "kinds": kinds,
                                "seconds": round(times.get(name, 0.0), 2)}
        out[summary["repo"]] = {"summary": summary, "collectors": collectors}
    return out


def baseline_path(repo: str) -> str:
    return os.path.join(BASELINE_DIR, repo.replace("/", "-") + ".json")


def load_baselines() -> dict:
    return {b["repo"]: b for b in (json.load(open(p)) for p in sorted(glob.glob(os.path.join(BASELINE_DIR, "*.json"))))}


def counted(entry: dict, ignored: list) -> tuple:
    kinds = {k: v for k, v in entry.get("kinds", {}).items() if k not in ignored}
    return sum(kinds.values()), kinds


def check_repo(base: dict, res: dict) -> tuple:
    """Return (problems, rows): repo-level problems, and one table row per collector."""
    problems, rows = [], []
    s = res["summary"]
    if s.get("sha") != base["sha"]:
        problems.append(f"scanned {s.get('sha')} but the baseline is pinned to {base['sha']}")
    if s.get("clone_depth") != base["depth"]:
        problems.append(f"clone depth {s.get('clone_depth')} != baseline depth {base['depth']}")
    if s.get("scan_exit", 0) != 0:
        problems.append(f"stringer scan exited {s['scan_exit']} (see scan.stderr)")
    if s.get("scan_stderr", {}).get("panics"):
        problems.append("scan.stderr mentions a panic")
    ignored_map = base.get("ignored_kinds", DEFAULT_IGNORED_KINDS)
    perf = {**DEFAULT_PERF, **base.get("perf", {})}
    bcol, rcol = base["collectors"], res["collectors"]
    for name in sorted(set(bcol) | set(rcol)):
        if name in NETWORK_COLLECTORS and name in rcol:
            problems.append(f"{name} ran; run bench.sh with --exclude {','.join(NETWORK_COLLECTORS)}")
        ignored = ignored_map.get(name, [])
        b, r = bcol.get(name, {}), rcol.get(name, {})
        bn, bk = counted(b, ignored)
        rn, rk = counted(r, ignored)
        tol = b.get("tolerance", 0)
        status = []
        diffs = [f"{k} {bk.get(k, 0)}->{rk.get(k, 0)}" for k in sorted(set(bk) | set(rk)) if bk.get(k, 0) != rk.get(k, 0)]
        if abs(rn - bn) > tol or (tol == 0 and diffs):
            status.append("COUNT " + ", ".join(diffs))
        if name not in rcol:
            status.append("MISSING (collector did not run)")
        bs, rs = b.get("seconds", 0.0), r.get("seconds", 0.0)
        if rs > perf["max_seconds"]:
            status.append(f"SLOW {rs:.1f}s > {perf['max_seconds']}s ceiling")
        elif bs > 0 and rs > perf["max_ratio"] * bs and rs > perf["ratio_floor_seconds"]:
            status.append(f"SLOW {rs / bs:.0f}x baseline")
        rows.append((name, bn, rn, tol, bs, rs, "; ".join(status) or "ok"))
    return problems, rows


def failed_rows(rows: list) -> list:
    return [r for r in rows if r[-1] != "ok"]


def render(repo: str, sha: str, rows: list, problems: list) -> str:
    bad = problems or failed_rows(rows)
    lines = [f"### {repo} @ `{sha[:10]}`: {'FAIL' if bad else 'ok'}", "",
             "| Collector | Baseline | Now | Tol | Base s | Now s | Status |",
             "|-----------|---------:|----:|----:|-------:|------:|--------|"]
    lines += [f"| {n} | {bn} | {rn} | {t} | {bs:.2f} | {rs:.2f} | {st} |" for n, bn, rn, t, bs, rs, st in rows]
    if problems:
        lines += [""] + [f"- {p}" for p in problems]
    return "\n".join(lines) + "\n"


def update(results: dict) -> None:
    os.makedirs(BASELINE_DIR, exist_ok=True)
    for repo, res in results.items():
        s = res["summary"]
        old = {}
        if os.path.exists(baseline_path(repo)):
            old = json.load(open(baseline_path(repo)))
        collectors = {}
        for name, c in res["collectors"].items():
            if name in NETWORK_COLLECTORS:
                sys.exit(f"{repo}: {name} ran; re-run bench.sh with --exclude {','.join(NETWORK_COLLECTORS)}")
            entry = {"signals": c["signals"], "kinds": c["kinds"], "seconds": c["seconds"]}
            if old.get("collectors", {}).get(name, {}).get("tolerance"):
                entry["tolerance"] = old["collectors"][name]["tolerance"]
            collectors[name] = entry
        base = {
            "repo": repo, "sha": s["sha"], "sha_date": s.get("sha_date"), "depth": s["clone_depth"],
            "generated": datetime.date.today().isoformat(), "stringer": s.get("stringer"),
            "source_commit": subprocess.run(["git", "-C", HERE, "rev-parse", "HEAD"], capture_output=True,
                                            text=True).stdout.strip() or None,
            "ignored_kinds": old.get("ignored_kinds", DEFAULT_IGNORED_KINDS),
            "perf": old.get("perf", DEFAULT_PERF), "collectors": collectors,
        }
        # One line per collector keeps the file short and its diffs readable.
        head = ",\n".join(f"  {json.dumps(k)}: {json.dumps(v)}" for k, v in base.items() if k != "collectors")
        body = ",\n".join(f"    {json.dumps(n)}: {json.dumps(c)}" for n, c in sorted(collectors.items()))
        with open(baseline_path(repo), "w") as f:
            f.write("{\n" + head + ',\n  "collectors": {\n' + body + "\n  }\n}\n")
        print(f"wrote {os.path.relpath(baseline_path(repo))}")


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("results", nargs="?", help="eval/bench.sh --out directory")
    ap.add_argument("--update", action="store_true", help="rewrite eval/baseline/ from the results")
    ap.add_argument("--targets", action="store_true", help="print the pinned owner/repo@sha targets")
    ap.add_argument("--markdown", help="also write the report as Markdown to this file")
    args = ap.parse_args()

    baselines = load_baselines()
    if args.targets:
        print(" ".join(f"{b['repo']}@{b['sha']}" for b in baselines.values()))
        return 0
    if not args.results:
        ap.error("results dir required")
    results = load_results(args.results)
    if not results:
        print(f"no summary.json files under {args.results}", file=sys.stderr)
        return 2
    if args.update:
        update(results)
        return 0

    report, drift = [], []
    for repo, base in baselines.items():
        if repo not in results:
            report.append(f"### {repo}: FAIL\n\n- no results (clone or scan failed?)\n")
            drift.append(f"- {repo}: no results")
            continue
        problems, rows = check_repo(base, results[repo])
        drift += [f"- {repo}: {p}" for p in problems] + [f"- {repo} {r[0]}: {r[-1]}" for r in failed_rows(rows)]
        report.append(render(repo, base["sha"], rows, problems))
    failed = bool(drift)
    if failed:
        report.insert(0, "## Drift\n\n" + "\n".join(drift) + "\n")
    verdict = ("**Benchmark regression check failed.** If the change is intended, run "
               "`python3 eval/bench-check.py --update <results-dir>` and commit eval/baseline/ "
               "(see eval/README.md)." if failed else "Benchmark regression check passed.")
    text = "\n".join(report) + "\n" + verdict + "\n"
    print(text)
    if args.markdown:
        with open(args.markdown, "w") as f:
            f.write(text)
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
