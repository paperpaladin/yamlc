package main

// Flow collections, section 7.4. This is also the JSON subset: every JSON
// document is a flow collection or a scalar, so the exactness required
// against Python's json module is required of the code in this file.
//
// Indentation inside a flow collection is deliberately not enforced. The
// grammar asks for s-indent(n) on a continuation line, which would refuse
// "a: [1,\n2]"; PyYAML accepts it, most parsers accept it, and the README
// records the laxity rather than this file pretending to be strict.

// isFlowEnd reports the characters that end an entry. The distinction from
// isFlowIndicator matters: "[" and "{" are flow indicators and they OPEN a
// node, so treating them as terminators turns "{b: [c, d]}" into a refusal.
func isFlowEnd(c byte) bool { return c == ',' || c == ']' || c == '}' }

// parseFlow reads a flow sequence or a flow mapping into n.
func (p *parser) parseFlow(n *Node, ind int) error {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > p.maxDepth {
		return p.s.errf("nesting deeper than %d, refused", p.maxDepth)
	}
	open := p.s.peek(0)
	oline, ocol := p.s.line, p.s.col
	closing := byte(']')
	n.Kind, n.Flow = KSeq, true
	if open == '{' {
		closing = '}'
		n.Kind = KMap
	}
	p.s.next()
	for {
		if err := p.flowSkip(ind, oline, ocol, closing); err != nil {
			return err
		}
		if p.s.peek(0) == closing {
			p.s.next()
			return nil
		}
		if p.s.peek(0) == ',' {
			return p.s.errf("an entry is missing before this comma")
		}
		if n.Kind == KSeq {
			ent, err := p.flowSeqEntry(ind, oline, ocol, closing)
			if err != nil {
				return err
			}
			n.Seq = append(n.Seq, ent)
		} else {
			pr, err := p.flowMapEntry(ind, oline, ocol, closing)
			if err != nil {
				return err
			}
			n.Map = append(n.Map, pr)
		}
		if err := p.flowSkip(ind, oline, ocol, closing); err != nil {
			return err
		}
		switch p.s.peek(0) {
		case ',':
			p.s.next()
		case closing:
			p.s.next()
			return nil
		default:
			if p.s.eof() {
				return p.s.errAt(oline, ocol, "flow collection opened here is never closed")
			}
			return p.s.errf("expected %q or %q here, not %q",
				",", string(closing), string(p.s.peek(0)))
		}
	}
}

// flowSkip consumes separation inside a flow collection: blanks, comments
// and line breaks. A document marker at column zero means the collection
// was never closed, which is reported against the opening bracket.
func (p *parser) flowSkip(ind, oline, ocol int, closing byte) error {
	for {
		p.skipBlanks()
		switch {
		case p.s.peek(0) == '#':
			p.skipLine()
		case p.s.peek(0) == '\n':
			p.s.next()
		case p.s.eof():
			return p.s.errAt(oline, ocol, "flow collection opened here is never closed")
		default:
			if p.s.col == 0 && p.atAnyMarker() {
				return p.s.errAt(oline, ocol, "flow collection opened here is never closed")
			}
			return nil
		}
	}
}

// flowSeqEntry reads one entry of a flow sequence. An entry may be a node,
// or a single pair written without braces: ns-flow-seq-entry is
// ns-flow-pair or ns-flow-node, so "[a: 1]" is a sequence of one mapping.
func (p *parser) flowSeqEntry(ind, oline, ocol int, closing byte) (*Node, error) {
	line, col := p.s.line, p.s.col
	if p.s.peek(0) == '?' && (spaceOrEnd(p.s.peek(1)) || isFlowEnd(p.s.peek(1))) {
		p.s.next()
		pr, err := p.flowExplicitPair(ind, oline, ocol, closing)
		if err != nil {
			return nil, err
		}
		return &Node{Kind: KMap, Flow: true, Map: []Pair{pr}, Line: line, Col: col + 1}, nil
	}
	nd, jsonLike, err := p.flowNode(ind, oline, ocol, closing)
	if err != nil {
		return nil, err
	}
	// ns-s-implicit-yaml-key ends with s-separate-in-line?, so a plain
	// key's colon has to be on the key's own line. A JSON-like key goes
	// through c-ns-flow-map-json-key-entry, which allows s-separate and so
	// allows a line break.
	if jsonLike {
		if err := p.flowSkip(ind, oline, ocol, closing); err != nil {
			return nil, err
		}
	} else {
		p.skipBlanks()
	}
	if p.s.peek(0) == ':' && (jsonLike || spaceOrEnd(p.s.peek(1)) || isFlowEnd(p.s.peek(1))) {
		p.s.next()
		val, err := p.flowValue(ind, oline, ocol, closing)
		if err != nil {
			return nil, err
		}
		return &Node{Kind: KMap, Flow: true, Map: []Pair{{Key: nd, Val: val}},
			Line: line, Col: col + 1}, nil
	}
	return nd, nil
}

