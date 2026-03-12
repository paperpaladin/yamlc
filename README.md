# yamlc

A YAML 1.2 parser, and a tool for finding where parsers disagree about the
same document. Go, standard library only, no `require` block in `go.mod`.

```
yamlc check FILE...       parse and compose; print ok, or the refusal with a position
yamlc events FILE...      the parse as an event stream, for diffing against PyYAML
yamlc json FILE...        one JSON value per document
yamlc dump FILE...        one canonical value per document, types included
yamlc scalars FILE...     every scalar, with both readings of it
yamlc diverge FILE...     where a 1.1 reader and a 1.2 reader differ
yamlc counts FILE...      how many of each construct the files hold
```

Options where they apply: `--schema=1.1|1.2`, `--dup=error|first|last`,
`--merge=on|off`, `--max-depth=N`, `--max-nodes=N`, `--batch`.

`make test` builds and runs the suite. It needs `python3` and PyYAML and
refuses to run without either, rather than reporting a smaller number
having checked less.

The parser implements block and flow collections, the five scalar styles
with their folding and chomping rules, anchors and aliases, tags in all
four spellings, the `%YAML` and `%TAG` directives, multiple documents in a
stream, the merge key, and the resolution rules of both the 1.2 core
schema and YAML 1.1. Input may be UTF-8, UTF-16 or UTF-32, with or
without a byte order mark.

## Why the second half exists

YAML is where configuration is written, and parsers genuinely disagree
about what a document means. 1.1 reads `yes`, `no`, `on` and `off` as
booleans and 1.2 does not. 1.1 reads a leading zero as octal, so `010` is
eight; 1.2 reads it as ten and spells octal `0o10`. 1.1 has sexagesimal,
so `1:30` is ninety. 1.1 has timestamps, so `2001-12-14` is a date in one
reader and a string in another. `1e5` is a float in 1.2 and a string in
1.1, because the 1.1 float pattern requires a sign on the exponent.

None of that is visible in the document. So:

```
$ cat example.yaml
port: 1:30
flag: yes
mask: 010
when: 2001-12-14
base: &b {a: 1}
use:
  <<: *b
  b: 2
both: {yes: 1, true: 2}

$ yamlc diverge example.yaml
example.yaml:1:7: scalar $.port "1:30": 1.2 reads str 1:30, 1.1 reads int 90
example.yaml:2:7: scalar $.flag "yes": 1.2 reads str yes, 1.1 reads true
example.yaml:3:7: scalar $.mask "010": 1.2 reads int 10, 1.1 reads int 8
example.yaml:4:7: scalar $.when "2001-12-14": 1.2 reads str 2001-12-14, 1.1 reads date 2001-12-14
example.yaml:7:3: merge $.use a merge key: 1.1 inherits the mapping it names, and the 1.2 core schema has no merge type, so a strict 1.2 reader sees an ordinary key spelled <<
example.yaml:9:7: dupkey $.both the keys of this mapping resolve to 2 distinct values under 1.2 and 1 under 1.1, so one reader has a duplicate key error
example.yaml:9:8: scalar $.both.{yes} "yes": 1.2 reads str yes, 1.1 reads true
7 divergences, 0 refusals (dupkey=1 merge=1 scalar=5)
```

The `dupkey` line is the one worth having a tool for. Under 1.1 both keys
of `{yes: 1, true: 2}` resolve to the boolean true, so a 1.1 reader has a
duplicate key where a 1.2 reader has a two-entry mapping. Nothing about
the document says so.

The seven kinds of finding are counted apart because they need different
work: `scalar`, `scalar-refused` (one reader has no value at all),
`dupkey`, `merge`, `linebreak` (U+0085, U+2028 and U+2029, which cut the
document into different lines before any resolution happens),
`tab-in-plain`, and `directive` (the document itself asking for 1.1).

## What it is measured against

Nothing here is checked against a value worked out by hand, except the
eleven cases marked `res12` in the suite, which are discussed below.

