#!/usr/bin/env python3
"""PyYAML 6.0.2 as an oracle, driven from t/run.sh.

PyYAML implements YAML 1.1. It is used here for two separate questions,
which are kept separate because they have different answers:

  accept FILE    does a 1.1 parser read this document at all
  events FILE    the structure it sees, with no schema resolution in it
  dump FILE...   what the 1.1 resolution rules make of it, one tab
                 separated line per document

Only the standard library plus PyYAML. Nothing here is allowed to know
anything about yamlc.
"""
import struct
import sys


def need_yaml():
    try:
        import yaml
    except ImportError:
        sys.stderr.write("t/pyyaml.py: PyYAML is not installed\n")
        sys.exit(2)
    return yaml


def cmd_accept(path):
    yaml = need_yaml()
    with open(path, "rb") as f:
        data = f.read()
    try:
        for _ in yaml.parse(data):
            pass
    except Exception as e:  # noqa: BLE001 - any refusal counts as a refusal
        print("refused: %s" % type(e).__name__)
        return 1
    print("ok")
    return 0


def escape(v):
    """The escaping yamlc's events.go describes, which is the YAML test
    suite's: backslash, the two breaks, tab and backspace go to C escapes,
    every other control character and every surrogate goes to \\uXXXX, and
    the rest is raw UTF-8."""
    out = []
    for ch in v:
        o = ord(ch)
        if ch == "\\":
            out.append("\\\\")
        elif ch == "\n":
            out.append("\\n")
        elif ch == "\r":
            out.append("\\r")
        elif ch == "\t":
            out.append("\\t")
        elif ch == "\b":
            out.append("\\b")
        elif o < 0x20 or o == 0x7F or 0xD800 <= o <= 0xDFFF:
            out.append("\\u%04X" % o)
        else:
            out.append(ch)
    return "".join(out)


STYLE = {None: ":", "": ":", "'": "'", '"': '"', "|": "|", ">": ">"}


def cmd_events(paths, batch=False):
    rc = 0
    for p in paths:
        if batch:
            print("=== %s" % p)
        rc |= events_one(p)
    return rc


def events_one(path):
    yaml = need_yaml()
    with open(path, "rb") as f:
        data = f.read()
    out = []
    try:
        for e in yaml.parse(data):
            n = type(e).__name__
            if n == "StreamStartEvent":
                out.append("+STR")
            elif n == "StreamEndEvent":
                out.append("-STR")
            elif n == "DocumentStartEvent":
                out.append("+DOC ---" if e.explicit else "+DOC")
            elif n == "DocumentEndEvent":
                out.append("-DOC ..." if e.explicit else "-DOC")
            elif n == "ScalarEvent":
                p = ""
                if e.anchor:
                    p += " &" + e.anchor
                if e.tag:
                    p += " <" + e.tag + ">"
                out.append("=VAL%s %s%s" % (p, STYLE[e.style], escape(e.value)))
            elif n == "AliasEvent":
                out.append("=ALI *" + e.anchor)
            elif n in ("SequenceStartEvent", "MappingStartEvent"):
                k = "+SEQ" if n[0] == "S" else "+MAP"
                # Flow indicator first, then the anchor, then the tag.
                # That order is the test suite's, and matching it lets the
                # comparison in t/yts.py be a byte comparison.
                p = ""
                if e.flow_style:
                    p += " []" if k == "+SEQ" else " {}"
                if e.anchor:
                    p += " &" + e.anchor
                if e.tag:
                    p += " <" + e.tag + ">"
                out.append(k + p)
            elif n == "SequenceEndEvent":
                out.append("-SEQ")
            elif n == "MappingEndEvent":
                out.append("-MAP")
    except Exception as e:  # noqa: BLE001
        print("!!! %s: %s" % (type(e).__name__, str(e).replace("\n", " ")))
        return 1
    print("\n".join(out))
    return 0


def canon(v):
    """The canonical form yamlc's dump.go describes, built from what
    yaml.safe_load returned. A float goes out as the hexadecimal of its
    IEEE 754 bits, because the shortest round-trip decimal is spelled
    differently by Go and by Python and a comparison that normalises both
    sides is measuring the normaliser. NaN is the exception and goes out as
    a word: the quiet NaN PyYAML builds from -inf/inf and the one Go's
    math.NaN returns have different payloads and different signs, and
    neither reader claims to preserve either."""
    import datetime
    if v is None:
        return "null"
    if v is True:
        return "true"
    if v is False:
        return "false"
    if isinstance(v, int):
        return "int %d" % v
    if isinstance(v, float):
        if v != v:
            return "float nan"
        return "float %016x" % struct.unpack("<Q", struct.pack("<d", v))[0]
    if isinstance(v, str):
        return "str " + escape(v)
    if isinstance(v, bytes):
        return "bin " + v.hex()
    if isinstance(v, datetime.datetime):
        out = "datetime " + v.strftime("%Y-%m-%d %H:%M:%S.%f")
        off = v.utcoffset()
        if off is not None:
            total = int(off.total_seconds())
            sign = "+" if total >= 0 else "-"
            total = abs(total)
            out += " %s%02d:%02d" % (sign, total // 3600, (total % 3600) // 60)
        return out
    if isinstance(v, datetime.date):
        return "date " + v.strftime("%Y-%m-%d")
    if isinstance(v, list):
        return "[" + ", ".join(canon(x) for x in v) + "]"
    if isinstance(v, dict):
        return "{" + ", ".join("%s: %s" % (canon(k), canon(x)) for k, x in v.items()) + "}"
    if isinstance(v, tuple):
        # construct_yaml_omap and construct_yaml_pairs return lists of
        # pairs rather than mappings, which is a data model yamlc does not
        # have: it keeps the sequence of single-pair mappings and carries
        # the tag. Rendered rather than refused so the difference is
        # legible in the report.
        return "(" + ", ".join(canon(x) for x in v) + ")"
    if isinstance(v, set):
        return "set{" + ", ".join(sorted(canon(x) for x in v)) + "}"
    return "unsupported %s" % type(v).__name__


def cmd_dump(paths):
    yaml = need_yaml()
    rc = 0
    for path in paths:
        with open(path, "rb") as f:
            data = f.read()
        try:
            docs = list(yaml.safe_load_all(data))
        except Exception as e:  # noqa: BLE001
            print("%s\terr\t%s: %s" % (path, type(e).__name__,
                                        str(e).replace("\n", " ")))
            rc = 1
            continue
        for d in docs:
            try:
                print("%s\tok\t%s" % (path, canon(d)))
            except RecursionError:
                print("%s\terr\tRecursionError" % path)
                rc = 1
    return rc


def cmd_version():
    yaml = need_yaml()
    print(yaml.__version__)
    return 0


def main(argv):
    if len(argv) < 2:
        sys.stderr.write(__doc__)
        return 2
    cmd = argv[1]
    if cmd == "version":
        return cmd_version()
    if cmd == "accept":
        return cmd_accept(argv[2])
    if cmd == "events":
        if argv[2] == "--batch":
            return cmd_events(argv[3:], batch=True)
        return cmd_events(argv[2:])
    if cmd == "dump":
        return cmd_dump(argv[2:])
    sys.stderr.write("t/pyyaml.py: no such command: %s\n" % cmd)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv))
