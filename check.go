package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

func readInput(name string) ([]byte, error) {
	if name == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(name)
}

// cmdCheck parses and composes each file and prints either ok or the
// refusal. Composing matters: an alias cycle and a duplicate key are only
// visible there, and a command that stopped at the parse would call both
// of those documents fine.
func cmdCheck(o *opts) int {
	bad := 0
	for _, name := range o.files {
		if _, _, err := o.load(name); err != nil {
			fmt.Println(err)
			bad++
			continue
		}
		fmt.Printf("%s: ok\n", name)
	}
	if bad > 0 {
		return 1
	}
	return 0
}

func cmdJSON(o *opts) int {
	// --batch exists so that a corpus of several thousand files costs one
	// process rather than several thousand. Each line is the file name, a
	// status and then either the JSON or the refusal, separated by tabs,
	// which JSON output cannot contain unescaped.
	if o.batch {
		rc := 0
		for _, name := range o.files {
			_, vals, err := o.load(name)
			if err != nil {
				fmt.Printf("%s\terr\t%v\n", name, err)
				rc = 1
				continue
			}
			for _, v := range vals {
				var b strings.Builder
				if err := jsonValue(&b, v); err != nil {
					fmt.Printf("%s\terr\t%v\n", name, err)
					rc = 1
					continue
				}
				fmt.Printf("%s\tok\t%s\n", name, b.String())
			}
		}
		return rc
	}
	for _, name := range o.files {
		_, vals, err := o.load(name)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		for _, v := range vals {
			if err := writeJSON(os.Stdout, v); err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
				return 1
			}
		}
	}
	return 0
}

func cmdDump(o *opts) int {
	rc := 0
	for _, name := range o.files {
		_, vals, err := o.load(name)
		if err != nil {
			if o.batch {
				fmt.Printf("%s\terr\t%v\n", name, err)
				rc = 1
				continue
			}
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		for _, v := range vals {
			if o.batch {
				fmt.Printf("%s\tok\t%s\n", name, canon(v))
			} else {
				fmt.Println(canon(v))
			}
		}
	}
	return rc
}