**Python's `json` module, as an exact oracle.** YAML 1.2 is a strict
superset of JSON, so every valid JSON document must parse to exactly the
data `json.load` parses it to. That is not a second opinion to weigh
against ours, it is the specification, and one disagreement means the
parser is wrong. The corpus is every valid JSON file on this machine,
found by walking `/usr/share`, `/usr/lib`, `/usr/lib64` and `/etc` and
keeping what `json.load` reads, so it is JSON by the oracle's own
definition rather than by its extension. The comparison in
`t/jsoncmp.py` is stricter than Python's `==`: it compares types first, so
`True` and `1` and `1.0` are three different answers; it compares a float
by its bits; and it compares the key order of an object, which
`json.load` preserves in the dict.

**PyYAML 6.0.2**, which implements YAML 1.1, used three ways. Its event
stream from `yaml.parse` is about the grammar and carries no resolution at
all, so a disagreement there is never about a schema. Its `safe_load` is
the authority for what the 1.1 rules produce, and yamlc's 1.1 table is
copied from PyYAML's `resolver.py` rather than from the 1.1 specification
for that reason. And `yaml.dump` is the YAML corpus generator, since this
machine has nine YAML files and a few thousand JSON ones.

**The YAML 1.2.2 specification** for the resolution rules and the
productions, named at the decision in the source. No copy of it is
installed on this machine, so the production names in this README and in
the comments are the part to trust: they are distinctive enough to look
up. The section numbers beside them are written from memory and are not
checked by anything here.

**Hand-written documents** for the cases no generator produces, and for
every refusal.

The 1.2 core schema is the one thing with no outside authority. Nothing
else on this machine implements 1.2, so eleven assertions carry values
taken from the table in section 10.2.1.2, quoted in `resolve.go`, and the
suite names them `res12` so a reader can see which claims are unbacked.
Where a scalar is also valid JSON the json comparison checks the same
reading against an oracle, which covers the integers, floats, booleans and
nulls in their JSON spellings but not `0o17`, `.inf`, `~` or `Null`.

## Numbers

Measured on 21 October 2026, on Go 1.24.4, Python 3.13.5 and PyYAML 6.0.2.
`make test` prints all of them.

| what | measured |
|---|---|
| assertions | 560 passed, 0 failed |
| JSON documents against Python's `json` | 2,753 compared, 2,752 identical, 0 differ |
| of those, refused by yamlc | 1, and the cause is in the YAML spec |
| YAML documents against PyYAML's event stream | 1,206 compared, 1,206 identical, 0 differ |
| YAML documents against PyYAML's `safe_load` | 1,206 compared, 1,203 identical, 1 differs |
| the same documents against the data they were built from | 1,200 compared, 0 differ |
| generated documents in the differential fuzzer | 1,500 per run, 12,000 over eight seeds, 0 event differences, 0 value differences |
| malformed and hostile documents | 1,589 built, 424 read, 1,160 refused, 0 crashed, 0 hung |
| of those, refusals with no position in them | 0 |
| constructs counted over the whole corpus | 49, all non-zero |
| JSON files on this machine holding a 1.1 against 1.2 divergence | 2 of 2,714, 302 findings, all of them numbers in exponent form |
| generated YAML documents holding one | 7 of 1,206, 45 findings |
| build reproducibility | `unrepro` says reproducible across varied directory, locale, timezone, umask, parallelism and `SOURCE_DATE_EPOCH` |
| documents the counter saw | 1,563, deepest nesting 391 |
| mutants | 62 applied, 61 killed, 1 equivalent |

Refusals are counted apart from disagreements everywhere, because the two
need different work. A disagreement is a bug. A refusal where the oracle
has a value is either a missing feature or a deliberate difference, and
every one of them is named below.

## Where yamlc and PyYAML disagree

Every one of these has a case in `t/run.sh` that asserts the disagreement
is still there, so one that quietly goes away turns the suite red rather
than sitting as a passing exception. They divide into three groups.

### 1.1 against 1.2: the document itself reads differently

