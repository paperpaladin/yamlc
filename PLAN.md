# yamlc

## Contract

Parse a YAML 1.2 stream: block and flow collections, plain, single-quoted,
double-quoted, literal and folded scalars with their chomping and indentation
indicators, anchors, aliases, tags with handles and verbatim form, directives,
multiple documents, and the merge key. Resolve plain scalars by the 1.2 core
schema, and separately by the 1.1 schema, so that the two readings of the same
document can be put side by side.

Refuse a malformed document with a line and column rather than guessing at it.
A tab where indentation is expected, an alias with no anchor, a block scalar
whose content is less indented than its header says, a length of nesting or of
alias expansion that would exhaust memory: all of those are refusals with a
position.

Report, for a given document, every place where a 1.1 reader and a 1.2 reader
disagree about what it means.

## Hard invariant

Every valid JSON document must parse to exactly the data Python's `json`
module parses it to. YAML 1.2 is a strict superset of JSON, so this is not a
second opinion to be weighed against ours, it is the specification. A single
disagreement on the JSON subset means the parser is wrong.

Second: a refusal is counted apart from a disagreement everywhere, in every
report. A run that cannot tell the two apart has measured nothing.

## Oracle

Ranked by how little of it we control.

1. Python's `json` module, as an exact oracle over the JSON subset. Every
   JSON file on the machine plus generated awkward cases: deep nesting,
   duplicate keys, lone surrogates, integers wider than a double, empty
   containers.
2. PyYAML 6.0.2, which implements YAML 1.1. Used two ways: its event stream
   from `yaml.parse`, which is about structure and not about schema, and
   `yaml.safe_load`, which is the authority for what the 1.1 resolution rules
   produce. A disagreement is either a 1.1-versus-1.2 difference, which is a
   finding, or a bug in one of the two, which the spec text settles.
3. The YAML 1.2.2 specification for the resolution rules and for the
   productions. Quoted by section when a decision is not obvious.
4. Hand-written documents for the cases no generator produces, and for every
   refusal.

## Non-goals

- Emitting YAML. yamlc reads; it writes JSON and event streams.
- A tag-driven object model. Tags are carried and reported, not dispatched on.
- Recursive data. An alias that encloses itself is refused, not followed.
- `gopkg.in/yaml`, or any third-party module. `go.mod` has no require block.
- YAML 1.0, and the 1.1 `!!python/...` tag family.

## Commit sequence

### Milestone 1: the lexical layer

- [x] repository skeleton: go.mod, Makefile, .gitignore, LICENSE, this plan
      why: the collection's gate runs make, so make has to exist first.
- [x] source cursor with line and column, break normalisation, BOM
      why: every refusal in this project is a position, so positions come first.
- [x] refusal type and its one-line form, with a case per field
      why: a parse error that does not say where is a guess with a message.
- [x] t/run.sh with the pass and fail protocol
      why: the harness has to exist before the thing it measures.
- [x] comment skipping, blank lines, end of input
      why: the smallest piece of the grammar that every other piece calls.
- [x] non-printable character refusal, and the tab rules
      why: a tab where indentation is expected is the classic YAML trap and
      has to be a refusal rather than a quiet reinterpretation.

### Milestone 2: scalars

- [x] plain scalars on one line, with the indicator exclusions
      why: the commonest node in any real document.
- [x] single-quoted scalars, doubled quote escape
      why: the simplest quoted form, and the folding rules are shared.
- [x] double-quoted scalars: the whole escape table
      why: `\u` and `\U` are where a parser quietly produces the wrong bytes.
- [x] lone surrogate escapes kept as written
      why: JSON allows them and the json oracle will produce them.
- [x] escaped line breaks in double-quoted scalars
      why: a backslash at end of line removes the fold, which nothing else does.
- [x] multi-line plain and quoted scalar folding
      why: line folding is a rule about blank lines, not about joining.
- [x] literal block scalars, chomping clip, strip and keep
      why: the three chomping modes give three different trailing newlines.
- [x] folded block scalars, including the more-indented line exception
      why: folding stops at a line that is indented more than the block, which
      is the rule implementations most often skip.
- [x] explicit indentation indicators on block scalars
      why: `|2` changes where the content starts and is unguessable otherwise.

### Milestone 3: collections

- [x] block sequences, nested without an intervening key
      why: `- - -` is three nested sequences and is easy to get wrong.
- [x] block mappings with simple keys
      why: the shape every configuration file is written in.
- [x] a sequence at the same indentation as its parent mapping key
      why: valid, extremely common, and a special case in the grammar.
- [x] explicit key and value indicators in block context
      why: a key that is itself a collection has no other spelling.
- [x] flow sequences and flow mappings
      why: the JSON subset is entirely flow, so the json oracle needs these.
- [x] flow collections spanning several lines
      why: the indentation rules inside a flow collection are different.
- [x] the JSON-like key exception: a quoted key followed directly by a colon
      why: `{"a":1}` has no space after the colon and must still parse.
