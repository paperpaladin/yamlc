package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// The second half of the project: given a document, say where a 1.1 reader
// and a 1.2 reader disagree about what it means.
//
// Each finding is one line, and the kinds are counted separately because
// they need different work. A scalar read two ways is the common case. A
// mapping whose keys collide under one set of rules and not the other is
// rarer and worse, because one reader has a document and the other has an
// error. The merge key is in here because 1.2's core schema has no merge
// type at all, so a strict 1.2 reader sees a mapping with a key spelled
// "<<" where a 1.1 reader sees an inherited mapping.

type finding struct {
	line, col int
	kind      string
	path      string
	detail    string
}

type reporter struct {
	name     string
	findings []finding
	c11      *composer
	c12      *composer
	scalars  []scalarLine
}

type scalarLine struct {
	line, col int
	path      string
	style     Style
	raw       string
	v12, v11  string
}

func (r *reporter) add(line, col int, kind, path, detail string) {
	r.findings = append(r.findings, finding{line, col, kind, path, detail})
}

// readBoth gives the two readings of one scalar as canonical text, or a
// message saying that this reader has no value for it.
func (r *reporter) readBoth(n *Node) (string, string) {
	one := func(c *composer) string {
		v, err := c.scalarValue(n)
		if err != nil {
			return "refused: " + refusalText(err)
		}
		return canon(v)
	}
	return one(r.c12), one(r.c11)
}

func refusalText(err error) string {
	s := err.Error()
	if i := strings.Index(s, ": "); i >= 0 {
		// Drop the file:line:col prefix; the finding carries its own.
		if j := strings.Index(s[i+2:], ": "); j >= 0 && strings.Count(s[:i], ":") >= 2 {
			return s[i+2+j+2:]
		}
	}
	return s
}

func (r *reporter) walk(n *Node, path string) {
	switch n.Kind {
	case KScalar:
		v12, v11 := r.readBoth(n)
		r.scalars = append(r.scalars, scalarLine{n.Line, n.Col, path, n.Style, n.Val, v12, v11})
		if v12 != v11 {
			kind := "scalar"
			if strings.HasPrefix(v11, "refused") || strings.HasPrefix(v12, "refused") {
				kind = "scalar-refused"
			}
			r.add(n.Line, n.Col, kind, path, fmt.Sprintf("%q: 1.2 reads %s, 1.1 reads %s", n.Val, v12, v11))
		}
		if n.Style == StPlain && strings.ContainsRune(n.Val, '\t') {
			r.add(n.Line, n.Col, "tab-in-plain", path,
				"a tab inside a plain scalar: content in 1.2, and PyYAML refuses the document")
		}
	case KSeq:
		for i, x := range n.Seq {
			r.walk(x, fmt.Sprintf("%s[%d]", path, i))
		}
	case KMap:
		r.mapFindings(n, path)
		for _, pr := range n.Map {
			kp := path + ".{" + keyText(pr.Key) + "}"
			r.walk(pr.Key, kp)
			r.walk(pr.Val, path+"."+keyText(pr.Key))
		}
	}
}

func keyText(n *Node) string {
	switch n.Kind {
	case KScalar:
		if n.Val == "" {
			return "(empty)"
		}
		return n.Val
	case KAlias:
		return "*" + n.Alias
	case KSeq:
		return "(sequence)"
	}
	return "(mapping)"
}

// mapFindings looks for the two things about a mapping that depend on the
// schema: whether its keys collide, and whether it has a merge key.
func (r *reporter) mapFindings(n *Node, path string) {
	count := func(c *composer) (int, bool) {
		seen := map[string]bool{}
		for _, pr := range n.Map {
			if pr.Key.Kind != KScalar {
				continue
			}
			v, err := c.scalarValue(pr.Key)
			if err != nil {
				return 0, false
			}
			seen[canon(v)] = true
		}
		return len(seen), true
	}
	n12, ok12 := count(r.c12)
	n11, ok11 := count(r.c11)
	if ok12 && ok11 && n12 != n11 {
		r.add(n.Line, n.Col, "dupkey", path, fmt.Sprintf(
			"the keys of this mapping resolve to %d distinct values under 1.2 and %d under 1.1, so one reader has a duplicate key error",
			n12, n11))
	}
	for _, pr := range n.Map {
		if isMergeKey(pr.Key) {
			r.add(pr.Key.Line, pr.Key.Col, "merge", path,
				"a merge key: 1.1 inherits the mapping it names, and the 1.2 core schema has no merge type, so a strict 1.2 reader sees an ordinary key spelled <<")
		}
	}
}

