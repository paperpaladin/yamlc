#!/usr/bin/env python3
"""Compare yamlc against the YAML test suite, which is the YAML project's own.

  t/yts.py YAMLC DIR

DIR is where the data is kept. The tarball for one pinned tag is fetched
once and unpacked there; a later run reuses it. Without a network and
without a cached copy the script prints "unavailable" and a reason, and
t/run.sh turns that into a visible skip rather than a smaller pass count
that looks like a clean run.

What this oracle settles that nothing else here does: PyYAML implements
YAML 1.1, so it cannot say whether a 1.2 reading is right, and the json
module only covers the JSON subset. The test suite is 402 cases written
by the people who wrote the specification, and each one carries the event
stream a conformant parser must produce, the JSON the 1.2 core schema
must resolve it to, or a marker saying the document is invalid.

Every case yamlc gets wrong is in KNOWN below with its cause. The script
reports a failure that is not in that list as "unexpected" and a case in
the list that now passes as "fixed", and t/run.sh requires both to be
zero, so neither a regression nor a quiet repair can pass unnoticed.
"""
import hashlib
import json
import os
import struct
import subprocess
import sys
import tarfile
import urllib.request

TAG = "data-2022-01-17"
URL = "https://codeload.github.com/yaml/yaml-test-suite/tar.gz/refs/tags/" + TAG

# sha256 over the unpacked case files, not over the tarball: GitHub
# generates the archive on demand and has changed its gzip settings
# before, so the bytes of the download are not a stable identity and the
# contents are.
DIGEST = "54e364c611c649a673c918ce58f708714bc841b898ea9e878afffdc19ff186d6"

CHUNK = 200

# case id -> (kind, cause). kind is "read" for a valid document yamlc
# refuses, and "refuse" for an invalid one it reads. The cause is what
# the parser does wrong, in a few words; the README groups them.
KNOWN = {
    # A node property written on one line, with the node it belongs to on
    # the next, where that node is a block mapping whose own first key
    # carries a property too. yamlc gives both properties to one node.
    "26DV": ("read", "anchor on a block mapping, alias as its first key"),
    "6BFJ": ("read", "anchor on a block mapping and on its first key"),
    "7BMT": ("read", "anchor on a block mapping and on its first key"),
    "9KAX": ("read", "anchor and tag on separate lines before a node"),
    "U3XV": ("read", "anchor on a block mapping and on its first key"),
    "FH7J": ("read", "a tag on an empty key, read as a tag on the mapping"),
    "PW8X": ("read", "an anchor on an empty key, read as one on the mapping"),

    # A plain scalar may begin with ":" when what follows is not space,
    # in flow context as well as block.
    "58MP": ("read", "plain scalar beginning with : as a flow map value"),
    "5T43": ("read", "plain scalar beginning with : after a JSON-like key"),
    "HM87/00": ("read", "plain scalar beginning with : in a flow sequence"),
    "DBG4": ("read", "plain scalar beginning with :: in a flow sequence"),

    # The : of a flow mapping entry may be on a later line than its key.
    "4MUZ/02": ("read", "flow mapping colon on the line after a plain key"),
    "VJP3/01": ("read", "flow mapping colon on its own line"),

    # A tab is separation whitespace, so it may precede a flow collection
    # at the top level, and may separate items inside one.
    "6CA3": ("read", "tab before a top level flow collection"),
    "Q5MG": ("read", "tab before a top level flow collection"),
    "6HB6": ("read", "a comment inside a flow collection"),

    # A block scalar at indentation zero, which the content indentation
    # rules allow after --- .
    "DK3J": ("read", "block scalar content at column 1"),
    "FP8R": ("read", "block scalar content at column 1"),
    "M7A3": ("read", "block scalar content at column 1, then a bare document"),
    "W4TN": ("read", "block scalar content at column 1 after a directive"),
    "JEF9/01": ("read", "a block scalar holding only a blank line"),

    # White space that an escape produced is content and must survive the
    # trimming that folding does to the white space that was written as
    # white space. yamlc trims the produced bytes, so it loses both.
    "DE56/00": ("event", "an escaped tab at the end of a folded line"),
    "DE56/01": ("event", "an escaped tab at the end of a folded line"),
    "DE56/02": ("event", "an escaped tab at the end of a folded line"),
    "DE56/03": ("event", "an escaped tab at the end of a folded line"),
    "NP9H": ("event", "an escaped space beside a tab across a folded line"),
    "Q8AD": ("event", "an escaped space beside a tab across a folded line"),

    # A block scalar whose last line is white space only.
    "JEF9/02": ("event", "a block scalar holding one line of spaces, kept"),
    "L24T/01": ("event", "a trailing line of spaces inside a block scalar"),

    # Documents yamlc reads and the suite marks invalid.
    "9C9N": ("refuse", "a flow sequence continued at a lesser indentation"),
    "9JBA": ("refuse", "a comment with no space after a flow sequence"),
    "SU5Z": ("refuse", "a comment with no space after a quoted scalar"),
    "X4QW": ("refuse", "a comment with no space after a block scalar header"),
    "9HCY": ("refuse", "a directive with no document end marker before it"),
    "EB22": ("refuse", "a directive with no document end marker before it"),
    "RHX7": ("refuse", "a directive with no document end marker before it"),
    "MUS6/00": ("refuse", "a directive with no document end marker before it"),
    "9KBC": ("refuse", "a block mapping beginning on the --- line"),
    "CXX2": ("refuse", "an anchor on the --- line, then a block mapping"),
    "C2SP": ("refuse", "a flow mapping key spread over two lines"),
    "ZXT5": ("refuse", "an implicit key with its value on the next line"),
    "Y79Y/000": ("refuse", "a tab where indentation is expected"),
    "Y79Y/003": ("refuse", "a tab where indentation is expected"),
    "Y79Y/005": ("refuse", "a tab where indentation is expected"),
}


