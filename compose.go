package main

import "fmt"

// Composition: turning the parse tree into values, which means resolving
// aliases, applying the schema, deciding what a duplicate key is, and
// honouring the merge key.
//
// Two of the refusals here are about resource use rather than about syntax.
// An alias that encloses itself is refused rather than followed, because
// following one does not return. And the total number of nodes an
// expansion may produce is capped, because nine nested nine-element
// aliases is a billion nodes out of forty lines of input.

const (
	dupError = 0
	dupFirst = 1
	dupLast  = 2
)

const defaultBudget = 1000000

type composer struct {
	name string
	// schema is the rules in force for the document being composed, and
	// base is what to go back to for the next one. A %YAML directive's
	// scope is the document it precedes (section 6.8.1), so a stream
	// whose first document asks for 1.1 and whose second does not must
	// read the second under 1.2. Keeping one field here read both
	// documents as 1.1.
	schema  int
	base    int
	dup     int
	merge   bool
	budget  int
	used    int
	anchors map[string]*Node
	busy    map[*Node]bool
	// schemaFromDoc: a document carrying %YAML 1.1 asks for the 1.1
	// resolution rules, and honouring that is the default. --schema on the
	// command line turns it off, because a comparison needs to choose.
	schemaFromDoc bool
}

func newComposer(name string, schema, dup int, merge bool, budget int) *composer {
	if budget <= 0 {
		budget = defaultBudget
	}
	return &composer{name: name, schema: schema, base: schema, dup: dup, merge: merge,
		budget: budget, anchors: map[string]*Node{}, busy: map[*Node]bool{}}
}

func (c *composer) errAt(n *Node, format string, a ...any) error {
	return &Refusal{c.name, n.Line, n.Col, fmt.Sprintf(format, a...)}
}

// composeDocument resolves one document. The anchor table is per document:
// section 3.2.2.2 says an anchor's scope is the document it is in.
func (c *composer) composeDocument(d *Document) (*Value, error) {
	c.anchors = map[string]*Node{}
	c.busy = map[*Node]bool{}
	c.used = 0
	c.schema = c.base
	if d.Version == "1.1" && c.schemaFromDoc {
		c.schema = 11
	}
	return c.node(d.Root)
}

func (c *composer) node(n *Node) (*Value, error) {
	c.used++
	if c.used > c.budget {
		return nil, c.errAt(n, "this document expands to more than %d nodes, refused", c.budget)
	}
	if n.Anchor != "" {
		c.anchors[n.Anchor] = n
	}
	switch n.Kind {
	case KAlias:
		t, ok := c.anchors[n.Alias]
		if !ok {
			return nil, c.errAt(n, "alias *%s has no anchor before it", n.Alias)
		}
		if c.busy[t] {
			return nil, c.errAt(n, "alias *%s names a node that contains this alias: refused rather than followed", n.Alias)
		}
		return c.node(t)
	case KScalar:
		return c.scalarValue(n)
	case KSeq:
		c.busy[n] = true
		defer delete(c.busy, n)
		v := &Value{Kind: VSeq, Tag: collectionTag(n, tagSeq), Line: n.Line, Col: n.Col}
		for _, it := range n.Seq {
			x, err := c.node(it)
			if err != nil {
				return nil, err
			}
			v.Seq = append(v.Seq, x)
		}
		return v, nil
	case KMap:
		c.busy[n] = true
		defer delete(c.busy, n)
		return c.mapValue(n)
	}
	return nil, c.errAt(n, "unknown node kind")
}

func collectionTag(n *Node, dflt string) string {
	switch n.Tag {
	case "", "?", "!":
		return dflt
	}
	return n.Tag
}

func (c *composer) mapValue(n *Node) (*Value, error) {
	v := &Value{Kind: VMap, Tag: collectionTag(n, tagMap), Line: n.Line, Col: n.Col}
	var merged []KV
	var explicit []KV
	seen := map[string]*Value{}
	for _, pr := range n.Map {
		if c.merge && isMergeKey(pr.Key) {
			m, err := c.node(pr.Val)
			if err != nil {
				return nil, err
			}
			more, err := c.flattenMerge(pr.Val, m)
			if err != nil {
				return nil, err
			}
			merged = append(merged, more...)
			continue
		}
		k, err := c.node(pr.Key)
		if err != nil {
			return nil, err
		}
		key := canon(k)
		if prev, dup := seen[key]; dup {
			switch c.dup {
			case dupError:
				return nil, &Refusal{c.name, k.Line, k.Col,
					fmt.Sprintf("this key repeats the one at line %d column %d; YAML 1.2 section 3.2.1.1 requires mapping keys to be unique",
						prev.Line, prev.Col)}
			}
		}
		seen[key] = k
		val, err := c.node(pr.Val)
		if err != nil {
			return nil, err
		}
		explicit = append(explicit, KV{Key: k, Val: val})
	}
	// The merged entries go in front, which is what PyYAML's
	// flatten_mapping does, and then the whole list is reduced with
	// last-assignment-wins and first-position ordering. That is what gives
	// an explicit key precedence over a merged one while leaving it where
	// the merge put it.
	return c.reduce(v, append(merged, explicit...))
}

// reduce applies mapping semantics to a list of pairs: the first position a
// key appears at is where it stays, and the last value given for it is the
// one kept. dupFirst keeps the first value instead.
func (c *composer) reduce(v *Value, all []KV) (*Value, error) {
	at := map[string]int{}
	for _, kv := range all {
		key := canon(kv.Key)
		if i, ok := at[key]; ok {
			if c.dup != dupFirst {
				v.Map[i].Val = kv.Val
			}
			continue
		}
		at[key] = len(v.Map)
		v.Map = append(v.Map, kv)
	}
	return v, nil
}

func isMergeKey(n *Node) bool {
	if n.Kind != KScalar || n.Style != StPlain || n.Val != "<<" {
		return false
	}
	return n.Tag == "" || n.Tag == "?" || n.Tag == tagMerge
}

// flattenMerge turns the value of a << key into the pairs it contributes.
// A sequence of mappings is merged last first, so that an earlier mapping
// in the sequence takes precedence, which is what the 1.1 merge type says
// and what PyYAML implements by reversing the list.
func (c *composer) flattenMerge(n *Node, m *Value) ([]KV, error) {
	switch m.Kind {
	case VMap:
		return m.Map, nil
	case VSeq:
		var out []KV
		for i := len(m.Seq) - 1; i >= 0; i-- {
			if m.Seq[i].Kind != VMap {
				return nil, c.errAt(n, "a << value given as a sequence must hold mappings only")
			}
			out = append(out, m.Seq[i].Map...)
		}
		return out, nil
	}
	return nil, c.errAt(n, "a << value must be a mapping or a sequence of mappings")
}
