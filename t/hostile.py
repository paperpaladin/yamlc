#!/usr/bin/env python3
"""Hostile input: build it, run it through yamlc, and report.

  t/hostile.py YAMLC DIR [--every N]

A parser is a denial-of-service surface, so some of these are about
resource use rather than about syntax. A billion-laughs alias expansion and
a document nested ten thousand deep must both be refused rather than
exhausting memory, and an alias cycle must be refused rather than followed.
The suite proves termination by the fact that this script finishes.

The rest is mechanical: every prefix of a valid document at a stride, and
single bytes of valid documents overwritten with 0xFF, 0x7F, 0x09 and 0x3A,
which is where a length or an indicator turns into something else. None of
these has a right answer. The assertion is that yamlc either reads the
document or refuses it with a position, and never crashes, hangs, or
produces a refusal with no position in it.

Output is machine readable, one fact per line.
"""
import os
import re
import subprocess
import sys

CHUNK = 300

NAMED = {
    # The classic. Nine levels of nine-fold aliasing is 387,420,489 leaf
    # nodes out of a dozen lines.
    "laughs.yaml": "a: &a [x,x,x,x,x,x,x,x,x]\n"
                   "b: &b [*a,*a,*a,*a,*a,*a,*a,*a,*a]\n"
                   "c: &c [*b,*b,*b,*b,*b,*b,*b,*b,*b]\n"
                   "d: &d [*c,*c,*c,*c,*c,*c,*c,*c,*c]\n"
                   "e: &e [*d,*d,*d,*d,*d,*d,*d,*d,*d]\n"
                   "f: &f [*e,*e,*e,*e,*e,*e,*e,*e,*e]\n"
                   "g: &g [*f,*f,*f,*f,*f,*f,*f,*f,*f]\n"
                   "h: &h [*g,*g,*g,*g,*g,*g,*g,*g,*g]\n"
                   "i: &i [*h,*h,*h,*h,*h,*h,*h,*h,*h]\n",
    # Just under the node budget, so that the budget is known to be a
    # boundary and not a wall everything hits.
    "laughs-small.yaml": "a: &a [x,x,x]\nb: &b [*a,*a,*a]\nc: [*b,*b,*b]\n",
    # Cycles. Each of these refers to a node that encloses it.
    "cycle-seq.yaml": "&a [*a]\n",
    "cycle-map.yaml": "&a {b: *a}\n",
    "cycle-nested.yaml": "&a [ &b [ *a ] ]\n",
    "cycle-deep.yaml": "&a\n- x\n- &b\n  - y\n  - *a\n",
    "cycle-key.yaml": "&a {? *a : 1}\n",
    "alias-undefined.yaml": "*nope\n",
    "alias-later.yaml": "a: *x\nb: &x 1\n",
    "anchor-huge.yaml": "&" + "a" * 100000 + " 1\n",
    "many-anchors.yaml": "".join("k%d: &a%d %d\n" % (i, i, i) for i in range(20000)),
    "many-aliases.yaml": "a: &x 1\n" + "".join("k%d: *x\n" % i for i in range(20000)),
    # Unterminated everything.
    "open-quote.yaml": "'" + "a" * 1000,
    "open-dquote.yaml": '"' + "a" * 1000,
    "open-flow-seq.yaml": "[" * 50,
    "open-flow-map.yaml": "{" * 50,
    "open-verbatim-tag.yaml": "!<tag:" + "x" * 1000,
    # Long lines and long scalars.
    "long-line.yaml": "a: " + "x" * 1000000 + "\n",
    "long-key.yaml": "x" * 1000000 + ": 1\n",
    "many-keys.yaml": "".join("k%d: %d\n" % (i, i) for i in range(100000)),
    "many-docs.yaml": "--- 1\n" * 50000,
    # Tabs where indentation goes, which has to be a refusal.
    "tab-indent.yaml": "a:\n\t- 1\n",
    "tab-seq.yaml": "-\n\t- 1\n",
}


def deep(n, text, step=""):
    return text * n


def build(outdir):
    os.makedirs(outdir, exist_ok=True)
    for name, text in NAMED.items():
        with open(os.path.join(outdir, name), "w") as f:
            f.write(text)
    # Ten thousand deep, four ways, all of which must be refused.
    with open(os.path.join(outdir, "deep-flow-seq.yaml"), "w") as f:
        f.write("[" * 10000 + "1" + "]" * 10000 + "\n")
    with open(os.path.join(outdir, "deep-flow-map.yaml"), "w") as f:
        f.write("{a: " * 10000 + "1" + "}" * 10000 + "\n")
    with open(os.path.join(outdir, "deep-block-seq.yaml"), "w") as f:
        f.write("".join("%s- \n" % (" " * (2 * i)) for i in range(10000)))
    with open(os.path.join(outdir, "deep-block-map.yaml"), "w") as f:
        f.write("".join("%sk:\n" % (" " * (2 * i)) for i in range(10000)))
    with open(os.path.join(outdir, "deep-flow-unclosed.yaml"), "w") as f:
        f.write("[" * 10000 + "\n")


