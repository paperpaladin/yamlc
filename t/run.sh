#!/bin/sh
# yamlc test suite.
#
# Every assertion compares yamlc against something yamlc did not produce:
# Python's json module, PyYAML, or the text of the YAML 1.2.2 specification
# quoted at the assertion. An assertion that compares yamlc with yamlc is
# not an assertion.
#
# The last line is exactly "N passed, M failed", which the collection's
# tooling parses.
set -u
cd "$(dirname "$0")/.." || exit 2
Y=./yamlc
[ -x "$Y" ] || { echo "yamlc binary missing: run make" >&2; exit 2; }

# The binary must be newer than its sources, or an edit is measured against
# whatever was lying around. Three mutations elsewhere in this collection
# were each recorded as surviving for exactly this reason.
for f in *.go go.mod; do
	if [ "$f" -nt "$Y" ]; then
		echo "$f is newer than $Y: run make, not t/run.sh" >&2
		exit 2
	fi
done

# A missing oracle is a refusal, not a reduced run. Python's json module is
# the authority for the whole JSON subset and PyYAML is the authority for
# what a 1.1 reader makes of a document; without them this suite would
# print a number having checked almost nothing.
command -v python3 >/dev/null 2>&1 || {
	echo "yamlc: python3 is not installed and is both oracles" >&2
	exit 2
}
PYYAML=$(python3 t/pyyaml.py version 2>/dev/null) || {
	echo "yamlc: PyYAML is not installed and is the oracle for YAML 1.1" >&2
	exit 2
}
case "$PYYAML" in
6.0.2) ;;
*) echo "yamlc: PyYAML $PYYAML, the recorded numbers are against 6.0.2" >&2 ;;
esac

W=$(mktemp -d "${TMPDIR:-/tmp}/yamlc.XXXXXXXX") || exit 2
trap 'rm -rf "$W"' EXIT INT TERM
# $W/d holds the documents the cases below write, and nothing else, so
# that the construct counter at the end can be pointed at exactly what
# this run fed the parser.
mkdir -p "$W/d" "$W/o"

pass=0
fail=0
ok()  { pass=$((pass+1)); echo "ok   $*"; }
bad() { fail=$((fail+1)); echo "FAIL $*"; }

# eq NAME EXPECTED ACTUAL. Both sides are printed on a mismatch, because a
# failure that does not say what it saw costs a reproduction run.
eq() {
	if [ "$2" = "$3" ]; then
		ok "$1"
	else
		bad "$1"
		echo "       expected: $2"
		echo "       actual:   $3"
	fi
}

# status NAME WANT CMD... -- the exit status of a command.
status() {
	name=$1; want=$2; shift 2
	"$@" >/dev/null 2>&1
	eq "$name" "$want" "$?"
}

# doc NAME FMT -- write a document from a printf format string, so that a
# case can hold an exact byte without a separate file.
doc() {
	printf '%b' "$2" > "$W/d/$1"
	echo "$W/d/$1"
}

# accept NAME FMT -- yamlc reads the document, and PyYAML reads it too.
# The second half is the part that matters: a parser that refuses valid
# input is as wrong as one that reads invalid input, and only an outside
# reader can say which this is.
accept() {
	f=$(doc "$1" "$2")
	got=$($Y check "$f" 2>&1)
	case "$got" in
	*": ok") ok "accept/$1" ;;
	*) bad "accept/$1"; echo "       yamlc: $got" ;;
	esac
	o=$(python3 t/pyyaml.py accept "$f")
	case "$o" in
	ok) ok "accept/$1/pyyaml" ;;
	*) bad "accept/$1/pyyaml: PyYAML $o on a document we accept" ;;
	esac
}

# refuse NAME POS FMT -- yamlc refuses at exactly POS (line:col), and
# PyYAML refuses the document as well. METHOD's rule: a refusal needs an
# outside authority too, or the suite is only checking that we are
# consistently fussy.
refuse() {
	f=$(doc "$1" "$3")
	got=$($Y check "$f" 2>&1)
	eq "refuse/$1" "$f:$2" "$(echo "$got" | sed 's/: [^:]*$//; s/\(:[0-9]*:[0-9]*\).*/\1/')"
	o=$(python3 t/pyyaml.py accept "$f")
	case "$o" in
	refused:*) ok "refuse/$1/pyyaml" ;;
	*) bad "refuse/$1/pyyaml: PyYAML reads a document we refuse" ;;
	esac
}

# accept_only NAME FMT -- yamlc reads it and PyYAML does not, because 1.1
# and 1.2 disagree about the document itself rather than about a scalar.
# Recorded in the README with its cause.
accept_only() {
	f=$(doc "$1" "$2")
	got=$($Y check "$f" 2>&1)
	case "$got" in
	*": ok") ok "accept1.2/$1" ;;
	*) bad "accept1.2/$1"; echo "       yamlc: $got" ;;
	esac
	o=$(python3 t/pyyaml.py accept "$f")
	case "$o" in
	refused:*) ok "accept1.2/$1/pyyaml-refuses-it" ;;
	*) bad "accept1.2/$1: PyYAML reads it too, so this belongs in accept" ;;
	esac
}

# refuse_only NAME POS FMT -- yamlc refuses and PyYAML does not, for a
# reason recorded in the README. Counted apart from refuse so that the
# number of places the two readers differ is never hidden inside a pass.
refuse_only() {
	f=$(doc "$1" "$3")
	got=$($Y check "$f" 2>&1)
	eq "refuse1.2/$1" "$f:$2" "$(echo "$got" | sed 's/\(:[0-9]*:[0-9]*\).*/\1/')"
	o=$(python3 t/pyyaml.py accept "$f")
	case "$o" in
	ok) ok "refuse1.2/$1/pyyaml-reads-it" ;;
	*) bad "refuse1.2/$1: PyYAML also refuses, so this belongs in refuse" ;;
	esac
}

# ev NAME FMT -- the event stream must be identical to PyYAML's. This is the
# structural oracle and it carries no resolution at all: a plain scalar comes
# out as a plain scalar whatever its content looks like, so a disagreement
# here is about the grammar and never about a schema.
ev() {
	f=$(doc "$1" "$2")
	a=$(python3 t/pyyaml.py events "$f" 2>/dev/null)
	b=$($Y events "$f" 2>&1)
	eq "ev/$1" "$a" "$b"
}

# ev_diff NAME FMT -- the two event streams must DIFFER, for a reason
# recorded in the README. Asserting the difference rather than skipping the
# case means that a divergence which quietly goes away turns the suite red
# instead of sitting here as a passing exception.
ev_diff() {
	f=$(doc "$1" "$2")
	a=$(python3 t/pyyaml.py events "$f" 2>/dev/null)
	b=$($Y events "$f" 2>&1)
	# yamlc has to READ the document. Without this line the case passes
	# when yamlc refuses it, because a refusal differs from PyYAML's
	# events just as a different reading does, and that is how
	# plain-colon-flow sat green for a week while "[:x]" was refused.
	case "$b" in
	"+STR"*) ;;
	*) bad "ev1.2/$1: yamlc does not read it: $b"; return ;;
	esac
	if [ "$a" = "$b" ]; then
		bad "ev1.2/$1: PyYAML now agrees, so this is no longer a divergence"
	else
		ok "ev1.2/$1"
	fi
}

# res NAME FMT -- the 1.1 reading of a document must be exactly what
# PyYAML's safe_load made of it, compared as canonical text. This is the
# oracle for the whole 1.1 resolution table, including its octal, its
# sexagesimal and its timestamps. --dup=last is PyYAML's duplicate key
# policy; yamlc's own default is to refuse.
res() {
	f=$(doc "$1" "$2")
	a=$(python3 t/pyyaml.py dump "$f" | cut -f3-)
	b=$($Y dump --schema=1.1 --dup=last "$f" 2>&1)
	eq "res11/$1" "$a" "$b"
}

# r12 NAME FMT WANT -- the 1.2 core schema reading. There is no second
# implementation of YAML 1.2 on this machine, so WANT comes from the table
# in section 10.2.1.2, quoted in resolve.go, and not from an oracle. The
# README says so. Where a case is also valid JSON the json comparison
# checks the same scalar against Python's json, which is an oracle.
r12() {
	f=$(doc "$1" "$2")
	eq "res12/$1" "$3" "$($Y dump --dup=last "$f" 2>&1)"
}

