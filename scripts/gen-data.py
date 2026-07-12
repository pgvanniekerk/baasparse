#!/usr/bin/env python3
"""Generate benchmark input files for baasparse in DSV, NDJSON or XML.

Usage: gen-data.py <dsv|ndjson|xml> <fields> <rows> <out>
Fields are f1..fN with a deterministic mix of ints / words / decimals /
msisdn-like tokens (same value pattern as gen-dsv.py, so outputs are
comparable across formats). XML wraps rows in <recs><rec>...</rec></recs>.
"""
import sys

WORDS = ["alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel"]


def cell(r, c):
    m = c & 3
    if m == 0:
        return str(r * 100 + c)
    if m == 1:
        return "%s-%d" % (WORDS[(r + c) % len(WORDS)], c)
    if m == 2:
        return "%.3f" % ((r + c) * 0.5)
    return "27%09d" % ((r * 7 + c) % 1000000000)


def main():
    if len(sys.argv) != 5:
        sys.exit("usage: gen-data.py <dsv|ndjson|xml> <fields> <rows> <out>")
    fmt, fields, rows, out = sys.argv[1], int(sys.argv[2]), int(sys.argv[3]), sys.argv[4]
    names = ["f%d" % i for i in range(1, fields + 1)]
    with open(out, "w") as f:
        if fmt == "dsv":
            f.write(",".join(names) + "\n")
            for r in range(rows):
                f.write(",".join(cell(r, c) for c in range(fields)) + "\n")
        elif fmt == "ndjson":
            for r in range(rows):
                parts = []
                for c in range(fields):
                    v = cell(r, c)
                    if c & 3 in (0, 2):  # int / decimal stay numeric
                        parts.append('"%s":%s' % (names[c], v))
                    else:
                        parts.append('"%s":"%s"' % (names[c], v))
                f.write("{" + ",".join(parts) + "}\n")
        elif fmt == "xml":
            f.write('<?xml version="1.0"?>\n<recs>\n')
            for r in range(rows):
                f.write("<rec>" + "".join(
                    "<%s>%s</%s>" % (names[c], cell(r, c), names[c]) for c in range(fields)
                ) + "</rec>\n")
            f.write("</recs>\n")
        else:
            sys.exit("unknown format %r" % fmt)
    print("wrote %s: %s, %d fields x %d rows" % (out, fmt, fields, rows))


if __name__ == "__main__":
    main()
