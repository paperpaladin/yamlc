package main

import (
	"fmt"
	"math"
	"strings"
)

// canon writes a value in a canonical text form, so that two readers of the
// same document can be compared as text and so that two mapping keys can be
// compared for equality.
//
// t/pyyaml.py prints the same form from what yaml.safe_load returns. The
// shapes that matter:
//
//	null                 true   false
//	int 42               float 3ff0000000000000      str abc
//	date 2001-12-14      datetime 2001-12-14 21:59:43.100000 +00:00
//	bin 414243           tag:example.com,2000:x abc
//	[a, b]               {k: v, k: v}
//
// A float is written as the sixteen hexadecimal digits of its IEEE 754
// bits rather than as a decimal. Two shortest round-trip decimal forms are
// not the same string in Go and in Python (100000.0 against 100000), and a
// comparison that has to normalise both sides is measuring the normaliser.
func canon(v *Value) string {
	var b strings.Builder
	writeCanon(&b, v)
	return b.String()
}

func writeCanon(b *strings.Builder, v *Value) {
	switch v.Kind {
	case VNull:
		b.WriteString("null")
	case VBool:
		if v.Bool {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case VInt:
		fmt.Fprintf(b, "int %s", v.Int.String())
	case VFloat:
		// NaN is written as a word rather than as bits. Go's math.NaN and
		// the one PyYAML builds from -inf/inf are both quiet NaNs with
		// different payloads and different signs, and a comparison on the
		// bits would report a disagreement about something neither reader
		// claims to preserve.
		if math.IsNaN(v.Float) {
			b.WriteString("float nan")
		} else {
			fmt.Fprintf(b, "float %016x", math.Float64bits(v.Float))
		}
	case VStr:
		fmt.Fprintf(b, "str %s", escapeEvent(v.Str))
	case VStamp:
		b.WriteString(v.Str)
	case VBin:
		fmt.Fprintf(b, "bin %x", v.Str)
	case VOther:
		fmt.Fprintf(b, "%s %s", v.Tag, escapeEvent(v.Str))
	case VSeq:
		b.WriteByte('[')
		for i, x := range v.Seq {
			if i > 0 {
				b.WriteString(", ")
			}
			writeCanon(b, x)
		}
		b.WriteByte(']')
	case VMap:
		b.WriteByte('{')
		for i, kv := range v.Map {
			if i > 0 {
				b.WriteString(", ")
			}
			writeCanon(b, kv.Key)
			b.WriteString(": ")
			writeCanon(b, kv.Val)
		}
		b.WriteByte('}')
	}
}