def fetch(dirname):
    """Return the directory holding the unpacked cases, or raise."""
    root = os.path.join(dirname, TAG)
    if os.path.isdir(root):
        return root
    os.makedirs(dirname, exist_ok=True)
    tgz = os.path.join(dirname, TAG + ".tar.gz")
    if not os.path.exists(tgz):
        with urllib.request.urlopen(URL, timeout=60) as r:
            body = r.read()
        tmp = tgz + ".part"
        with open(tmp, "wb") as f:
            f.write(body)
        os.replace(tmp, tgz)
    with tarfile.open(tgz) as t:
        # The archive's top directory is named for the tag, and nothing
        # in it is a link to anywhere outside.
        for m in t.getmembers():
            if m.name.startswith("/") or ".." in m.name.split("/"):
                raise ValueError("archive member escapes the directory: %s" % m.name)
        t.extractall(dirname)
    inner = os.path.join(dirname, "yaml-test-suite-" + TAG)
    os.replace(inner, root)
    return root


def cases(root):
    """Every case directory, in a fixed order.

    A case is a directory holding in.yaml. Some ids hold numbered
    subdirectories instead, one case each. "name" and "tags" are
    directories of symbolic links to the same cases under another
    spelling, and counting those would count 350 cases twice.
    """
    out = []
    for d in sorted(os.listdir(root)):
        if d in ("name", "tags"):
            continue
        p = os.path.join(root, d)
        if not os.path.isdir(p):
            continue
        if os.path.exists(os.path.join(p, "in.yaml")):
            out.append((d, p))
            continue
        for s in sorted(os.listdir(p)):
            q = os.path.join(p, s)
            if os.path.isdir(q) and os.path.exists(os.path.join(q, "in.yaml")):
                out.append((d + "/" + s, q))
    return out


def digest(root, ids):
    h = hashlib.sha256()
    for cid, path in ids:
        for name in ("in.yaml", "test.event", "in.json", "error"):
            f = os.path.join(path, name)
            if not os.path.exists(f):
                continue
            with open(f, "rb") as fh:
                body = fh.read()
            h.update(("%s/%s %d\n" % (cid, name, len(body))).encode())
            h.update(body)
    return h.hexdigest()