**U+0085 NEL, U+2028 LINE SEPARATOR and U+2029 PARAGRAPH SEPARATOR.** Line
breaks in 1.1 and ordinary printable content in 1.2 (section 5.4 lists LF
and CR only). `a b<NEL>c` is one scalar here and `a b c` to PyYAML, which
folds the break into a space. This is reported by `yamlc diverge` as a
`linebreak` finding, and it is the only divergence in this list that
changes how the document is cut into lines before anything else happens.

**A bare document after `...`.** `l-any-document` in section 9.2 includes
`l-bare-document`, and a document suffix may precede it, so `a` newline
`...` newline `b` is two documents. PyYAML refuses the second. A lone
`...` with no document before it is the same production and the same
disagreement, and gives a stream of zero documents here.

### PyYAML restrictions that 1.2 does not have

These are not version differences. The 1.2 text says the document is
valid, and PyYAML's scanner refuses it or reads it differently.

**A tab inside a plain scalar.** `nb-ns-plain-in-line` is
`( s-white* ns-plain-char )*` and `s-white` includes the tab, so `a<TAB>b`
is one scalar. PyYAML refuses it with "found character '\t' that cannot
start any token".

**A trailing tab after a plain scalar**, for the same reason, with the
trailing run trimmed by the folding rules.

**A tab after a block indicator.** `s-separate-in-line` is `s-white+`, so
`a:<TAB>b`, `-<TAB>b` and `?<TAB>a` are all separation whitespace followed
by content. PyYAML refuses all three.

**An empty key.** `e-node` stands in the key position of
`l-block-map-implicit-entry`, so `: a` is a mapping from null to `a`, and
`c-ns-flow-map-empty-key-entry` makes `{: 1}` the same thing in flow.
PyYAML refuses both.

**A JSON-like key with its colon on the next line.**
`c-ns-flow-map-json-key-entry` allows `s-separate` between the key and the
colon, so `{"a"` newline `: 1}` parses. PyYAML requires the colon on the
key's own line.

**A plain scalar beginning `?` or `:` in flow context.**
`ns-plain-first` allows those two characters when what follows is not
whitespace, in flow as well as in block. PyYAML's `check_plain` allows
them only when `flow_level` is zero, so `[?x]` is a one-element sequence
here and a single-pair mapping there.

**A surrogate pair escape.** YAML defines escapes for characters and a
surrogate is not one, so a pair of them has no defined meaning. Python's
`json` joins the pair into U+1F600 and PyYAML leaves two lone surrogates.
yamlc follows `json`, because exact agreement over the JSON subset is the
property this project is not allowed to break, and a lone surrogate
escape on its own is kept as the three bytes that would encode it.

### Where PyYAML's host language shows through

**Two keys that are distinct YAML nodes and equal Python objects.**
PyYAML's mapping is a `dict`, and in Python `0.0 == False == 0`,
`5 == 5.0` and `1000 == 1.0e+3`, with `hash` agreeing. So `{0: a, false: b}`
is one entry there and two here. This is not a version difference and not
a bug in either reader, and it means a duplicate key check written in
Python reports duplicates that are not in the document. The differential
fuzzer found three separate ways for this to leak into the generated
corpus and the README of this project is where the second and third are
recorded.

**A collection used as a mapping key.** Legal YAML, and unhashable in
Python, so `safe_load` refuses `? [1,2]` with "found unhashable key".
126 of 3,000 generated documents land here. yamlc reads them.

**An unknown local tag.** `safe_load` has no constructor for `!space`, so
it refuses the document. yamlc carries the tag and the scalar text.

**`!!omap`, `!!pairs` and `!!set`.** PyYAML turns the first two into lists
of Python tuples and the third into a `set`. yamlc keeps the underlying
sequence or mapping and carries the tag, because not dispatching on tags
is a stated non-goal. This is the one value disagreement left in the YAML
corpus comparison, on `/usr/share/doc/python3-yaml/examples/pygments-lexer/example.yaml`.

**`<<` and `=` outside the key position of a mapping.** Both resolve to
1.1 tags with no constructor outside a mapping, so `[<<, =]` is refused by
`safe_load`. yamlc reads both as strings: refusing a sequence because it
holds two less-than signs is a worse answer than reading them.