# refuse_res NAME FMT -- the 1.1 reading must be a refusal on both sides.
# A document can parse and still have no 1.1 value, which is a different
# failure from a syntax error and is counted separately.
refuse_res() {
	f=$(doc "$1" "$2")
	a=$(python3 t/pyyaml.py dump "$f" | cut -f2)
	b=$($Y dump --schema=1.1 --dup=last "$f" 2>&1)
	eq "refuseres/$1/pyyaml" "err" "$a"
	case "$b" in
	*": "*) ok "refuseres/$1" ;;
	*) bad "refuseres/$1: yamlc read it as $b" ;;
	esac
}

# res_diff NAME FMT -- the two 1.1 readings must DIFFER, for a reason in
# the README.
res_diff() {
	f=$(doc "$1" "$2")
	a=$(python3 t/pyyaml.py dump "$f" | cut -f3-)
	b=$($Y dump --schema=1.1 --dup=last "$f" 2>&1)
	if [ "$a" = "$b" ]; then
		bad "res1.1/$1: PyYAML now agrees, so this is no longer a divergence"
	else
		ok "res1.1/$1"
	fi
}

# refuse_at NAME POS FMT -- yamlc refuses at exactly POS, with the oracle
# assertion made separately by whichever helper fits that case. Used where
# PyYAML refuses the document too but at a different stage, so neither
# refuse nor refuse_dup says the right thing.
refuse_at() {
	f=$(doc "$1" "$3")
	got=$($Y check "$f" 2>&1)
	eq "refuseat/$1" "$f:$2" "$(echo "$got" | sed 's/\(:[0-9]*:[0-9]*\).*/\1/')"
}

# refuse_dup NAME POS FMT -- yamlc refuses the document for a repeated
# key at exactly POS, and PyYAML reads it, because PyYAML keeps the last
# value. The two behaviours are both in the README; this asserts the
# refusal has the right position and that PyYAML really does differ.
refuse_dup() {
	f=$(doc "$1" "$3")
	got=$($Y check "$f" 2>&1)
	eq "dup/$1" "$f:$2" "$(echo "$got" | sed 's/\(:[0-9]*:[0-9]*\).*/\1/')"
	a=$(python3 t/pyyaml.py dump "$f" | cut -f2)
	eq "dup/$1/pyyaml-reads-it" "ok" "$a"
}

# dup_policy NAME POLICY FMT WANT -- what --dup=first and --dup=last do.
dup_policy() {
	f=$(doc "$1" "$3")
	eq "duppolicy/$1" "$4" "$($Y dump --dup=$2 "$f" 2>&1)"
}

# dup_schema11 NAME FMT -- a document that is a duplicate key error under
# the 1.1 rules and a valid two-entry mapping under 1.2. This is the shape
# the whole second half of the project is about.
dup_schema11() {
	f=$(doc "$1" "$2")
	out=$($Y dump --schema=1.1 "$f" 2>&1)
	case "$out" in
	*"key repeats"*) ok "dupschema/$1" ;;
	*) bad "dupschema/$1: 1.1 read it as $out" ;;
	esac
}

# go vet is a gate here rather than a separate target nobody runs. It
# costs about a second and it has caught a printf verb mismatch in this
# project already.
GOPROXY=off GOTOOLCHAIN=local go vet ./... 2>"$W/o/vet.err"
if [ $? -eq 0 ]; then ok "go vet"; else bad "go vet"; cat "$W/o/vet.err"; fi

status "usage/no-args"      2 $Y
status "usage/bad-command"  2 $Y nonesuch
status "usage/check-nofile" 2 $Y check

# --- the input character set (YAML 1.2.2 section 5.1) -----------------
#
# c-printable is TAB, LF, CR, #x20-#x7E, #x85, #xA0-#xD7FF, #xE000-#xFFFD
# and #x10000-#x10FFFF. Anything else is refused with its position.
accept "plain"      'a b\n'
# A TAB between two plain characters: nb-ns-plain-in-line in section 7.3.3
# is ( s-white* ns-plain-char )*, so a tab inside a plain scalar is content.
# PyYAML refuses it outright with "cannot start any token", which is a
# restriction in that scanner rather than a 1.1 rule.
accept_only "tab-inline" 'a\tb\n'
# U+0085 NEL is a printable character in 1.2 and a line break in 1.1. Both
# readers accept the document and they read different scalars out of it:
# PyYAML folds the break into a space and gets "a b c".
accept "nel"        'a b\302\205c\n'
accept "high"       'a \343\201\202\n'
accept "bom"        '\357\273\277a b\n'
accept "crlf"       'a b\r\nc d\r\n'
accept "cr-only"    'a b\rc d\r'

refuse "bel"        '2:1' 'a b\n\007c\n'
refuse "nul"        '1:4' 'a b\000\n'
refuse "del"        '1:4' 'a b\177\n'
refuse "c1"         '1:4' 'a b\302\200\n'
refuse "bad-utf8"   '1:3' 'ab\303\050\n'
refuse "trunc-utf8" '1:3' 'ab\303\n'
refuse "ffff"       '1:4' 'a b\357\277\277\n'

# --- plain scalars and the stream (sections 7.3.3, 6.5, 9.1) -----------
#
# Folding, section 6.5: "a single line break is folded into a single
# space", and "n+1 consecutive breaks are folded into n line feeds".
ev "plain-one"       'foo\n'
ev "plain-words"     'foo bar\n'
ev "plain-fold"      'foo\nbar\n'
ev "plain-fold-2"    'foo\n\nbar\n'
ev "plain-fold-3"    'foo\n\n\nbar\n'
ev "plain-strip"     'foo  \n  bar\n'
ev "plain-comment"   'foo # c\n'
ev "plain-dash"      '-foo\n'
ev "plain-colon"     'a:b\n'
ev "plain-colons"    'a::b\n'
ev "plain-question"  '?foo\n'
ev "plain-indented"  '  foo\n'
ev "plain-bracket"   'a]b\n'
ev "empty-stream"    ''
ev "comment-only"    '# nothing here\n'
ev "blank-only"      '\n\n\n'
ev "doc-explicit"    '--- foo\n'
ev "doc-empty"       '--- \n'
ev "doc-two"         '--- a\n--- b\n'
ev "doc-suffix"      'foo\n...\n'
ev "doc-trailing"    'a\n# tail comment\n'
ev "anchor"          '&a foo\n'
ev "alias"           '*a\n'
ev "tag-secondary"   '!!str foo\n'
ev "tag-local"       '!foo bar\n'
ev "tag-verbatim"    '!<tag:example.com,2000:x> y\n'
ev "tag-nonspecific" '! foo\n'
ev "tag-percent"     '!<tag:a%2Cb> y\n'
ev "prop-both"       '&a !!str foo\n'
ev "prop-both-rev"   '!!str &a foo\n'
ev "prop-own-line"   '&a\n  foo\n'
ev "yaml-directive"  '%YAML 1.2\n--- foo\n'
ev "tag-directive"   '%TAG !e! tag:example.com,2000:\n--- !e!foo x\n'
ev "tag-primary-dir" '%TAG ! tag:example.com,2000:\n--- !foo x\n'
ev "reserved-dir"    '%FOO bar baz\n--- x\n'

# --- quoted scalars (sections 7.3.1, 7.3.2, 5.7) -----------------------
ev "sq-simple"       "'abc'\n"
ev "sq-escape"       "'it''s'\n"
ev "sq-empty"        "''\n"
ev "sq-fold"         "'x\n  y'\n"
ev "sq-fold-blank"   "'x\n\n  y'\n"
ev "sq-trailing-ws"  "'x  \n  y'\n"
ev "sq-hash"         "'a # b'\n"
ev "sq-colon"        "'a: b'\n"
ev "dq-simple"       '"abc"\n'
ev "dq-empty"        '""\n'
ev "dq-tab"          '"a\\tb"\n'
ev "dq-escapes"      '"\\0\\a\\b\\t\\n\\v\\f\\r\\e\\ \\"\\/\\\\"\n'
ev "dq-named"        '"\\N\\_\\L\\P"\n'
ev "dq-hex"          '"\\x41\\xe9"\n'
ev "dq-u16"          '"\\u00e9\\u20ac"\n'
ev "dq-u32"          '"\\U0001F600"\n'
ev "dq-lone-surr"    '"\\ud800"\n'
ev "dq-fold"         '"x\n  y"\n'
ev "dq-escaped-nl"   '"a\\\nb"\n'
ev "dq-escaped-nl-blank" '"a\\\n\n  b"\n'
ev "dq-real-tab"     '"a\tb"\n'

