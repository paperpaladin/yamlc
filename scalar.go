package main

import "strings"

// scanSingle reads a single-quoted scalar. Section 7.3.2: the only escape
// is a doubled quote, and the line folding is the same as a plain scalar's.
func (p *parser) scanSingle(ind int) (string, bool, error) {
	oline, ocol := p.s.line, p.s.col
	p.s.next()
	var out []byte
	multi := false
	for {
		c := p.s.peek(0)
		switch {
		case c == 0:
			return "", false, p.s.errAt(oline, ocol, "single-quoted scalar is never closed")
		case c == '\'':
			if p.s.peek(1) == '\'' {
				out = append(out, '\'')
				p.s.advance(2)
				continue
			}
			p.s.next()
			return string(out), multi, nil
		case c == '\n':
			out = trimRightBlanks(out)
			breaks, ind2, ok, err := p.foldBreaks(ind)
			if err != nil {
				return "", false, err
			}
			if !ok || ind2 <= ind || (ind2 == 0 && p.atAnyMarker()) {
				return "", false, p.s.errAt(oline, ocol, "single-quoted scalar is never closed")
			}
			multi = true
			if breaks == 1 {
				out = append(out, ' ')
			} else {
				out = append(out, strings.Repeat("\n", breaks-1)...)
			}
		default:
			st := p.s.pos
			p.s.next()
			out = append(out, p.s.b[st:p.s.pos]...)
		}
	}
}

func trimRightBlanks(b []byte) []byte {
	n := len(b)
	for n > 0 && isBlank(b[n-1]) {
		n--
	}
	return b[:n]
}

// scanDouble reads a double-quoted scalar, the only form with escapes.
// Section 5.7 gives the table; anything else after a backslash is a
// refusal, because a parser that passes an unknown escape through produces
// a string the author did not write.
func (p *parser) scanDouble(ind int) (string, bool, error) {
	oline, ocol := p.s.line, p.s.col
	p.s.next()
	var out []byte
	multi := false
	for {
		c := p.s.peek(0)
		switch {
		case c == 0:
			return "", false, p.s.errAt(oline, ocol, "double-quoted scalar is never closed")
		case c == '"':
			p.s.next()
			return string(out), multi, nil
		case c == '\\':
			if p.s.peek(1) == '\n' {
				// s-double-escaped: the whitespace before the backslash
				// and the break itself are both dropped, and any blank
				// line after it still counts as a line feed.
				out = trimRightBlanks(out)
				p.s.next()
				breaks, ind2, ok, err := p.foldBreaks(ind)
				if err != nil {
					return "", false, err
				}
				if !ok || ind2 <= ind || (ind2 == 0 && p.atAnyMarker()) {
					return "", false, p.s.errAt(oline, ocol, "double-quoted scalar is never closed")
				}
				multi = true
				out = append(out, strings.Repeat("\n", breaks-1)...)
				continue
			}
			b, err := p.escape()
			if err != nil {
				return "", false, err
			}
			out = append(out, b...)
		case c == '\n':
			out = trimRightBlanks(out)
			breaks, ind2, ok, err := p.foldBreaks(ind)
			if err != nil {
				return "", false, err
			}
			if !ok || ind2 <= ind || (ind2 == 0 && p.atAnyMarker()) {
				return "", false, p.s.errAt(oline, ocol, "double-quoted scalar is never closed")
			}
			multi = true
			if breaks == 1 {
				out = append(out, ' ')
			} else {
				out = append(out, strings.Repeat("\n", breaks-1)...)
			}
		default:
			st := p.s.pos
			p.s.next()
			out = append(out, p.s.b[st:p.s.pos]...)
		}
	}
}