**Duplicate keys.** Section 3.2.1.1 requires the keys of a mapping to be
unique. PyYAML keeps the last value silently. yamlc refuses by default
and reports both positions, which is why every resolution comparison in
the suite runs with `--dup=last`.

**A redefined anchor.** An alias refers to the most recent node with
that anchor, which only means anything if an anchor may be used twice, so
`- &a 1` `- &a 2` `- *a` is 1, 2, 2. PyYAML refuses the document with
"found duplicate anchor".

**A stream that is not UTF-8.** PyYAML's reader detects the two UTF-16
byte order marks and the UTF-8 one. A UTF-16 stream without a mark, and
any UTF-32 stream with or without one, is a ReaderError there and a
document here. Section 5.2 says a processor must accept all three
encodings and gives the zero-byte patterns that identify them without a
mark.

**The 1.1 boolean table.** The 1.1 type repository's `bool` also accepts a
bare `y`, `Y`, `n` and `N`. PyYAML's regex does not, so `y` is a string to
PyYAML and true to a reader following the 1.1 text. yamlc follows PyYAML,
because PyYAML is the oracle the table is checked against, and this
sentence is the only record of the difference.

### Where JSON and YAML 1.2 disagree

One place, and it is the single exception to the hard invariant. JSON
requires escaping only below #x20, so a raw DEL inside a string is legal
JSON, and `c-printable` in section 5.1 leaves #x7F out of the character
set. No YAML 1.2 reader can hold that document. The suite names the file
rather than counting it as a pass. The mirror case, a raw U+0085, IS
readable, because 1.2 calls NEL printable and not a line break.

## What the corpus holds

Nothing is committed. `make test` builds all of it, which takes about
twenty seconds of the suite's twenty-five.

2,714 JSON files from the machine, listed by `t/corpus.py system`, which
keeps a path only if `json.load` reads it. 41 generated JSON documents for
the cases the system files do not reach: deep nesting at 100, 390 and
10,000 levels, duplicate keys four ways, lone surrogates and a surrogate
pair and a split pair, numbers that do not fit a double in both
directions, a 20,000-character string, a 5,000-entry array and object,
tabs between tokens, CRLF, and the two raw characters above.

1,206 YAML documents written by PyYAML's emitter from a sample of the JSON
corpus, in six styles: block, flow, canonical, explicit start and end, a
width of 20 that forces line folding, and `allow_unicode=False` that
forces `\uXXXX` escapes. Plus the nine YAML files this machine has.

1,589 hostile documents from `t/hostile.py`: billion laughs, five shapes
of alias cycle including one where the alias is the mapping key, ten
thousand deep in four spellings, unterminated quotes and brackets and
verbatim tags, a 100,000-character anchor name, 20,000 anchors, 20,000
aliases, 50,000 documents, a 1,000,000-character scalar, tabs used as
indentation, and every prefix and every single-byte overwrite of five
seed documents with 0xFF, 0x7F, 0x09, 0x3A and 0x00.

1,500 documents per run from `t/fuzzcmp.py`, built from the grammar with a
printed seed.

`make fuzz` adds a coverage-guided run on top of all of that, seeded from
the three generated corpora.

The construct counts come from `yamlc counts` pointed at exactly the
documents the suite fed the parser, and the suite asserts every one of
them is non-zero. Writing that found five constructs the corpus did not
hold at all: a keep chomping indicator, an explicit indentation
indicator, an empty mapping key, a tab inside a plain scalar, and a named
tag handle. Every assertion about those five was vacuous until the
counter existed.

