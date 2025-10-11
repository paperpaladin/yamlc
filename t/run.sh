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

status "usage/no-args"      2 $Y
status "usage/bad-command"  2 $Y nonesuch

echo
echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ] || exit 1
