#!/usr/bin/env python3
"""Mutate the parser one edit at a time and require the suite to go red.

  python3 t/mutants.py [GROUP]...
  python3 t/mutants.py --check     patterns still match, which t/run.sh runs

Each mutant is one exact string replacement in one file. The file is
rebuilt, the whole suite runs, and the mutant is KILLED if the suite goes
red and SURVIVES if it stays green. A survivor is a hole in the suite and
is recorded rather than quietly dropped: some of them are equivalent
mutants, which produce the same behaviour as the original, and saying
which is part of the result.

The sources are copied before anything is edited and put back in a finally
block, so an interrupted run leaves the tree as it found it.

Each suite run gets an address space limit, because two of the mutants
below remove a resource check and the documents that check exists for
would otherwise try to build a few hundred million nodes.
"""
import os
import resource
import shutil
import subprocess
import sys
import tempfile
import time

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
ADDRESS_LIMIT = 6 * 1024 * 1024 * 1024
SUITE_TIMEOUT = 600

# group, name, file, old, new
MUTANTS = [
    # --- the input layer ---
    ("src", "printable-fffd", "src.go",
     "case r >= 0xe000 && r <= 0xfffd:", "case r >= 0xe000 && r <= 0xffff:"),
    ("src", "crlf-pair", "src.go",
     "\t\t\t\tif i < len(in) && in[i] == '\\n' {\n\t\t\t\t\ti++\n\t\t\t\t}",
     "\t\t\t\tif false {\n\t\t\t\t\ti++\n\t\t\t\t}"),
    # The BOM mutant used to be "in = in[3:]" and had been dead since
    # the commit that added UTF-16 and UTF-32 detection moved that code
    # into sniff(). A mutant whose pattern no longer matches is reported
    # as PATTERN and is neither killed nor a survivor, so it had stopped
    # asking its question without ever saying so.
    ("src", "bom", "src.go",
     "\t\treturn \"utf8\", 3", "\t\treturn \"utf8\", 0"),
    ("src", "col-counts-bytes", "src.go",
     "\t\t_, w := utf8.DecodeRune(s.b[s.pos:])\n\t\ts.pos += w",
     "\t\ts.pos++"),
    ("src", "del-printable", "src.go",
     "case c == '\\t' || (c >= 0x20 && c <= 0x7e):",
     "case c == '\\t' || (c >= 0x20 && c <= 0x7f):"),

    # --- plain scalars and folding ---
    ("plain", "hash-first", "parse.go",
     "\tcase '#', '&', '*', '!', '|', '>', '\\'', '\"', '%', '@', '`':",
     "\tcase '&', '*', '!', '|', '>', '\\'', '\"', '%', '@', '`':"),
    ("plain", "colon-flow-term", "parse.go",
     "\t\t\t\tif spaceOrEnd(nx) || (flow && isFlowIndicator(nx)) {",
     "\t\t\t\tif spaceOrEnd(nx) {"),
    ("plain", "fold-one-break", "parse.go",
     "\t\tif breaks == 1 {\n\t\t\tout = append(out, ' ')\n\t\t} else {\n\t\t\tout = append(out, strings.Repeat(\"\\n\", breaks-1)...)\n\t\t}\n\t\treadLine()",
     "\t\tout = append(out, strings.Repeat(\"\\n\", breaks)...)\n\t\treadLine()"),
    ("plain", "fold-count", "parse.go",
     "\t\t\tout = append(out, strings.Repeat(\"\\n\", breaks-1)...)\n\t\t}\n\t\treadLine()",
     "\t\t\tout = append(out, strings.Repeat(\"\\n\", breaks)...)\n\t\t}\n\t\treadLine()"),
    ("plain", "continues-flow", "parse.go",
     "\tif flow && isFlowIndicator(c) {\n\t\treturn false\n\t}", "\tif false {\n\t\treturn false\n\t}"),
    ("plain", "trailing-blanks", "parse.go",
     "\t\tout = append(out, line[:len(line)-trail]...)", "\t\tout = append(out, line...)"),
    ("plain", "comment-needs-space", "parse.go",
     "\t\t\tif c == '#' && len(line) > 0 && isBlank(line[len(line)-1]) {",
     "\t\t\tif c == '#' {"),

    # --- quoted scalars ---
    ("quoted", "single-doubled", "scalar.go",
     "\t\t\tif p.s.peek(1) == '\\'' {", "\t\t\tif false {"),
    ("quoted", "escape-esc", "scalar.go",
     "\t\treturn []byte{0x1b}, nil", "\t\treturn []byte{0x1c}, nil"),
    ("quoted", "escape-hex-len", "scalar.go",
     "\t\tv, err := p.hexDigits(2, line, col)", "\t\tv, err := p.hexDigits(1, line, col)"),
    ("quoted", "surrogate-pair", "scalar.go",
     "\tif v >= 0xd800 && v <= 0xdbff && p.s.peek(0) == '\\\\' && p.s.peek(1) == 'u' {",
     "\tif false && v >= 0xd800 && v <= 0xdbff {"),
    ("quoted", "escaped-break-space", "scalar.go",
     "\t\t\t\tout = append(out, strings.Repeat(\"\\n\", breaks-1)...)\n\t\t\t\tcontinue",
     "\t\t\t\tout = append(out, strings.Repeat(\" \", breaks)...)\n\t\t\t\tcontinue"),
    ("quoted", "quoted-indent", "scalar.go",
     "\t\t\tif !ok || ind2 <= ind || (ind2 == 0 && p.atAnyMarker()) {\n\t\t\t\treturn \"\", false, p.s.errAt(oline, ocol, \"single-quoted scalar is never closed\")",
     "\t\t\tif !ok || (ind2 == 0 && p.atAnyMarker()) {\n\t\t\t\treturn \"\", false, p.s.errAt(oline, ocol, \"single-quoted scalar is never closed\")"),

    # --- block scalars ---
    ("block-scalar", "chomp-clip", "scalar.go",
     "\tif chomp >= 0 {\n\t\tout = append(out, lineBreak...)",
     "\tif chomp > 0 {\n\t\tout = append(out, lineBreak...)"),
    ("block-scalar", "chomp-keep", "scalar.go",
     "\tif chomp > 0 {\n\t\tout = append(out, breaks...)\n\t}", "\tif false {\n\t\tout = append(out, breaks...)\n\t}"),
    ("block-scalar", "fold-leading-text", "scalar.go",
     "\t\tif folded && lineBreak == \"\\n\" && leadingText && !isBlank(p.s.peek(0)) {",
     "\t\tif folded && lineBreak == \"\\n\" && (leadingText || true) {"),
    ("block-scalar", "fold-breaks-empty", "scalar.go",
     "\t\t\tif len(breaks) == 0 {\n\t\t\t\tout = append(out, ' ')\n\t\t\t}",
     "\t\t\tout = append(out, ' ')"),
    ("block-scalar", "min-indent-floor", "scalar.go",
     "\tif minIndent < floor {\n\t\tminIndent = floor\n\t}", "\tif false {\n\t\tminIndent = floor\n\t}"),
    ("block-scalar", "indicator-offset", "scalar.go",
     "\t\tindent = minIndent + incr - 1", "\t\tindent = minIndent + incr"),
    ("block-scalar", "leading-deep-empty", "scalar.go",
     "\t\tif hasContent && maxEmpty > indent {", "\t\tif false && maxEmpty > indent {"),
    ("block-scalar", "cursor-reset", "scalar.go",
     "\tp.reset(lineStart)\n\tif chomp >= 0 {",
     "\tlineStart = p.mark()\n\tp.reset(lineStart)\n\tif chomp >= 0 {"),
    ("block-scalar", "zero-indicator", "scalar.go",
     "\t\t\tif c == '0' {", "\t\t\tif false {"),

    # --- block collections ---
    ("block", "value-needs-space", "block.go",
     "\t\t\tif !isBlank(p.s.peek(0)) && !p.atLineEnd() {\n\t\t\t\treturn nil, p.s.errf(\"a mapping value on the same line needs a space after the colon\")\n\t\t\t}",
     ""),
    ("block", "explicit-compact", "block.go",
     "\t\t\t\tval, err = p.parseBlockNode(blockCtx{ind: ind, inline: true,\n\t\t\t\t\tcompact: true, seqAtInd: true})",
     "\t\t\t\tval, err = p.parseBlockNode(blockCtx{ind: ind, inline: true,\n\t\t\t\t\tseqAtInd: true})"),
    ("block", "key-length", "parse.go",
     "\tif utf8.RuneCount(p.s.b[startPos:p.s.pos]) > maxSimpleKey {",
     "\tif utf8.RuneCount(p.s.b[startPos:p.s.pos]) > maxSimpleKey*1000 {"),
    ("block", "seq-at-ind", "parse.go",
     "\t\t\t(c.seqAtInd && ind == c.ind && p.s.peek(0) == '-' && spaceOrEnd(p.s.peek(1)))",
     "\t\t\t(false && ind == c.ind)"),
    ("block", "content-indent", "parse.go",
     "\t\tokHere := ind > c.ind ||", "\t\tokHere := ind >= c.ind ||"),
    ("block", "tab-indent", "parse.go",
     "\t\t\tif ind <= c.ind {\n\t\t\t\tp.reset(k)\n\t\t\t\treturn false, p.s.errf(\"tab cannot be used for indentation\")\n\t\t\t}",
     ""),
    ("block", "props-split", "parse.go",
     "\tif !pr.empty() && pr.line() == line {", "\tif false {"),
    ("block", "compact-map", "parse.go",
     "\t\tif !c.compact {\n\t\t\treturn p.s.errf(\"a %s cannot begin on the same line as the value it belongs to\", what)\n\t\t}",
     ""),
    ("block", "boundary-directive", "parse.go",
     "\treturn p.atAnyMarker() || (p.s.col == 0 && p.s.peek(0) == '%')",
     "\treturn p.atAnyMarker()"),

    # --- flow collections ---
    ("flow", "flow-end", "flow.go",
     "func isFlowEnd(c byte) bool { return c == ',' || c == ']' || c == '}' }",
     "func isFlowEnd(c byte) bool { return isFlowIndicator(c) }"),
    ("flow", "empty-entry", "flow.go",
     "\t\tif p.s.peek(0) == ',' {\n\t\t\treturn p.s.errf(\"an entry is missing before this comma\")\n\t\t}",
     ""),
    ("flow", "json-key", "flow.go",
     "\t\tn.Style = StSingle\n\t\tv, _, err := p.scanSingle(ind)\n\t\tn.Val = v\n\t\treturn n, true, err",
     "\t\tn.Style = StSingle\n\t\tv, _, err := p.scanSingle(ind)\n\t\tn.Val = v\n\t\treturn n, false, err"),
    ("flow", "depth", "flow.go",
     "\tif p.depth > p.maxDepth {\n\t\treturn p.s.errf(\"nesting deeper than %d, refused\", p.maxDepth)\n\t}",
     "\tif p.depth > p.maxDepth*1000 {\n\t\treturn p.s.errf(\"nesting deeper than %d, refused\", p.maxDepth)\n\t}"),

    # --- composition ---
    ("compose", "cycle", "compose.go",
     "\t\tif c.busy[t] {", "\t\tif false {"),
    ("compose", "budget", "compose.go",
     "\tif c.used > c.budget {", "\tif c.used > c.budget*1000000 {"),
    ("compose", "anchor-missing", "compose.go",
     "\t\tif !ok {\n\t\t\treturn nil, c.errAt(n, \"alias *%s has no anchor before it\", n.Alias)\n\t\t}",
     "\t\tif !ok {\n\t\t\treturn &Value{Kind: VNull}, nil\n\t\t}"),
    ("compose", "dup-key", "compose.go",
     "\t\tif prev, dup := seen[key]; dup {", "\t\tif prev, dup := seen[key]; false && dup {"),
    ("compose", "merge-reverse", "compose.go",
     "\t\tfor i := len(m.Seq) - 1; i >= 0; i-- {", "\t\tfor i := 0; i < len(m.Seq); i++ {"),
    ("compose", "merge-front", "compose.go",
     "\treturn c.reduce(v, append(merged, explicit...))", "\treturn c.reduce(v, append(explicit, merged...))"),
    ("compose", "reduce-last-wins", "compose.go",
     "\t\t\tif c.dup != dupFirst {\n\t\t\t\tv.Map[i].Val = kv.Val\n\t\t\t}", "\t\t\t_ = i"),
    ("compose", "directive-scope", "compose.go",
     "\tc.schema = c.base\n\tif d.Version == \"1.1\" && c.schemaFromDoc {", "\tif d.Version == \"1.1\" && c.schemaFromDoc {"),

    # --- resolution ---
    # Equivalent: 0?[0-9]+ matches exactly the strings [0-9]+ matches,
    # because the optional zero is itself one of the digits the second
    # part accepts. Left in the list rather than deleted, so that the
    # count of survivors stays honest about why each one survives.
    ("resolve", "12-leading-zero", "resolve.go",
     "\tre12Dec   = regexp.MustCompile(`^[-+]?[0-9]+$`)",
     "\tre12Dec   = regexp.MustCompile(`^[-+]?0?[0-9]+$`)"),
    ("resolve", "12-oct", "resolve.go",
     "\tre12Oct   = regexp.MustCompile(`^0o[0-7]+$`)",
     "\tre12Oct   = regexp.MustCompile(`^0[0-7]+$`)"),
    ("resolve", "11-exp-sign", "resolve.go",
     "\tre11Float = regexp.MustCompile(`^([-+]?([0-9][0-9_]*)\\.[0-9_]*([eE][-+][0-9]+)?` +",
     "\tre11Float = regexp.MustCompile(`^([-+]?([0-9][0-9_]*)\\.[0-9_]*([eE][-+]?[0-9]+)?` +"),
    ("resolve", "sexagesimal-base", "resolve.go",
     "\tsixty := big.NewInt(60)", "\tsixty := big.NewInt(10)"),
    ("resolve", "11-octal", "resolve.go",
     "\t\tcase t[0] == '0':\n\t\t\tn = parseBase(t, 8)", "\t\tcase t[0] == '0':\n\t\t\tn = parseBase(t, 10)"),
    ("resolve", "11-bool-on", "resolve.go",
     "|on|On|ON|off|Off|OFF)$`)", "|off|Off|OFF)$`)"),
    ("resolve", "stamp-validate", "resolve.go",
     "\tif !validDate(y, mo, dy) {", "\tif false && !validDate(y, mo, dy) {"),
    ("resolve", "explicit-tag", "resolve.go",
     "func explicitTag(n *Node) bool {\n\tswitch n.Tag {", "func explicitTag(n *Node) bool {\n\treturn false\n\tswitch n.Tag {"),
    ("resolve", "12-float-order", "resolve.go",
     "\tcase re12Dec.MatchString(text), re12Oct.MatchString(text), re12Hex.MatchString(text):\n\t\treturn tagInt",
     "\tcase re12Float.MatchString(text):\n\t\treturn tagFloat"),

    # --- output ---
    ("output", "json-float-dot", "json.go",
     "\tif !strings.ContainsAny(s, \".eE\") {\n\t\ts += \".0\"\n\t}", ""),
    ("output", "json-surrogate", "json.go",
     "\t\tcase c == 0xed && i+2 < len(s) && s[i+1] >= 0xa0 && s[i+1] <= 0xbf:",
     "\t\tcase false:"),
    ("output", "json-int-exact", "json.go",
     "\t\tb.WriteString(v.Int.String())", "\t\tb.WriteString(jsonFloat(float64(v.Int.Int64())))"),
    ("output", "canon-float-bits", "dump.go",
     "\t\t\tfmt.Fprintf(b, \"float %016x\", math.Float64bits(v.Float))",
     "\t\t\tfmt.Fprintf(b, \"float %g\", v.Float)"),
    ("output", "event-style", "node.go",
     "\tcase StSingle:\n\t\treturn \"'\"", "\tcase StSingle:\n\t\treturn \"\\\"\""),

    # --- the rules the YAML test suite settled ---
    # Each of these is a rule yamlc had wrong until the suite was wired
    # in as an oracle, so each one is also a check that the suite is
    # still being asked the question.
    ("yts", "flow-colon-plain", "flow.go",
     "\tcase ch == ':' && !plainFirst(ch, p.s.peek(1), true):",
     "\tcase ch == ':':"),
    ("yts", "flow-colon-indent", "flow.go",
     "\tif p.s.line != kl && p.s.col <= ind {",
     "\tif false {"),
    ("yts", "comment-needs-blank", "src.go",
     "\treturn c == ' ' || c == '\\t'",
     "\treturn true"),
    ("yts", "blockscalar-root-floor", "scalar.go",
     "\tfloor := 0\n\tif incr != 0 {\n\t\tfloor = 1\n\t}",
     "\tfloor := 1\n\tif incr != 0 {\n\t\tfloor = 1\n\t}"),
    ("yts", "blockscalar-marker", "scalar.go",
     "\tfor p.s.col == indent && !p.s.eof() && !(p.s.col == 0 && p.atAnyMarker()) {",
     "\tfor p.s.col == indent && !p.s.eof() {"),
    ("yts", "blockscalar-eof-break", "scalar.go",
     "\t\t} else if p.s.eof() {\n\t\t\t// The stream ends with a line break whether or not one is\n\t\t\t// written, so the last content line has one: the suite's\n\t\t\t// L24T/01 against L24T/00, the same document with and\n\t\t\t// without the final newline.\n\t\t\tlineBreak = \"\\n\"",
     "\t\t} else if p.s.eof() {\n\t\t\tlineBreak = \"\""),
    ("yts", "blockscalar-no-content", "scalar.go",
     "\t\thasContent := !p.s.eof() && content > ind",
     "\t\thasContent := true"),
    ("yts", "escaped-white-kept", "scalar.go",
     "\tfor n > keep && isBlank(b[n-1]) {",
     "\tfor n > 0 && isBlank(b[n-1]) {"),
    ("yts", "directive-needs-suffix", "parse.go",
     "\t\t\tif needMarker {\n\t\t\t\treturn nil, p.s.errf(\"a directive needs ... to close the document before it\")\n\t\t\t}",
     "\t\t\tif false {\n\t\t\t\treturn nil, p.s.errf(\"a directive needs ... to close the document before it\")\n\t\t\t}"),
    ("yts", "props-group-by-line", "parse.go",
     "\t\tif pr.empty() || pr.line() == pline {",
     "\t\tif pr.empty() {"),
    ("yts", "alias-props-key", "block.go",
     "\tif n.Kind == KAlias && (n.Anchor != \"\" || n.TagRaw != \"\") {",
     "\tif false {"),
]


