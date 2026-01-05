#!/usr/bin/env python3
"""Generate YAML documents and compare both parsers on every one.

  t/fuzzcmp.py YAMLC DIR [--count N] [--seed N]

The hand-written cases in t/run.sh are cases somebody thought of. This
builds documents from the grammar instead: block and flow collections to a
random depth, the five scalar styles, chomping and indentation indicators,
anchors and aliases to anchors already defined, tags, several documents per
stream, comments and blank lines in the places they are allowed.

Two comparisons per document, the event stream against PyYAML's and the
1.1 values against safe_load, and both must agree on every document. The
generator deliberately avoids the constructs where yamlc and PyYAML are
already known to differ, which are all in t/run.sh with their causes, so
that anything this reports is a new finding rather than a known one. The
list of what it avoids is in AVOIDED below, and it is part of the result:
a fuzzer that steers around the interesting cases has to say which ones.

The seed is printed and taken, so a difference is reproducible. The
generated documents are kept in DIR for the same reason.
"""
import os
import random
import subprocess
import sys

CHUNK = 300


def need_yaml():
    try:
        import yaml
    except ImportError:
        sys.stderr.write("t/fuzzcmp.py: PyYAML is not installed\n")
        sys.exit(2)
    return yaml

AVOIDED = [
    "a tab inside a plain scalar, or a trailing tab (PyYAML refuses)",
    "U+0085, U+2028, U+2029 (line breaks in 1.1 only)",
    "an empty key written ': v', and '{: v}' (PyYAML refuses)",
    "a JSON-like key with its colon on the next line (PyYAML refuses)",
    "a bare document after '...' (PyYAML refuses)",
    "a surrogate pair escape (PyYAML does not join it)",
    "'<<' and '=' outside a merge (no 1.1 constructor for either)",
    "local tags (safe_load has no constructor for them)",
    "a plain scalar beginning '?' or ':' (PyYAML allows those only in"
    " block context)",
    "two keys of one mapping that are distinct YAML nodes and equal Python"
    " objects: 0 and 0.0, false and 0, 1000 and 1.0e+3. PyYAML's mapping"
    " is a dict, so those collide there and not here",
]

PLAIN = [
    "abc", "Abc", "a b", "a-b", "a.b", "x/y", "a:b", "a#b", "-x",
    "yes", "no", "on", "off", "true", "False", "TRUE", "y", "n",
    "null", "Null", "~", "NULL",
    "0", "7", "-7", "+7", "010", "0x1f", "0o17", "0b101", "1_000",
    "1:30", "12:00:01", "1.5", ".5", "5.", "-1.5", "1e5", "1.0e+3", "1.0E5",
    ".inf", "-.Inf", ".nan", "0.0",
    "2001-12-14", "2001-12-14 21:59:43.10 -5", "2001-12-14t21:59:43.10-05:00",
    "08", "1.2.3", "0x1p3", "1:60", "Yes!", "NULLs", "no.",
]

# A continuation line in a quoted scalar has to be indented deeper than
# the block the scalar sits in, which is s-l+flow-in-block(n+1), so the
# multi-line forms are built against the current indentation rather than
# written out flat. The first version of this file hard-coded two spaces
# and produced 67 invalid documents in 500.
SINGLE = ["abc", "it''s", "a: b", "a # b", "", " x ", "@fold@", "@blank@"]
DOUBLE = ["abc", "", "a\\tb", "a\\nb", "a\\u00e9b", "a\\x41b", "a\\\\b",
          "a\\\"b", "@fold@", "@esc@", "\\U0001F600", "\\ud800", "a\\0b",
          "\\N\\_\\L\\P", "a\\ b"]


def expand(text, indent):
    pad = " " * (indent + 2)
    return (text.replace("@fold@", "a\n" + pad + "b")
                .replace("@blank@", "a\n\n" + pad + "b")
                .replace("@esc@", "a\\\n" + pad + "b"))