```
doc 1596                        doc.explicit 481                doc.bare 1115
doc.end-marked 206              directive.yaml 6                scalar.plain 63552
scalar.plain.multiline 541      scalar.plain.tab 2              scalar.empty 47
scalar.single 5615              scalar.single.multiline 49      scalar.double 18578
scalar.double.multiline 925     scalar.literal 32               scalar.folded 13
blockscalar.chomp.clip 36       blockscalar.chomp.strip 6       blockscalar.chomp.keep 3
blockscalar.indent.given 4      blockscalar.blank-line 5        block.seq 1895
block.map 6460                  flow.seq 1732                   flow.map 4134
collection.empty 883            seq.nested-in-seq 1230          map.key.explicit 201
map.key.empty 5                 map.key.collection 9            map.value.empty 31
anchor 44                       alias 38                        merge.key 16
tag.primary 15                  tag.secondary 13457             tag.verbatim 5
tag.non-specific 3              tag.named-handle 1              resolve.1.2.null 295
resolve.1.2.bool 342            resolve.1.2.int 10827           resolve.1.2.float 88
resolve.1.2.str 51970           resolve.1.1.timestamp 25        resolve.1.1.merge 18
resolve.1.1.value 7             diverge.scalar 97               diverge.scalar-refused 2
diverge.same-tag-other-value 5
```

The counter is produced by the same parser the tests use, which is a real
weakness: a construct the parser cannot see is one the counter cannot
count either, and the pair would agree that the corpus is missing
something it holds. The event comparison against PyYAML is the check on
that, since PyYAML counts nothing and does report the structure it found.

## The differential fuzzer

`t/fuzzcmp.py` builds documents from the grammar rather than from a list
of cases: block and flow collections to a random depth, all five scalar
styles, chomping and indentation indicators, anchors and aliases to
anchors already defined, tags, several documents per stream, comments and
blank lines where they are allowed. Both parsers read every document and
both the event stream and the 1.1 values have to agree.

It prints what it deliberately avoids, and that list is part of the
result, because a fuzzer that steers around the interesting cases has to
say which ones. Everything on it is a divergence with its own case above.

Four defects came out of it, and three were in the fuzzer:

The parser one: the indentation check on a quoted scalar's continuation
lines had been relaxed because 67 documents in 500 were refused on it.
Every one of those was the generator writing a two-space continuation line
whatever the nesting depth. `s-l+flow-in-block(n)` is `s-separate(n+1)
ns-flow-node(n+1)`, so the check was right. Relaxing it also made the two
parsers disagree about the structure of documents neither of them should
have read, which is the better argument for keeping it: a lax rule does
not produce one reading, it produces two.

The three generator defects were one class, the Python key collision
above, arriving by three routes. Hand-listing the colliding pairs missed
`0b101` against `5.`, so the key pool is now worked out by asking PyYAML
what each candidate resolves to and keeping one representative per
Python-distinct value. That still left the explicit key forms `? k` and
`{? k}` drawing from the value pool, found on the fifth seed, and an alias
in a key position reaching the value pool through the node it names, found
on the fifteenth. Three holes, one defect class, each found after the
previous fix.

## The round trip

`fuzz_test.go` is a `go test -fuzz` target, which the byte-level sweep in
`t/hostile.py` is not: that one applies a fixed list of mutations to five
seeds and stops. It checks three properties, in increasing order of what
they can catch. No input makes the parser panic. Every refusal carries a
position. And if a document composes to a value that can be written as
JSON, then parsing that JSON again gives the same value, which goes out
through the writer and back in through the whole parser.

The third found two defects in the first ninety seconds. The JSON writer
passed a C1 control through raw, and a double-quoted scalar may
legitimately contain one, since the escape table has `\x` for any byte
and `\0` for NUL and none of those characters is in `c-printable`; the
output was JSON that this parser could not read, and Python's `json`
reads the raw byte and the escape as the same character, so the oracle
comparison could never have seen it. Anything outside `c-printable` is
escaped now.

The other is not fixed and cannot be, and it is under "what does not
work" below. Six of the inputs the fuzzer reached are fixed cases in
`t/run.sh`, so `make test` sees them without a fuzz run.

## Mutation testing

`make mutants` applies 62 single edits to the parser, one at a time,
rebuilds, and runs the whole suite for each. Each run gets a 6GB address
space limit, because two of the mutants remove a resource check and the
documents those checks exist for would otherwise try to build a few
hundred million nodes.

MUTATION_RESULTS_PLACEHOLDER

## What does not work

