package main

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// defaultMaxDepth bounds how deeply nested a document may be. A parser is a
// denial-of-service surface and ten thousand open brackets should be a
// refusal rather than a stack overflow. Configuration files are not deep;
// this is well above anything written by hand.
const defaultMaxDepth = 400

type parser struct {
	s        *src
	handles  map[string]string
	version  string
	depth    int
	maxDepth int
}

// blockCtx is what a block node needs to know about where it sits.
//
//	ind      the parent's indentation; content must begin at a column > ind
//	seqAtInd a block sequence indicator at column == ind is allowed, which
//	         is the special case that makes "a:\n- 1" valid
//	inline   the cursor may already be on content on the current line
//	compact  a block collection may begin on the current line
//	tabbed   a tab was used as separation before the content, which rules
//	         out a block collection: s-indent is spaces only
type blockCtx struct {
	ind      int
	seqAtInd bool
	inline   bool
	compact  bool
	tabbed   bool
	// afterProps: an anchor or tag was read on this same line, which rules
	// out a block collection
	afterProps bool
}

func isBlank(c byte) bool { return c == ' ' || c == '\t' }

// spaceOrEnd reports whether c can follow a block indicator. The indicators
// "-", "?" and ":" are only indicators when followed by whitespace or the
// end of a line, which is why "-foo" and "a:b" are plain scalars.
func spaceOrEnd(c byte) bool { return isBlank(c) || c == '\n' || c == 0 }

func isFlowIndicator(c byte) bool {
	return c == ',' || c == '[' || c == ']' || c == '{' || c == '}'
}

func (p *parser) mark() src   { return *p.s }
func (p *parser) reset(m src) { *p.s = m }
func (p *parser) skipBlanks() {
	for isBlank(p.s.peek(0)) {
		p.s.next()
	}
}
func (p *parser) skipSpaces() int {
	n := 0
	for p.s.peek(0) == ' ' {
		p.s.next()
		n++
	}
	return n
}

// atMarker reports whether the cursor sits at column zero on a "---" or
// "..." line. Those two end a document wherever they appear, which is why
// every scan that crosses a line break has to ask.
func (p *parser) atMarker(m string) bool {
	if p.s.col != 0 {
		return false
	}
	for i := 0; i < 3; i++ {
		if p.s.peek(i) != m[i] {
			return false
		}
	}
	return spaceOrEnd(p.s.peek(3))
}

func (p *parser) atAnyMarker() bool { return p.atMarker("---") || p.atMarker("...") }

// atBoundary is atAnyMarker plus a directive. A "%" at column zero can
// only be a directive, since ns-plain-first excludes it, so it ends
// whatever collection is open: a %TAG between two documents was being read
// as the next key of the mapping above it.
func (p *parser) atBoundary() bool {
	return p.atAnyMarker() || (p.s.col == 0 && p.s.peek(0) == '%')
}

// skipLine consumes the rest of the line including its break.
func (p *parser) skipLine() {
	for !p.s.eof() && p.s.peek(0) != '\n' {
		p.s.next()
	}
	if p.s.peek(0) == '\n' {
		p.s.next()
	}
}

// endLine consumes blanks, an optional comment and the line break. Anything
// else on the line is a refusal: a node followed by another token on the
// same line is not something to guess at.
func (p *parser) endLine() error {
	p.skipBlanks()
	if p.s.peek(0) == '#' && p.s.afterBlank() {
		p.skipLine()
		return nil
	}
	if p.s.eof() {
		return nil
	}
	if p.s.peek(0) == '\n' {
		p.s.next()
		return nil
	}
	return p.s.errf("unexpected %q after a node", p.s.rest())
}

// atLineEnd reports whether only a comment or the break is left. A "#"
// counts only where a comment may begin, which is after a break or after
// at least one blank: c-comment is preceded by s-separate-in-line. So
// key: "value"# c is not a comment and not a valid document.
func (p *parser) atLineEnd() bool {
	return p.s.eof() || p.s.peek(0) == '\n' ||
		(p.s.peek(0) == '#' && p.s.afterBlank())
}

