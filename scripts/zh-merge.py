#!/usr/bin/env python3
"""Merge (key, translation) pairs into internal/i18n/zh-Hans.json, sorted."""
import json
import sys
import os

path = os.path.join(os.path.dirname(__file__), "..", "internal", "i18n", "zh-Hans.json")
path = os.path.normpath(path)


def main():
    pairs = {}
    data = sys.stdin.read()
    for line in data.split("\n"):
        if not line.strip() or line.startswith("#"):
            continue
        key, _, value = line.partition("\t")
        if not value:
            raise SystemExit("bad line: " + line)
        pairs[key.strip()] = value
    with open(path) as f:
        current = json.load(f)
    before = len(current)
    current.update(pairs)
    with open(path, "w") as f:
        json.dump(current, f, ensure_ascii=False, indent=1, sort_keys=True)
        f.write("\n")
    print(f"{before} -> {len(current)} entries (+{len(current) - before})")


main()
