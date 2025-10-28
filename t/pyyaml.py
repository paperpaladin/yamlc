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
    sys.stderr.write("t/pyyaml.py: no such command: %s\n" % cmd)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv))
