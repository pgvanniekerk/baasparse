#!/usr/bin/env python3
"""Generate a DSV (CSV) file for benchmarking baasparse.

Usage: gen-dsv.py <fields> <rows> <out.csv>
Writes a header row (f1..fN) plus <rows> data rows of <fields> comma-separated
values (a deterministic mix of ints / strings / decimals / msisdn-like tokens).
No external dependencies.
"""
import sys

WORDS = ["alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel"]


def main():
    if len(sys.argv) != 4:
        sys.exit("usage: gen-dsv.py <fields> <rows> <out.csv>")
    fields, rows, out = int(sys.argv[1]), int(sys.argv[2]), sys.argv[3]
    parts = [",".join("f%d" % i for i in range(1, fields + 1))]
    for r in range(rows):
        vals = []
        for c in range(fields):
            m = c & 3
            if m == 0:
                vals.append(str(r * 100 + c))
            elif m == 1:
                vals.append("%s-%d" % (WORDS[(r + c) % len(WORDS)], c))
            elif m == 2:
                vals.append("%.3f" % ((r + c) * 0.5))
            else:
                vals.append("27%09d" % ((r * 7 + c) % 1000000000))
        parts.append(",".join(vals))
    with open(out, "w") as f:
        f.write("\n".join(parts) + "\n")
    print("wrote %s: %d fields x %d rows" % (out, fields, rows))


if __name__ == "__main__":
    main()
