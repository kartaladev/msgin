#!/usr/bin/env python3
"""Round-7 join check (counter-rule 6).

For each decision, extract from EVERY bundle document the owning task number(s)
cited near each mention, and report disagreements. A decision's statement, its
gate and its task number are one artifact spread across four files; round 7
found three blockers because nothing compared them.
"""
import re, sys, glob, os, collections

DECISIONS = ["D-J", "D-K", "D-L", "D-M", "D-N", "D-O"]
WINDOW = 200  # chars after a mention in which a "Task N" citation counts as attached

def load(d):
    out = {}
    for p in sorted(glob.glob(os.path.join(d, "*.md"))):
        out[os.path.basename(p)] = open(p, encoding="utf-8").read()
    return out

HEADING = re.compile(r"^#{1,4}\s+Task\s+(\d+(?:\.\d+)?)", re.M)


def enclosing_task(text, pos):
    """The nearest '## Task N' heading at or before pos, if any.

    Proximity alone is a false-NEGATIVE generator: a decision recorded as a
    checkbox inside a task's own section often never repeats the words
    "Task N", so a citation-only scan reports it unowned. Round 7 hit exactly
    this for D-O.
    """
    last = None
    for h in HEADING.finditer(text):
        if h.start() > pos:
            break
        last = h.group(1)
    return last


def tasks_near(text, dec):
    """Task numbers owning each mention of dec.

    Two sources, both reported: an explicit `Task N` citation within WINDOW
    chars after the mention, and the section the mention physically sits in.
    """
    found = collections.Counter()
    for m in re.finditer(r"\b" + re.escape(dec) + r"\b", text):
        seg = text[m.end(): m.end() + WINDOW]
        cited = re.findall(r"Task\s+(\d+(?:\.\d+)?)", seg)
        for t in cited:
            found[t] += 1
        if not cited:
            enc = enclosing_task(text, m.start())
            if enc:
                found[enc + "*"] += 1   # * = inferred from the enclosing section
    return found

def main():
    docs = load(sys.argv[1] if len(sys.argv) > 1 else ".")
    print(f"documents: {len(docs)}\n")
    problems = []
    for dec in DECISIONS:
        rows = []
        for name, text in docs.items():
            n = len(re.findall(r"\b" + re.escape(dec) + r"\b", text))
            if not n:
                continue
            t = tasks_near(text, dec)
            rows.append((name, n, t))
        if not rows:
            print(f"### {dec}: NOT MENTIONED IN ANY BUNDLE DOCUMENT")
            problems.append(f"{dec}: absent from the whole bundle")
            continue
        print(f"### {dec}")
        union = collections.Counter()
        for name, n, t in rows:
            union.update(t)
            shown = ", ".join(f"Task {k}({v})" for k, v in sorted(t.items())) or "— no task cited"
            print(f"    {name:<46} mentions={n:<3} {shown}")
        # disagreement: a task cited for this decision in one doc but not another that mentions it
        cited = set(union)
        if len(cited) > 1:
            for name, n, t in rows:
                missing = cited - set(t)
                if missing and n >= 2:
                    problems.append(
                        f"{dec}: {name} cites {sorted(set(t)) or '[]'} but the bundle also cites {sorted(missing)}")
        print()
    print("=" * 72)
    if problems:
        print("JOIN PROBLEMS\n")
        for p in problems:
            print("  -", p)
    else:
        print("no task-number disagreements found")

main()
