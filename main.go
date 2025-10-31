// yamlc reads a YAML 1.2 stream and reports what it means, under the 1.2
// core schema and under the 1.1 resolution rules, so that the two readings
// can be put side by side.
package main

import (
	"fmt"
	"os"
)

const usage = `usage: yamlc COMMAND [OPTION]... FILE...

  check FILE...      parse each file; print ok, or the refusal with a position
  events FILE...     the parse as an event stream, for diffing against PyYAML
`

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	switch args[0] {
	case "check":
		os.Exit(cmdCheck(args[1:]))
	case "events":
		os.Exit(cmdEvents(args[1:]))
	default:
		fmt.Fprintf(os.Stderr, "yamlc: no such command: %s\n", args[0])
		os.Exit(2)
	}
}
