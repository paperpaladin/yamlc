package main

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// A Refusal is what yamlc returns instead of a guess. Every one of them
// carries a position, because a parse error that does not say where is a
// guess with a message attached.
type Refusal struct {
	File string
	Line int // 1 based
	Col  int // 1 based, counted in characters and not in bytes
	Msg  string
}

func (r *Refusal) Error() string {
	f := r.File
	if f == "" {
		f = "-"
	}
	return fmt.Sprintf("%s:%d:%d: %s", f, r.Line, r.Col, r.Msg)
}

// src is the cursor every production reads through. It holds the whole
// input, already checked for UTF-8 validity and for the printable character
// set, and with line breaks normalised. After that check a zero byte from
// peek can only mean end of input, which is what the rest of the parser
// relies on.
type src struct {
	name string
	b    []byte
	pos  int
	line int // 1 based
	col  int // 0 based: indentation arithmetic counts from zero
}

// newSrc validates the input and normalises its line breaks.
//
// YAML 1.2.2 section 5.5: "the only characters that are line breaks are
// LF and CR"; section 5.8 requires a CR LF pair and a lone CR to be
// normalised to a single LF on input. 1.1 also counted NEL, LS and PS as
// line breaks, which is one of the places a 1.1 reader sees a different
// document; see diverge.go.
//
// Section 5.1 gives the printable set: TAB, LF, CR, #x20 to #x7E, NEL,
// #xA0 to #xD7FF, #xE000 to #xFFFD, and #x10000 to #x10FFFF. Everything
// else is refused, with the position of the offending character.
func newSrc(name string, in []byte) (*src, error) {
	// A byte order mark at the start of the stream is allowed and is not
	// part of the content (section 5.2).
	if len(in) >= 3 && in[0] == 0xef && in[1] == 0xbb && in[2] == 0xbf {
		in = in[3:]
	}
	out := make([]byte, 0, len(in)+1)
	line, col := 1, 1
	for i := 0; i < len(in); {
		c := in[i]
		if c < 0x80 {
			switch {
			case c == '\n':
				out = append(out, '\n')
				i++
				line, col = line+1, 1
				continue
			case c == '\r':
				out = append(out, '\n')
				i++
				if i < len(in) && in[i] == '\n' {
					i++
				}
				line, col = line+1, 1
				continue
			case c == '\t' || (c >= 0x20 && c <= 0x7e):
				out = append(out, c)
				i++
				col++
				continue
			}
			return nil, &Refusal{name, line, col,
				fmt.Sprintf("character #x%02X is not printable", c)}
		}
		r, w := utf8.DecodeRune(in[i:])
		if r == utf8.RuneError && w <= 1 {
			return nil, &Refusal{name, line, col,
				fmt.Sprintf("byte #x%02X does not begin a valid UTF-8 character", c)}
		}
		if !printable(r) {
			return nil, &Refusal{name, line, col,
				fmt.Sprintf("character U+%04X is not printable", r)}
		}
		out = append(out, in[i:i+w]...)
		i += w
		col++
	}
	return &src{name: name, b: out, line: 1, col: 0}, nil
}

func printable(r rune) bool {
	switch {
	case r == 0x85: // NEL, printable in 1.2 but not a line break
		return true
	case r >= 0xa0 && r <= 0xd7ff:
		return true
	case r >= 0xe000 && r <= 0xfffd:
		return true
	case r >= 0x10000 && r <= 0x10ffff:
		return true
	}
	return false
}

// peek returns the byte n ahead of the cursor, or 0 at end of input. A zero
// byte in the input itself was refused by newSrc, so 0 means end here.
func (s *src) peek(n int) byte {
	if s.pos+n >= len(s.b) {
		return 0
	}
	return s.b[s.pos+n]
}

func (s *src) eof() bool { return s.pos >= len(s.b) }

// next advances one character, keeping line and column in step. Columns
// count characters, so a multi-byte character moves the column by one.
func (s *src) next() {
	if s.pos >= len(s.b) {
		return
	}
	c := s.b[s.pos]
	if c == '\n' {
		s.pos++
		s.line++
		s.col = 0
		return
	}
	if c < 0x80 {
		s.pos++
	} else {
		_, w := utf8.DecodeRune(s.b[s.pos:])
		s.pos += w
	}
	s.col++
}

func (s *src) advance(n int) {
	for i := 0; i < n; i++ {
		s.next()
	}
}

// errf refuses at the cursor.
func (s *src) errf(format string, a ...any) error {
	return &Refusal{s.name, s.line, s.col + 1, fmt.Sprintf(format, a...)}
}

// errAt refuses at a position recorded earlier, which is what a message
// about an unclosed bracket or a duplicated key needs.
func (s *src) errAt(line, col int, format string, a ...any) error {
	return &Refusal{s.name, line, col + 1, fmt.Sprintf(format, a...)}
}

// rest returns the remainder of the current line, for messages.
func (s *src) rest() string {
	i := strings.IndexByte(string(s.b[s.pos:]), '\n')
	if i < 0 {
		return string(s.b[s.pos:])
	}
	return string(s.b[s.pos : s.pos+i])
}