func (p *parser) flowMapEntry(ind, oline, ocol int, closing byte) (Pair, error) {
	if p.s.peek(0) == '?' && (spaceOrEnd(p.s.peek(1)) || isFlowEnd(p.s.peek(1))) {
		p.s.next()
		return p.flowExplicitPair(ind, oline, ocol, closing)
	}
	if p.s.peek(0) == ':' && (spaceOrEnd(p.s.peek(1)) || isFlowEnd(p.s.peek(1))) {
		// c-ns-flow-map-empty-key-entry: "{: 1}" has a null key.
		key := p.empty()
		p.s.next()
		val, err := p.flowValue(ind, oline, ocol, closing)
		return Pair{Key: key, Val: val}, err
	}
	key, jsonLike, err := p.flowNode(ind, oline, ocol, closing)
	if err != nil {
		return Pair{}, err
	}
	if jsonLike {
		if err := p.flowSkip(ind, oline, ocol, closing); err != nil {
			return Pair{}, err
		}
	} else {
		p.skipBlanks()
	}
	if p.s.peek(0) == ':' && (jsonLike || spaceOrEnd(p.s.peek(1)) || isFlowEnd(p.s.peek(1))) {
		p.s.next()
		val, err := p.flowValue(ind, oline, ocol, closing)
		return Pair{Key: key, Val: val}, err
	}
	return Pair{Key: key, Val: p.empty()}, nil
}

// flowExplicitPair reads the "? key : value" form, where the value's colon
// may sit on a later line.
func (p *parser) flowExplicitPair(ind, oline, ocol int, closing byte) (Pair, error) {
	if err := p.flowSkip(ind, oline, ocol, closing); err != nil {
		return Pair{}, err
	}
	var key *Node
	if p.s.peek(0) == ':' || isFlowEnd(p.s.peek(0)) {
		key = p.empty()
	} else {
		k, _, err := p.flowNode(ind, oline, ocol, closing)
		if err != nil {
			return Pair{}, err
		}
		key = k
	}
	if err := p.flowSkip(ind, oline, ocol, closing); err != nil {
		return Pair{}, err
	}
	if p.s.peek(0) == ':' {
		p.s.next()
		val, err := p.flowValue(ind, oline, ocol, closing)
		return Pair{Key: key, Val: val}, err
	}
	return Pair{Key: key, Val: p.empty()}, nil
}

// flowValue reads what follows a colon, which may be nothing at all.
func (p *parser) flowValue(ind, oline, ocol int, closing byte) (*Node, error) {
	if err := p.flowSkip(ind, oline, ocol, closing); err != nil {
		return nil, err
	}
	if isFlowEnd(p.s.peek(0)) {
		return p.empty(), nil
	}
	nd, _, err := p.flowNode(ind, oline, ocol, closing)
	return nd, err
}

// flowNode reads one node inside a flow collection: properties and then an
// alias, a nested collection, a quoted scalar or a plain scalar. jsonLike
// reports whether the node may be followed by a colon with no space, which
// is what makes {"a":1} parse.
func (p *parser) flowNode(ind, oline, ocol int, closing byte) (*Node, bool, error) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > p.maxDepth {
		return nil, false, p.s.errf("nesting deeper than %d, refused", p.maxDepth)
	}
	line, col := p.s.line, p.s.col
	n := &Node{Kind: KScalar, Line: line, Col: col + 1}
	props := false
	for p.s.peek(0) == '&' || p.s.peek(0) == '!' {
		props = true
		if p.s.peek(0) == '&' {
			if n.Anchor != "" {
				return nil, false, p.s.errf("a node can carry only one anchor")
			}
			a, err := p.readAnchor()
			if err != nil {
				return nil, false, err
			}
			n.Anchor = a
		} else {
			if n.TagRaw != "" {
				return nil, false, p.s.errf("a node can carry only one tag")
			}
			t, raw, err := p.readTag()
			if err != nil {
				return nil, false, err
			}
			n.Tag, n.TagRaw = t, raw
		}
		// After properties an empty node is allowed with no separation at
		// all, which is the e-scalar branch of ns-flow-node: "[&a, b]" is
		// an anchored null and then b.
		if isFlowEnd(p.s.peek(0)) || p.s.peek(0) == ':' {
			return n, false, nil
		}
		if !isBlank(p.s.peek(0)) && p.s.peek(0) != '\n' && !p.s.eof() {
			return nil, false, p.s.errf("a property must be followed by a space")
		}
		if err := p.flowSkip(ind, oline, ocol, closing); err != nil {
			return nil, false, err
		}
	}
	switch ch := p.s.peek(0); {
	case ch == '*':
		a, err := p.readAlias()
		if err != nil {
			return nil, false, err
		}
		if props {
			return nil, false, p.s.errAt(line, col, "an alias cannot carry an anchor or a tag")
		}
		n.Kind, n.Alias = KAlias, a
		return n, false, nil
	case ch == '[' || ch == '{':
		if err := p.parseFlow(n, ind); err != nil {
			return nil, false, err
		}
		return n, true, nil
	case ch == '\'':
		n.Style = StSingle
		v, _, err := p.scanSingle(ind)
		n.Val = v
		return n, true, err
	case ch == '"':
		n.Style = StDouble
		v, _, err := p.scanDouble(ind)
		n.Val = v
		return n, true, err
	case ch == '|' || ch == '>':
		return nil, false, p.s.errf("a block scalar cannot appear inside a flow collection")
	case isFlowEnd(ch) || ch == ':':
		return n, false, nil
	}
	n.Style = StPlain
	v, _, err := p.scanPlain(ind, true)
	n.Val = v
	return n, false, err
}
