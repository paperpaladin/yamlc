#!/usr/bin/env python3
"""PyYAML 6.0.2 as an oracle, driven from t/run.sh.

PyYAML implements YAML 1.1. It is used here for two separate questions,
which are kept separate because they have different answers:

  accept FILE   does a 1.1 parser read this document at all
  events FILE   the structure it sees, with no schema resolution in it
  load FILE     what the 1.1 resolution rules make of the scalars

Only the standard library plus PyYAML. Nothing here is allowed to know
anything about yamlc.
"""
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
    """The escaping yamlc's events.go describes. Kept deliberately small:
    backslash, break and tab go to C escapes, every other control character
    and every surrogate goes to \\uXXXX, and the rest is raw UTF-8."""
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
        elif o < 0x20 or o == 0x7F or 0xD800 <= o <= 0xDFFF:
            out.append("\\u%04X" % o)
        else:
            out.append(ch)
    return "".join(out)


STYLE = {None: ":", "": ":", "'": "'", '"': '"', "|": "|", ">": ">"}


def cmd_events(path):
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
                p = ""
                if e.anchor:
                    p += " &" + e.anchor
                if e.tag:
                    p += " <" + e.tag + ">"
                if e.flow_style:
                    p += " []" if k == "+SEQ" else " {}"
                out.append(k + p)
            elif n == "SequenceEndEvent":
                out.append("-SEQ")
            elif n == "MappingEndEvent":
                out.append("-MAP")
    except Exception as e:  # noqa: BLE001
        sys.stderr.write("refused: %s\n" % type(e).__name__)
        print("\n".join(out))
        return 1
    print("\n".join(out))
    return 0


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
        return cmd_events(argv[2])
    sys.stderr.write("t/pyyaml.py: no such command: %s\n" % cmd)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv))
