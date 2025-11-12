package main

// parseFlow reads a flow collection. Not written yet: it refuses, so that
// nothing in the suite can pass by parsing one of these wrongly.
func (p *parser) parseFlow(n *Node, ind int) error {
	return p.s.errf("yamlc: flow collections are not implemented yet")
}