**No oracle for the 1.2 core schema.** Nothing else on this machine
implements YAML 1.2. Eleven assertions take their expected values from the
specification's own table, and the JSON comparison covers the overlap with
JSON but not `0o17`, `.inf`, `~`, `Null` or the non-specific tag. If this
parser and I have misread the same sentence, nothing here would notice.

**The event stream's escaping is written twice, by me.** `events.go` and
`t/pyyaml.py` both implement the same small escape scheme, so a symmetric
mistake in it would cancel. The values are checked a second time by the
json comparison, which shares none of that code, but the escaping itself
is not oracle-backed.

**The `diverge` output format is asserted against yamlc's own output.**
Seventeen cases check that a given document produces a given kind of
finding at a given position. The 1.1 readings those findings print are
oracle-backed, through the same code path the `res` cases compare against
`safe_load`; the format and the kinds are not.

**Indentation inside a flow collection is not enforced.** The grammar asks
for `s-indent(n)` on a continuation line, which would refuse
`a: [1,` newline `2]`. PyYAML accepts it, almost everything accepts it,
and yamlc accepts it. Quoted scalars are checked, flow collections are
not, and that asymmetry is deliberate rather than considered: the quoted
case was measured to change the structure two readers see, and the flow
case was not.

**No emitter.** yamlc reads YAML and writes JSON and event streams. A
round trip through yamlc is not possible, so there is no round-trip
property to test.

**Tags are carried, not dispatched on.** `!!omap`, `!!pairs`, `!!set` and
every unknown tag come out as the underlying node with the tag attached.
This is a stated non-goal and it is also the last value disagreement in
the YAML corpus comparison.

**Recursive data is refused.** An alias naming a node that contains it is
a refusal with the alias's position, not a cyclic graph. JSON cannot
represent one and neither can the canonical form, and a reader that
follows one does not return. This means yamlc refuses documents PyYAML
loads successfully.

**`yamlc json` refuses what JSON cannot hold**: a mapping key that is not
a string, a `!!binary` scalar, and any node with a tag it does not
interpret.

**An infinity does not survive a round trip through `yamlc json`.** It is
written as `Infinity`, which is what Python's `json` writes and reads, and
which YAML 1.2 reads back as the ordinary string `Infinity`: the core
schema spells an infinity `.inf`. So the output is right against the
oracle and is not re-readable here, and the fuzz target skips the round
trip for any value holding a non-finite float. Writing `.inf` instead
would break the comparison with `json`, which is the invariant that wins.
NaN is the same.

**Timestamps are text.** A 1.1 timestamp is canonicalised to a string in
PyYAML's format, with the offset kept rather than normalised to UTC,
because that is what PyYAML's constructor does and the comparison is
against PyYAML. There is no time type here and no arithmetic on one.

**Comments are discarded.** The parser reads them and throws them away, so
a tool that wanted to report the comment attached to a divergence could
not be built on this without changing the node type.

**The whole input is read into memory**, and the parse tree and the value
tree both exist at once. A document larger than a third of RAM cannot be
read. The node budget bounds expansion, not input size.

**The depth limit is 400 by default**, which refuses a legitimately deeper
document. `--max-depth` raises it; the Go stack would take a few thousand
before it complained.

**Columns count characters, and a tab counts as one.** A refusal's column
will not line up with a terminal's cursor on a line that holds tabs.

**No sanitizer run, and nothing to sanitize.** The C projects in this
collection hunt a defect class where a buffer larger than its data turns
ASan off. Go bounds-checks every slice access and panics rather than
reading past the end, so that class does not exist here and there is no
`make overread` target to copy. The hostile sweep asserts that no input
produces a panic, which is the closest equivalent: 1,589 documents,
including every single-byte mutation of five seeds, with zero crashes.

**The construct counter can only count what the parser sees.** Stated
above and repeated here because it is the weakest link in the
vacuous-pass guard.

**One machine, one PyYAML, one Python.** Every number here comes from
PyYAML 6.0.2 on Python 3.13.5 on one Linux box. Several of the recorded
divergences are specific to that version of PyYAML rather than to YAML
1.1, and the README says which; a different 1.1 implementation would move
them.
