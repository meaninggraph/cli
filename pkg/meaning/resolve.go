package meaning

import (
	"math"
	"strings"
)

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
	for n, ok := (node{concept: c, graph: g}), true; ok && len(chain) < 50; n, ok = r.parent(n) {
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

// parentEntry is the memoised result of resolving the extends of a concept.
type parentEntry struct {
	parent node
	ok     bool
}

// parent resolves what a concept extends, once per concept.
func (r *run) parent(n node) (node, bool) {
	if e, done := r.parents[n.concept]; done {
		return e.parent, e.ok
	}
	p, ok := r.resolveConcept(n.concept.Extends, n.graph)
	r.parents[n.concept] = parentEntry{parent: p, ok: ok}
	return p, ok
}

// chainInfo is what is known of the extends chain that starts at a concept:
// whether it runs into a cycle, and the concept where the cycle starts.
type chainInfo struct {
	cyclic bool
	entry  *Concept
}

// extendsCycle follows the extends chain of a concept and reports whether it
// comes back to a concept it has passed, with the chain written out for the
// message. Every concept's chain is followed once, however long the chains
// are, so the work is linear in the number of concepts.
func (r *run) extendsCycle(start node) (string, bool) {
	if known, done := r.chains[start.concept]; done {
		// Walked as part of the chain of another concept; it starts its own cycle,
		// or leads into one, where that walk found out.
		return r.cyclePathIf(known, start), known.cyclic
	}
	walked := []node{start}
	index := map[*Concept]int{start.concept: 0}
	info := chainInfo{}
	member := math.MaxInt // the walked concepts from this one on are in the cycle itself
	for cur := start; ; {
		next, ok := r.parent(cur)
		if !ok {
			break
		}
		if known, done := r.chains[next.concept]; done {
			info = known
			break
		}
		if at, seen := index[next.concept]; seen {
			info, member = chainInfo{cyclic: true, entry: next.concept}, at
			break
		}
		walked = append(walked, next)
		index[next.concept] = len(walked) - 1
		cur = next
	}
	// A concept in a cycle starts it itself; one that leads into it starts it
	// where it is entered.
	for i, w := range walked {
		if i >= member {
			r.chains[w.concept] = chainInfo{cyclic: true, entry: w.concept}
		} else {
			r.chains[w.concept] = info
		}
	}
	return r.cyclePathIf(info, start), info.cyclic
}

// cyclePathIf writes the cycle of a chain, or nothing when it has none.
func (r *run) cyclePathIf(info chainInfo, start node) string {
	if !info.cyclic {
		return ""
	}
	return r.cyclePath(start, info.entry)
}

// maxCycleNames bounds the names written into a message about a cycle.
const maxCycleNames = 12

// cyclePath writes the chain from a concept to the concept where the cycle
// starts and once around it: "a -> b -> a".
func (r *run) cyclePath(start node, entry *Concept) string {
	var names []string
	for cur, passed := start, false; ; {
		if len(names) == maxCycleNames {
			return strings.Join(names, " -> ") + " -> ... -> " + entry.ID + " (a longer chain)"
		}
		names = append(names, cur.concept.ID)
		if cur.concept == entry {
			if passed {
				return strings.Join(names, " -> ")
			}
			passed = true
		}
		cur, _ = r.parent(cur)
	}
}
