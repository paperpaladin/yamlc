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
	printf '%b' "$2" > "$W/$1"
	echo "$W/$1"
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
	if [ "$a" = "$b" ]; then
		bad "ev1.2/$1: PyYAML now agrees, so this is no longer a divergence"
	else
		ok "ev1.2/$1"
	fi
}

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

refuse "dir-no-doc"    '2:1' '%YAML 1.2\nfoo\n'
refuse "dir-bare-pct"  '1:1' '%\n--- foo\n'
refuse "tab-indent"    '1:1' '\tfoo\n'
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

echo
echo "pyyaml $PYYAML"
echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ] || exit 1
