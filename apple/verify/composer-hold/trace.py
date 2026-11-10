#!/usr/bin/env python3
"""Print a TLC counterexample one step per line, in the code's words.

Usage: trace.py <tlc-output> [var ...]
Each action of the spec records itself in `last` = [<who> |-> n, at |-> "file:line", did |-> "what"].
Every line shows the step number, who moved (- for the initial state or the environment), the code
line, what it did, then each listed variable after the step as TLC printed it, squeezed. run.sh
passes the variables named on expect.txt's "# cols:" line. Prints "no trace" when the run found no
violation. Standard library only.
"""
import re
import sys

STATE = re.compile(r"^State (\d+): ")
VAR = re.compile(r"^/\\ (\w+) = (.*)$")
LAST = re.compile(r'\[\s*\w+ \|-> (\d+),\s*at \|-> "(.*?)",\s*did \|-> "(.*?)"\s*\]')
WIDTH = 40


def states(text):
    out, cur, key = [], None, None
    for line in text.splitlines():
        m = STATE.match(line)
        if m:
            cur, key = {"_n": m.group(1)}, None
            out.append(cur)
        elif cur is not None and not line.strip():
            cur, key = None, None
        elif cur is not None:
            v = VAR.match(line)
            if v:
                key = v.group(1)
                cur[key] = v.group(2)
            elif key:
                cur[key] += " " + line.strip()
    return out


def squeeze(value):
    v = re.sub(r"\s+", " ", value).replace(" |-> ", ":").replace('"', "").replace("<<", "(").replace(">>", ")")
    return v if len(v) <= WIDTH else v[:WIDTH - 1] + "…"


def main():
    if len(sys.argv) < 2:
        sys.exit(__doc__)
    text = open(sys.argv[1], encoding="utf-8").read()
    cols = sys.argv[2:]
    head = next((l for l in text.splitlines() if l.startswith("Error: Invariant")), None)
    sts = states(text)
    if not head or not sts:
        print("no trace (no invariant violated)")
        return
    rows = [["#", "who", "code", "step"] + cols]
    for s in sts:
        m = LAST.search(re.sub(r"\s+", " ", s.get("last", "")))
        who, at, did = m.groups() if m else ("?", "?", "?")
        rows.append([s["_n"], who if who != "0" else "-", at or "-", did] + [squeeze(s.get(c, "?")) for c in cols])
    widths = [max(len(r[i]) for r in rows) for i in range(len(rows[0]))]
    print(head.replace("Error: ", ""))
    for r in rows:
        print("  ".join(c.ljust(w) for c, w in zip(r, widths)).rstrip())


if __name__ == "__main__":
    main()
