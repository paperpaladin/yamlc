// yamlc reads a YAML 1.2 stream and reports what it means, under the 1.2
// core schema and under the 1.1 resolution rules, so that the two readings
// can be put side by side.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

const usage = `usage: yamlc COMMAND [OPTION]... FILE...

  check FILE...      parse and compose each file; print ok or the refusal
  events FILE...     the parse as an event stream, for diffing against PyYAML
  json FILE...       one JSON value per document
  dump FILE...       one canonical value per document, types included
  scalars FILE...    every scalar, with both readings of it
  diverge FILE...    where a 1.1 reader and a 1.2 reader differ
  counts FILE...     how many of each construct the files hold

options, where they apply:
  --schema=1.1|1.2   which resolution rules (default 1.2, or 1.1 if the
                     document says %YAML 1.1)
  --dup=error|first|last   what a repeated mapping key means (default error)
  --merge=on|off     honour the << merge key (default on)
  --max-depth=N      refuse nesting deeper than this (default 400)
  --max-nodes=N      refuse an expansion larger than this (default 1000000)
  --batch            json and dump only: one tab separated line per
                     document, naming the file, so a corpus of thousands
                     costs one process
`

type opts struct {
	schema    string
	dup       string
	merge     string
	maxDepth  int
	maxNodes  int
	files     []string
	schemaSet bool
	batch     bool
}

func parseOpts(cmd string, args []string) (*opts, error) {
	o := &opts{}
	fs := flag.NewFlagSet("yamlc "+cmd, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.schema, "schema", "1.2", "")
	fs.StringVar(&o.dup, "dup", "error", "")
	fs.StringVar(&o.merge, "merge", "on", "")
	fs.IntVar(&o.maxDepth, "max-depth", defaultMaxDepth, "")
	fs.IntVar(&o.maxNodes, "max-nodes", defaultBudget, "")
	fs.BoolVar(&o.batch, "batch", false, "")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "schema" {
			o.schemaSet = true
		}
	})
	if o.schema != "1.1" && o.schema != "1.2" {
		return nil, fmt.Errorf("--schema takes 1.1 or 1.2, not %q", o.schema)
	}
	switch o.dup {
	case "error", "first", "last":
	default:
		return nil, fmt.Errorf("--dup takes error, first or last, not %q", o.dup)
	}
	if o.merge != "on" && o.merge != "off" {
		return nil, fmt.Errorf("--merge takes on or off, not %q", o.merge)
	}
	o.files = fs.Args()
	if len(o.files) == 0 {
		return nil, fmt.Errorf("no files")
	}
	return o, nil
}

func (o *opts) composer(name string) *composer {
	schema := 12
	if o.schema == "1.1" {
		schema = 11
	}
	dup := dupError
	switch o.dup {
	case "first":
		dup = dupFirst
	case "last":
		dup = dupLast
	}
	c := newComposer(name, schema, dup, o.merge == "on", o.maxNodes)
	c.schemaFromDoc = !o.schemaSet
	return c
}

// load parses and composes one file, which is what every command but
// events needs.
func (o *opts) load(name string) ([]*Document, []*Value, error) {
	raw, err := readInput(name)
	if err != nil {
		return nil, nil, err
	}
	docs, err := parseStream(name, raw, o.maxDepth)
	if err != nil {
		return nil, nil, err
	}
	c := o.composer(name)
	var vals []*Value
	for _, d := range docs {
		v, err := c.composeDocument(d)
		if err != nil {
			return nil, nil, err
		}
		vals = append(vals, v)
	}
	return docs, vals, nil
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmd, rest := args[0], args[1:]
	if cmd == "events" {
		os.Exit(cmdEvents(rest))
	}
	o, err := parseOpts(cmd, rest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "yamlc %s: %v\n", cmd, err)
		os.Exit(2)
	}
	switch cmd {
	case "check":
		os.Exit(cmdCheck(o))
	case "json":
		os.Exit(cmdJSON(o))
	case "dump":
		os.Exit(cmdDump(o))
	case "scalars":
		os.Exit(cmdScalars(o))
	case "diverge":
		os.Exit(cmdDiverge(o))
	case "counts":
		os.Exit(cmdCounts(o))
	default:
		fmt.Fprintf(os.Stderr, "yamlc: no such command: %s\n", cmd)
		os.Exit(2)
	}
}
