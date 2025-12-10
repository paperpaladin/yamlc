#!/usr/bin/env python3
"""A YAML corpus written by PyYAML's emitter, read back by both parsers.

  t/yamlcmp.py YAMLC OUTDIR [--sample N]

There are nine YAML files on this machine, which is not a corpus. What
there is instead is a few thousand JSON files, and PyYAML's emitter, which
is a program this project does not control. So: load a JSON document with
the json module, write it out as YAML in several styles, and then read it
back with both parsers.

That gives three comparisons on each generated file, and they fail in
different ways, so they are counted separately:

  events   yamlc's event stream against PyYAML's, which is about the
           grammar and carries no resolution
  values   yamlc under --schema=1.1 against PyYAML's safe_load, as
           canonical text, which is about the resolution rules
  origin   both of those against the canonical form of the original
           Python object, which is the only one of the three that knows
           what the document was supposed to say

The styles exercise different parts of the grammar: block and flow,
default and canonical, a narrow width to force line folding, and
allow_unicode off to force \\uXXXX escapes.
"""
import json
import os
import subprocess
import sys

CHUNK = 300


def need_yaml():
    try:
        import yaml
    except ImportError:
        sys.stderr.write("t/yamlcmp.py: PyYAML is not installed\n")
        sys.exit(2)
    return yaml


STYLES = [
    ("block", dict(default_flow_style=False, sort_keys=False)),
    ("flow", dict(default_flow_style=True, sort_keys=False)),
    ("narrow", dict(default_flow_style=False, width=20, sort_keys=False)),
    ("ascii", dict(default_flow_style=False, allow_unicode=False, sort_keys=False)),
    ("canon", dict(canonical=True, sort_keys=False)),
    ("explicit", dict(default_flow_style=False, explicit_start=True,
                      explicit_end=True, sort_keys=False)),
]

MAX_SOURCE = 64 * 1024


def build(outdir, sources, sample):
    yaml = need_yaml()
    os.makedirs(outdir, exist_ok=True)
    made, failed = 0, 0
    picked = []
    for p in sources:
        try:
            if os.path.getsize(p) > MAX_SOURCE:
                continue
        except OSError:
            continue
        picked.append(p)
    # Every nth file rather than the first n, so the sample is not one
    # directory's worth of near-identical package manifests.
    if sample and len(picked) > sample:
        step = len(picked) // sample
        picked = picked[::step][:sample]
    data = {}
    for p in picked:
        try:
            with open(p, "rb") as f:
                obj = json.load(f)
        except Exception:  # noqa: BLE001
            continue
        base = "s%04d" % len(data)
        data[base] = obj
        for name, kw in STYLES:
            try:
                text = yaml.dump(obj, **kw)
            except Exception:  # noqa: BLE001 - unrepresentable, e.g. a
                # lone surrogate, which the emitter cannot encode
                failed += 1
                continue
            with open(os.path.join(outdir, "%s-%s.yaml" % (base, name)), "w",
                      encoding="utf-8") as f:
                f.write(text)
            made += 1
    # The real YAML files on the machine go in as they are.
    for p in sources:
        if p.endswith((".yaml", ".yml")) and os.path.getsize(p) < MAX_SOURCE:
            with open(p, "rb") as f:
                raw = f.read()
            with open(os.path.join(outdir, "native-" + os.path.basename(p)), "wb") as f:
                f.write(raw)
            made += 1
    print("built %d" % made)
    print("unrepresentable %d" % failed)
    return data


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


def run_batch(cmd, paths):
    out = {}
    for i in range(0, len(paths), CHUNK):
        r = subprocess.run(cmd + paths[i:i + CHUNK], capture_output=True,
                           text=True, timeout=900)
        if "panic" in r.stderr:
            print("panic %s" % r.stderr.split("\n")[0])
        out.update(split_events(r.stdout))
    return out


