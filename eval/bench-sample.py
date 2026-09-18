#!/usr/bin/env python3
"""Sample signals from a bench.sh result directory for a manual precision check.

Usage: python3 eval/bench-sample.py <result-dir> [--top N] [--kind-sample N] [--seed S]

Prints the N highest-confidence signals and N deterministic samples from the
most numerous kind, with enough context to judge each against the source.
"""
import collections
import json
import os
import random
import sys


def main() -> None:
    args = sys.argv[1:]
    if not args:
        sys.exit(__doc__)
    d = args[0]
    top = int(args[args.index("--top") + 1]) if "--top" in args else 10
    ks = int(args[args.index("--kind-sample") + 1]) if "--kind-sample" in args else 10
    seed = int(args[args.index("--seed") + 1]) if "--seed" in args else 2026
    sigs = json.load(open(os.path.join(d, "scan.json")))["signals"]
    kinds = collections.Counter(s["Kind"] for s in sigs)
    biggest = kinds.most_common(1)[0][0]

    def show(s, i):
        desc = (s.get("Description") or "").strip().replace("\n", " ")[:240]
        print(f"{i:2d}. [{s['Source']}/{s['Kind']}] conf={s['Confidence']:.2f} {s['FilePath']}:{s.get('Line', 0)}")
        print(f"    {s['Title'][:160]}")
        if desc:
            print(f"    {desc}")

    print(f"# {len(sigs)} signals; kinds: {dict(kinds.most_common(8))}\n")
    print(f"## Top {top} by confidence")
    for i, s in enumerate(sorted(sigs, key=lambda s: -s["Confidence"])[:top], 1):
        show(s, i)
    pool = [s for s in sigs if s["Kind"] == biggest]
    random.Random(seed).shuffle(pool)
    print(f"\n## {ks} sampled from most numerous kind: {biggest} ({len(pool)})")
    for i, s in enumerate(pool[:ks], 1):
        show(s, i)


if __name__ == "__main__":
    main()