// rawFindings are properties of the bytes rather than of the tree. The
// three characters here are line breaks in 1.1 and ordinary content in
// 1.2, which changes how the document is cut into lines before any
// resolution happens.
func (r *reporter) rawFindings(s *src) {
	line, col := 1, 1
	for i := 0; i < len(s.b); {
		c := s.b[i]
		if c < 0x80 {
			if c == '\n' {
				line, col = line+1, 1
			} else {
				col++
			}
			i++
			continue
		}
		switch {
		case c == 0xc2 && i+1 < len(s.b) && s.b[i+1] == 0x85:
			r.add(line, col, "linebreak", "",
				"U+0085 NEL: a line break in 1.1 and ordinary content in 1.2")
			i += 2
		case c == 0xe2 && i+2 < len(s.b) && s.b[i+1] == 0x80 &&
			(s.b[i+2] == 0xa8 || s.b[i+2] == 0xa9):
			name := "U+2028 LINE SEPARATOR"
			if s.b[i+2] == 0xa9 {
				name = "U+2029 PARAGRAPH SEPARATOR"
			}
			r.add(line, col, "linebreak", "",
				name+": a line break in 1.1 and ordinary content in 1.2")
			i += 3
		default:
			i++
			for i < len(s.b) && s.b[i]&0xc0 == 0x80 {
				i++
			}
		}
		col++
	}
}

func report(name string, raw []byte, o *opts) (*reporter, error) {
	docs, err := parseStream(name, raw, o.maxDepth)
	if err != nil {
		return nil, err
	}
	r := &reporter{name: name}
	r.c12 = newComposer(name, 12, dupLast, o.merge == "on", o.maxNodes)
	r.c11 = newComposer(name, 11, dupLast, o.merge == "on", o.maxNodes)
	s, err := newSrc(name, raw)
	if err != nil {
		return nil, err
	}
	r.rawFindings(s)
	for i, d := range docs {
		path := "$"
		if len(docs) > 1 {
			path = fmt.Sprintf("$%d", i)
		}
		if d.Version == "1.1" {
			r.add(d.Line, 1, "directive", path,
				"%YAML 1.1: the document asks for the 1.1 resolution rules, and a 1.2 processor is not obliged to give them")
		}
		r.walk(d.Root, path)
	}
	sort.SliceStable(r.findings, func(i, j int) bool {
		if r.findings[i].line != r.findings[j].line {
			return r.findings[i].line < r.findings[j].line
		}
		return r.findings[i].col < r.findings[j].col
	})
	return r, nil
}

func cmdDiverge(o *opts) int {
	rc := 0
	total := map[string]int{}
	withFindings := 0
	for _, name := range o.files {
		raw, err := readInput(name)
		if err == nil {
			var r *reporter
			r, err = report(name, raw, o)
			if err == nil {
				if len(r.findings) > 0 {
					withFindings++
				}
				// --batch is for measuring a corpus rather than reading
				// one document: one line per file that has anything to
				// say, and nothing at all for the files that do not.
				if o.batch {
					if len(r.findings) > 0 {
						per := map[string]int{}
						for _, f := range r.findings {
							per[f.kind]++
							total[f.kind]++
						}
						fmt.Printf("%s\t%d\t%s\n", name, len(r.findings), kindList(per))
					}
					continue
				}
				for _, f := range r.findings {
					total[f.kind]++
					fmt.Printf("%s:%d:%d: %s %s %s\n", name, f.line, f.col, f.kind, f.path, f.detail)
				}
				continue
			}
		}
		if o.batch {
			fmt.Printf("%s\trefused\t%v\n", name, err)
		} else {
			fmt.Printf("%s: refused: %v\n", name, err)
		}
		total["refused"]++
		rc = 1
	}
	if o.batch {
		fmt.Printf("files %d\n", len(o.files))
		fmt.Printf("with-findings %d\n", withFindings)
		fmt.Printf("refused %d\n", total["refused"])
		n := 0
		for k, v := range total {
			if k != "refused" {
				n += v
				fmt.Printf("kind-%s %d\n", k, v)
			}
		}
		fmt.Printf("findings %d\n", n)
		return rc
	}
	kinds := make([]string, 0, len(total))
	n := 0
	for k, v := range total {
		kinds = append(kinds, fmt.Sprintf("%s=%d", k, v))
		if k != "refused" {
			n += v
		}
	}
	sort.Strings(kinds)
	fmt.Printf("%d divergences, %d refusals", n, total["refused"])
	if len(kinds) > 0 {
		fmt.Printf(" (%s)", strings.Join(kinds, " "))
	}
	fmt.Println()
	return rc
}

func kindList(per map[string]int) string {
	ks := make([]string, 0, len(per))
	for k, v := range per {
		ks = append(ks, fmt.Sprintf("%s=%d", k, v))
	}
	sort.Strings(ks)
	return strings.Join(ks, " ")
}

func cmdScalars(o *opts) int {
	for _, name := range o.files {
		raw, err := readInput(name)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		r, err := report(name, raw, o)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		for _, s := range r.scalars {
			mark := " "
			if s.v12 != s.v11 {
				mark = "*"
			}
			fmt.Printf("%d:%d%s %s %s%s | 1.2 %s | 1.1 %s\n",
				s.line, s.col, mark, s.path, s.style.char(), escapeEvent(s.raw), s.v12, s.v11)
		}
	}
	return 0
}