def key_pool():
    """Keys come from a narrower pool than values, and the pool is worked
    out by asking PyYAML rather than by hand.

    Once PyYAML has turned a key into a Python dict key, two keys that are
    distinct YAML nodes collide if their Python values are equal:
    0.0 == False == 0, 5 == 5.0, 1000 == 1.0e+3. That is a property of
    Python, not of YAML, and yamlc keeps such keys apart. So the pool keeps
    one representative of each Python-distinct value, which is also the
    widest pool that cannot produce the collision. Hand-listing the pairs
    was tried first and missed 0b101 against 5., which the fuzzer found.

    NaN is left out on its own account: PyYAML builds a fresh one per
    scalar and NaN != NaN, so two NaN keys are two entries in a dict and
    one key here."""
    yaml = need_yaml()
    buckets = {}
    for t in PLAIN:
        if ".nan" in t or ".NaN" in t or ".NAN" in t:
            continue
        try:
            v = yaml.safe_load(t)
            hash(v)
        except Exception:  # noqa: BLE001
            continue
        buckets.setdefault(v, t)
    return sorted(buckets.values())


KEYS = key_pool()


class Gen:
    def __init__(self, rnd):
        self.rnd = rnd
        self.anchors = []
        self.n = 0

    def name(self):
        self.n += 1
        return "a%d" % self.n

    def maybe_props(self, kind):
        """An anchor and/or a tag in front of an inline node."""
        r = self.rnd
        out = ""
        anchor = None
        if r.random() < 0.12:
            anchor = self.name()
            out += "&%s " % anchor
        if r.random() < 0.10:
            out += {"scalar": "!!str ", "seq": "!!seq ", "map": "!!map "}[kind]
        return out, anchor

    def inline(self, depth, indent=0, keyish=False):
        """One line of YAML holding a complete node, except that a quoted
        scalar may fold onto further lines indented past indent.

        keyish draws plain scalars from KEYS rather than PLAIN, for the
        positions where the node becomes a mapping key. Without it the
        explicit key forms, "? k" in block context and "{? k}" in flow,
        reach the full pool and can pair 0 with off in one mapping, which
        is the Python collision in AVOIDED. The fuzzer found that gap on
        its fifth seed, after the pool itself had been fixed."""
        r = self.rnd
        pool = KEYS if keyish else PLAIN
        # An alias in a key position reaches the full value pool through
        # the node it names, which is the third way the Python collision
        # got in. Found on the fifteenth seed: an alias to 5. used as a key
        # beside 0b101.
        if self.anchors and not keyish and r.random() < 0.10:
            return "*" + r.choice(self.anchors)
        choice = r.random()
        if depth > 0 and choice < 0.18:
            pre, anchor = self.maybe_props("seq")
            items = [self.inline(depth - 1, indent) for _ in range(r.randint(0, 3))]
            text = pre + "[" + ", ".join(items) + "]"
            if anchor:
                self.anchors.append(anchor)
            return text
        if depth > 0 and choice < 0.33:
            pre, anchor = self.maybe_props("map")
            items = []
            for _ in range(r.randint(0, 3)):
                k = r.choice(KEYS)
                if r.random() < 0.3:
                    k = "'%s'" % r.choice(["a", "b", "k k"])
                if r.random() < 0.15:
                    items.append("? " + self.inline(depth - 1, indent, True))
                elif r.random() < 0.2:
                    items.append(k)
                else:
                    items.append("%s: %s" % (k, self.inline(depth - 1, indent)))
            text = pre + "{" + ", ".join(items) + "}"
            if anchor:
                self.anchors.append(anchor)
            return text
        pre, anchor = self.maybe_props("scalar")
        if choice < 0.5:
            text = pre + r.choice(pool)
        elif choice < 0.7:
            text = pre + "'" + expand(r.choice(SINGLE), indent) + "'"
        else:
            text = pre + '"' + expand(r.choice(DOUBLE), indent) + '"'
        if anchor:
            self.anchors.append(anchor)
        return text

    def block(self, indent, depth):
        """A list of lines whose first character is at column indent."""
        r = self.rnd
        pad = " " * indent
        if depth <= 0:
            return [pad + self.inline(0, indent)]
        c = r.random()
        if c < 0.30:
            return self.block_seq(indent, depth)
        if c < 0.62:
            return self.block_map(indent, depth)
        if c < 0.74:
            return self.block_scalar(indent)
        return [pad + self.inline(depth - 1, indent)]

    def block_scalar(self, indent):
        r = self.rnd
        style = r.choice("|>")
        chomp = r.choice(["", "", "-", "+"])
        ind = r.choice(["", "", "", "2", "3"])
        head = style + (ind + chomp if r.random() < 0.5 else chomp + ind)
        body_indent = indent + (int(ind) if ind else r.choice([1, 2, 4]))
        lines = [" " * indent + head]
        for _ in range(r.randint(1, 4)):
            w = r.random()
            if w < 0.2:
                lines.append("")
            elif w < 0.35:
                lines.append(" " * (body_indent + r.randint(1, 3)) + "more indented")
            else:
                lines.append(" " * body_indent + r.choice(["x", "y z", "# hash", "a: b"]))
        # The first content line decides the indentation when none is
        # given, so make sure one exists and is not more indented.
        if not any(line.startswith(" " * body_indent) and
                   line[body_indent:body_indent + 1] not in ("", " ")
                   for line in lines[1:]):
            lines.append(" " * body_indent + "x")
        return lines

    def block_seq(self, indent, depth):
        r = self.rnd
        pad = " " * indent
        lines = []
        for _ in range(r.randint(1, 4)):
            if r.random() < 0.12:
                lines.append(pad + "# comment")
            if r.random() < 0.08:
                lines.append("")
            if r.random() < 0.55:
                lines.append(pad + "- " + self.inline(depth - 1, indent))
            elif r.random() < 0.5:
                lines.append(pad + "-")
                lines.extend(self.block(indent + 2, depth - 1))
            else:
                sub = self.block(indent + 2, depth - 1)
                lines.append(pad + "- " + sub[0].lstrip() if False else pad + "-")
                lines.extend(sub)
        return lines

    def block_map(self, indent, depth):
        r = self.rnd
        pad = " " * indent
        lines = []
        for _ in range(r.randint(1, 4)):
            if r.random() < 0.12:
                lines.append(pad + "# comment")
            if r.random() < 0.08:
                lines.append("")
            pre, anchor = ("", None)
            if r.random() < 0.08:
                anchor = self.name()
                pre = "&%s " % anchor
            k = pre + r.choice(KEYS)
            if r.random() < 0.2:
                k = pre + "'" + r.choice(["a", "k k", "1"]) + "'"
            if anchor:
                self.anchors.append(anchor)
            w = r.random()
            if w < 0.10:
                lines.append(pad + "? " + self.inline(depth - 1, indent, True))
                if r.random() < 0.8:
                    lines.append(pad + ": " + self.inline(depth - 1, indent))
            elif w < 0.45:
                lines.append("%s%s: %s" % (pad, k, self.inline(depth - 1, indent)))
            elif w < 0.60:
                lines.append("%s%s:%s" % (pad, k, " # why" if r.random() < 0.5 else ""))
            elif w < 0.80:
                lines.append("%s%s:" % (pad, k))
                lines.extend(self.block(indent + 2, depth - 1))
            else:
                lines.append("%s%s:" % (pad, k))
                lines.extend(self.block_seq(indent, depth - 1))
        return lines

    def document(self):
        r = self.rnd
        self.anchors = []
        lines = []
        if r.random() < 0.12:
            lines.append("%YAML 1.2")
        if lines or r.random() < 0.4:
            head = "---"
            if r.random() < 0.3:
                lines.append(head + " " + self.inline(2, 0))
                if r.random() < 0.3:
                    lines.append("...")
                return "\n".join(lines) + "\n"
            lines.append(head)
        lines.extend(self.block(0, r.randint(1, 4)))
        if r.random() < 0.15:
            lines.append("...")
        return "\n".join(lines) + "\n"

    def stream(self):
        n = self.rnd.randint(1, 3)
        out = []
        for i in range(n):
            d = self.document()
            if i > 0 and not d.startswith(("---", "%")):
                d = "---\n" + d
            out.append(d)
        return "".join(out)