// escape reads one escape sequence, the cursor sitting on the backslash.
func (p *parser) escape() ([]byte, error) {
	line, col := p.s.line, p.s.col
	p.s.next()
	c := p.s.peek(0)
	p.s.next()
	switch c {
	case '0':
		return []byte{0}, nil
	case 'a':
		return []byte{7}, nil
	case 'b':
		return []byte{8}, nil
	case 't', '\t':
		return []byte{'\t'}, nil
	case 'n':
		return []byte{'\n'}, nil
	case 'v':
		return []byte{0x0b}, nil
	case 'f':
		return []byte{0x0c}, nil
	case 'r':
		return []byte{'\r'}, nil
	case 'e':
		return []byte{0x1b}, nil
	case ' ':
		return []byte{' '}, nil
	case '"':
		return []byte{'"'}, nil
	case '/':
		return []byte{'/'}, nil
	case '\\':
		return []byte{'\\'}, nil
	case 'N':
		return []byte{0xc2, 0x85}, nil // U+0085
	case '_':
		return []byte{0xc2, 0xa0}, nil // U+00A0
	case 'L':
		return []byte{0xe2, 0x80, 0xa8}, nil // U+2028
	case 'P':
		return []byte{0xe2, 0x80, 0xa9}, nil // U+2029
	case 'x':
		v, err := p.hexDigits(2, line, col)
		if err != nil {
			return nil, err
		}
		return encodeRune(v), nil
	case 'u':
		v, err := p.hexDigits(4, line, col)
		if err != nil {
			return nil, err
		}
		return p.maybePair(v), nil
	case 'U':
		v, err := p.hexDigits(8, line, col)
		if err != nil {
			return nil, err
		}
		if v > 0x10ffff {
			return nil, p.s.errAt(line, col, "\\U%08X is not a character", v)
		}
		return p.maybePair(v), nil
	case 0:
		return nil, p.s.errAt(line, col, "a backslash at end of input is not an escape")
	}
	return nil, p.s.errAt(line, col, "\\%s is not an escape in YAML 1.2", string(c))
}

// maybePair joins a surrogate pair, as JSON requires, and keeps a lone
// surrogate as the three bytes that would encode it.
//
// This is a deliberate departure from the letter of the YAML spec, which
// only defines escapes for characters and says a surrogate is not one. It
// is here because the hard invariant of this project is exact agreement
// with Python's json module over the JSON subset, and JSON allows a lone
// surrogate escape: json.loads('"\\ud800"') is a one-character string.
// Refusing it would mean refusing valid JSON. The encoding used is the
// obvious extension of UTF-8 to surrogate code points, which is what Go,
// Python and most other runtimes do internally.
func (p *parser) maybePair(v int) []byte {
	if v >= 0xd800 && v <= 0xdbff && p.s.peek(0) == '\\' && p.s.peek(1) == 'u' {
		m := p.mark()
		line, col := p.s.line, p.s.col
		p.s.advance(2)
		lo, err := p.hexDigits(4, line, col)
		if err == nil && lo >= 0xdc00 && lo <= 0xdfff {
			return encodeRune(0x10000 + (v-0xd800)<<10 + (lo - 0xdc00))
		}
		p.reset(m)
	}
	return encodeRune(v)
}

// encodeRune writes a code point as UTF-8, extended to the surrogate range
// rather than replacing it, which is the point of not using utf8.AppendRune
// here.
func encodeRune(v int) []byte {
	switch {
	case v < 0x80:
		return []byte{byte(v)}
	case v < 0x800:
		return []byte{byte(0xc0 | v>>6), byte(0x80 | v&0x3f)}
	case v < 0x10000:
		return []byte{byte(0xe0 | v>>12), byte(0x80 | v>>6&0x3f), byte(0x80 | v&0x3f)}
	}
	return []byte{byte(0xf0 | v>>18), byte(0x80 | v>>12&0x3f), byte(0x80 | v>>6&0x3f), byte(0x80 | v&0x3f)}
}

func (p *parser) hexDigits(n, line, col int) (int, error) {
	v := 0
	for i := 0; i < n; i++ {
		d, ok := hexVal(p.s.peek(0))
		if !ok {
			return 0, p.s.errAt(line, col, "an escape needs %d hexadecimal digits", n)
		}
		v = v<<4 | d
		p.s.next()
	}
	return v, nil
}