# PyYAML does not join a surrogate pair: it produces two lone surrogates
# where yamlc produces U+1F600. Python's json joins the pair, and the JSON
# subset is where this project is not allowed to differ, so yamlc follows
# json here. YAML 1.2 defines no escape for a surrogate at all.
ev_diff "dq-surrogate-pair" '"\\ud83d\\ude00"\n'

refuse "sq-unclosed"   '1:1' "'abc\n"
refuse "dq-unclosed"   '1:1' '"abc\n'
refuse "dq-bad-escape" '1:3' '"a\\qb"\n'
refuse "dq-short-hex"  '1:3' '"a\\x4"\n'
refuse "dq-short-u"    '1:3' '"a\\u00"\n'

# --- block scalars (sections 8.1.1 to 8.1.3) ---------------------------
ev "lit-simple"      '--- |\n  x\n'
ev "lit-two-lines"   '--- |\n  x\n  y\n'
ev "lit-more-ind"    '--- |\n  x\n   y\n  z\n'
ev "lit-blank"       '--- |\n  x\n\n  y\n'
ev "lit-strip"       '--- |-\n  x\n\n'
ev "lit-keep"        '--- |+\n  x\n\n'
ev "lit-clip"        '--- |\n  x\n\n\n'
ev "lit-explicit-2"  '--- |2\n  x\n'
ev "lit-explicit-4"  '--- |4\n      x\n'
ev "lit-ind-then-ch" '--- |2-\n  x\n'
ev "lit-ch-then-ind" '--- |-2\n  x\n'
ev "lit-comment"     '--- | # why\n  x\n'
ev "lit-empty"       '--- |\n'
ev "lit-only-blanks" '--- |\n\n\n'
ev "lit-keep-blanks" '--- |+\n\n'
ev "lit-tab-content" '--- |\n  \tx\n'
ev "lit-trailing-sp" '--- |\n  x  \n'
ev "fold-simple"     '--- >\n  x\n  y\n'
ev "fold-blank"      '--- >\n  x\n\n  y\n'
ev "fold-more-ind"   '--- >\n  x\n   y\n  z\n'
ev "fold-lead-blank" '--- >\n\n  x\n'
ev "fold-strip-blank" '--- >-\n  a\n  \n  b\n'
ev "fold-three"      '--- >\n x\n\n\n y\n'
ev "fold-keep"       '--- >+\n  x\n\n'

refuse "blk-zero-ind"  '1:6' '--- |0\n  x\n'
refuse "blk-two-chomp" '1:7' '--- |+-\n  x\n'
refuse "blk-two-ind"   '1:7' '--- |22\n  x\n'
refuse "blk-junk"      '1:6' '--- |x\n  x\n'
refuse "blk-lead-deep" '2:1' '--- |\n    \n  x\n'

# --- block collections (sections 8.2.1, 8.2.2) -------------------------
ev "seq-two"         '- a\n- b\n'
ev "seq-nested-same" '- - a\n'
ev "seq-nested-deep" '- - - a\n'
ev "seq-empty-first" '-\n- a\n'
ev "seq-empty-mid"   '- a\n-\n- c\n'
ev "seq-fold-entry"  '- a\n  b\n'
ev "seq-wide-dash"   '-   a\n'
ev "seq-indented"    '  - a\n  - b\n'
ev "seq-comment"     '- a\n# why\n- b\n'
ev "seq-blankline"   '- a\n\n- b\n'
ev "seq-blockscalar" '- |\n  x\n- y\n'
ev "map-one"         'a: b\n'
ev "map-two"         'a: b\nc: d\n'
ev "map-nested"      'a:\n  b: 1\n'
ev "map-deep"        'a:\n  b:\n    c: 1\n'
ev "map-seq-same"    'a:\n- 1\n- 2\n'
ev "map-seq-indent"  'a:\n  - 1\n  - 2\n'
ev "map-empty-val"   'a:\n'
ev "map-empty-both"  'a:\nb:\n'
ev "map-comment-val" 'a: #c\n'
ev "map-comment-mid" 'a: b\n# why\nc: d\n'
ev "map-colons"      'a:::\n'
ev "map-quoted-key"  '"a": b\n'
ev "map-sq-key"      "'a': b\n"
refuse "map-json-adjacent" '1:5' '"a":b\n'
ev "map-anchor-key"  '&a b: c\n'
ev "map-tag-key"     '!!str a: b\n'
ev "map-anchor-val"  'a: &x\n  b: 1\n'
ev "map-tag-node"    '!!map\n  a: 1\n'
ev "map-in-seq"      '- a: 1\n  b: 2\n'
ev "map-seq-map"     'a:\n- b: 1\n- c: 2\n'
ev "map-blockscalar" 'a: |\n  x\nb: 1\n'
ev "map-folded-val"  'a: >-\n  x\nb: 1\n'
ev "map-multiline-v" 'a: b\n  c\nd: e\n'
ev "exp-key"         '? a\n: b\n'
ev "exp-key-noval"   '? a\n'
ev "exp-key-twice"   '? a\n? b\n'
ev "exp-key-mixed"   'a: 1\n? b\n: 2\n'
ev "exp-key-seq"     '? - a\n: b\n'
ev "exp-key-empty"   '? \n: a\n'
ev "exp-key-nested"  '? a\n:\n  b: 1\n'
ev "deep-mix"        '- a:\n    - b:\n        - c\n'

# A mapping with no key at all. e-node stands in the key position of
# l-block-map-implicit-entry, so ": a" is a mapping from null to "a".
# PyYAML refuses it.
accept_only "map-no-key"  ': a\n'
ev_diff "map-no-key-ev"   ': a\n'

# s-separate-in-line is s-white+, and s-white is space OR tab, so a tab
# after a block indicator is separation whitespace. PyYAML refuses every
# one of these with "cannot start any token".
accept_only "tab-after-colon" 'a:\tb\n'
accept_only "tab-after-dash"  '-\tb\n'
accept_only "tab-after-quest" '?\ta\n'

refuse "map-value-twice"  '1:5' 'a: b: c\n'
refuse "map-bad-indent"   '1:4' 'a: 1\n b: 2\n'
refuse "map-deeper-key"   '3:2' 'a:\n  b: 1\n c: 2\n'
refuse "seq-bad-indent"   '3:3' 'a:\n   - 1\n  - 2\n'
refuse "key-no-colon"     '2:2' '? a\nb\n'
refuse "blk-less-indent"  '3:2' 'a: |\n  x\n y\n'
refuse "seq-then-deeper"  '1:6' '- a: 1\n   b: 2\n'
refuse "key-multiline"    '1:4' 'a: b\n  c: 1\n'

# --- flow collections (section 7.4), which is also the JSON subset ----
ev "flow-seq"        '[1,2]\n'
ev "flow-map"        '{a: 1}\n'
ev "flow-json"       '{"a":1}\n'
ev "flow-seq-empty"  '[]\n'
ev "flow-map-empty"  '{}\n'
ev "flow-nested"     '[a, [b, [c]]]\n'
ev "flow-nest-empty" '[[[]]]\n'
ev "flow-seq-trail"  '[1, 2,]\n'
ev "flow-map-trail"  '{a: b,}\n'
ev "flow-pair-in-seq" '[a: b]\n'
ev "flow-explicit"   '{? a}\n'
ev "flow-explicit-v" '{? a: b}\n'
ev "flow-bare-key"   '{a}\n'
ev "flow-empty-val"  '{a: }\n'
ev "flow-colon-in"   '[a:b]\n'
ev "flow-colon-map"  '{a:b}\n'
ev "flow-over-lines" 'a: [1,\n2]\n'
ev "flow-multiline"  '[\n  1,\n  2\n]\n'
ev "flow-comment"    '[a, # why\n b]\n'
ev "flow-seq-key"    '[1,2]: x\n'
ev "flow-map-key"    '{a: 1}: x\n'
ev "flow-key-explicit" '? [a]\n: b\n'
ev "flow-anchor-null" '[&a, b]\n'
ev "flow-anchor"     '[&a 1, *a]\n'
ev "flow-tag"        '[!!str a]\n'
ev "flow-in-map"     'a: {b: [c, d]}\n'
ev "flow-quoted-fold" '["a\n  b"]\n'
ev "flow-deep-json"  '{"a":[1,{"b":null}],"c":true}\n'

