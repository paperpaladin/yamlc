package main

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// JSON output. This exists so that the one thing this project is not
// allowed to get wrong can be measured: YAML 1.2 is a strict superset of
// JSON, so a JSON document must parse to exactly what Python's json module
// parses it to. t/jsoncmp.py reads this output with json.loads and compares
// the structures, so the writing has to be lossless before the comparison
// means anything.
//
// Three places where that is not automatic:
//
// An integer is written from its exact decimal text, not through a float.
// Python's int is arbitrary precision and json gives back every digit.
//
// A float is written with the shortest form that reads back as the same
// double, and always with a "." or an "e" in it, because "100" and "100.0"
// are an int and a float to Python and 1E2 in the input has to come back
// as a float.
//
// A surrogate code point is written as \uXXXX. It can only be in the data
// because an escape put it there, and json.loads produces exactly that
// character for exactly that escape.
func writeJSON(w io.Writer, v *Value) error {
	var b strings.Builder
	if err := jsonValue(&b, v); err != nil {
		return err
	}
	_, err := io.WriteString(w, b.String()+"\n")
	return err
}

func jsonValue(b *strings.Builder, v *Value) error {
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
		b.WriteString(v.Int.String())
	case VFloat:
		b.WriteString(jsonFloat(v.Float))
	case VStr, VStamp:
		jsonString(b, v.Str)
	case VBin:
		return fmt.Errorf("line %d: !!binary has no JSON form", v.Line)
	case VOther:
		return fmt.Errorf("line %d: the tag %s has no JSON form", v.Line, v.Tag)
	case VSeq:
		b.WriteByte('[')
		for i, x := range v.Seq {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := jsonValue(b, x); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case VMap:
		b.WriteByte('{')
		for i, kv := range v.Map {
			if i > 0 {
				b.WriteByte(',')
			}
			if kv.Key.Kind != VStr {
				return fmt.Errorf("line %d column %d: a JSON object key must be a string, and this one resolves to %s",
					kv.Key.Line, kv.Key.Col, kv.Key.Tag)
			}
			jsonString(b, kv.Key.Str)
			b.WriteByte(':')
			if err := jsonValue(b, kv.Val); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	}
	return nil
}

// jsonFloat writes a double so that Python reads back the same double and
// reads it back as a float rather than as an int. Infinity and NaN have no
// JSON spelling; the words used are the ones Python's json writes and
// accepts, and nothing in the JSON corpus can produce them.
func jsonFloat(f float64) string {
	switch {
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	case math.IsNaN(f):
		return "NaN"
	}
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

func jsonString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '"':
			b.WriteString(`\"`)
			i++
		case c == '\\':
			b.WriteString(`\\`)
			i++
		case c == '\n':
			b.WriteString(`\n`)
			i++
		case c == '\r':
			b.WriteString(`\r`)
			i++
		case c == '\t':
			b.WriteString(`\t`)
			i++
		case c == 0x08:
			b.WriteString(`\b`)
			i++
		case c == 0x0c:
			b.WriteString(`\f`)
			i++
		case c < 0x20:
			fmt.Fprintf(b, `\u%04x`, c)
			i++
		case c == 0xed && i+2 < len(s) && s[i+1] >= 0xa0 && s[i+1] <= 0xbf:
			// A surrogate kept as WTF-8 by the double-quoted scanner.
			cp := rune(0xd000) | rune(s[i+1]&0x3f)<<6 | rune(s[i+2]&0x3f)
			fmt.Fprintf(b, `\u%04x`, cp)
			i += 3
		default:
			b.WriteByte(c)
			i++
		}
	}
	b.WriteByte('"')
}
