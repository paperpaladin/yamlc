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

- [ ] repository skeleton: go.mod, Makefile, .gitignore, LICENSE, this plan
      why: the collection's gate runs make, so make has to exist first.
- [ ] source cursor with line and column, break normalisation, BOM
      why: every refusal in this project is a position, so positions come first.
- [ ] refusal type and its one-line form, with a case per field
      why: a parse error that does not say where is a guess with a message.
- [ ] t/run.sh with the pass and fail protocol
      why: the harness has to exist before the thing it measures.
- [ ] comment skipping, blank lines, end of input
      why: the smallest piece of the grammar that every other piece calls.
- [ ] non-printable character refusal, and the tab rules
      why: a tab where indentation is expected is the classic YAML trap and
      has to be a refusal rather than a quiet reinterpretation.

### Milestone 2: scalars

- [ ] plain scalars on one line, with the indicator exclusions
      why: the commonest node in any real document.
- [ ] single-quoted scalars, doubled quote escape
      why: the simplest quoted form, and the folding rules are shared.
- [ ] double-quoted scalars: the whole escape table
      why: `\u` and `\U` are where a parser quietly produces the wrong bytes.
- [ ] lone surrogate escapes kept as written
      why: JSON allows them and the json oracle will produce them.
- [ ] escaped line breaks in double-quoted scalars
      why: a backslash at end of line removes the fold, which nothing else does.
- [ ] multi-line plain and quoted scalar folding
      why: line folding is a rule about blank lines, not about joining.
- [ ] literal block scalars, chomping clip, strip and keep
      why: the three chomping modes give three different trailing newlines.
- [ ] folded block scalars, including the more-indented line exception
      why: folding stops at a line that is indented more than the block, which
      is the rule implementations most often skip.
- [ ] explicit indentation indicators on block scalars
      why: `|2` changes where the content starts and is unguessable otherwise.

### Milestone 3: collections

- [ ] block sequences, nested without an intervening key
      why: `- - -` is three nested sequences and is easy to get wrong.
- [ ] block mappings with simple keys
      why: the shape every configuration file is written in.
- [ ] a sequence at the same indentation as its parent mapping key
      why: valid, extremely common, and a special case in the grammar.
- [ ] explicit key and value indicators in block context
      why: a key that is itself a collection has no other spelling.
- [ ] flow sequences and flow mappings
      why: the JSON subset is entirely flow, so the json oracle needs these.
- [ ] flow collections spanning several lines
      why: the indentation rules inside a flow collection are different.
- [ ] the JSON-like key exception: a quoted key followed directly by a colon
      why: `{"a":1}` has no space after the colon and must still parse.
- [ ] empty entries in flow collections, and the empty key
      why: `{a: }`, `[,]` and `{}` all have defined meanings.

### Milestone 4: the stream

- [ ] document start and end markers, bare documents, several per stream
      why: a stream of documents is the unit the tool reports on.
- [ ] the `%YAML` directive and the version check
      why: a document asking for 1.1 is asking for different resolution rules.
- [ ] the `%TAG` directive, tag handles, and shorthand expansion
      why: a tag shorthand that expands wrongly silently changes the type.
- [ ] verbatim tags, and a tag on a null node
      why: both are places where the property and the node are separable.
- [ ] anchors and aliases
      why: the second half of this project is about what aliases mean.
- [ ] an alias to an anchor that does not exist yet, refused
      why: anchors are defined before use and the opposite must be an error.
- [ ] an alias cycle refused, with the suite proving termination
      why: a parser that follows one does not return.
- [ ] a nesting depth limit, refused with a position
      why: ten thousand open brackets should not be a stack overflow.
- [ ] an alias expansion budget, refused
      why: billion laughs is the denial-of-service case this format has.

### Milestone 5: resolution

- [ ] the 1.2 core schema: null, bool, int in three bases, float, inf, nan
      why: the heart of the specification half of this project.
- [ ] integers wider than a double, kept exactly
      why: the json oracle will hand us some and a float would lose them.
- [ ] the 1.1 resolution table, as PyYAML implements it
      why: the comparison needs a second reading of the same scalar.
- [ ] 1.1 sexagesimal integers and floats
      why: `1:30` is ninety in 1.1 and a string in 1.2.
- [ ] 1.1 timestamps
      why: a date that is a string in one reader and a date in another.
- [ ] the merge key, and its interaction with duplicate keys
      why: `<<` is a 1.1 type that every 1.2 implementation also carries.
- [ ] duplicate key detection with both positions
      why: accepted by some parsers and refused by others, and the cause of
      more than one production incident.

### Milestone 6: output

- [ ] JSON output: exact integers, round-tripping floats, escaped surrogates
      why: the json oracle compares parsed structures, so the writing has to
      be lossless before the comparison means anything.
- [ ] the event stream form, for comparison against PyYAML's events
      why: structure and schema are separate questions and separate oracles.
- [ ] the per-scalar report: position, path, raw text, both readings
      why: this is the output the second half of the project exists for.
- [ ] `yamlc diverge`: the list of places the two readings differ
      why: the deliverable.
- [ ] `yamlc counts`: how many of each construct a document holds
      why: a folded-scalar test over a corpus with no folded scalars passes
      having run nothing.

### Milestone 7: measurement

- [ ] the json oracle over every JSON file on the machine
      why: a body of JSON nobody here wrote.
- [ ] generated awkward JSON: nesting, duplicates, surrogates, precision
      why: the system files are well formed and will not probe the edges.
- [ ] the PyYAML event comparison over the whole corpus
      why: the structural oracle.
- [ ] the PyYAML resolution comparison, every disagreement classified
      why: an unclassified disagreement is an unread bug report.
- [ ] hand-written corpus: tabs, flow over lines, cycles, tags on nulls
      why: the cases no generator produces.
- [ ] hostile corpus: billion laughs, ten thousand deep, truncation
      why: refusing these is part of the contract.
- [ ] construct counts asserted non-zero, printed every run
      why: the vacuous-pass guard.
- [ ] mutation run over the scanner, survivors recorded
      why: the suite is worth the mutants it kills and nothing more.
- [ ] mutation run over the resolution tables, survivors recorded
      why: a resolution table is a list of regexes and a deleted line is
      exactly the defect this project is about.
- [ ] README with the measured numbers, every disagreement, and what does
      not work
      why: a README that only lists successes is not finished.