// skipPrefix consumes blank lines and comment lines, leaving the cursor at
// column zero of the next line that holds something, or at end of input.
func (p *parser) skipPrefix() error {
	for !p.s.eof() {
		m := p.mark()
		p.skipSpaces()
		if p.s.peek(0) == '\t' {
			k := p.mark()
			p.skipBlanks()
			if p.s.peek(0) == '\n' || p.s.eof() {
				p.skipLine()
				continue
			}
			// At the document level the required indentation is zero,
			// so a tab here is s-separate-in-line and not indentation:
			// s-l+flow-in-block(-1) is s-separate(0,flow-out) and then
			// a flow node, which is why a tab may precede "[1]" or a
			// scalar at column zero. A block collection needs s-indent,
			// which is spaces, and the guard for that is in blockNode
			// below; it is what refuses a tab before "- a".
			p.reset(k)
			p.skipBlanks()
		}
		switch {
		case p.s.peek(0) == '\n':
			p.s.next()
		case p.s.peek(0) == '#':
			p.skipLine()
		case p.s.eof():
			return nil
		default:
			p.reset(m)
			return nil
		}
	}
	return nil
}

// toContent positions the cursor on the content of the node being parsed.
// It returns false when there is none, which means the node is empty: that
// is e-node in the grammar, and it is extremely common ("a:" with nothing
// after it).
func (p *parser) toContent(c *blockCtx) (bool, error) {
	if c.inline {
		if p.s.peek(0) == '\t' {
			c.tabbed = true
		}
		p.skipBlanks()
		if !p.atLineEnd() {
			return true, nil
		}
		if err := p.endLine(); err != nil {
			return false, err
		}
		c.inline = false
		c.compact = true
		c.tabbed = false
	}
	for {
		if p.s.eof() {
			return false, nil
		}
		m := p.mark()
		ind := p.skipSpaces()
		if p.s.peek(0) == '\t' {
			k := p.mark()
			p.skipBlanks()
			if p.s.peek(0) == '\n' || p.s.eof() {
				p.skipLine()
				continue
			}
			if ind <= c.ind {
				p.reset(k)
				return false, p.s.errf("tab cannot be used for indentation")
			}
			p.reset(k)
			p.skipBlanks()
			c.tabbed = true
		}
		switch {
		case p.s.peek(0) == '\n':
			p.s.next()
			continue
		case p.s.peek(0) == '#':
			p.skipLine()
			continue
		case p.s.eof():
			return false, nil
		}
		if ind == 0 && p.atAnyMarker() {
			p.reset(m)
			return false, nil
		}
		okHere := ind > c.ind ||
			(c.seqAtInd && ind == c.ind && p.s.peek(0) == '-' && spaceOrEnd(p.s.peek(1)))
		if !okHere {
			p.reset(m)
			return false, nil
		}
		c.inline = true
		c.compact = true
		return true, nil
	}
}

func (p *parser) empty() *Node {
	return &Node{Kind: KScalar, Style: StPlain, Line: p.s.line, Col: p.s.col + 1}
}

// parseStream reads the whole input. The productions are l-yaml-stream and
// its children in section 9 of YAML 1.2.2.
func parseStream(name string, in []byte, maxDepth int) ([]*Document, error) {
	s, err := newSrc(name, in)
	if err != nil {
		return nil, err
	}
	if maxDepth <= 0 {
		maxDepth = defaultMaxDepth
	}
	p := &parser{s: s, maxDepth: maxDepth}
	var docs []*Document
	needMarker := false
	for {
		if err := p.skipPrefix(); err != nil {
			return nil, err
		}
		if p.s.eof() {
			break
		}
		if p.atMarker("...") {
			p.s.advance(3)
			if err := p.endLine(); err != nil {
				return nil, err
			}
			if len(docs) > 0 {
				docs[len(docs)-1].EndMarked = true
			}
			needMarker = false
			continue
		}
		d := &Document{Line: p.s.line}
		p.resetDirectives()
		if p.s.peek(0) == '%' {
			// l-directive-document is l-directive+ then an explicit
			// document, and directives may only follow a document
			// suffix. A "---" ends the previous document but is not a
			// suffix, so a directive after an unterminated document is
			// a refusal: the suite's 9HCY, EB22 and RHX7, all of which
			// yamlc read.
			if needMarker {
				return nil, p.s.errf("a directive needs ... to close the document before it")
			}
			if err := p.directives(); err != nil {
				return nil, err
			}
			d.Version = p.version
			if !p.atMarker("---") {
				return nil, p.s.errf("a directive must be followed by ---")
			}
		}
		if p.atMarker("---") {
			p.s.advance(3)
			d.Explicit = true
		} else if needMarker {
			return nil, p.s.errf("expected --- or ... before this: the stream is already in a document")
		}
		root, err := p.parseBlockNode(blockCtx{ind: -1, inline: d.Explicit, compact: true})
		if err != nil {
			return nil, err
		}
		d.Root = root
		docs = append(docs, d)
		needMarker = true
	}
	return docs, nil
}