def batch(yamlc, cmd, paths, args):
    """Run one yamlc command over many files and return name -> (ok, body)."""
    out = {}
    for i in range(0, len(paths), CHUNK):
        r = subprocess.run([yamlc, cmd, "--batch"] + args + paths[i:i + CHUNK],
                           capture_output=True, text=True)
        # split("\n") rather than splitlines(), which also breaks on
        # U+0085 and U+2028: a scalar holding one would split a line of
        # output in two. That is the divergence this project is about,
        # arriving in the harness.
        for line in r.stdout.split("\n"):
            parts = line.split("\t", 2)
            if len(parts) != 3:
                continue
            name, status, body = parts
            out.setdefault(name, []).append((status, body))
    return out


def main(argv):
    if len(argv) != 3:
        sys.stderr.write(__doc__)
        return 2
    yamlc, dirname = argv[1], argv[2]
    try:
        root = fetch(dirname)
    except Exception as e:  # noqa: BLE001
        print("unavailable %s" % str(e).replace("\n", " "))
        return 0
    ids = cases(root)
    print("cases %d" % len(ids))
    d = digest(root, ids)
    print("digest %s" % d)
    print("digest-match %s" % ("yes" if d == DIGEST else "no"))

    invalid = {cid for cid, p in ids if os.path.exists(os.path.join(p, "error"))}
    print("error-cases %d" % len(invalid))

    paths = [os.path.join(p, "in.yaml") for _, p in ids]
    bypath = {os.path.join(p, "in.yaml"): cid for cid, p in ids}
    # --dup=last because the suite's expected output keeps the last value
    # of a repeated key, as PyYAML does; refusing is yamlc's default and
    # is a separate decision, asserted elsewhere.
    got = batch(yamlc, "check", paths, ["--dup=last"])

    failures = {}
    read = refused = 0
    for path in paths:
        cid = bypath[path]
        lines = got.get(path, [])
        ok = bool(lines) and lines[0][0] == "ok"
        if ok:
            read += 1
        else:
            refused += 1
        if cid in invalid and ok:
            failures[cid] = ("refuse", "yamlc reads a document the suite marks invalid")
        elif cid not in invalid and not ok:
            why = lines[0][1] if lines else "no output"
            failures[cid] = ("read", why)
    print("read %d" % read)
    print("refused %d" % refused)

    # The event comparison. test.event is what a conformant parser must
    # produce, in the shape events.go writes, so this is a byte
    # comparison with no translation step in between. A case already
    # counted above as read-or-refused is left alone: one case produces
    # at most one entry, or KNOWN would need two spellings for one bug.
    evsame = evdiffer = 0
    for cid, path in ids:
        if cid in invalid or cid in failures:
            continue
        with open(os.path.join(path, "test.event")) as f:
            want = f.read().strip("\n")
        r = subprocess.run([yamlc, "events", os.path.join(path, "in.yaml")],
                           capture_output=True, text=True)
        got = r.stdout.strip("\n")
        if r.returncode == 0 and got == want:
            evsame += 1
            continue
        evdiffer += 1
        failures[cid] = ("event", firstdiff(want, got))
    print("event-same %d" % evsame)
    print("event-differ %d" % evdiffer)

    # in.json is what the 1.2 core schema must resolve the document to,
    # which is the one thing in this project that had no outside
    # authority at all: PyYAML answers for 1.1 and the json module only
    # for documents that are already JSON.
    # Three cases carry both an error marker and an in.json, left from
    # when the suite still thought the document was valid. The marker
    # wins: there is no right JSON for a document that must be refused.
    withjson = [(cid, p) for cid, p in ids
                if cid not in invalid
                and os.path.exists(os.path.join(p, "in.json"))]
    jpaths = [os.path.join(p, "in.yaml") for _, p in withjson]
    jgot = batch(yamlc, "json", jpaths, ["--dup=last"])
    jsame = jdiffer = jnotag = 0
    for cid, p in withjson:
        if cid in failures:
            continue
        with open(os.path.join(p, "in.json")) as f:
            want = jsonvalues(f.read())
        lines = jgot.get(os.path.join(p, "in.yaml"), [])
        err = [b for s, b in lines if s == "err"]
        if err:
            # yamlc json refuses a node whose tag it does not interpret,
            # and carrying tags rather than dispatching on them is a
            # stated non-goal. Counted apart so that the number is
            # visible and a new cause in this bucket turns the suite red.
            if all("has no JSON form" in e for e in err):
                jnotag += 1
                continue
            jdiffer += 1
            failures[cid] = ("json", err[0])
            continue
        try:
            mine = [json.loads(b) for s, b in lines if s == "ok"]
        except ValueError as e:
            jdiffer += 1
            failures[cid] = ("json", "output is not JSON: %s" % e)
            continue
        if strict_same(want, mine):
            jsame += 1
        else:
            jdiffer += 1
            failures[cid] = ("json", "want %.90s got %.90s" % (want, mine))
    print("json-cases %d" % len(withjson))
    print("json-same %d" % jsame)
    print("json-differ %d" % jdiffer)
    print("json-no-form %d" % jnotag)

    report(failures)
    return 0