refuse "flow-seq-comma"  '1:2' '[,]\n'
refuse "flow-map-comma"  '1:2' '{,}\n'
refuse "flow-unclosed"   '1:1' '[1, 2\n'
refuse "flow-map-unclosed" '1:1' '{a: 1\n'
refuse "flow-wrong-close" '1:6' '[1, 2}\n'
refuse "flow-blockscalar" '1:2' '[|\n  x\n]\n'
refuse "flow-marker-in"  '1:1' '[1,\n--- 2\n'
refuse "flow-two-anchors" '1:5' '[&a &b x]\n'

# c-ns-flow-map-empty-key-entry: a flow mapping entry may have no key, so
# "{: 1}" is a mapping from null. PyYAML refuses it.
accept_only "flow-empty-key" '{: 1}\n'
ev_diff "flow-empty-key-ev"  '{: 1}\n'

# c-ns-flow-map-json-key-entry allows s-separate between a JSON-like key
# and its colon, so the colon may be on the next line. PyYAML refuses that
# and requires the colon to follow on the key's own line.
accept_only "flow-colon-nextline" '{"a"\n: 1}\n'
ev_diff "flow-colon-nextline-ev"  '{"a"\n: 1}\n'

# ns-flow-map-yaml-key-entry allows s-separate(n,c) there too, so a plain
# key may have its colon on a later line, which PyYAML also refuses. The
# break is allowed only onto a line indented past the block level, so the
# same document at column zero is a refusal. A pair written inside a flow
# SEQUENCE is ns-flow-pair, which ends in s-separate-in-line and does not
# cross a break at all, so "[ key" newline " : value ]" stays refused.
# c-comment needs s-separate-in-line before it, which is a break or at
# least one blank, and the blank may have been eaten by whatever scanned
# the node before. A plain scalar inside a flow collection takes its own
# trailing spaces, so the comment after it looked like content.
ev "flow-comment-after-plain"  '[ b # c\n]\n'
ev "flow-comment-after-plain2" 'a: [\n b # c\n ]\n'

accept_only "flow-plain-colon-nextline" '{foo\n: bar}\n'
accept_only "flow-plain-colon-indented" 'k: {\n k\n :\n v\n }\n'
refuse "flow-plain-colon-column0" "3:1" 'k: {\nk\n:\nv\n}\n'
refuse "flow-seq-pair-colon-nextline" "3:3" '---\n[ key\n  : value ]\n'

# --- resolution (section 10.2.1.2 for 1.2, PyYAML's resolver for 1.1) --
res "r-bools"      '[yes, no, on, off, true, false, True, FALSE, y, n]\n'
res "r-nulls"      '[~, null, Null, NULL, ]\n'
res "r-ints"       '[0, 7, -7, +7, 010, 0x1f, 0b101, 1_000, 1:30, 12:00:01]\n'
res "r-floats"     '[1.0, .5, 5., -1.5, 1.0e+3, .inf, -.Inf, .nan, 190:20:30.15]\n'
res "r-not-nums"   '[0o17, 1e5, 1.0e5, 0x1p3, 1.2.3, +.inf, 1:60, 08]\n'
res "r-stamps"     '[2001-12-14, 2001-12-14 21:59:43.10 -5, 2001-12-14t21:59:43.10-05:00, 2001-12-14 21:59:43Z]\n'
res "r-not-stamps" '[2001-1-1, 20011214]\n'
res "r-strings"    '[abc, "123", '"'"'1.0'"'"', a b, "", Yes!, NULLs]\n'
res "r-tagged"     '[!!str 1, !!int "7", !!bool "yes", !!float "1", !!null ""]\n'
res "r-tagged-oct" '[!!int 010, !!int 0x10]\n'
res "r-binary"     '!!binary "QUJD"\n'
res "r-nested"     'a:\n  b: [1, {c: 2.5}]\n'
res "r-blockscalar" 'a: |\n  1\nb: >\n  yes\n'
res "r-quoted-num" '["1", 1]\n'
res "r-big-int"    '[99999999999999999999, -99999999999999999999]\n'
res "r-float-prec" '[0.1, 1e308, 5e-324, 1.7976931348623157e+308]\n'

# The 1.2 core schema. No second 1.2 implementation is installed, so these
# come from the specification table and are not oracle-backed.
r12 "c-bools"      '[yes, no, true, True, TRUE, false]\n' '[str yes, str no, true, true, true, false]'
r12 "c-nulls"      '[~, null, Null, NULL, ]\n'            '[null, null, null, null]'
r12 "c-ints"       '[7, -7, +7, 0o17, 0x1f, 010, 0b101]\n' '[int 7, int -7, int 7, int 15, int 31, int 10, str 0b101]'
r12 "c-sexagesimal" '[1:30]\n'                            '[str 1:30]'
r12 "c-floats"     '[1.0, .5, 5., 1e5, 1.0E+3, .inf, -.INF, .NaN]\n' '[float 3ff0000000000000, float 3fe0000000000000, float 4014000000000000, float 40f86a0000000000, float 408f400000000000, float 7ff0000000000000, float fff0000000000000, float nan]'
r12 "c-stamp-str"  '[2001-12-14]\n'                       '[str 2001-12-14]'
r12 "c-underscore" '[1_000]\n'                            '[str 1_000]'
r12 "c-merge-str"  '[<<, =]\n'                            '[str <<, str =]'
r12 "c-quoted"     '["true", '"'"'7'"'"']\n'              '[str true, str 7]'
r12 "c-nonspecific" '[! 7, ! true]\n'                     '[str 7, str true]'
r12 "c-tag-bool-no" '!!bool "true"\n'                     'true'
r12 "c-tag-int-oct" '[!!int 010, !!int 0o10, !!int 0x10]\n' '[int 10, int 8, int 16]'

# An impossible date matches the 1.1 timestamp regex and then fails to be a
# date. PyYAML raises ValueError from datetime and refuses the document, so
# yamlc refuses it too rather than inventing a thirteenth month.
refuse_res "r-bad-date" '2001-13-99\n'

# A merge or value key outside the key position of a mapping has a 1.1 tag
# and no 1.1 constructor, so PyYAML refuses the whole document. yamlc reads
# both as strings. Recorded rather than fixed: refusing a sequence because
# it holds the two characters "<<" would be a worse answer.
res_diff "r-merge-loose" '[<<, =]\n'

refuse "dir-no-doc"    '2:1' '%YAML 1.2\nfoo\n'
refuse "dir-bare-pct"  '1:1' '%\n--- foo\n'
# A tab at column zero in a bare document is s-separate-in-line, not
# indentation: s-l+flow-in-block(-1) opens with s-separate(0,flow-out).
# So a flow node or a scalar may follow one, and the YAML test suite says
# so in 6CA3 and Q5MG. This used to be a refuse case asserting the
# opposite, and it passed because PyYAML refuses the document as well.
# PyYAML is not the authority for a 1.2 production, which is the whole
# reason the test suite is now an oracle here.
accept_only "tab-then-scalar" '\tfoo\n'
accept_only "tab-then-flow-map" '\t{}\n'
accept_only "tab-then-flow-seq" '\t[\n\t]\n'
# A block collection still needs s-indent, which is spaces.
refuse "tab-then-block-map" '1:5' '\tfoo: 1\n'
refuse "tab-then-block-seq" '1:2' '\t- a\n'
refuse "unknown-handle" '1:1' '!e!foo x\n'
refuse "two-anchors"   '1:4' '&a &b x\n'
refuse "two-tags"      '1:4' '!a !b x\n'
refuse "anchor-noname" '1:2' '& x\n'
refuse "verbatim-open" '1:8' '!<tag:x y\n'

