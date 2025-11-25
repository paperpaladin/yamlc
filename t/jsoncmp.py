#!/usr/bin/env python3
"""Compare yamlc against Python's json module over a body of JSON.

This is the one comparison in the project that is not a second opinion.
YAML 1.2 is a strict superset of JSON, so a JSON document must parse to
exactly what json.load parses it to, and a single disagreement means the
parser is wrong.

  t/jsoncmp.py YAMLC LISTFILE DIR...

Every path in LISTFILE and every *.json under each DIR is read twice, once
by json.load and once by "yamlc json --batch --dup=last", and the two
structures are compared. The comparison is stricter than ==: it compares
types first, so that True and 1 and 1.0 are three different answers, it
compares a float by its bits, and it compares the key order of an object,
which json.load preserves in the dict.

Output is machine readable, one fact per line, because t/run.sh asserts on
it.
"""
import json
import os
import struct
import subprocess
import sys

CHUNK = 400


def strict_same(a, b):
    if type(a) is not type(b):
        return False
    if isinstance(a, dict):
        ka, kb = list(a.keys()), list(b.keys())
        if ka != kb:
            return False
        return all(strict_same(a[k], b[k]) for k in ka)
    if isinstance(a, list):
        return len(a) == len(b) and all(strict_same(x, y) for x, y in zip(a, b))
    if isinstance(a, float):
        if a != a and b != b:
            return True
        return struct.pack("<d", a) == struct.pack("<d", b)
    return a == b


def collect(args):
    paths = []
    for arg in args:
        if os.path.isdir(arg):
            for name in sorted(os.listdir(arg)):
                if name.endswith(".json"):
                    paths.append(os.path.join(arg, name))
        elif os.path.isfile(arg):
            with open(arg) as f:
                paths.extend(line.rstrip("\n") for line in f if line.strip())
    return paths


def main(argv):
    if len(argv) < 3:
        sys.stderr.write(__doc__)
        return 2
    yamlc = argv[1]
    paths = collect(argv[2:])
    want = {}
    unreadable = 0
    for p in paths:
        try:
            with open(p, "rb") as f:
                want[p] = json.load(f)
        except Exception as e:  # noqa: BLE001
            unreadable += 1
            print("not-json-path %s %s" % (p, e))
    paths = [p for p in paths if p in want]

    got = {}
    refused = {}
    for i in range(0, len(paths), CHUNK):
        chunk = paths[i:i + CHUNK]
        out = subprocess.run([yamlc, "json", "--batch", "--dup=last"] + chunk,
                             capture_output=True, text=True)
        # split("\n") and not splitlines(): Python's splitlines also breaks
        # on U+0085, U+000B, U+2028 and U+2029, so a JSON string holding a
        # raw NEL split one output line in two and the comparison reported
        # unterminated JSON. That is the 1.1 against 1.2 line break
        # difference this project is about, arriving in the harness.
        for line in out.stdout.split("\n"):
            parts = line.split("\t", 2)
            if len(parts) != 3:
                continue
            name, status, body = parts
            if status == "err":
                refused[name] = body
            else:
                got.setdefault(name, []).append(body)
        if out.stderr.strip():
            print("stderr %s" % out.stderr.strip().replace("\n", " "))

    same = differ = 0
    missing = 0
    for p in paths:
        if p in refused:
            continue
        docs = got.get(p)
        if docs is None:
            missing += 1
            print("missing-path %s" % p)
            continue
        if len(docs) != 1:
            differ += 1
            print("differ-path %s produced %d documents" % (p, len(docs)))
            continue
        try:
            mine = json.loads(docs[0])
        except Exception as e:  # noqa: BLE001
            differ += 1
            print("differ-path %s output is not JSON: %s" % (p, e))
            continue
        if strict_same(want[p], mine):
            same += 1
        else:
            differ += 1
            print("differ-path %s" % p)

    print("files %d" % len(paths))
    print("not-json %d" % unreadable)
    print("same %d" % same)
    print("differ %d" % differ)
    print("missing %d" % missing)
    print("refused %d" % len(refused))
    for name in sorted(refused):
        print("refused-path %s %s" % (os.path.basename(name), refused[name]))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
