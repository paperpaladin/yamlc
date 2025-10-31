package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// The event stream is the form used to compare yamlc's structure against
// PyYAML's, and it deliberately carries no resolution: a plain scalar comes
// out as a plain scalar whatever its content looks like. Structure and
// schema are separate questions, so they get separate comparisons.
//
// The shape is the one the YAML test suite uses:
//
//	+STR
//	+DOC ---            --- only when the document said so
//	+MAP {}             {} only when the mapping was written in flow form
//	=VAL &a <tag> :v    style is : ' " | or >
//	=ALI *a
//	-MAP
//	-DOC ...
//	-STR
//
// t/eventcmp.py prints the same shape from PyYAML's own event stream. The
// escaping of a value is defined in escapeEvent below and implemented
// twice, once there and once here; it is a presentation detail, and the
// values themselves are checked a second time by the json oracle, which
// shares none of this code.
func writeEvents(w io.Writer, docs []*Document) {
	fmt.Fprintln(w, "+STR")
	for _, d := range docs {
		if d.Explicit {
			fmt.Fprintln(w, "+DOC ---")
		} else {
			fmt.Fprintln(w, "+DOC")
		}
		writeNodeEvents(w, d.Root)
		if d.EndMarked {
			fmt.Fprintln(w, "-DOC ...")
		} else {
			fmt.Fprintln(w, "-DOC")
		}
	}
	fmt.Fprintln(w, "-STR")
}

func props(n *Node) string {
	var b strings.Builder
	if n.Anchor != "" {
		b.WriteString(" &" + n.Anchor)
	}
	if n.Tag != "" {
		b.WriteString(" <" + n.Tag + ">")
	}
	return b.String()
}

func writeNodeEvents(w io.Writer, n *Node) {
	switch n.Kind {
	case KAlias:
		fmt.Fprintf(w, "=ALI *%s\n", n.Alias)
	case KScalar:
		fmt.Fprintf(w, "=VAL%s %s%s\n", props(n), n.Style.char(), escapeEvent(n.Val))
	case KSeq:
		flow := ""
		if n.Flow {
			flow = " []"
		}
		fmt.Fprintf(w, "+SEQ%s%s\n", props(n), flow)
		for _, c := range n.Seq {
			writeNodeEvents(w, c)
		}
		fmt.Fprintln(w, "-SEQ")
	case KMap:
		flow := ""
		if n.Flow {
			flow = " {}"
		}
		fmt.Fprintf(w, "+MAP%s%s\n", props(n), flow)
		for _, pr := range n.Map {
			writeNodeEvents(w, pr.Key)
			writeNodeEvents(w, pr.Val)
		}
		fmt.Fprintln(w, "-MAP")
	}
}

// escapeEvent makes a scalar printable on one line. Backslash, the line
// break and the tab go to their C escapes; every other control character
// and every surrogate code point goes to \uXXXX. A surrogate can only be
// here because a double-quoted scalar escaped one, which YAML does not
// define and JSON does; see the note in scalar.go.
func escapeEvent(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		if c < 0x80 {
			switch {
			case c == '\\':
				b.WriteString(`\\`)
			case c == '\n':
				b.WriteString(`\n`)
			case c == '\r':
				b.WriteString(`\r`)
			case c == '\t':
				b.WriteString(`\t`)
			case c < 0x20 || c == 0x7f:
				fmt.Fprintf(&b, `\u%04X`, c)
			default:
				b.WriteByte(c)
			}
			i++
			continue
		}
		r, w := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && w <= 1 {
			// A surrogate kept as WTF-8 by the double-quoted scanner.
			if i+3 <= len(s) && s[i] == 0xed {
				cp := rune(0xd000) | rune(s[i+1]&0x3f)<<6 | rune(s[i+2]&0x3f)
				fmt.Fprintf(&b, `\u%04X`, cp)
				i += 3
				continue
			}
			fmt.Fprintf(&b, `\x%02X`, s[i])
			i++
			continue
		}
		b.WriteString(s[i : i+w])
		i += w
	}
	return b.String()
}

func cmdEvents(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "yamlc events: no files")
		return 2
	}
	rc := 0
	for _, name := range args {
		raw, err := readInput(name)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			rc = 1
			continue
		}
		docs, err := parseStream(name, raw, 0)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			rc = 1
			continue
		}
		writeEvents(os.Stdout, docs)
	}
	return rc
}
