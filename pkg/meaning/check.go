package meaning

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Rule identifiers of findings. They are stable: scripts may match on them.
const (
	RuleYAML             = "yaml"
	RuleSchema           = "schema"
	RuleNoFiles          = "no-meaning-files"
	RuleSubdirectoryFile = "subdirectory-meaning-file"
	RuleDuplicateConcept = "duplicate-concept"
	RuleDuplicateSource  = "duplicate-source"
	RuleDuplicateValue   = "duplicate-value"
	RuleAmbiguousWord    = "ambiguous-word"
	RuleReferenceSyntax  = "reference-syntax"
	RuleUnknownConcept   = "unknown-concept"
	RuleUnresolved       = "unresolved-graph"
	RulePinMismatch      = "pin-mismatch"
	RuleSelfPin          = "self-pin"
	RuleTargetKind       = "target-kind"
	RuleExtendsKind      = "extends-kind"
	RuleExtendsCycle     = "extends-cycle"
	RuleNarrowing        = "narrowing"
	RuleUnit             = "unit"
	RuleMeasureInput     = "measure-input"
	RuleMeasureDimension = "measure-dimension"
	RuleRatio            = "ratio-aggregation"
	RuleSource           = "undeclared-source"
	RuleEntityBindings   = "entity-bindings"
	RuleModels           = "models"
	RuleBindingModel     = "binding-model"
	RuleBindingRole      = "binding-role"
	RuleUniversal        = "universal"
)

// Resolver gives the graph a meaning:// reference names: repo is
// {host}/{org}/{repo} and pin is the ?ref= value (empty when there is none).
// An error says why the graph is not available. Nothing in this package
// fetches anything: a Resolver reads what the caller has at hand.
type Resolver func(repo, pin string) (*Graph, error)

// GraphResolver resolves references from graphs the caller already has, keyed
// by address. It follows the reference checker's rules for a repository it
// reads from git: a reference to another repository must carry a ?ref= pin,
// and a repository that cannot be read (a file that is not YAML) is reported
// as such. The pin itself cannot be verified against a directory.
func GraphResolver(graphs map[string]*Graph) Resolver {
	return func(repo, pin string) (*Graph, error) {
		g, ok := graphs[repo]
		switch {
		case !ok:
			return nil, fmt.Errorf("meaning://%s is not available: no local copy of it was supplied, and nothing is fetched", repo)
		case pin == "":
			return nil, fmt.Errorf("meaning://%s needs a ?ref= pin", repo)
		}
		for _, f := range g.Files {
			if f.ParseErr != nil {
				return nil, fmt.Errorf("meaning://%s?ref=%s cannot be read: %s: %s", repo, pin, f.Path, f.ParseErr.Message)
			}
		}
		return g, nil
	}
}

// Profile selects extra rules on top of the format's.
type Profile string

const (
	// ProfileDefault applies the rules of the format only.
	ProfileDefault Profile = ""
	// ProfileUniversal also applies the rules of a repository of universal
	// concepts, such as github.com/meaninggraph/core: a LICENSE file, CC0-1.0
	// on every file, no models and no bindings, no meaning file outside the
	// root, and one word names one concept.
	ProfileUniversal Profile = "universal"
)

// Checker checks a Graph. The zero value checks against the embedded schema,
// with no other graph available.
type Checker struct {
	// Schema validates each file; the embedded schema when nil.
	Schema Validator
	// Resolve reads other graphs; when nil none is available.
	Resolve Resolver
	// Models reads the models bindings name; HCLReader when nil.
	Models ModelReader
	// FS reads the directory tree around the graph and the models it names;
	// the host's file system when nil.
	FS FS
	// Profile selects extra rules.
	Profile Profile
}

type node struct {
	concept *Concept
	graph   *Graph
}

type run struct {
	c        Checker
	local    *Graph
	other    Resolver
	findings []Finding
	pins     map[string]string
}

// Check validates every file of g against the schema and checks what the
// schema cannot: references resolve, extends joins compatible kinds without a
// cycle, values-of and units-of name entities, measures and ratios are
// consistent, and ids and words are unique. The findings are sorted.
func (c Checker) Check(g *Graph) []Finding {
	if c.Schema == nil {
		c.Schema = DefaultSchema()
	}
	if c.FS == nil {
		c.FS = OSFS{}
	}
	if c.Models == nil {
		c.Models = HCLReader{}
	}
	r := &run{c: c, local: g, other: c.Resolve, pins: map[string]string{}}
	if r.other == nil {
		r.other = func(repo, _ string) (*Graph, error) {
			return nil, fmt.Errorf("meaning://%s is not available: no graph was supplied", repo)
		}
	}
	if len(g.Files) == 0 {
		r.add(g.Dir, 0, RuleNoFiles, Error, "no %s file", "*"+FileSuffix)
	}
	for _, d := range g.duplicates {
		r.add(d.second.File.Path, d.second.Concept.Line, RuleDuplicateConcept, Error,
			"concept %s is declared twice (%s and %s)", d.first.Concept.ID, d.first.File.Path, d.second.File.Path)
	}
	if g.Dir != "" {
		r.hiddenFiles()
	}
	for _, f := range g.Files {
		r.checkFile(f)
	}
	if c.Profile == ProfileUniversal {
		r.universal()
	}
	SortFindings(r.findings)
	return r.findings
}