# Section 6.8.1: a 1.2 processor must refuse a major version it does not
# implement. PyYAML accepts any version at all, so this one is ours alone.
refuse "yaml-2.0"       '1:1' '%YAML 2.0\n--- foo\n'
refuse "yaml-twice"     '2:1' '%YAML 1.2\n%YAML 1.2\n--- foo\n'

# A bare document after an explicit ... is l-any-document in section 9.2,
# and PyYAML refuses it.
accept_only "bare-after-suffix" 'a\n...\nb\n'
accept_only "suffix-alone"      '...\n'
ev_diff "bare-after-suffix-ev"  'a\n...\nb\n'
ev_diff "nel-fold"              'a b\302\205c\n'

# --- the merge key and duplicate keys ---------------------------------
#
# << is a 1.1 type that every 1.2 implementation also carries, and the
# comparison here is against PyYAML's flatten_mapping.
res "merge-one"      'b: &b {x: 1, y: 2}\nd:\n  <<: *b\n  y: 3\n'
res "merge-seq"      'p: &p {x: 1}\nq: &q {x: 2, y: 2}\nd:\n  <<: [*p, *q]\n  z: 3\n'
res "merge-inline"   'b: &b {x: 1}\nd: {<<: *b, y: 2}\n'
res "merge-nested"   'a: &a {x: 1}\nb: &b {<<: *a, y: 2}\nc: {<<: *b, z: 3}\n'
res "merge-literal"  'd:\n  <<: {x: 1}\n  y: 2\n'
res "merge-only"     'b: &b {x: 1}\nd:\n  <<: *b\n'
res "merge-order"    'b: &b {x: 1, y: 2}\nd:\n  y: 9\n  <<: *b\n  z: 3\n'
res "merge-twice"    'p: &p {x: 1}\nq: &q {y: 2}\nd:\n  <<: *p\n  <<: *q\n'
res "merge-quoted"   'd:\n  "<<": 1\n'
res "merge-value-eq" 'd:\n  =: 1\n'

# A << whose value is not a mapping is a refusal in both readers, but
# PyYAML only notices when it constructs, so the comparison is against its
# loader rather than against its parser.
refuse_res "merge-scalar"  'd:\n  <<: 1\n'
refuse_at  "merge-scalar"  '2:7' 'd:\n  <<: 1\n'
refuse_res "merge-seq-bad" 'd:\n  <<: [1]\n'
refuse_at  "merge-seq-bad" '2:7' 'd:\n  <<: [1]\n'

# Duplicate keys. Section 3.2.1.1: "the content of a mapping node is an
# unordered set of key/value node pairs, with the restriction that each of
# the keys is unique". yamlc refuses by default and PyYAML keeps the last,
# so every comparison above runs with --dup=last and these check the
# refusal itself.
refuse_dup "dup-plain"    '2:1' 'a: 1\na: 2\n'
refuse_dup "dup-flow"     '1:8' '{a: 1, a: 2}\n'
refuse_dup "dup-quoted"   '1:8' '{a: 1, "a": 2}\n'
refuse_dup "dup-anchor"   '1:11' '{a: &x 1, a: 2}\n'
# A sequence as a mapping key is unhashable in Python, so PyYAML refuses
# this document for its own reason rather than for the duplicate.
refuse_at  "dup-seq-key" '1:10' '{[1]: a, [1]: b}\n'
refuse_res "dup-seq-key" '{[1]: a, [1]: b}\n'
refuse_dup "dup-null-key" '1:8' '{~: a, null: b}\n'
refuse_dup "dup-nested"   '1:11' 'x: {k: 1, k: 2}\ny: 1\n'

dup_policy "dup-last"  'last'  'a: 1\na: 2\n' '{str a: int 2}'
dup_policy "dup-first" 'first' 'a: 1\na: 2\n' '{str a: int 1}'

# 1 and 1.0 are different keys because they resolve to different types.
r12 "dup-int-float" '{1: a, 1.0: b}\n' '{int 1: str a, float 3ff0000000000000: str b}'

# And the one that only a tool like this will tell you about: yes and true
# are the SAME key under 1.1 and two different keys under 1.2, so this
# document is a duplicate key error to a 1.1 reader and a two-entry
# mapping to a 1.2 reader.
r12 "dup-schema-11"  '{yes: 1, true: 2}\n' '{str yes: int 1, true: int 2}'
dup_schema11 "dup-schema-11-refused" '{yes: 1, true: 2}\n'

# --- implicit key length, and a trailing tab --------------------------
#
# The note in section 7.4.2: "implicit keys are restricted to a single line
# and must not span more than 1024 Unicode characters". PyYAML enforces it
# too, with "simple key is too long", so the boundary is checked on both
# sides of it.
LONGKEY=$(python3 -c 'print("x"*1030, end="")')
SHORTKEY=$(python3 -c 'print("x"*1020, end="")')
refuse "key-too-long"  '1:1' "$LONGKEY: 1\n"
accept "key-just-fits"       "$SHORTKEY: 1\n"
refuse "key-too-long-flow" '1:2' "{$LONGKEY: 1}\n"

# A trailing tab after a plain scalar. nb-ns-plain-in-line allows s-white
# between plain characters and the trailing run is trimmed by the folding
# rules, so this is a document in 1.2. PyYAML refuses it with "while
# scanning for the next token".
accept_only "trailing-tab" 'a: 1 \nb: 2\t\n'
ev_diff "trailing-tab-ev" 'a: 1 \nb: 2\t\n'

# --- documents the task named, which no generator produces ------------
ev "hand-nested-dash"  '- - -\n'
ev "hand-tag-on-null"  '!!null ~\n'
ev "hand-tag-empty"    'a: !!null\n'
ev "hand-tag-str-key"  '? !!str a\n: b\n'
ev "hand-anchor-empty" 'a: &x\nb: *x\n'
ev "hand-binary"       '- !!binary QUJD\n'
ev "hand-marker-in-block" '--- |\n  ---\n'
ev "hand-comment-in-folded" 'a: >\n  x\n  # not a comment\n'
ev "hand-comment-before-val" 'a:\n  # c\n  b: 1\n'
ev "hand-comment-after-dash" '- # c\n  a\n'
ev "hand-blank-before-val" 'a:\n\n  b: 1\n'
ev "hand-blanks-after-block" 'a: |\n  x\n\n\nb: 2\n'
res "hand-binary-value" '- !!binary QUJD\n'

# An alias whose anchor is defined later in the document. Section 3.2.2.2
# scopes an anchor from its definition onwards, so this is a refusal and
# not a forward reference.
refuse_res "alias-before-anchor" 'a: *x\nb: &x 1\n'
refuse_at  "alias-before-anchor" '1:4' 'a: *x\nb: &x 1\n'

# An alias cycle. Refused rather than followed: a reader that follows one
# does not return. PyYAML builds the cycle happily, so its own dump of it
# recurses, which is why the oracle call here is on the parse and the
# refusal is ours.
refuse_at "cycle-flow-seq" '1:5' '&a [*a]\n'
refuse_at "cycle-flow-map" '1:8' '&a {b: *a}\n'
refuse_at "cycle-block"    '5:5' '&a\n- x\n- &b\n  - y\n  - *a\n'
refuse_at "cycle-as-key"   '1:7' '&a {? *a : 1}\n'

# --- what the %YAML directive asks for --------------------------------
#
# A document carrying %YAML 1.1 is asking for the 1.1 resolution rules.
# yamlc honours that unless --schema says otherwise, and diverge reports
# the directive itself as a finding, because a 1.2 processor is not
# obliged to give a 1.1 document 1.1 semantics.
eq "directive11/honoured" "true" \
	"$($Y dump "$(doc dir11 '%YAML 1.1\n--- yes\n')")"
eq "directive11/overridden" "str yes" \
	"$($Y dump --schema=1.2 "$(doc dir11b '%YAML 1.1\n--- yes\n')")"
eq "directive12/default" "str yes" \
	"$($Y dump "$(doc dir12 '%YAML 1.2\n--- yes\n')")"