# Valid documents whose prefixes and bytes get mangled. Every construct the
# parser has is in here somewhere, so that the sweep reaches the scanners
# and not just the stream loop.
SEEDS = {
    "seed-block": "a: 1\nb:\n  - x\n  - y\nc: |2\n    lit\nd: >-\n  fold\n  more\n",
    "seed-flow": '{"a": [1, 2.5, null, true], b: {c: &x [1], d: *x}}\n',
    "seed-props": "%YAML 1.2\n%TAG !e! tag:example.com,2000:\n--- !e!t\n&a k: !!str v\n? [1,2]\n: {x: y}\n",
    "seed-quoted": "- 'it''s'\n- \"a\\tb\\u00e9\\\n  c\"\n- plain multi\n  line\n",
    "seed-docs": "--- 1\n--- a: 1\n...\n--- |\n  x\n",
}


def mangle(outdir):
    built = []
    for name, text in SEEDS.items():
        raw = text.encode()
        with open(os.path.join(outdir, name + ".yaml"), "wb") as f:
            f.write(raw)
        built.append(name + ".yaml")
        step = max(1, len(raw) // 40)
        for i in range(0, len(raw), step):
            fn = "%s-cut%d.yaml" % (name, i)
            with open(os.path.join(outdir, fn), "wb") as f:
                f.write(raw[:i])
            built.append(fn)
        for b in (0xFF, 0x7F, 0x09, 0x3A, 0x00):
            for i in range(len(raw)):
                fn = "%s-b%d-%02x.yaml" % (name, i, b)
                with open(os.path.join(outdir, fn), "wb") as f:
                    f.write(raw[:i] + bytes([b]) + raw[i + 1:])
                built.append(fn)
    return built


POS = re.compile(r":[0-9]+:[0-9]+: ")


def run(yamlc, outdir, every):
    names = sorted(n for n in os.listdir(outdir) if n.endswith(".yaml"))
    if every > 1:
        heavy = [n for n in names if "-b" in n and n.startswith("seed-")]
        light = [n for n in names if n not in heavy]
        names = sorted(light + heavy[::every])
    paths = [os.path.join(outdir, n) for n in names]
    status = {}
    docs = {}
    crashed = noposition = 0
    for i in range(0, len(paths), CHUNK):
        chunk = paths[i:i + CHUNK]
        out = subprocess.run([yamlc, "dump", "--batch", "--dup=last"] + chunk,
                             capture_output=True, text=True, timeout=900)
        for line in out.stdout.split("\n"):
            parts = line.split("\t", 2)
            if len(parts) != 3:
                continue
            name, st, body = parts
            docs[name] = docs.get(name, 0) + 1
            if st == "err":
                status[name] = "err"
                if not POS.search(body):
                    noposition += 1
                    print("noposition %s %s" % (os.path.basename(name), body))
            else:
                status.setdefault(name, "ok")
        if "panic" in out.stderr or out.returncode > 1:
            crashed += 1
            print("crash chunk %d status %d %s"
                  % (i, out.returncode, out.stderr.strip().split("\n")[0]))
    read = refused = empty = missing = 0
    for p in paths:
        st = status.get(p)
        if st == "ok":
            read += 1
        elif st == "err":
            refused += 1
        elif os.path.getsize(p) == 0:
            # An empty stream holds no documents, so no output is the
            # right answer and not a missing one.
            empty += 1
        else:
            missing += 1
            print("missing %s" % os.path.basename(p))
        base = os.path.basename(p)
        if not base.startswith("seed-"):
            print("file %s %s %d" % (base, st or "empty", docs.get(p, 0)))
    print("built %d" % len(paths))
    print("read %d" % read)
    print("refused %d" % refused)
    print("empty %d" % empty)
    print("crashed %d" % crashed)
    print("missing %d" % missing)
    print("noposition %d" % noposition)
    return 0


def main(argv):
    if len(argv) < 3:
        sys.stderr.write(__doc__)
        return 2
    yamlc, outdir = argv[1], argv[2]
    every = 1
    if "--every" in argv:
        every = int(argv[argv.index("--every") + 1])
    build(outdir)
    mangle(outdir)
    return run(yamlc, outdir, every)


if __name__ == "__main__":
    sys.exit(main(sys.argv))
