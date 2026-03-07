package main

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FuzzParse is coverage-guided fuzzing over the parser, which the
// byte-level sweep in t/hostile.py cannot be: that one applies a fixed
// list of mutations to five seeds and stops. Run it with
//
//	make fuzz            thirty seconds
//	make fuzz FUZZTIME=10m
//
// It is not part of make test, because a fuzz run has no fixed length and
// a suite that sometimes takes ten minutes is a suite people stop running.
//
// Three properties, in increasing order of what they can catch:
//
//  1. No input makes the parser panic. A panic is a refusal that did not
//     happen, and in a parser it is also the whole denial-of-service
//     surface.
//  2. Every refusal carries a position. A message with no line and column
//     in it is a guess with an apology attached.
//  3. If a document composes to a value that can be written as JSON, then
//     parsing that JSON again gives the same value. JSON is a subset of
//     YAML, so this is a real round trip and not a tautology: it goes out
//     through the writer and back in through the whole parser.
func finiteOnly(v *Value) bool {
	switch v.Kind {
	case VFloat:
		return !math.IsInf(v.Float, 0) && !math.IsNaN(v.Float)
	case VSeq:
		for _, x := range v.Seq {
			if !finiteOnly(x) {
				return false
			}
		}
	case VMap:
		for _, kv := range v.Map {
			if !finiteOnly(kv.Key) || !finiteOnly(kv.Val) {
				return false
			}
		}
	}
	return true
}

func FuzzParse(f *testing.F) {
	for _, dir := range []string{"t/corpus/hostile", "t/corpus/fuzz", "t/corpus/yaml"} {
		names, err := filepath.Glob(filepath.Join(dir, "*"))
		if err != nil {
			continue
		}
		for i, n := range names {
			if i > 200 {
				break
			}
			b, err := os.ReadFile(n)
			if err == nil && len(b) < 64*1024 {
				f.Add(string(b))
			}
		}
	}
	f.Add("a: 1\n- b\n")
	f.Add("--- |2\n  x\n")
	f.Add("&a [*a]\n")
	f.Fuzz(func(t *testing.T, in string) {
		docs, err := parseStream("fuzz", []byte(in), 0)
		if err != nil {
			if !strings.Contains(err.Error(), "fuzz:") {
				t.Fatalf("refusal with no position: %v", err)
			}
			return
		}
		c := newComposer("fuzz", 12, dupLast, true, 20000)
		for _, d := range docs {
			v, err := c.composeDocument(d)
			if err != nil {
				if !strings.Contains(err.Error(), "fuzz:") {
					t.Fatalf("refusal with no position: %v", err)
				}
				continue
			}
			if !finiteOnly(v) {
				// An infinity or a NaN is written as Infinity or NaN,
				// which Python's json reads back and YAML 1.2 does not:
				// those two words are ordinary strings in the core
				// schema. So the output is still correct against the
				// oracle and is not re-readable here. The README says so
				// under what does not work.
				continue
			}
			var b strings.Builder
			if err := jsonValue(&b, v); err != nil {
				continue // not representable as JSON, which is allowed
			}
			again, err := parseStream("round", []byte(b.String()), 0)
			if err != nil {
				t.Fatalf("our own JSON does not parse: %v\ninput %q\njson %s", err, in, b.String())
			}
			if len(again) != 1 {
				t.Fatalf("our own JSON is %d documents\njson %s", len(again), b.String())
			}
			c2 := newComposer("round", 12, dupLast, true, 20000)
			v2, err := c2.composeDocument(again[0])
			if err != nil {
				t.Fatalf("our own JSON does not compose: %v\njson %s", err, b.String())
			}
			if canon(v) != canon(v2) {
				t.Fatalf("round trip changed the value\nbefore %s\nafter  %s\njson %s",
					canon(v), canon(v2), b.String())
			}
		}
	})
}
