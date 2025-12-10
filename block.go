package main

// Block sequences and block mappings, sections 8.2.1 and 8.2.2.
//
// Two indentation rules here are not obvious from the shape of a document
// and are what most of the cases in t/run.sh are about. A block sequence
// may begin at the same column as the mapping key it belongs to, which is
// seq-spaces in the grammar and the reason "a:\n- 1" is valid. And a block
// collection cannot begin on the same line as the mapping value it belongs
// to, which is why "a: b: c" is a refusal rather than a nested mapping.

// parseBlockSeq reads a block sequence whose entry indicators sit at
// column ind. The cursor is on the first "-".
func (p *parser) parseBlockSeq(ind int, n *Node) (*Node, error) {
	n.Kind = KSeq
	for {
		p.s.next() // the "-"
		ent, err := p.parseBlockNode(blockCtx{ind: ind, inline: true, compact: true})
		if err != nil {
			return nil, err
		}
		n.Seq = append(n.Seq, ent)
		m := p.mark()
		if err := p.skipPrefix(); err != nil {
			return nil, err
		}
		if p.s.eof() || p.atBoundary() {
			p.reset(m)
			return n, nil
		}
		ind2 := p.skipSpaces()
		if ind2 > ind {
			return nil, p.s.errf("this line is indented more than the sequence it follows")
		}
		if ind2 < ind || p.s.peek(0) != '-' || !spaceOrEnd(p.s.peek(1)) {
			p.reset(m)
			return n, nil
		}
	}
}

// parseBlockMap reads a block mapping whose keys sit at column ind.
// firstKey, when given, has already been read and the cursor sits just
// after it with its ":" still to come.
func (p *parser) parseBlockMap(ind int, firstKey *Node, n *Node) (*Node, error) {
	n.Kind = KMap
	for {
		var key *Node
		explicit := false
		var err error
		switch {
		case firstKey != nil:
			key, firstKey = firstKey, nil
		case p.s.peek(0) == '?' && spaceOrEnd(p.s.peek(1)):
			explicit = true
			p.s.next()
			key, err = p.parseBlockNode(blockCtx{ind: ind, inline: true, compact: true, seqAtInd: true})
			if err != nil {
				return nil, err
			}
		case p.s.peek(0) == ':' && spaceOrEnd(p.s.peek(1)):
			// An entry with no key at all. e-node is allowed in the key
			// position of l-block-map-implicit-entry, so ": a" is a
			// mapping from null to "a".
			key = p.empty()
		default:
			key, err = p.parseKeyNode(ind)
			if err != nil {
				return nil, err
			}
		}

		var val *Node
		if explicit {
			// The value's ":" is on a line of its own at column ind. When
			// there is none the value is empty, which is what "? a" alone
			// means.
			m := p.mark()
			if err := p.skipPrefix(); err != nil {
				return nil, err
			}
			ind2 := p.skipSpaces()
			if ind2 == ind && p.s.peek(0) == ':' && spaceOrEnd(p.s.peek(1)) {
				p.s.next()
				// l-block-map-explicit-value is ":" then
				// s-l+block-indented, which includes ns-l-compact-mapping
				// and ns-l-compact-sequence, so a collection MAY begin on
				// this line. The implicit form is different: it uses
				// s-l+block-node, which puts a line break first, which is
				// why "a: b: c" is a refusal and "? a" then ": b: c" is
				// not. PyYAML's emitter writes the second shape whenever a
				// mapping has an empty key, so a corpus of its output is
				// full of them.
				val, err = p.parseBlockNode(blockCtx{ind: ind, inline: true,
					compact: true, seqAtInd: true})
			} else {
				p.reset(m)
				val = p.empty()
			}
			if err != nil {
				return nil, err
			}
		} else {
			p.skipBlanks()
			if p.s.peek(0) != ':' {
				return nil, p.s.errf("a mapping key must be followed by \":\"")
			}
			p.s.next()
			// c-l-block-map-implicit-value is ":" then either
			// s-l+block-node, which begins with s-separate, or an empty
			// node followed by comments. So a value on the same line needs
			// whitespace after the colon: '"a":b' is a refusal in block
			// context even though '{"a":b}' is fine in flow.
			if !isBlank(p.s.peek(0)) && !p.atLineEnd() {
				return nil, p.s.errf("a mapping value on the same line needs a space after the colon")
			}
			val, err = p.parseBlockNode(blockCtx{ind: ind, inline: true, seqAtInd: true})
			if err != nil {
				return nil, err
			}
		}
		n.Map = append(n.Map, Pair{Key: key, Val: val})

		m := p.mark()
		if err := p.skipPrefix(); err != nil {
			return nil, err
		}
		if p.s.eof() || p.atBoundary() {
			p.reset(m)
			return n, nil
		}
		ind2 := p.skipSpaces()
		if ind2 > ind {
			return nil, p.s.errf("this line is indented more than the mapping it belongs to")
		}
		if ind2 < ind {
			p.reset(m)
			return n, nil
		}
	}
}

// parseKeyNode reads an implicit mapping key: properties and then one
// scalar or flow node, all on a single line. Section 7.4.2 limits an
// implicit key to one line, which is why a folded plain scalar cannot be
// one.
func (p *parser) parseKeyNode(ind int) (*Node, error) {
	line, col := p.s.line, p.s.col
	n := &Node{Line: line, Col: col + 1}
	for p.s.peek(0) == '&' || p.s.peek(0) == '!' {
		if p.s.peek(0) == '&' {
			if n.Anchor != "" {
				return nil, p.s.errf("a node can carry only one anchor")
			}
			a, err := p.readAnchor()
			if err != nil {
				return nil, err
			}
			n.Anchor = a
		} else {
			if n.TagRaw != "" {
				return nil, p.s.errf("a node can carry only one tag")
			}
			t, raw, err := p.readTag()
			if err != nil {
				return nil, err
			}
			n.Tag, n.TagRaw = t, raw
		}
		if !isBlank(p.s.peek(0)) {
			return nil, p.s.errf("a property must be followed by a space")
		}
		p.skipBlanks()
	}
	multi, err := p.inlineNode(n, ind)
	if err != nil {
		return nil, err
	}
	if multi {
		return nil, p.s.errAt(line, col, "a mapping key written like this must be on one line")
	}
	return n, nil
}

// inlineNode fills in one node that is not a block collection and not a
// block scalar: an alias, a quoted or plain scalar, or a flow collection.
// It reports whether a scalar spanned more than one line.
func (p *parser) inlineNode(n *Node, ind int) (bool, error) {
	switch ch := p.s.peek(0); {
	case ch == '*':
		a, err := p.readAlias()
		if err != nil {
			return false, err
		}
		if n.Anchor != "" || n.TagRaw != "" {
			return false, p.s.errf("an alias cannot carry an anchor or a tag")
		}
		n.Kind, n.Alias = KAlias, a
		return false, nil
	case ch == '\'':
		n.Kind, n.Style = KScalar, StSingle
		v, multi, err := p.scanSingle(ind)
		n.Val = v
		return multi, err
	case ch == '"':
		n.Kind, n.Style = KScalar, StDouble
		v, multi, err := p.scanDouble(ind)
		n.Val = v
		return multi, err
	case ch == '[' || ch == '{':
		err := p.parseFlow(n, ind)
		return false, err
	case ch == '|' || ch == '>':
		return false, p.s.errf("a block scalar cannot be used here")
	}
	n.Kind, n.Style = KScalar, StPlain
	v, multi, err := p.scanPlain(ind, false)
	n.Val = v
	return multi, err
}