def split_events(text):
    out, name, buf = {}, None, []
    for line in text.split("\n"):
        if line.startswith("=== "):
            if name is not None:
                out[name] = "\n".join(buf)
            name, buf = line[4:], []
        elif name is not None:
            buf.append(line)
    if name is not None:
        out[name] = "\n".join(buf)
    return out


def run_batch(cmd, paths, splitter):
    out = {}
    for i in range(0, len(paths), CHUNK):
        r = subprocess.run(cmd + paths[i:i + CHUNK], capture_output=True,
                           text=True, timeout=900)
        if "panic" in r.stderr:
            print("panic %s" % r.stderr.split("\n")[0])
        out.update(splitter(r.stdout))
    return out


def split_tabbed(text):
    out = {}
    for line in text.split("\n"):
        parts = line.split("\t", 2)
        if len(parts) == 3:
            out.setdefault(parts[0], []).append((parts[1], parts[2]))
    return out


def main(argv):
    if len(argv) < 3:
        sys.stderr.write(__doc__)
        return 2
    yamlc, outdir = argv[1], argv[2]
    count = 2000
    seed = random.randrange(1 << 30)
    if "--count" in argv:
        count = int(argv[argv.index("--count") + 1])
    if "--seed" in argv:
        seed = int(argv[argv.index("--seed") + 1])
    print("seed %d" % seed)
    for line in AVOIDED:
        print("avoided %s" % line)
    os.makedirs(outdir, exist_ok=True)
    for old in os.listdir(outdir):
        os.unlink(os.path.join(outdir, old))
    rnd = random.Random(seed)
    paths = []
    for i in range(count):
        g = Gen(rnd)
        text = g.stream()
        p = os.path.join(outdir, "f%05d.yaml" % i)
        with open(p, "w", encoding="utf-8") as f:
            f.write(text)
        paths.append(p)

    mine = run_batch([yamlc, "events", "--batch"], paths, split_events)
    theirs = run_batch(["python3", "t/pyyaml.py", "events", "--batch"], paths,
                       split_events)
    ev_same = ev_differ = ev_mine_only = ev_theirs_only = ev_both = 0
    shown = 0
    for p in paths:
        a, b = theirs.get(p, "?"), mine.get(p, "?")
        ar, br = a.startswith("!!!"), b.startswith("!!!")
        if ar and br:
            ev_both += 1
        elif ar:
            ev_mine_only += 1
            if shown < 6:
                print("ev-yamlc-only %s %s" % (os.path.basename(p), a.split("\n")[0]))
                shown += 1
        elif br:
            ev_theirs_only += 1
            if shown < 6:
                print("ev-pyyaml-only %s %s" % (os.path.basename(p), b.split("\n")[0]))
                shown += 1
        elif a == b:
            ev_same += 1
        else:
            ev_differ += 1
            if shown < 6:
                print("ev-differ %s" % os.path.basename(p))
                shown += 1

    mv = run_batch([yamlc, "dump", "--schema=1.1", "--dup=last", "--batch"],
                   paths, split_tabbed)
    tv = run_batch(["python3", "t/pyyaml.py", "dump"], paths, split_tabbed)
    v_same = v_differ = v_both_err = v_mine_err = v_theirs_err = 0
    v_unhashable = 0
    shown = 0
    shown_differ = 0
    for p in paths:
        a, b = tv.get(p), mv.get(p)
        aerr = a is None or any(s == "err" for s, _ in a)
        berr = b is None or any(s == "err" for s, _ in b)
        if aerr and berr:
            v_both_err += 1
            continue
        if berr:
            v_mine_err += 1
            if shown < 6:
                print("val-yamlc-only %s %s" % (os.path.basename(p),
                      [x[1] for x in (b or []) if x[0] == "err"][:1]))
                shown += 1
            continue
        if aerr:
            v_theirs_err += 1
            why = " ".join(x[1] for x in (a or []) if x[0] == "err")
            if "unhashable" in why:
                v_unhashable += 1
            elif shown < 6:
                print("val-pyyaml-only %s %s" % (os.path.basename(p), why[:160]))
                shown += 1
            continue
        if [x[1] for x in a] == [x[1] for x in b]:
            v_same += 1
        else:
            v_differ += 1
            if shown_differ < 8:
                print("val-differ-path %s" % os.path.basename(p))
                shown_differ += 1

    print("documents %d" % len(paths))
    print("ev-same %d" % ev_same)
    print("ev-differ %d" % ev_differ)
    print("ev-yamlc-only %d" % ev_mine_only)
    print("ev-pyyaml-only %d" % ev_theirs_only)
    print("ev-both-refused %d" % ev_both)
    print("val-same %d" % v_same)
    print("val-differ %d" % v_differ)
    print("val-both-refused %d" % v_both_err)
    print("val-yamlc-only %d" % v_mine_err)
    print("val-pyyaml-only %d" % v_theirs_err)
    # Nearly all of those are one cause: a sequence or a mapping used as a
    # mapping key, which Python cannot hash. Counted apart so that a new
    # cause appearing in this bucket is visible rather than absorbed.
    print("val-pyyaml-unhashable %d" % v_unhashable)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