func (p *parser) resetDirectives() {
	p.handles = map[string]string{"!": "!", "!!": "tag:yaml.org,2002:"}
	p.version = ""
}

// directives reads the %YAML and %TAG lines before a document.
//
// Section 6.8.1 on %YAML: "a version 1.2 processor must refuse a document
// with a major version other than 1" and should warn, not refuse, on a
// later minor version.
func (p *parser) directives() error {
	seenYAML := false
	for p.s.col == 0 && p.s.peek(0) == '%' {
		line, col := p.s.line, p.s.col
		p.s.next()
		word := p.word()
		switch word {
		case "YAML":
			if seenYAML {
				return p.s.errAt(line, col, "two %%YAML directives in one document")
			}
			seenYAML = true
			p.skipBlanks()
			ver := p.word()
			major, minor, ok := splitVersion(ver)
			if !ok {
				return p.s.errAt(line, col, "%%YAML needs a version like 1.2, got %q", ver)
			}
			if major != 1 {
				return p.s.errAt(line, col, "unsupported YAML version %s: this is a 1.2 processor", ver)
			}
			_ = minor
			p.version = ver
		case "TAG":
			p.skipBlanks()
			h := p.word()
			if !validHandle(h) {
				return p.s.errAt(line, col, "%%TAG needs a handle like ! or !!  or !name!, got %q", h)
			}
			p.skipBlanks()
			prefix := p.word()
			if prefix == "" {
				return p.s.errAt(line, col, "%%TAG %s has no prefix", h)
			}
			if _, dup := p.handles[h]; dup && (h != "!" && h != "!!") {
				return p.s.errAt(line, col, "handle %s is defined twice", h)
			}
			p.handles[h] = unescapeURI(prefix)
		case "":
			return p.s.errAt(line, col, "a %% at the start of a line must name a directive")
		default:
			// Section 6.8: an unknown directive must be ignored with a
			// warning, not refused. Reserved directives are how the format
			// is meant to grow, so the whole line goes unread.
			for !p.s.eof() && p.s.peek(0) != '\n' {
				p.s.next()
			}
		}
		if err := p.endLine(); err != nil {
			return err
		}
		if err := p.skipPrefix(); err != nil {
			return err
		}
	}
	return nil
}

// word reads a run of non-space characters, which is what a directive's
// fields are.
func (p *parser) word() string {
	var b strings.Builder
	for !p.atLineEnd() && !isBlank(p.s.peek(0)) {
		b.WriteByte(p.s.peek(0))
		p.s.next()
	}
	return b.String()
}

func splitVersion(v string) (int, int, bool) {
	i := strings.IndexByte(v, '.')
	if i <= 0 || i == len(v)-1 {
		return 0, 0, false
	}
	major, ok1 := atoiAll(v[:i])
	minor, ok2 := atoiAll(v[i+1:])
	return major, minor, ok1 && ok2
}

func atoiAll(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
		n = n*10 + int(s[i]-'0')
	}
	return n, true
}

func validHandle(h string) bool {
	if h == "!" || h == "!!" {
		return true
	}
	if len(h) < 3 || h[0] != '!' || h[len(h)-1] != '!' {
		return false
	}
	for i := 1; i < len(h)-1; i++ {
		if !isWordChar(h[i]) {
			return false
		}
	}
	return true
}

func isWordChar(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-'
}

