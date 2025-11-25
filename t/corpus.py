#!/usr/bin/env python3
"""Build the corpus. Nothing it writes is committed.

Two parts:

  system DIR   a list of every valid JSON file on the machine, written to
               DIR/system.list. These are bytes nobody here wrote, which
               is the point of them.
  hard DIR     generated JSON documents for the cases the system files do
               not reach: deep nesting, duplicate keys, lone surrogates,
               numbers that do not fit a double, empty containers.

A file goes in the system list only if Python's json module reads it, so
the list is JSON by the oracle's own definition and not by its extension.
"""
import json
import os
import sys

ROOTS = [
    "/usr/share",
    "/usr/lib",
    "/usr/lib64",
    "/etc",
]

# A few megabytes of JSON is plenty and the suite has to stay under a
# minute. iso-codes alone carries several large files, which are the most
# interesting ones here because they hold non-ASCII text in many scripts.
MAX_BYTES = 4 * 1024 * 1024


def cmd_system(outdir):
    os.makedirs(outdir, exist_ok=True)
    found, skipped = [], 0
    for root in ROOTS:
        for dirpath, dirnames, filenames in os.walk(root, followlinks=False):
            dirnames[:] = [d for d in dirnames if d != "__pycache__"]
            for name in filenames:
                if not name.endswith(".json"):
                    continue
                path = os.path.join(dirpath, name)
                try:
                    if os.path.getsize(path) == 0 or os.path.getsize(path) > MAX_BYTES:
                        skipped += 1
                        continue
                    with open(path, "rb") as f:
                        json.load(f)
                except Exception:  # noqa: BLE001 - not JSON, so not our corpus
                    skipped += 1
                    continue
                found.append(path)
    found.sort()
    with open(os.path.join(outdir, "system.list"), "w") as f:
        for p in found:
            f.write(p + "\n")
    print("system json: %d files, %d .json files skipped as unreadable or not JSON"
          % (len(found), skipped))
    return 0


def deep(n, kind):
    if kind == "array":
        return "[" * n + "1" + "]" * n
    return '{"a":' * n + "1" + "}" * n


def write(outdir, name, text):
    with open(os.path.join(outdir, name), "w", encoding="utf-8") as f:
        f.write(text)


def cmd_hard(outdir):
    os.makedirs(outdir, exist_ok=True)
    cases = {
        # Empty and nearly empty.
        "empty-object.json": "{}",
        "empty-array.json": "[]",
        "empty-nested.json": '{"a":{},"b":[],"c":[{},[]]}',
        "empty-string-key.json": '{"":1}',
        # Top level scalars, which RFC 7159 allows and RFC 4627 did not.
        "scalar-int.json": "42",
        "scalar-str.json": '"x"',
        "scalar-true.json": "true",
        "scalar-null.json": "null",
        "scalar-float.json": "1.5",
        # Duplicate keys. Python's json keeps the last and puts it at the
        # first position; yamlc refuses by default and is run with
        # --dup=last for this comparison.
        "dup-simple.json": '{"a":1,"a":2}',
        "dup-three.json": '{"a":1,"b":2,"a":3,"a":4}',
        "dup-nested.json": '{"o":{"k":1,"k":2},"o":{"k":3}}',
        "dup-value-types.json": '{"a":1,"a":"1",       "a":null}',
        # Numbers. 1e400 is an infinity to Python's json and 1e-400 is a
        # zero, so both have to be read and not refused.
        "num-big-int.json": "[123456789012345678901234567890, -99999999999999999999]",
        "num-precision.json": "[0.1, 1.7976931348623157e+308, 5e-324, 2.2250738585072014e-308]",
        "num-overflow.json": "[1e400, -1e400, 1e-400]",
        "num-exponents.json": "[1e2, 1E2, 1e+2, 1e-2, 1.0e2, 0e0, -0.0, -0]",
        "num-zeroes.json": "[0, 0.0, 0.00, 0e-10]",
        "num-long-fraction.json": "[3.141592653589793238462643383279502884197]",
        # Strings. The surrogate cases are the reason the double-quoted
        # scanner keeps a surrogate as the bytes that would encode it.
        "str-lone-high.json": '["\\ud800"]',
        "str-lone-low.json": '["\\udfff"]',
        "str-pair.json": '["\\ud83d\\ude00"]',
        "str-pair-split.json": '["\\ud83d", "\\ude00"]',
        "str-high-then-text.json": '["\\ud800x"]',
        "str-escapes.json": '["\\" \\\\ \\/ \\b \\f \\n \\r \\t"]',
        "str-controls.json": '["\\u0000\\u001f\\u007f"]',
        "str-utf8-raw.json": '["\u00e9\u20ac\U0001f600\u4e2d\u6587"]',
        "str-long.json": '["' + "x" * 20000 + '"]',
        # Structure.
        "deep-array-100.json": deep(100, "array"),
        "deep-object-100.json": deep(100, "object"),
        "deep-array-390.json": deep(390, "array"),
        "wide-array.json": "[" + ",".join(str(i) for i in range(5000)) + "]",
        "wide-object.json": "{" + ",".join('"k%d":%d' % (i, i) for i in range(5000)) + "}",
        "mixed.json": json.dumps(
            {"a": [1, 2.5, None, True, False, "s", {"b": [[[]]]}],
             "c": {"d": {"e": []}}, "": 0},
            ensure_ascii=False),
        # Whitespace, including tabs between tokens, which YAML reads as
        # separation whitespace.
        "ws-tabs.json": '{\t"a"\t:\t[\t1\t,\t2\t]\t}',
        "ws-newlines.json": '{\n  "a" : [\n    1,\n    2\n  ]\n}\n',
        "ws-crlf.json": '{\r\n"a": 1\r\n}\r\n',
    }
    for name, text in cases.items():
        write(outdir, name, text)
    # Deliberately beyond the limits, so the refusals can be counted. These
    # are valid JSON and yamlc must refuse them rather than exhaust memory.
    write(outdir, "over-deep-array.json", deep(10000, "array"))
    write(outdir, "over-deep-object.json", deep(10000, "object"))
    # Valid JSON that YAML 1.2 cannot hold: JSON escapes only below #x20,
    # so a raw DEL in a string is legal JSON, and section 5.1 leaves #x7F
    # out of c-printable. Counted as a refusal with its cause, not as a
    # disagreement.
    with open(os.path.join(outdir, "raw-del.json"), "wb") as f:
        f.write(b'["a\x7fb"]')
    with open(os.path.join(outdir, "raw-c1.json"), "wb") as f:
        f.write(b'["a\xc2\x85b"]')
    n = len([x for x in os.listdir(outdir) if x.endswith(".json")])
    print("hard json: %d files" % n)
    return 0


def main(argv):
    if len(argv) < 3:
        sys.stderr.write(__doc__)
        return 2
    if argv[1] == "system":
        return cmd_system(argv[2])
    if argv[1] == "hard":
        return cmd_hard(argv[2])
    sys.stderr.write("t/corpus.py: no such command: %s\n" % argv[1])
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv))
