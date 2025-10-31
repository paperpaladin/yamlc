package main

import (
	"fmt"
	"io"
	"os"
)

func readInput(name string) ([]byte, error) {
	if name == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(name)
}

// cmdCheck reads each file and prints either "ok" or the refusal. Its exit
// status is 1 if anything was refused, so a shell can use it directly.
func cmdCheck(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "yamlc check: no files")
		return 2
	}
	bad := 0
	for _, name := range args {
		raw, err := readInput(name)
		if err != nil {
			fmt.Printf("%s: %v\n", name, err)
			bad++
			continue
		}
		if _, err := parseStream(name, raw, 0); err != nil {
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