eq "directive11/per-document" "true;str yes" \
	"$($Y dump "$(doc dir11c '%YAML 1.1\n--- yes\n--- yes\n')" | tr '\n' ';' | sed 's/;$//')"

# --- two more divergences the fuzzer found ----------------------------
#
# ns-plain-first in 7.3.3 allows "?" and ":" to begin a plain scalar when
# what follows them is not whitespace, in flow context as well as block.
# PyYAML's check_plain allows those two only when flow_level is zero, so
# "[?x]" is a one-element sequence here and a single-pair mapping there.
ev      "plain-question-block" '? x\n'
ev_diff "plain-question-flow"  '[?x]\n'
ev_diff "plain-colon-flow"     '[:x]\n'
ev_diff "plain-colon-flow-value" '{x: :x}\n'
ev      "plain-colon-json-key"  '{\"k\"::v}\n'

# PyYAML's mapping is a Python dict, so two keys that are distinct YAML
# nodes and equal Python objects collide there and not here. This is not a
# version difference and not a bug in either reader; it is the data model
# of the host language showing through.
res_diff "key-zero-false"  '{0: a, false: b}\n'
res_diff "key-int-float"   '{5: a, 5.0: b}\n'
res_diff "key-one-true"    '{1: a, true: b}\n'
r12 "key-zero-false-1.2" '{0: a, false: b}\n' '{int 0: str a, false: str b}'

# --- cases that exist because a mutant survived without them ----------
#
# Each of these was added after t/mutants.py reported a survivor, and each
# one kills exactly that mutant. The pattern is always the same: an
# assertion that the document is ACCEPTED says nothing about what was read
# out of it, and an assertion about a refusal elsewhere in the code says
# nothing about this path.
#
# A CRLF pair is one line break (section 5.8). accept/crlf only checked
# that the document parsed, and dropping the pair handling leaves it
# parsing, with one more line break in every scalar.
ev "crlf-block-scalar" '--- |\n  a\r\n  b\r\n'
ev "crlf-plain-fold"   'a\r\nb\r\n'

# The byte order mark is not content (section 5.2). accept/bom likewise
# only checked that the document parsed.
ev "bom-value"         '\357\273\277a b\n'

# Columns count characters, and the cursor has to agree with the input
# validator about that. The first case is the validator's own count and
# the second is the parser's, after a three-byte character: a mutant that
# counted bytes in the cursor survived the first on its own.
refuse "col-after-multibyte" '1:4' 'a \343\201\202\007\n'
refuse_dup "col-dup-multibyte" '1:8' '{\343\201\202: 1, \343\201\202: 2}\n'

# ns-plain-first excludes "#", so a flow entry cannot begin with one. The
# position matters: before the flow comment rule was tightened this was
# refused as an unclosed bracket at 1:1, because the whole line had been
# eaten as a comment, and the exclusion in plainFirst was unreachable.
refuse "flow-hash-first"  '1:2' '[#x]\n'
ev     "flow-hash-after-space" '[a, # c\n b]\n'
ev     "flow-hash-in-plain"    '{a:#x}\n'

# In flow context a ":" followed by a flow indicator ends the plain
# scalar, so the key here is "a" and not "a:".
ev "flow-colon-brace"   '{a:}\n'
ev "flow-colon-bracket" '[a:]\n'

# A single-quoted key is JSON-like, so its colon needs no space.
ev "flow-sq-adjacent"   "{'a':1}\n"

# A tab where the indentation of a mapping value belongs. The existing
# tab case goes through the stream prefix; this one goes through the
# node-content path, which is a different line of code.
refuse "tab-as-value-indent" '2:1' 'a:\n\tb\n'

# And a tab where the indentation is shallower than the node it would
# belong to, which is the one path to that refusal that the stream prefix
# does not reach first.
refuse "tab-shallower"       '3:2' 'a:\n  b:\n \tc\n'

# s-l+flow-in-block(n) is s-separate(n+1) ns-flow-node(n+1), so a quoted
# scalar inside a sequence at column 2 must continue at column 3 or
# deeper. PyYAML does not check this at all and reads the document.
refuse_only "quoted-dedent" '2:5' "a:\n  - 'x\n b'\n"

# --- UTF-16 and UTF-32 input (section 5.2) ----------------------------
#
# A stream may be in any of the three encodings and a processor must work
# out which from the first bytes. PyYAML's reader knows the two UTF-16
# marks and the UTF-8 one, so a UTF-16 stream WITH a mark can be compared
# against it; the four other cases are ours alone and are asserted
# against the UTF-8 spelling of the same document.
enc() {
	python3 -c "import sys;sys.stdout.buffer.write($2)" > "$W/d/$1"
	echo "$W/d/$1"
}