def limit():
    resource.setrlimit(resource.RLIMIT_AS, (ADDRESS_LIMIT, ADDRESS_LIMIT))


def run(cmd, **kw):
    return subprocess.run(cmd, cwd=ROOT, capture_output=True, text=True, **kw)


def check():
    """Every mutant's pattern must appear exactly once in its file.

    A pattern that no longer matches is reported as PATTERN by the run
    below and is neither killed nor a survivor, so a mutant can quietly
    stop asking its question when the code it points at is edited. That
    happened: "in = in[3:]" went dead when UTF-16 and UTF-32 detection
    moved the byte order mark into sniff(), and nothing said so until
    four more patterns broke at once. t/run.sh calls this on every run,
    which costs nothing and is the thing that would have noticed.
    """
    bad = 0
    for group, name, path, old, _ in MUTANTS:
        with open(os.path.join(ROOT, path)) as f:
            n = f.read().count(old)
        if n != 1:
            bad += 1
            print("stale %s/%s in %s: pattern appears %d times" % (group, name, path, n))
    print("mutants %d" % len(MUTANTS))
    print("stale %d" % bad)
    return 1 if bad else 0


def main(argv):
    if argv[1:2] == ["--check"]:
        return check()
    groups = set(argv[1:])
    chosen = [m for m in MUTANTS if not groups or m[0] in groups]
    if not chosen:
        sys.stderr.write("no such group; groups are %s\n"
                         % " ".join(sorted({m[0] for m in MUTANTS})))
        return 2
    files = sorted({m[2] for m in MUTANTS})
    backup = tempfile.mkdtemp(prefix="yamlc-mutants.")
    for f in files:
        shutil.copy(os.path.join(ROOT, f), os.path.join(backup, f))
    killed, survived, broken = [], [], []
    try:
        for i, (group, name, path, old, new) in enumerate(chosen, 1):
            full = os.path.join(ROOT, path)
            src = open(os.path.join(backup, path)).read()
            if src.count(old) != 1:
                broken.append((group, name, "pattern appears %d times" % src.count(old)))
                print("%3d/%d %-14s %-22s PATTERN %d" % (i, len(chosen), group, name,
                                                         src.count(old)))
                continue
            open(full, "w").write(src.replace(old, new, 1))
            t0 = time.time()
            build = run(["make"])
            if build.returncode != 0:
                broken.append((group, name, "does not compile"))
                print("%3d/%d %-14s %-22s NO BUILD" % (i, len(chosen), group, name))
                shutil.copy(os.path.join(backup, path), full)
                continue
            try:
                r = run(["./t/run.sh"], timeout=SUITE_TIMEOUT, preexec_fn=limit)
                rc = r.returncode
                tail = [l for l in r.stdout.split("\n") if l.startswith("FAIL")]
                why = tail[0][:70] if tail else (r.stdout.strip().split("\n") or [""])[-1][:70]
            except subprocess.TimeoutExpired:
                rc, why = 99, "suite did not finish in %ds" % SUITE_TIMEOUT
            if rc == 0:
                survived.append((group, name))
                print("%3d/%d %-14s %-22s SURVIVED  (%.0fs)"
                      % (i, len(chosen), group, name, time.time() - t0))
            else:
                killed.append((group, name, why))
                print("%3d/%d %-14s %-22s killed    (%.0fs) %s"
                      % (i, len(chosen), group, name, time.time() - t0, why))
            shutil.copy(os.path.join(backup, path), full)
    finally:
        for f in files:
            shutil.copy(os.path.join(backup, f), os.path.join(ROOT, f))
        run(["make"])
        shutil.rmtree(backup)
    print()
    print("%d mutants: %d killed, %d survived, %d not applicable"
          % (len(chosen), len(killed), len(survived), len(broken)))
    for group, name in survived:
        print("SURVIVOR %s/%s" % (group, name))
    for group, name, why in broken:
        print("UNAPPLIED %s/%s: %s" % (group, name, why))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