func (r *run) add(file string, line int, rule string, severity Severity, format string, args ...any) {
	r.findings = append(r.findings, Finding{File: file, Line: line, Rule: rule, Severity: severity, Message: fmt.Sprintf(format, args...)})
}

func (r *run) err(f *File, line int, rule, format string, args ...any) {
	r.add(f.Path, line, rule, Error, format, args...)
}

// resolve reads a graph: the one being checked when repo is its own address,
// else through the Resolver.
func (r *run) resolve(repo, pin string) (*Graph, error) {
	if r.local.Address != "" && repo == r.local.Address {
		return r.local, nil
	}
	return r.other(repo, pin)
}

func (r *run) checkFile(f *File) {
	if f.ParseErr != nil {
		r.err(f, f.ParseErr.Line, RuleYAML, "%s", f.ParseErr.Message)
		return
	}
	problems := schemaProblems(r.c.Schema, f.Root)
	for _, p := range problems {
		r.err(f, f.Root.At(p.path), RuleSchema, "schema: %s", p)
	}
	if len(problems) > 0 {
		return
	}
	seen := map[string]bool{}
	for _, s := range f.Sources {
		if seen[s.ID] {
			r.err(f, s.Line, RuleDuplicateSource, "source %s is declared twice", s.ID)
		}
		seen[s.ID] = true
	}
	models := r.loadModels(f)
	for _, c := range f.Concepts {
		r.checkConcept(f, c, seen)
		r.checkBindings(f, c, "concept "+c.ID, models)
	}
}

func (r *run) checkConcept(f *File, c *Concept, sources map[string]bool) {
	label := "concept " + c.ID
	if c.Of != "" {
		if owner, ok := r.lookup(f, c.lineOf("of"), label+" of", c.Of); ok && owner.concept.Kind != "entity" {
			r.err(f, c.lineOf("of"), RuleTargetKind, "%s: of names %s, which is %s, not an entity", label, c.Of, an(owner.concept.Kind))
		}
	}
	if c.Extends != "" {
		r.checkExtends(f, c, label)
	}
	for _, key := range []string{"values-of", "units-of"} {
		target := c.targetOf(key)
		if target == "" {
			continue
		}
		if found, ok := r.lookup(f, c.lineOf(key), label+" "+key, target); ok && found.concept.Kind != "entity" {
			r.err(f, c.lineOf(key), RuleTargetKind, "%s: %s names %s, which is %s, not an entity", label, key, target, an(found.concept.Kind))
		}
		if c.Extends != "" {
			r.checkNarrowing(f, c, label, key, target)
		}
	}
	r.checkUnit(f, c, label)
	if c.Measure != nil {
		r.checkMeasure(f, c, label)
	}
	if c.Source != "" && !sources[c.Source] {
		r.err(f, c.lineOf("source"), RuleSource, "%s: source %s is not declared in sources", label, c.Source)
	}
	r.checkValues(f, c, label)
}

// targetOf is the value of values-of or units-of.
func (c *Concept) targetOf(key string) string {
	if key == "values-of" {
		return c.ValuesOf
	}
	return c.UnitsOf
}

func (r *run) checkExtends(f *File, c *Concept, label string) {
	line := c.lineOf("extends")
	if parent, ok := r.lookup(f, line, label+" extends", c.Extends); ok {
		allowed := extendsCompatibility[c.Kind]
		if !contains(allowed, parent.concept.Kind) {
			r.err(f, line, RuleExtendsKind, "%s: %s cannot extend %s, which is %s; extends means \"is a kind of\", and %s may extend only %s",
				label, an(c.Kind), c.Extends, an(parent.concept.Kind), an(c.Kind), strings.Join(allowed, " or "))
		}
	}
	// The chain of extends, through any graph, comes back to a concept it has passed.
	seen := []*Concept{c}
	for n, ok := r.resolveConcept(c.Extends, r.local); ok; n, ok = r.resolveConcept(n.concept.Extends, n.graph) {
		if contains(seen, n.concept) {
			var names []string
			for _, s := range seen {
				names = append(names, s.ID)
			}
			names = append(names, n.concept.ID)
			r.err(f, line, RuleExtendsCycle, "%s: extends forms a cycle (%s)", label, strings.Join(names, " -> "))
			break
		}
		seen = append(seen, n.concept)
	}
}

// extendsCompatibility says which kinds a concept of a kind may extend: extends
// means "is a kind of". An attribute and a dimension are both a property of an
// entity (a dimension is one that answers are grouped by), so they may extend
// each other.
var extendsCompatibility = map[string][]string{
	"entity":    {"entity"},
	"attribute": {"attribute", "dimension"},
	"dimension": {"dimension", "attribute"},
	"measure":   {"measure"},
}

func contains[T comparable](list []T, v T) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