def run_tabbed(cmd, paths):
    out = {}
    for i in range(0, len(paths), CHUNK):
        r = subprocess.run(cmd + paths[i:i + CHUNK], capture_output=True,
                           text=True, timeout=900)
        if "panic" in r.stderr:
            print("panic %s" % r.stderr.split("\n")[0])
        for line in r.stdout.split("\n"):
            parts = line.split("\t", 2)
            if len(parts) == 3:
                out.setdefault(parts[0], []).append((parts[1], parts[2]))
    return out


def main(argv):
    if len(argv) < 3:
        sys.stderr.write(__doc__)
        return 2
    yamlc, outdir = argv[1], argv[2]
    sample = 0
    if "--sample" in argv:
        sample = int(argv[argv.index("--sample") + 1])

    sources = []
    listfile = os.path.join(os.path.dirname(outdir), "system.list")
    if os.path.exists(listfile):
        with open(listfile) as f:
            sources = [line.rstrip("\n") for line in f if line.strip()]
    for root in ("/usr/share", "/etc"):
        for dirpath, dirnames, filenames in os.walk(root, followlinks=False):
            dirnames[:] = [d for d in dirnames if d != "__pycache__"]
            for n in filenames:
                if n.endswith((".yaml", ".yml")):
                    sources.append(os.path.join(dirpath, n))

    data = build(outdir, sources, sample)
    paths = sorted(os.path.join(outdir, n) for n in os.listdir(outdir)
                   if n.endswith((".yaml", ".yml")))

    mine_ev = run_batch([yamlc, "events", "--batch"], paths)
    theirs_ev = run_batch(["python3", "t/pyyaml.py", "events", "--batch"], paths)
    ev_same = ev_differ = ev_refused = 0
    for p in paths:
        a, b = theirs_ev.get(p, "?py"), mine_ev.get(p, "?yamlc")
        if a == b:
            ev_same += 1
        elif b.startswith("!!!") or a.startswith("!!!"):
            ev_refused += 1
            print("ev-refused %s" % os.path.basename(p))
        else:
            ev_differ += 1
            print("ev-differ %s" % os.path.basename(p))

    mine_v = run_tabbed([yamlc, "dump", "--schema=1.1", "--dup=last", "--batch"], paths)
    theirs_v = run_tabbed(["python3", "t/pyyaml.py", "dump"], paths)
    v_same = v_differ = v_refused = 0
    for p in paths:
        a, b = theirs_v.get(p), mine_v.get(p)
        if a is None or b is None:
            v_refused += 1
            print("val-missing %s" % os.path.basename(p))
            continue
        if any(s == "err" for s, _ in a) or any(s == "err" for s, _ in b):
            v_refused += 1
            print("val-refused %s %s" % (os.path.basename(p),
                                         [x[1] for x in a + b if x[0] == "err"][:1]))
            continue
        if [x[1] for x in a] == [x[1] for x in b]:
            v_same += 1
        else:
            v_differ += 1
            print("val-differ %s" % os.path.basename(p))

    # The third comparison: against what the document was supposed to say.
    # The styles of one source all have to agree with the source's data,
    # which is the only check here that is not two parsers agreeing.
    yaml = need_yaml()
    origin_same = origin_differ = 0
    for base, obj in data.items():
        want = canon_of(obj)
        for name, _ in STYLES:
            p = os.path.join(outdir, "%s-%s.yaml" % (base, name))
            if not os.path.exists(p):
                continue
            docs = mine_v.get(p)
            if docs is None or any(s == "err" for s, _ in docs):
                continue
            if len(docs) == 1 and docs[0][1] == want:
                origin_same += 1
            else:
                origin_differ += 1
                print("origin-differ %s" % os.path.basename(p))
    print("files %d" % len(paths))
    print("ev-same %d" % ev_same)
    print("ev-differ %d" % ev_differ)
    print("ev-refused %d" % ev_refused)
    print("val-same %d" % v_same)
    print("val-differ %d" % v_differ)
    print("val-refused %d" % v_refused)
    print("origin-same %d" % origin_same)
    print("origin-differ %d" % origin_differ)
    return 0


def canon_of(obj):
    sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
    import pyyaml as helper
    return helper.canon(obj)


if __name__ == "__main__":
    sys.exit(main(sys.argv))