def jsonvalues(s):
    """in.json holds one JSON value per document, written end to end."""
    dec = json.JSONDecoder()
    out = []
    i = 0
    while True:
        while i < len(s) and s[i].isspace():
            i += 1
        if i >= len(s):
            return out
        v, i = dec.raw_decode(s, i)
        out.append(v)


def strict_same(a, b):
    """Compare two readings of a document, with two deliberate holes.

    t/jsoncmp.py compares key order and compares a float by its bits,
    because there the oracle is json.load over a JSON file and both of
    those are properties of the document. Here the oracle is the suite's
    in.json, which is generated, and neither property survived the
    generator. Two cases prove it rather than two guesses:

    RR7F is written a then d, its own test.event says a then d, and its
    in.json says d then a. So in.json does not record key order and
    comparing it would report a defect that the suite's other file
    contradicts.

    UGM3 writes price: 450.00, which the 1.2 core schema resolves to a
    float, and its in.json says 450. So in.json does not separate an
    integer from a float of the same value. What does separate them is
    test.event, which keeps the scalar as written, and the json module
    comparison over 2,753 documents, which is strict about type. Both
    run here, so the hole is covered rather than merely accepted.
    """
    if isinstance(a, bool) != isinstance(b, bool):
        return False
    if isinstance(a, (int, float)) and isinstance(b, (int, float)):
        if isinstance(a, float) and isinstance(b, float):
            if a != a and b != b:
                return True
            return struct.pack("<d", a) == struct.pack("<d", b)
        return a == b
    if type(a) is not type(b):
        return False
    if isinstance(a, dict):
        if set(a.keys()) != set(b.keys()):
            return False
        return all(strict_same(a[k], b[k]) for k in a)
    if isinstance(a, list):
        return len(a) == len(b) and all(strict_same(x, y) for x, y in zip(a, b))
    return a == b


def firstdiff(want, got):
    """The first line the two event streams disagree on."""
    a, b = want.split("\n"), got.split("\n")
    for i in range(max(len(a), len(b))):
        x = a[i] if i < len(a) else "(end)"
        y = b[i] if i < len(b) else "(end)"
        if x != y:
            return "line %d: want %s, got %s" % (i + 1, x, y)
    return "same"


def report(failures):
    unexpected = 0
    for cid in sorted(failures):
        kind, why = failures[cid]
        if KNOWN.get(cid, (None,))[0] != kind:
            unexpected += 1
            print("unexpected %s %s %s" % (kind, cid, why))
    fixed = 0
    for cid in sorted(KNOWN):
        if cid not in failures:
            fixed += 1
            print("fixed %s %s" % (KNOWN[cid][0], cid))
    print("known %d" % len(KNOWN))
    print("unexpected-count %d" % unexpected)
    print("fixed-count %d" % fixed)


if __name__ == "__main__":
    sys.exit(main(sys.argv))
