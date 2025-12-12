package main

import (
	"fmt"
	"sort"
	"strings"
)

// The vacuous-pass guard. A folded-scalar test over a corpus with no
// folded scalars passes having run nothing, so every construct the parser
// has is counted over whatever the suite actually fed it, and the suite
// asserts that each count is non-zero.
//
// The weakness is the same one pcapd's README records about its own
// counter: it is produced by the parser the tests use, so a construct the
// parser cannot see is one the counter cannot count either, and the pair
// would agree that the corpus is missing something it holds. The event
// comparison against PyYAML is the check on that, since PyYAML counts
// nothing but does report the structure it found.

type counter struct {
	n     map[string]int
	depth int
	max   int
}

func newCounter() *counter { return &counter{n: map[string]int{}} }

func (c *counter) bump(k string) { c.n[k]++ }

func (c *counter) stream(docs []*Document) {
	for _, d := range docs {
		c.bump("doc")
		if d.Explicit {
			c.bump("doc.explicit")
		} else {
			c.bump("doc.bare")
		}
		if d.EndMarked {
			c.bump("doc.end-marked")
		}
		if d.Version != "" {
			c.bump("directive.yaml")
			c.bump("directive.yaml." + d.Version)
		}
		c.depth = 0
		c.node(d.Root, "")
	}
}

func (c *counter) node(n *Node, role string) {
	c.depth++
	if c.depth > c.max {
		c.max = c.depth
	}
	defer func() { c.depth-- }()
	if n.Anchor != "" {
		c.bump("anchor")
	}
	if n.TagRaw != "" {
		switch {
		case n.TagRaw == "!":
			c.bump("tag.non-specific")
		case strings.HasPrefix(n.TagRaw, "!<"):
			c.bump("tag.verbatim")
		case strings.HasPrefix(n.TagRaw, "!!"):
			c.bump("tag.secondary")
		case strings.Count(n.TagRaw, "!") >= 2:
			c.bump("tag.named-handle")
		default:
			c.bump("tag.primary")
		}
	}
	switch n.Kind {
	case KAlias:
		c.bump("alias")
	case KScalar:
		switch n.Style {
		case StPlain:
			c.bump("scalar.plain")
			if n.Val == "" {
				c.bump("scalar.empty")
			}
			if n.MultiLine {
				c.bump("scalar.plain.multiline")
			}
			if strings.ContainsRune(n.Val, '\t') {
				c.bump("scalar.plain.tab")
			}
		case StSingle:
			c.bump("scalar.single")
			if n.MultiLine {
				c.bump("scalar.single.multiline")
			}
		case StDouble:
			c.bump("scalar.double")
			if n.MultiLine {
				c.bump("scalar.double.multiline")
			}
		case StLiteral, StFolded:
			if n.Style == StLiteral {
				c.bump("scalar.literal")
			} else {
				c.bump("scalar.folded")
			}
			switch n.Chomp {
			case -1:
				c.bump("blockscalar.chomp.strip")
			case 1:
				c.bump("blockscalar.chomp.keep")
			default:
				c.bump("blockscalar.chomp.clip")
			}
			if n.IndentGiven {
				c.bump("blockscalar.indent.given")
			}
			if strings.Contains(n.Val, "\n\n") {
				c.bump("blockscalar.blank-line")
			}
		}
	case KSeq:
		if n.Flow {
			c.bump("flow.seq")
		} else {
			c.bump("block.seq")
		}
		if len(n.Seq) == 0 {
			c.bump("collection.empty")
		}
		for _, x := range n.Seq {
			if x.Kind == KSeq {
				c.bump("seq.nested-in-seq")
			}
			c.node(x, "item")
		}
	case KMap:
		if n.Flow {
			c.bump("flow.map")
		} else {
			c.bump("block.map")
		}
		if len(n.Map) == 0 {
			c.bump("collection.empty")
		}
		for _, pr := range n.Map {
			if pr.Explicit {
				c.bump("map.key.explicit")
			}
			if isMergeKey(pr.Key) {
				c.bump("merge.key")
			}
			switch pr.Key.Kind {
			case KSeq, KMap:
				c.bump("map.key.collection")
			case KScalar:
				if pr.Key.Val == "" && pr.Key.Style == StPlain {
					c.bump("map.key.empty")
				}
			}
			if pr.Val.Kind == KScalar && pr.Val.Val == "" && pr.Val.Style == StPlain {
				c.bump("map.value.empty")
			}
			c.node(pr.Key, "key")
			c.node(pr.Val, "value")
		}
	}
}

// resolutions counts what the two schemas make of the plain scalars, which
// is the other half of the guard: a divergence report over a corpus with no
// 1.1-only resolutions in it also proves nothing.
func (c *counter) resolutions(docs []*Document) {
	c11 := newComposer("", 11, dupLast, true, defaultBudget)
	c12 := newComposer("", 12, dupLast, true, defaultBudget)
	var walk func(n *Node)
	walk = func(n *Node) {
		switch n.Kind {
		case KScalar:
			if n.Style != StPlain || n.Tag != "" {
				return
			}
			t12 := resolveTag(n.Val, 12)
			t11 := resolveTag(n.Val, 11)
			c.bump("resolve.1.2." + short(t12))
			c.bump("resolve.1.1." + short(t11))
			v12, e12 := c12.scalarValue(n)
			v11, e11 := c11.scalarValue(n)
			switch {
			case e12 != nil || e11 != nil:
				c.bump("diverge.scalar-refused")
			case canon(v12) != canon(v11):
				c.bump("diverge.scalar")
				if t12 == t11 {
					c.bump("diverge.same-tag-other-value")
				}
			}
		case KSeq:
			for _, x := range n.Seq {
				walk(x)
			}
		case KMap:
			for _, pr := range n.Map {
				walk(pr.Key)
				walk(pr.Val)
			}
		}
	}
	for _, d := range docs {
		walk(d.Root)
	}
}

func short(tag string) string {
	return strings.TrimPrefix(tag, "tag:yaml.org,2002:")
}

func cmdCounts(o *opts) int {
	c := newCounter()
	read, refused := 0, 0
	for _, name := range o.files {
		raw, err := readInput(name)
		if err != nil {
			refused++
			continue
		}
		docs, err := parseStream(name, raw, o.maxDepth)
		if err != nil {
			refused++
			continue
		}
		read++
		c.stream(docs)
		c.resolutions(docs)
	}
	keys := make([]string, 0, len(c.n))
	for k := range c.n {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Printf("files.read %d\n", read)
	fmt.Printf("files.refused %d\n", refused)
	fmt.Printf("depth.max %d\n", c.max)
	for _, k := range keys {
		fmt.Printf("%s %d\n", k, c.n[k])
	}
	return 0
}
