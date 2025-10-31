package main

// Kind is what a node is, before any tag resolution happens.
type Kind uint8

const (
	KScalar Kind = iota
	KSeq
	KMap
	KAlias
)

// Style is how a scalar was written. It decides whether the scalar is
// resolvable: 1.2.2 section 10.2.1.1 says a plain scalar's tag is resolved
// by looking at its content, while a quoted or block scalar is always a
// string unless it carries a tag.
type Style uint8

const (
	StPlain Style = iota
	StSingle
	StDouble
	StLiteral
	StFolded
)

func (s Style) char() string {
	switch s {
	case StSingle:
		return "'"
	case StDouble:
		return "\""
	case StLiteral:
		return "|"
	case StFolded:
		return ">"
	}
	return ":"
}

// Pair is one mapping entry. Both halves are nodes: a key can be a
// collection, which is what the explicit key indicator is for.
type Pair struct {
	Key, Val *Node
}

type Node struct {
	Kind   Kind
	Style  Style
	Flow   bool   // a collection written in flow form
	Anchor string // without the '&'
	Tag    string // resolved to a URI, or "!" for the non-specific tag, or ""
	TagRaw string // as written, for the report
	Val    string // scalar content, after escape processing and folding
	Seq    []*Node
	Map    []Pair
	Alias  string // without the '*'
	Line   int
	Col    int // 1 based, for reports
}

// Document is one document of a stream.
type Document struct {
	Root      *Node
	Version   string // the %YAML directive's value, "" when absent
	Explicit  bool   // began with ---
	EndMarked bool   // ended with an explicit ...
	Line      int
}