// checkNarrowing: values-of and units-of may narrow an inherited one to a kind
// of it, never change it.
func (r *run) checkNarrowing(f *File, c *Concept, label, key, target string) {
	parent, ok := r.resolveConcept(c.Extends, r.local)
	if !ok {
		return
	}
	domain, ok := r.inherited(parent.concept, parent.graph, func(x *Concept) string { return x.targetOf(key) })
	if !ok {
		return
	}
	required, ok := r.resolveConcept(domain.concept.targetOf(key), domain.graph)
	if !ok {
		return
	}
	own, ok := r.resolveConcept(target, r.local)
	if !ok {
		return
	}
	for _, n := range r.lineage(own.concept, own.graph) {
		if n.concept == required.concept {
			return
		}
	}
	r.err(f, c.lineOf(key), RuleNarrowing, "%s: %s %s is neither %s nor a kind of it, which %s requires",
		label, key, target, domain.concept.targetOf(key), c.Extends)
}

// checkUnit: with units-of (own or inherited) the unit names one value of that entity.
func (r *run) checkUnit(f *File, c *Concept, label string) {
	if c.Unit == "" {
		return
	}
	domain, ok := r.inherited(c, r.local, func(x *Concept) string { return x.UnitsOf })
	if !ok {
		return
	}
	entity, ok := r.resolveConcept(domain.concept.UnitsOf, domain.graph)
	if !ok {
		return
	}
	unit := lower(c.Unit)
	var named []string
	for _, v := range entity.concept.Values {
		if v.hasWord(unit, true) {
			named = append(named, v.ID)
		}
	}
	if len(named) != 1 {
		found := "none"
		if len(named) > 0 {
			found = strings.Join(named, ", ")
		}
		r.err(f, c.lineOf("unit"), RuleUnit, "%s: unit %q must name exactly one value of %s (units-of), but names %s",
			label, c.Unit, domain.concept.UnitsOf, found)
	}
}

// hasWord reports whether a label or alias, and with codes also a code, equals
// the lower-cased word ignoring case.
func (v Value) hasWord(lowerWord string, codes bool) bool {
	for _, w := range v.words(codes) {
		if lower(w) == lowerWord {
			return true
		}
	}
	return false
}

// words are the labels and aliases of a value, in any language, and with codes also its codes.
func (v Value) words(codes bool) []string {
	var out []string
	for _, k := range sortedKeys(v.Labels) {
		out = append(out, v.Labels[k])
	}
	for _, k := range sortedKeys(v.Aliases) {
		out = append(out, v.Aliases[k]...)
	}
	if codes {
		for _, k := range sortedKeys(v.Codes) {
			out = append(out, v.Codes[k])
		}
	}
	return out
}

func (r *run) checkMeasure(f *File, c *Concept, label string) {
	line := c.lineOf("measure")
	for _, ref := range c.Measure.Inputs {
		if in, ok := r.lookup(f, line, label+" measure.inputs", ref); ok && !contains([]string{"attribute", "measure"}, in.concept.Kind) {
			r.err(f, line, RuleMeasureInput, "%s: measure.inputs names %s, which is %s; a measure is computed from attributes and measures only", label, ref, an(in.concept.Kind))
		}
	}
	for _, ref := range c.Measure.Dimensions {
		if d, ok := r.lookup(f, line, label+" measure.dimensions", ref); ok && !contains([]string{"dimension", "attribute"}, d.concept.Kind) {
			r.err(f, line, RuleMeasureDimension, "%s: measure.dimensions names %s, which is %s; a measure is grouped by dimensions or attributes only", label, ref, an(d.concept.Kind))
		}
	}
	// A kind of a measure inherits its aggregation, so a ratio that extends a measure which sums is wrong too.
	aggregation, from := r.effectiveAggregation(c)
	if !contains([]string{"sum", "count", "average"}, aggregation) {
		return
	}
	if ratio, ok := r.ratioInput(c); ok {
		inherited := ""
		if from != c {
			inherited = fmt.Sprintf("; %s is inherited from %s, state aggregation: none", aggregation, from.ID)
		}
		r.err(f, line, RuleRatio, "%s: aggregation %s on a ratio (it is computed from the measure %s); a ratio is recomputed per group from its inputs, so its aggregation is none%s", label, aggregation, ratio, inherited)
	}
}

// checkValues: value ids are unique within a concept, and one word does not
// name two values of it, in any language.
func (r *run) checkValues(f *File, c *Concept, label string) {
	ids := map[string]bool{}
	names := map[string]string{}
	for _, v := range c.Values {
		if ids[v.ID] {
			r.err(f, c.lineOf("values"), RuleDuplicateValue, "%s: value %s is declared twice", label, v.ID)
		}
		ids[v.ID] = true
		for _, word := range v.words(false) {
			key := lower(word)
			if other, taken := names[key]; taken && other != v.ID {
				r.err(f, c.lineOf("values"), RuleDuplicateValue, "%s: %q names both %s and %s", label, word, other, v.ID)
			}
			names[key] = v.ID
		}
	}
}

func sortedKeys[V any](m map[string]V) []string { return slices.Sorted(maps.Keys(m)) }
