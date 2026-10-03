package meaning

// lookup finds the concept a reference written in file f names, reporting a
// finding when it cannot: a reference that is no concept reference, a concept
// that is not declared, another graph that is not available, or a pin that
// differs from the one the other references to that graph carry.
func (r *run) lookup(f *File, line int, where, ref string) (node, bool) {
	p, ok := ParseConceptRef(ref)
	if !ok {
		r.err(f, line, RuleReferenceSyntax, "%s: %s is not a concept reference", where, ref)
		return node{}, false
	}
	if p.Repo == "" || (r.local.Address != "" && p.Repo == r.local.Address) {
		if p.Repo != "" && p.Pin != "" {
			r.err(f, line, RuleSelfPin, "%s: %s pins this repository's own concept; a reference to the repository itself cannot carry ?ref=", where, ref)
		}
		return r.find(f, line, where, r.local, p, "concept %s is not declared in this repository")
	}
	// One pin per referenced repository across all of this repository's
	// files: the repository resolves against one version of each dependency.
	if seen, had := r.pins[p.Repo]; had && seen != p.Pin {
		r.err(f, line, RulePinMismatch, "%s: meaning://%s is pinned to both %q and %q; use one pin per repository", where, p.Repo, seen, p.Pin)
	}
	r.pins[p.Repo] = p.Pin
	remote, err := r.resolve(p.Repo, p.Pin)
	if err != nil {
		r.err(f, line, RuleUnresolved, "%s: %v", where, err)
		return node{}, false
	}
	return r.find(f, line, where, remote, p, "concept %s does not exist in meaning://"+p.Repo)
}

func (r *run) find(f *File, line int, where string, g *Graph, p ConceptRef, missing string) (node, bool) {
	entry, ok := g.Concepts[p.ID]
	if !ok {
		r.err(f, line, RuleUnknownConcept, "%s: "+missing, where, p.ID)
		return node{}, false
	}
	return node{concept: entry.Concept, graph: g}, true
}

// resolveConcept finds the concept a reference names as written inside graph
// g: bare ids resolve in g, and so does a meaning:// reference to g's own
// address (without ?ref=); the rest through the resolver. It reports nothing.
func (r *run) resolveConcept(ref string, g *Graph) (node, bool) {
	p, ok := ParseConceptRef(ref)
	if !ok {
		return node{}, false
	}
	target := g
	if own := p.Repo == "" || (p.Pin == "" && p.Repo == g.Address); !own {
		var err error
		if target, err = r.resolve(p.Repo, p.Pin); err != nil {
			return node{}, false
		}
	}
	entry, ok := target.Concepts[p.ID]
	if !ok {
		return node{}, false
	}
	return node{concept: entry.Concept, graph: target}, true
}

// lineage is the concept and its ancestors through extends, nearest first. It
// stops at a repeat, so a cycle (reported by the check) cannot loop.
func (r *run) lineage(c *Concept, g *Graph) []node {
	var chain []node
	for n, ok := (node{concept: c, graph: g}), true; ok && len(chain) < 50; n, ok = r.resolveConcept(n.concept.Extends, n.graph) {
		if seenConcept(chain, n.concept) {
			break
		}
		chain = append(chain, n)
	}
	return chain
}

func seenConcept(chain []node, c *Concept) bool {
	for _, n := range chain {
		if n.concept == c {
			return true
		}
	}
	return false
}

// inherited is the nearest concept along the lineage for which get is not empty.
func (r *run) inherited(c *Concept, g *Graph, get func(*Concept) string) (node, bool) {
	for _, n := range r.lineage(c, g) {
		if get(n.concept) != "" {
			return n, true
		}
	}
	return node{}, false
}

// effectiveAggregation is how a measure's values combine when grouped: its own
// aggregation, else the nearest one along extends, else none. The second
// result is the concept that states it, nil when none does.
func (r *run) effectiveAggregation(c *Concept) (string, *Concept) {
	for _, n := range r.lineage(c, r.local) {
		if m := n.concept.Measure; m != nil && m.Aggregation != "" {
			return m.Aggregation, n.concept
		}
	}
	return "none", nil
}

// ratioInput returns the measure input that makes a measure a ratio: a measure
// computed from another measure, or a kind of a ratio.
func (r *run) ratioInput(c *Concept) (string, bool) {
	for _, n := range r.lineage(c, r.local) {
		if n.concept.Measure == nil {
			continue
		}
		for _, input := range n.concept.Measure.Inputs {
			if in, ok := r.resolveConcept(input, n.graph); ok && in.concept.Kind == "measure" {
				return input, true
			}
		}
	}
	return "", false
}