for e in "b'\\xff\\xfe'+'a: b\\n'.encode('utf-16-le')|utf16le-bom" \
	"b'\\xfe\\xff'+'a: b\\n'.encode('utf-16-be')|utf16be-bom" \
	"'a: b\\n'.encode('utf-16-le')|utf16le-bare" \
	"'a: b\\n'.encode('utf-16-be')|utf16be-bare" \
	"b'\\xff\\xfe\\x00\\x00'+'a: b\\n'.encode('utf-32-le')|utf32le-bom" \
	"b'\\x00\\x00\\xfe\\xff'+'a: b\\n'.encode('utf-32-be')|utf32be-bom" \
	"'a: b\\n'.encode('utf-32-le')|utf32le-bare" \
	"'a: b\\n'.encode('utf-32-be')|utf32be-bare"; do
	expr=${e%|*}; nm=${e#*|}
	f=$(enc "enc-$nm" "$expr")
	eq "encoding/$nm" "{str a: str b}" "$($Y dump "$f" 2>&1)"
done

# The two PyYAML knows about must agree with it on the event stream too,
# which is the only outside check on any of this.
for nm in utf16le-bom utf16be-bom; do
	f=$W/d/enc-$nm
	eq "encoding/$nm/pyyaml" "$(python3 t/pyyaml.py events "$f" 2>/dev/null)" "$($Y events "$f" 2>&1)"
done

# A UTF-16 stream carrying half a surrogate pair, and one whose length is
# not a whole number of units.
f=$(enc "enc-lone-high" "b'\\xff\\xfe\\x00\\xd8a\\x00'")
eq "encoding/lone-high" "$f:1:1" "$($Y check "$f" 2>&1 | sed 's/\(:[0-9]*:[0-9]*\).*/\1/')"
f=$(enc "enc-odd-length" "b'\\xff\\xfea\\x00:'")
eq "encoding/odd-length" "$f:1:1" "$($Y check "$f" 2>&1 | sed 's/\(:[0-9]*:[0-9]*\).*/\1/')"

# --- more of the grammar, and one more divergence ---------------------
ev "anchor-redefined"  '\055 &a 1\n\055 &a 2\n\055 *a\n'
ev "tag-secondary-redefined" '%TAG !! tag:e.com,2000:\n--- !!foo x\n'
ev "tag-verbatim-local" '!<!foo> x\n'
ev "tag-verbatim-bang"  '!<!> x\n'
ev "explicit-compact-key" '? a : b\n'
ev "tag-on-empty-doc"   '--- !!str\n'
ev "anchor-on-empty-doc" '--- &a\n'
ev "block-scalar-empty" 'a: |\n'
ev "flow-plain-fold"    '[a\n b]\n'
ev "doc-comment-only"   '--- #c\n'
ev "comment-at-col0"    'a:\n#c\nb: 1\n'
ev "comment-in-seq"     '- a\n # c\n- b\n'
ev "two-suffixes"       '--- a\n...\n...\n'
ev "empty-doc-after"    'a: 1\n---\n'

refuse "flow-key-next-line" '2:3' '{a: b\n c: d}\n'

# An alias may not name an anchor in a previous document: section 3.2.2.2
# scopes an anchor to the document it is in, and both readers refuse.
refuse_res "alias-across-documents" '&a 1\n--- *a\n'
refuse_at  "alias-across-documents" '2:5' '&a 1\n--- *a\n'

# An anchor may be redefined, and an alias takes the most recent
# definition before it. PyYAML refuses the document outright with
# "found duplicate anchor".
res_diff "anchor-redefined-value" '\055 &a 1\n\055 &a 2\n\055 *a\n'
r12 "anchor-redefined-12" '\055 &a 1\n\055 &a 2\n\055 *a\n' '[int 1, int 2, int 2]'

# --- the round trip the fuzz target checks, as fixed cases ------------
#
# yamlc json output is JSON, and JSON is YAML, so reading it back must
# give the same value. Both of these were found by go test -fuzz and are
# kept here so that make test sees them without a fuzz run.
roundtrip() {
	f=$(doc "$1" "$2")
	a=$($Y dump "$f" 2>&1)
	$Y json "$f" > "$W/d/$1.json" 2>&1
	b=$($Y dump "$W/d/$1.json" 2>&1)
	eq "roundtrip/$1" "$a" "$b"
}

roundtrip "rt-c1-escape"   '"\\x80"\n'
roundtrip "rt-del-escape"  '"\\x7f"\n'
roundtrip "rt-nul-escape"  '"\\0"\n'
roundtrip "rt-noncharacter" '"\\ufffe"\n'
roundtrip "rt-surrogate"   '"\\ud800"\n'
roundtrip "rt-mixed"       'a: [1, 2.5, "x\\ty", null, true, 99999999999999999999]\n'

# --- the json oracle --------------------------------------------------
#
# YAML 1.2 is a strict superset of JSON, so this is the one comparison in
# the project that is not a second opinion. A single disagreement means the
# parser is wrong.
python3 t/corpus.py system t/corpus || exit 2
python3 t/corpus.py hard t/corpus/hard || exit 2
JC=$W/o/jsoncmp.out
python3 t/jsoncmp.py $Y t/corpus/system.list t/corpus/hard > "$JC" || exit 2
jget() { sed -n "s/^$1 //p" "$JC"; }
echo "json: $(jget files) files, $(jget same) same, $(jget differ) differ, $(jget refused) refused"
eq "json/differ"  "0" "$(jget differ)"
eq "json/missing" "0" "$(jget missing)"
eq "json/same-is-most" "yes" "$([ "$(jget same)" -gt 2000 ] && echo yes || echo no)"

# Every refusal on a valid JSON document has to be named here with its
# cause, or it is a hole. There is exactly one, and it is in the YAML
# specification rather than in this parser: JSON escapes only characters
# below #x20, so a raw DEL in a string is legal JSON, and c-printable in
# section 5.1 leaves #x7F out. A 1.2 reader cannot hold that document.
eq "json/refusals" "raw-del.json" \
	"$(sed -n 's/^refused-path \([^ ]*\) .*/\1/p' "$JC" | tr '\n' ' ' | sed 's/ $//')"

# A raw U+0085 in a JSON string is the mirror case and is NOT a refusal:
# 1.2 counts NEL as printable and not as a line break, so the document
# reads and agrees with json. This was the case that found a bug in the
# comparison script rather than in the parser: Python's splitlines breaks
# on U+0085, so one output line became two.
eq "json/nel-in-string" "$(printf '%b' '["a\302\205b"]')" "$($Y json t/corpus/hard/raw-c1.json)"

# --- a YAML corpus written by PyYAML's emitter -------------------------
#
# There are nine YAML files on this machine, which is not a corpus. There
# are a few thousand JSON files and an emitter this project does not
# control, so the corpus is built by loading JSON with the json module and
# writing it out as YAML in six styles: block, flow, canonical, explicit
# start and end, a narrow width that forces line folding, and
# allow_unicode off that forces \uXXXX escapes.
#
# Three separate comparisons on each file, counted apart because they fail
# for different reasons. The event streams (the grammar), the 1.1 values
# against safe_load (the resolution rules), and both against the canonical
# form of the original Python object, which is the only one of the three
# that knows what the document was supposed to say.
YC=$W/o/yamlcmp.out
python3 t/yamlcmp.py $Y t/corpus/yaml --sample 200 > "$YC" || exit 2
yget() { sed -n "s/^$1 //p" "$YC"; }
echo "yaml corpus: $(yget files) files, events $(yget ev-same) same $(yget ev-differ) differ, values $(yget val-same) same $(yget val-differ) differ $(yget val-refused) refused"
eq "yamlcorpus/ev-differ"     "0" "$(yget ev-differ)"
eq "yamlcorpus/ev-refused"    "0" "$(yget ev-refused)"
eq "yamlcorpus/origin-differ" "0" "$(yget origin-differ)"
eq "yamlcorpus/enough"        "yes" "$([ "$(yget ev-same)" -gt 600 ] && echo yes || echo no)"

# Every value disagreement must be on one of the machine's own YAML files
# rather than on one of the generated ones, and those are named in the
# README with their causes. Asserting the shape rather than the file names
# keeps this true on a machine with a different set of YAML files.
eq "yamlcorpus/val-differ-native-only" "" \
	"$(sed -n 's/^val-\(differ\|refused\) //p' "$YC" | grep -v '^native-' | grep -v '^[0-9]*$' | tr '\n' ' ')"

# --- the differential fuzzer ------------------------------------------
#
# The cases above are cases somebody thought of. This builds documents
# from the grammar instead and compares both parsers on every one: the
# event stream, and the 1.1 values. The seed is fixed so that the suite is
# repeatable and a failure is reproducible; YAMLC_FUZZ_SEED overrides it,
# and YAMLC_FUZZ_COUNT makes the run longer.
FZ=$W/o/fuzzcmp.out
python3 t/fuzzcmp.py $Y t/corpus/fuzz --count "${YAMLC_FUZZ_COUNT:-1500}" \
	--seed "${YAMLC_FUZZ_SEED:-7}" > "$FZ" || exit 2
fget() { sed -n "s/^$1 //p" "$FZ"; }
echo "fuzzer: seed $(fget seed), $(fget documents) documents, events $(fget ev-same) same $(fget ev-differ) differ, values $(fget val-same) same $(fget val-differ) differ"
eq "fuzz/ev-differ"      "0" "$(fget ev-differ)"
eq "fuzz/ev-yamlc-only"  "0" "$(fget ev-yamlc-only)"
eq "fuzz/ev-pyyaml-only" "0" "$(fget ev-pyyaml-only)"
eq "fuzz/val-differ"     "0" "$(fget val-differ)"
eq "fuzz/val-yamlc-only" "0" "$(fget val-yamlc-only)"
eq "fuzz/enough"         "yes" "$([ "$(fget ev-same)" -gt 1000 ] && echo yes || echo no)"

# Every document PyYAML has no value for and yamlc does must be the one
# known cause: a collection used as a mapping key, which Python cannot
# hash. A second cause appearing in that bucket turns this red.
eq "fuzz/pyyaml-refusals-explained" "$(fget val-pyyaml-only)" "$(fget val-pyyaml-unhashable)"

# --- how often this happens in files nobody wrote for the occasion ----
#
# The divergence report over the whole corpus. The numbers are printed
# rather than asserted, because they are a property of this machine's
# files; what IS asserted is that the report handles every document the
# parser reads, since a reporter that refuses what the parser accepts
# would make the measurement meaningless.
DVJ=$W/o/diverge-json.out
xargs $Y diverge --batch < t/corpus/system.list > "$DVJ" 2>/dev/null
awk '/^files /{f+=$2} /^with-findings /{w+=$2} /^findings /{n+=$2} /^refused /{r+=$2}
     END{printf "diverge over %d JSON files: %d with findings, %d findings, %d refused\n", f, w, n, r}' "$DVJ"
eq "diverge/json-refused" "0" \
	"$(awk '/^refused /{r+=$2} END{print r+0}' "$DVJ")"

DVY=$W/o/diverge-yaml.out
$Y diverge --batch t/corpus/yaml/* > "$DVY" 2>/dev/null
awk '/^files /{f+=$2} /^with-findings /{w+=$2} /^findings /{n+=$2} /^refused /{r+=$2}
     END{printf "diverge over %d generated YAML files: %d with findings, %d findings, %d refused\n", f, w, n, r}' "$DVY"

# Two real JSON files on this machine hold numbers in exponent form, and
# a 1.1 reader makes strings of them. The finding is kept here as a
# document of its own so that it is still asserted on a machine without
# those files.
dv "dv-json-exponent" '{"a": 2E10, "b": 1e-09, "c": 0.5e5}\n' \
	'1:7: scalar;1:18: scalar;1:30: scalar;'

# --- hostile input ----------------------------------------------------
HO=$W/o/hostile.out
python3 t/hostile.py $Y t/corpus/hostile > "$HO" || exit 2
hget() { sed -n "s/^$1 //p" "$HO"; }
hfile() { sed -n "s/^file $1 \([a-z]*\) .*/\1/p" "$HO"; }
echo "hostile: $(hget built) built, $(hget read) read, $(hget refused) refused, $(hget crashed) crashed"
eq "hostile/crashed"    "0" "$(hget crashed)"
eq "hostile/missing"    "0" "$(hget missing)"
eq "hostile/noposition" "0" "$(hget noposition)"

# A refusal with no position is as bad as no refusal, so the sweep counts
# those separately and the count has to be zero.
for f in laughs.yaml deep-flow-seq.yaml deep-flow-map.yaml deep-block-seq.yaml \
	deep-block-map.yaml deep-flow-unclosed.yaml cycle-seq.yaml cycle-map.yaml \
	cycle-nested.yaml cycle-deep.yaml cycle-key.yaml alias-undefined.yaml \
	alias-later.yaml open-quote.yaml open-dquote.yaml open-flow-seq.yaml \
	open-flow-map.yaml open-verbatim-tag.yaml tab-indent.yaml tab-seq.yaml \
	long-key.yaml; do
	eq "hostile/refuse/$f" "err" "$(hfile $f)"
done

# ... and the ones that must still be read, so that the limits are
# boundaries and not a wall everything hits.
for f in laughs-small.yaml anchor-huge.yaml long-line.yaml \
	many-keys.yaml many-anchors.yaml many-aliases.yaml many-docs.yaml; do
	eq "hostile/read/$f" "ok" "$(hfile $f)"
done

# --- the YAML test suite ----------------------------------------------
#
# The YAML project's own conformance data, 402 cases, fetched once and
# cached. PyYAML cannot settle a 1.2 question because it implements 1.1,
# and the json module only covers the JSON subset; this is the first
# oracle here that speaks for YAML 1.2 across the whole grammar.
#
# Without a network and without a cached copy the section prints a skip
# and runs nothing. That is deliberate: this is one oracle among four, so
# a missing one is a loud gap rather than a refusal to run at all.
TS=$W/o/yts.out
python3 t/yts.py $Y t/corpus/yts > "$TS" || exit 2
tget() { sed -n "s/^$1 //p" "$TS"; }
if [ -n "$(tget unavailable)" ]; then
	echo "SKIP yaml test suite: $(tget unavailable)"
else
	echo "yaml test suite: $(tget cases) cases, $(tget read) read, $(tget refused) refused, events $(tget event-same) same $(tget event-differ) differ, json $(tget json-same) same $(tget json-differ) differ $(tget json-no-form) no JSON form, $(tget known) known failures"
	eq "yts/digest"     "yes" "$(tget digest-match)"
	eq "yts/cases"      "402" "$(tget cases)"
	# A floor, not the exact figure, which moves every time a refusal
	# is fixed and one more case reaches the event comparison. What
	# this guards is the comparison running on a corpus at all.
	eq "yts/event-enough" "yes" \
		"$([ "$(tget event-same)" -gt 250 ] && echo yes || echo no)"
	eq "yts/json-enough" "yes" \
		"$([ "$(tget json-same)" -gt 220 ] && echo yes || echo no)"
	# The only refusals allowed in the core schema comparison are the
	# tags yamlc carries rather than interprets, which is a stated
	# non-goal. A second cause arriving in that bucket turns this red.
	eq "yts/json-no-form" "12" "$(tget json-no-form)"
	eq "yts/unexpected" "0"   "$(tget unexpected-count)"
	eq "yts/fixed"      "0"   "$(tget fixed-count)"
fi

# --- the divergence report --------------------------------------------
#
# The deliverable. Each kind of finding is asserted on a document written
# to produce exactly that kind, because a report that prints nothing is
# indistinguishable from a document with nothing in it.
dv() {
	f=$(doc "$1" "$2")
	eq "diverge/$1" "$3" "$($Y diverge "$f" 2>&1 | sed -n "s|^$f:||p" | cut -d" " -f1-2 | tr "\n" ";")"
}

dv "dv-sexagesimal" 'a: 1:30\n' '1:4: scalar;'
dv "dv-octal"       'a: 010\n' '1:4: scalar;'
dv "dv-yes"         'a: yes\n' '1:4: scalar;'
dv "dv-timestamp"   'a: 2001-12-14\n' '1:4: scalar;'
dv "dv-oh-octal"    'a: 0o17\n' '1:4: scalar;'
dv "dv-exp"         'a: 1e5\n' '1:4: scalar;'
dv "dv-tilde"       'a: ~\n'         ''
dv "dv-merge"       'a:\n  <<: {b: 1}\n' '2:3: merge;'
dv "dv-dupkey"      '{yes: 1, true: 2}\n' '1:1: dupkey;1:2: scalar;'
dv "dv-nel"         'a: b\302\205c\n' '1:5: linebreak;'
dv "dv-ls"          'a: b\342\200\250c\n' '1:5: linebreak;'
dv "dv-ps"          'a: b\342\200\251c\n' '1:5: linebreak;'
dv "dv-directive"   '%YAML 1.1\n--- a: 1\n' '1:1: directive;'
dv "dv-tab-plain"   'a: b\tc\n' '1:4: tab-in-plain;'
dv "dv-bad-date"    'a: 2001-13-99\n' '1:4: scalar-refused;'
dv "dv-clean"       'a: [1, 2.5, "x", null, true]\n' ''
dv "dv-quoted-safe" 'a: "yes"\nb: '"'"'1:30'"'"'\n' ''

# scalars prints every scalar with both readings and marks the ones that
# differ, which is what a reader of a real document needs.
f=$(doc "sc-mixed" 'a: yes\nb: "yes"\n')
eq "scalars/marks-divergence" "1:4* \$.a :yes | 1.2 str yes | 1.1 true" \
	"$($Y scalars "$f" | sed -n 2p)"
eq "scalars/quoted-is-quiet"  "2:4  \$.b \"yes | 1.2 str yes | 1.1 str yes" \
	"$($Y scalars "$f" | sed -n 4p)"

# --- the construct counter --------------------------------------------
#
# Pointed at exactly the documents this run fed the parser, plus the
# generated corpora. A folded-scalar comparison over a corpus with no
# folded scalars passes having run nothing, so every construct the parser
# has is counted and every count below has to be non-zero.
CN=$W/o/counts.out
$Y counts "$W"/d/* t/corpus/yaml/* t/corpus/hard/*.json > "$CN" || exit 2
cget() { sed -n "s/^$1 //p" "$CN"; }
echo "corpus: $(cget files.read) documents read, $(cget files.refused) refused, deepest nesting $(cget depth.max)"
for k in doc doc.explicit doc.bare doc.end-marked directive.yaml \
	scalar.plain scalar.plain.multiline scalar.plain.tab scalar.empty \
	scalar.single scalar.single.multiline scalar.double scalar.double.multiline \
	scalar.literal scalar.folded \
	blockscalar.chomp.clip blockscalar.chomp.strip blockscalar.chomp.keep \
	blockscalar.indent.given blockscalar.blank-line \
	block.seq block.map flow.seq flow.map collection.empty seq.nested-in-seq \
	map.key.explicit map.key.empty map.key.collection map.value.empty \
	anchor alias merge.key \
	tag.primary tag.secondary tag.verbatim tag.non-specific tag.named-handle \
	resolve.1.2.null resolve.1.2.bool resolve.1.2.int resolve.1.2.float \
	resolve.1.2.str resolve.1.1.timestamp resolve.1.1.merge resolve.1.1.value \
	diverge.scalar diverge.scalar-refused diverge.same-tag-other-value; do
	n=$(cget "$k")
	[ -n "$n" ] || n=0
	if [ "$n" -gt 0 ]; then
		ok "counts/$k = $n"
	else
		bad "counts/$k is zero: the corpus holds none of these, so every assertion about them is vacuous"
	fi
done

echo
echo "pyyaml $PYYAML"
echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ] || exit 1