func hexVal(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10, true
	}
	return 0, false
}

// unescapeURI turns %XX back into bytes. Section 6.9.1: a tag shorthand's
// suffix may carry percent escapes, and a tag that is compared without
// decoding them is a different tag.
func unescapeURI(s string) string {
	if !strings.ContainsRune(s, '%') {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '%' && i+2 < len(s) {
			h1, ok1 := hexVal(s[i+1])
			h2, ok2 := hexVal(s[i+2])
			if ok1 && ok2 {
				b.WriteByte(byte(h1<<4 | h2))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// parseBlockNode is the dispatch for block context. Properties come first
// and may sit on their own line, then exactly one of the node forms.
func (p *parser) parseBlockNode(c blockCtx) (*Node, error) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > p.maxDepth {
		return nil, p.s.errf("nesting deeper than %d, refused", p.maxDepth)
	}
	found, err := p.toContent(&c)
	if err != nil {
		return nil, err
	}
	if !found {
		return p.empty(), nil
	}
	line, col := p.s.line, p.s.col
	// Two groups, because a block mapping and its first key may each
	// carry an anchor and a tag, and the two sets arrive here one after
	// the other with nothing between them but a line break:
	//
	//	top: &node
	//	  &key k: v
	//
	// pr is the group on the most recent property line and outer is
	// everything before it. A second anchor on the SAME line is still a
	// refusal, and so is a second group when the node turns out not to
	// be a mapping, which is how "&a" newline "&b" newline "x" stays
	// refused.
	var pr, outer props
	// Properties are grouped by the line they sit on, and there can be
	// two groups at most: one for the node and one for the first key of
	// a block mapping it may turn out to be.
	newGroup := func(pline int) error {
		if pr.empty() || pr.line() == pline {
			return nil
		}
		if !outer.empty() {
			return p.s.errf("a node and its first key carry one set of properties each, and this is a third")
		}
		outer, pr = pr, props{}
		return nil
	}
	for p.s.peek(0) == '&' || p.s.peek(0) == '!' {
		pline, pcol := p.s.line, p.s.col
		if err := newGroup(pline); err != nil {
			return nil, err
		}
		if p.s.peek(0) == '&' {
			if pr.anchor != "" {
				return nil, p.s.errf("a node can carry only one anchor")
			}
			if pr.anchor, err = p.readAnchor(); err != nil {
				return nil, err
			}
			pr.anchorLine, pr.anchorCol = pline, pcol
		} else {
			if pr.tagRaw != "" {
				return nil, p.s.errf("a node can carry only one tag")
			}
			if pr.tag, pr.tagRaw, err = p.readTag(); err != nil {
				return nil, err
			}
			pr.tagLine, pr.tagCol = pline, pcol
		}
		if !isBlank(p.s.peek(0)) && !p.atLineEnd() {
			return nil, p.s.errf("a property must be followed by a space")
		}
		found, err = p.toContent(&c)
		if err != nil {
			return nil, err
		}
		if !found {
			all, err := outer.merge(pr)
			if err != nil {
				return nil, p.s.errAt(line, col+1, "%s", err.Error())
			}
			n := p.empty()
			n.Anchor, n.Tag, n.TagRaw = all.anchor, all.tag, all.tagRaw
			n.Line, n.Col = line, col+1
			return n, nil
		}
	}
	// Which properties belong to this node and which to the mapping key
	// that may be about to appear is decided by the line they are on. See
	// the note on props.
	c.afterProps = pr.onLine(p.s.line)
	n, err := p.blockContent(c, outer, pr, line, col)
	if err != nil {
		return nil, err
	}
	return n, nil
}

// props is the anchor and tag read before a node, with the position of
// each. The positions matter because of one document shape:
//
//	--- !e!t
//	&a k: v
//	? [1,2]
//	: {x: y}
//
// The tag is on the --- line and belongs to the mapping. The anchor is on
// the first key's line and belongs to the key, which also means the mapping
// begins at the anchor's column and so takes the explicit entry below. The
// first version of this read both properties as the root node's, put the
// mapping at the tag's column, and refused the last two lines. PyYAML
// splits them the same way, because its scanner records a possible simple
// key at the anchor token.
var (
	errTwoAnchors = errors.New("a node can carry only one anchor")
	errTwoTags    = errors.New("a node can carry only one tag")
)

type props struct {
	anchor                string
	tag, tagRaw           string
	anchorLine, anchorCol int
	tagLine, tagCol       int
}

func (pr props) onLine(line int) bool {
	return (pr.anchor != "" && pr.anchorLine == line) ||
		(pr.tagRaw != "" && pr.tagLine == line)
}

func (pr props) empty() bool { return pr.anchor == "" && pr.tagRaw == "" }

// line is the line the group sits on. Every property in one group is on
// the same line, which is what makes a group a group.
func (pr props) line() int {
	if pr.anchor != "" {
		return pr.anchorLine
	}
	return pr.tagLine
}

// merge puts two groups on one node, which is legal only when they do
// not both carry the same kind of property.
func (pr props) merge(o props) (props, error) {
	if pr.empty() {
		return o, nil
	}
	if o.empty() {
		return pr, nil
	}
	if pr.anchor != "" && o.anchor != "" {
		return props{}, errTwoAnchors
	}
	if pr.tagRaw != "" && o.tagRaw != "" {
		return props{}, errTwoTags
	}
	if o.anchor != "" {
		pr.anchor, pr.anchorLine, pr.anchorCol = o.anchor, o.anchorLine, o.anchorCol
	}
	if o.tagRaw != "" {
		pr.tag, pr.tagRaw, pr.tagLine, pr.tagCol = o.tag, o.tagRaw, o.tagLine, o.tagCol
	}
	return pr, nil
}

// split divides the properties between a mapping and its first key by the
// line each group sits on, and gives the column the mapping starts at.
// The group on the key's own line is the key's, because an implicit key
// is an ns-flow-yaml-node and may carry properties; anything from an
// earlier line went through s-l+block-collection and is the mapping's.
func (pr props) split(outer props, line, here int) (keyP, mapP props, ind int) {
	ind = here
	if !pr.empty() && pr.line() == line {
		keyP = pr
		if pr.anchor != "" && pr.anchorCol < ind {
			ind = pr.anchorCol
		}
		if pr.tagRaw != "" && pr.tagCol < ind {
			ind = pr.tagCol
		}
		return keyP, outer, ind
	}
	// Both groups are the mapping's, which merge already refused if they
	// collide; blockContent checks that before it gets here.
	mapP, _ = outer.merge(pr)
	return keyP, mapP, ind
}

func (p *parser) blockContent(c blockCtx, outer, pr props, line, col int) (*Node, error) {
	ch := p.s.peek(0)
	here := p.s.col
	startPos := p.s.pos
	all, _ := outer.merge(pr)
	n := &Node{Kind: KScalar, Anchor: all.anchor, Tag: all.tag, TagRaw: all.tagRaw,
		Line: line, Col: col + 1}
	// outer is only ever non-empty because a property collided with one
	// already read, and the only document shape where that is legal is a
	// block mapping whose first key carries the second group. Every
	// other branch below has to refuse it, or "&a" newline "&b" newline
	// "x" would quietly anchor x twice.
	badOuter := func() error {
		if _, err := outer.merge(pr); err != nil {
			return p.s.errAt(line, col+1, "%s", err.Error())
		}
		return nil
	}
	// A block collection after inline properties is not a production:
	// s-l+block-collection puts s-l-comments between the properties and the
	// collection, so the indicator has to be on a later line.
	blockOK := func(what string) error {
		if c.tabbed {
			return p.s.errf("a tab cannot stand before a %s indicator: indentation is spaces only", what)
		}
		if !c.compact {
			return p.s.errf("a %s cannot begin on the same line as the value it belongs to", what)
		}
		return nil
	}
	// An indicator form on the same line as its own properties is not a
	// production: s-l+block-collection puts s-l-comments between the
	// properties and the collection. An implicit key is different, because
	// it is a ns-flow-yaml-node and may carry properties, so "&a b: c" is
	// fine while "&a - b" is not.
	indicatorOK := func(what string) error {
		if err := badOuter(); err != nil {
			return err
		}
		if err := blockOK(what); err != nil {
			return err
		}
		if c.afterProps {
			return p.s.errf("a %s cannot begin on the same line as the anchor or tag it carries", what)
		}
		return nil
	}
	switch {
	case ch == '-' && spaceOrEnd(p.s.peek(1)):
		if err := indicatorOK("block sequence"); err != nil {
			return nil, err
		}
		return p.parseBlockSeq(here, n)
	case ch == '?' && spaceOrEnd(p.s.peek(1)):
		if err := indicatorOK("block mapping"); err != nil {
			return nil, err
		}
		return p.parseBlockMap(here, nil, n)
	case ch == ':' && spaceOrEnd(p.s.peek(1)):
		// The key is e-node, and an anchor or tag on the colon's own
		// line belongs to THAT empty key rather than to the mapping:
		// "!!null : a" tags the null key. So this is blockOK and not
		// indicatorOK, and the mapping begins at the property's column.
		// Suite cases FH7J and PW8X.
		if err := blockOK("block mapping"); err != nil {
			return nil, err
		}
		keyP, mapP, mapInd := pr.split(outer, p.s.line, here)
		k := p.empty()
		k.Anchor, k.Tag, k.TagRaw = keyP.anchor, keyP.tag, keyP.tagRaw
		k.Line, k.Col = line, col+1
		m := &Node{Kind: KMap, Anchor: mapP.anchor, Tag: mapP.tag, TagRaw: mapP.tagRaw,
			Line: line, Col: mapInd + 1}
		return p.parseBlockMap(mapInd, k, m)
	case ch == '|' || ch == '>':
		if err := badOuter(); err != nil {
			return nil, err
		}
		if ch == '|' {
			n.Style = StLiteral
		} else {
			n.Style = StFolded
		}
		v, chomp, given, err := p.scanBlockScalar(c.ind, n.Style)
		if err != nil {
			return nil, err
		}
		n.Val, n.Chomp, n.IndentGiven = v, chomp, given
		return n, nil
	}
	multi, err := p.inlineNode(n, c.ind)
	if err != nil {
		return nil, err
	}
	n.MultiLine = multi
	// A ":" here turns what was just read into the first key of a block
	// mapping. After a quoted scalar or a flow collection it needs no
	// space after it, which is c-s-implicit-json-key in section 7.4.2 and
	// the reason {"a":1} parses.
	p.skipBlanks()
	jsonLike := n.Style == StSingle || n.Style == StDouble || n.Kind == KSeq || n.Kind == KMap
	if p.s.peek(0) == ':' && (jsonLike || spaceOrEnd(p.s.peek(1))) {
		if multi {
			return nil, p.s.errAt(line, col, "a mapping key written like this must be on one line")
		}
		if err := p.checkKeyLength(startPos, line, col); err != nil {
			return nil, err
		}
		if err := blockOK("block mapping"); err != nil {
			return nil, err
		}
		// An anchor or tag on the same line as the key belongs to the key,
		// because an implicit key is an ns-flow-yaml-node and may carry
		// properties. One on an earlier line went through
		// s-l+block-collection and belongs to the mapping: "a: &x" then
		// "  b: 1" anchors the inner mapping, not the key "b".
		keyP, mapP, mapInd := pr.split(outer, p.s.line, here)
		n.Anchor, n.Tag, n.TagRaw = keyP.anchor, keyP.tag, keyP.tagRaw
		m := &Node{Kind: KMap, Anchor: mapP.anchor, Tag: mapP.tag, TagRaw: mapP.tagRaw,
			Line: line, Col: mapInd + 1}
		return p.parseBlockMap(mapInd, n, m)
	}
	if err := badOuter(); err != nil {
		return nil, err
	}
	if n.Kind == KAlias && (pr.anchor != "" || pr.tagRaw != "") {
		return nil, p.s.errAt(line, col, "an alias cannot carry an anchor or a tag")
	}
	if err := p.endLine(); err != nil {
		return nil, err
	}
	return n, nil
}

// maxSimpleKey is the limit from the note in section 7.4.2: "implicit keys
// are restricted to a single line and must not span more than 1024 Unicode
// characters". It is a real restriction and not advice, and PyYAML
// enforces it too, with "simple key is too long".
const maxSimpleKey = 1024

func (p *parser) checkKeyLength(startPos, line, col int) error {
	if utf8.RuneCount(p.s.b[startPos:p.s.pos]) > maxSimpleKey {
		return p.s.errAt(line, col,
			"an implicit mapping key may not span more than %d characters", maxSimpleKey)
	}
	return nil
}

// plainFirst reports whether a plain scalar may begin with this character.
// Section 7.3.3: ns-plain-first excludes every indicator, except that "-",
// "?" and ":" are allowed when what follows them is not whitespace, which
// is why "-foo" and ":x" are scalars and "- foo" is a sequence entry.
func plainFirst(c, next byte, flow bool) bool {
	switch c {
	case 0, '\n', ' ', '\t':
		return false
	case ',', '[', ']', '{', '}':
		return !flow
	case '#', '&', '*', '!', '|', '>', '\'', '"', '%', '@', '`':
		return false
	case '-', '?', ':':
		return !spaceOrEnd(next) && !(flow && isFlowIndicator(next))
	}
	return true
}

// scanPlain reads a plain scalar, following it onto later lines while they
// are indented more than ind. It returns the folded value and whether the
// scalar spanned more than one line, which the caller needs because an
// implicit mapping key has to be on one line.
//
// Section 7.3.3 and section 6.5 on folding: a single line break between
// two non-empty lines becomes a space, and n+1 consecutive breaks become n
// line feeds.
func (p *parser) scanPlain(ind int, flow bool) (string, bool, error) {
	if !plainFirst(p.s.peek(0), p.s.peek(1), flow) {
		return "", false, p.s.errf("%q cannot begin a plain scalar", string(p.s.peek(0)))
	}
	var out []byte
	multi := false
	readLine := func() {
		var line []byte
		trail := 0 // trailing blanks, dropped unless more content follows
		for {
			c := p.s.peek(0)
			if c == 0 || c == '\n' {
				break
			}
			if c == ':' {
				nx := p.s.peek(1)
				if spaceOrEnd(nx) || (flow && isFlowIndicator(nx)) {
					break
				}
			}
			if c == '#' && len(line) > 0 && isBlank(line[len(line)-1]) {
				break
			}
			if flow && isFlowIndicator(c) {
				break
			}
			if isBlank(c) {
				trail++
			} else {
				trail = 0
			}
			st := p.s.pos
			p.s.next()
			line = append(line, p.s.b[st:p.s.pos]...)
		}
		out = append(out, line[:len(line)-trail]...)
	}
	readLine()
	for {
		if p.s.peek(0) != '\n' {
			break
		}
		m := p.mark()
		breaks, ind2, ok, err := p.foldBreaks(ind)
		if err != nil {
			return "", false, err
		}
		if !ok || ind2 <= ind || (ind2 == 0 && p.atAnyMarker()) || !p.plainContinues(flow) {
			p.reset(m)
			break
		}
		multi = true
		if breaks == 1 {
			out = append(out, ' ')
		} else {
			out = append(out, strings.Repeat("\n", breaks-1)...)
		}
		readLine()
	}
	return string(out), multi, nil
}

// plainContinues reports whether the character at the cursor can begin a
// continuation line of a plain scalar. s-ns-plain-next-line requires an
// ns-plain-char there, so a flow indicator ends the scalar in flow context
// and a "#" or a ":" followed by space ends it in either. Without this a
// flow sequence written over several lines gains a trailing space: the "]"
// on its own line looked like a continuation and folded the break into one.
func (p *parser) plainContinues(flow bool) bool {
	c := p.s.peek(0)
	if c == '#' {
		return false
	}
	if flow && isFlowIndicator(c) {
		return false
	}
	if c == ':' && (spaceOrEnd(p.s.peek(1)) || (flow && isFlowIndicator(p.s.peek(1)))) {
		return false
	}
	return true
}

// foldBreaks consumes one or more line breaks and the blank lines between
// them, stopping at the first line that holds content. It returns how many
// breaks it crossed and the indentation of that line. ok is false at end of
// input.
func (p *parser) foldBreaks(ind int) (breaks int, ind2 int, ok bool, err error) {
	for {
		if p.s.peek(0) != '\n' {
			return breaks, ind2, false, nil
		}
		p.s.next()
		breaks++
		ind2 = p.skipSpaces()
		c := p.s.peek(0)
		if c == '\t' {
			k := p.mark()
			p.skipBlanks()
			if p.s.peek(0) == '\n' {
				continue
			}
			if p.s.eof() {
				return breaks, ind2, false, nil
			}
			if ind2 <= ind {
				p.reset(k)
				return 0, 0, false, p.s.errf("tab cannot be used for indentation")
			}
			p.reset(k)
			p.skipBlanks()
			return breaks, ind2, true, nil
		}
		if c == '\n' {
			continue
		}
		if c == 0 {
			return breaks, ind2, false, nil
		}
		return breaks, ind2, true, nil
	}
}

// readAnchor reads "&name". Section 7.1: an anchor name is one or more
// ns-anchor-char, which is any non-space character that is not a flow
// indicator.
func (p *parser) readAnchor() (string, error) {
	p.s.next()
	name := p.anchorName()
	if name == "" {
		return "", p.s.errf("an anchor needs a name")
	}
	return name, nil
}

func (p *parser) readAlias() (string, error) {
	p.s.next()
	name := p.anchorName()
	if name == "" {
		return "", p.s.errf("an alias needs a name")
	}
	return name, nil
}

func (p *parser) anchorName() string {
	var b strings.Builder
	for {
		c := p.s.peek(0)
		if c == 0 || c == '\n' || isBlank(c) || isFlowIndicator(c) {
			break
		}
		st := p.s.pos
		p.s.next()
		b.Write(p.s.b[st:p.s.pos])
	}
	return b.String()
}

// readTag reads one of the four forms of section 6.9.1 and 7.1: the
// non-specific tag "!", a verbatim tag "!<uri>", a shorthand on a named or
// secondary handle, and a shorthand on the primary handle. It returns the
// resolved tag and the text as written, because the report wants both.
func (p *parser) readTag() (string, string, error) {
	start := p.s.pos
	tline, tcol := p.s.line, p.s.col
	p.s.next()
	if p.s.peek(0) == '<' {
		p.s.next()
		var b strings.Builder
		for {
			c := p.s.peek(0)
			if c == '>' {
				break
			}
			if c == 0 || c == '\n' || isBlank(c) {
				return "", "", p.s.errf("a verbatim tag needs a closing >")
			}
			st := p.s.pos
			p.s.next()
			b.Write(p.s.b[st:p.s.pos])
		}
		p.s.next()
		if b.Len() == 0 {
			return "", "", p.s.errf("a verbatim tag cannot be empty")
		}
		return unescapeURI(b.String()), string(p.s.b[start:p.s.pos]), nil
	}
	// Read the rest, which is either handle!suffix or just a suffix. The
	// characters allowed in a suffix exclude the flow indicators, so a tag
	// inside a flow collection ends where the collection's syntax resumes.
	var raw strings.Builder
	for {
		c := p.s.peek(0)
		if c == 0 || c == '\n' || isBlank(c) || isFlowIndicator(c) {
			break
		}
		st := p.s.pos
		p.s.next()
		raw.Write(p.s.b[st:p.s.pos])
	}
	body := raw.String()
	written := "!" + body
	if body == "" {
		// The non-specific tag. It means "this node is not a string by
		// resolution, it is whatever the kind says", and in 1.2 it stops
		// plain scalar resolution.
		return "!", "!", nil
	}
	handle, suffix := "!", body
	if i := strings.IndexByte(body, '!'); i >= 0 {
		if i == 0 {
			handle, suffix = "!!", body[1:]
		} else {
			handle, suffix = "!"+body[:i]+"!", body[i+1:]
		}
	}
	prefix, known := p.handles[handle]
	if !known {
		return "", "", p.s.errAt(tline, tcol, "tag handle %s was never declared with %%TAG", handle)
	}
	if suffix == "" {
		return "", "", p.s.errAt(tline, tcol, "tag %s has no suffix", written)
	}
	return prefix + unescapeURI(suffix), written, nil
}

// parseFlow is written in flow.go.