- [x] empty entries in flow collections, and the empty key
      why: `{a: }`, `[,]` and `{}` all have defined meanings.

### Milestone 4: the stream

- [x] document start and end markers, bare documents, several per stream
      why: a stream of documents is the unit the tool reports on.
- [x] the `%YAML` directive and the version check
      why: a document asking for 1.1 is asking for different resolution rules.
- [x] the `%TAG` directive, tag handles, and shorthand expansion
      why: a tag shorthand that expands wrongly silently changes the type.
- [x] verbatim tags, and a tag on a null node
      why: both are places where the property and the node are separable.
- [x] anchors and aliases
      why: the second half of this project is about what aliases mean.
- [x] an alias to an anchor that does not exist yet, refused
      why: anchors are defined before use and the opposite must be an error.
- [x] an alias cycle refused, with the suite proving termination
      why: a parser that follows one does not return.
- [x] a nesting depth limit, refused with a position
      why: ten thousand open brackets should not be a stack overflow.
- [x] an alias expansion budget, refused
      why: billion laughs is the denial-of-service case this format has.

### Milestone 5: resolution

- [x] the 1.2 core schema: null, bool, int in three bases, float, inf, nan
      why: the heart of the specification half of this project.
- [x] integers wider than a double, kept exactly
      why: the json oracle will hand us some and a float would lose them.
- [x] the 1.1 resolution table, as PyYAML implements it
      why: the comparison needs a second reading of the same scalar.
- [x] 1.1 sexagesimal integers and floats
      why: `1:30` is ninety in 1.1 and a string in 1.2.
- [x] 1.1 timestamps
      why: a date that is a string in one reader and a date in another.
- [x] the merge key, and its interaction with duplicate keys
      why: `<<` is a 1.1 type that every 1.2 implementation also carries.
- [x] duplicate key detection with both positions
      why: accepted by some parsers and refused by others, and the cause of
      more than one production incident.

### Milestone 6: output

- [x] JSON output: exact integers, round-tripping floats, escaped surrogates
      why: the json oracle compares parsed structures, so the writing has to
      be lossless before the comparison means anything.
- [x] the event stream form, for comparison against PyYAML's events
      why: structure and schema are separate questions and separate oracles.
- [x] the per-scalar report: position, path, raw text, both readings
      why: this is the output the second half of the project exists for.
- [x] `yamlc diverge`: the list of places the two readings differ
      why: the deliverable.
- [x] `yamlc counts`: how many of each construct a document holds
      why: a folded-scalar test over a corpus with no folded scalars passes
      having run nothing.

### Milestone 7: measurement

- [x] the json oracle over every JSON file on the machine
      why: a body of JSON nobody here wrote.
- [x] generated awkward JSON: nesting, duplicates, surrogates, precision
      why: the system files are well formed and will not probe the edges.
- [x] the PyYAML event comparison over the whole corpus
      why: the structural oracle.
- [x] the PyYAML resolution comparison, every disagreement classified
      why: an unclassified disagreement is an unread bug report.
- [x] hand-written corpus: tabs, flow over lines, cycles, tags on nulls
      why: the cases no generator produces.
- [x] hostile corpus: billion laughs, ten thousand deep, truncation
      why: refusing these is part of the contract.
- [x] construct counts asserted non-zero, printed every run
      why: the vacuous-pass guard.
- [ ] mutation run over the scanner, survivors recorded
      why: the suite is worth the mutants it kills and nothing more.
- [ ] mutation run over the resolution tables, survivors recorded
      why: a resolution table is a list of regexes and a deleted line is
      exactly the defect this project is about.
- [ ] README with the measured numbers, every disagreement, and what does
      not work
      why: a README that only lists successes is not finished.

### Milestone 8: what is still missing

- [ ] a second YAML 1.2 implementation as an oracle for the core schema
      why: eleven assertions take their expected values from the
      specification's own table and nothing outside this repository checks
      them. go-yaml v3 is 1.1 with 1.2 pieces and would not settle it; the
      reference parsers worth trying are libfyaml and the Rust saphyr.
- [ ] indentation inside a flow collection, decided rather than left lax
      why: quoted scalars are checked and flow collections are not, and the
      asymmetry is an accident of which one was measured.
- [ ] comments kept on the node they follow
      why: the divergence report would be far more useful if it could print
      the comment beside the scalar, and nothing in the node type can hold
      one today.
- [ ] an iterator over an io.Reader instead of one slice
      why: the whole document is in memory twice, as a parse tree and as a
      value tree, so a file larger than a third of RAM cannot be read.
- [ ] a Go fuzz target with the hostile corpus as its seed
      why: the byte-level sweep in t/hostile.py is a fixed list of
      mutations; go test -fuzz is coverage guided and would reach further.
- [ ] 1.1 collection tags, or a decision not to
      why: !!omap, !!pairs and !!set are the last value disagreement with
      PyYAML in the whole corpus, and "tags are carried, not dispatched on"
      is a non-goal that was chosen before that was measured.
