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
	printf "$2" > "$W/$1"
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

status "usage/no-args"      2 $Y
status "usage/bad-command"  2 $Y nonesuch
status "usage/check-nofile" 2 $Y check

# --- the input character set (YAML 1.2.2 section 5.1) -----------------
#
# c-printable is TAB, LF, CR, #x20-#x7E, #x85, #xA0-#xD7FF, #xE000-#xFFFD
# and #x10000-#x10FFFF. Anything else is refused with its position.
accept "plain"      'a: b\n'
accept "tab-inline" 'a: "x\ty"\n'
# U+0085 NEL is a printable character in 1.2 and a line break in 1.1, so
# PyYAML sees two lines here and a key with no colon on the second.
accept_only "nel"   'a: b\302\205c\n'
accept "high"       'a: \343\201\202\n'
accept "bom"        '\357\273\277a: b\n'
accept "crlf"       'a: b\r\nc: d\r\n'
accept "cr-only"    'a: b\rc: d\r'

refuse "bel"        '2:1' 'a: b\n\007c\n'
refuse "nul"        '1:5' 'a: b\000\n'
refuse "del"        '1:5' 'a: b\177\n'
refuse "c1"         '1:5' 'a: b\302\200\n'
refuse "bad-utf8"   '1:3' 'ab\303\050\n'
refuse "trunc-utf8" '1:3' 'ab\303\n'
refuse "ffff"       '1:5' 'a: b\357\277\277\n'

echo
echo "pyyaml $PYYAML"
echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ] || exit 1
