package meaning

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Rule identifiers of findings. They are stable: scripts may match on them.
const (
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
	RuleEarlierSpelling  = "deprecated-spelling"
	RuleBindingModel     = "binding-model"
	RuleBindingRole      = "binding-role"
	RuleUniversal        = "universal"
	RuleSymlink          = "symlink-meaning-file"
	RuleUnreadableDir    = "unreadable-directory"
	RuleFileTooLarge     = "file-too-large"
)

// Resolver gives the graph a meaning:// reference names: repo is
// {host}/{org}/{repo} and pin is the ?ref= value (empty when there is none).
// An error says why the graph is not available. Nothing in this package
// fetches anything: a Resolver reads what the caller has at hand.
type Resolver func(repo, pin string) (*Graph, error)

// GraphResolver resolves references from graphs the caller already has, keyed
// by address. It follows the reference checker's rules for a repository it
// reads from git: a reference to another repository must carry a ?ref= pin,
// and a repository that cannot be read is reported as such. A supplied graph
// must itself be valid: a file that is not YAML in the subset, or that breaks
// the schema, makes the graph unreadable. The pin cannot be verified against
// a directory here; see VerifyPin.
//
// A graph's own address (Graph.Address) must be set for references to
// meaning://<its address>/... made from inside that graph to resolve to itself;
// GraphResolver does not set it. The resolver is not safe for concurrent use.
func GraphResolver(graphs map[string]*Graph) Resolver {
	validated := map[string]error{}
	return func(repo, pin string) (*Graph, error) {
		g, ok := graphs[repo]
		switch {
		case !ok:
			return nil, fmt.Errorf("meaning://%s is not available: no local copy of it was supplied, and nothing is fetched", repo)
		case pin == "":
			return nil, fmt.Errorf("meaning://%s needs a ?ref= pin", repo)
		}
		problem, done := validated[repo]
		if !done {
			problem = unreadable(g)
			validated[repo] = problem
		}
		if problem != nil {
			return nil, &unreadableGraphError{repo: repo, pin: pin, problem: problem}
		}
		return g, nil
	}
}

// unreadableGraphError says that a supplied graph is not valid. Every
// reference to it fails the same way, so the check reports it once.
type unreadableGraphError struct {
	repo, pin string
	problem   error
}

func (e *unreadableGraphError) Error() string {
	return fmt.Sprintf("meaning://%s?ref=%s cannot be read: %v", e.repo, e.pin, e.problem)
}

// unreadable says why a supplied graph cannot be used: its first file that is
// not valid.
func unreadable(g *Graph) error {
	for _, f := range g.Files {
		if f.ParseErr != nil {
			return fmt.Errorf("%s: %s", f.Path, f.ParseErr.Message)
		}
		if problems := fileSchemaProblems(f, DefaultSchema(), Draft2Schema()); len(problems) > 0 {
			return fmt.Errorf("%s: schema: %s", f.Path, problems[0])
		}
	}
	return nil
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
	// Schema validates each file of Draft1 (and a file that is not a mapping);
	// the embedded schema of Draft1 when nil.
	Schema Validator
	// SchemaDraft2 validates each file of Draft2; the embedded schema of Draft2
	// when nil.
	SchemaDraft2 Validator
	// Resolve reads other graphs; when nil none is available.
	Resolve Resolver
	// Models reads the models bindings name; HCLReader when nil.
	Models ModelReader
	// Profile selects extra rules.
	Profile Profile
	// Noticed holds the model files already reported for an earlier ModelSpec
	// spelling, by path as written. A caller that checks several graphs in one
	// run shares one map between their Checkers, so that a model file listed by
	// more than one graph is reported once; when nil, the check keeps its own.
	Noticed map[string]bool
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
	// valueIndex holds the indexes of valuesNamed.
	valueIndex map[*Concept]map[string][]Value
	// steps counts the lookups of what a concept extends: a test reads it to show
	// that the work grows in step with the number of concepts.
	steps int
	// ambiguous counts the words reported as naming two concepts; see MaxAmbiguousWords.
	ambiguous int
	// unreadable holds the supplied graphs that were reported as not valid.
	unreadable map[string]bool
	parents    map[*Concept]parentEntry
	chains     map[*Concept]chainInfo
	// loaded holds the files that passed the schema, with their models, for the
	// links of the graph.
	loaded []loadedFile
}

// Check validates every file of g against the schema and checks what the
// schema cannot: references resolve, extends joins compatible kinds without a
// cycle, values-of and units-of name entities, measures and ratios are
// consistent, and ids and words are unique. The findings are sorted.
func (c Checker) Check(g *Graph) []Finding {
	r := c.check(g)
	SortFindings(r.findings)
	return r.findings
}

// CheckLinks checks g like Check and also returns the links of the graph, the
// written and the derived ones (see Link). The links are nil when the check
// finds an error, because a written line that contradicts the model makes the
// derived links unreliable: they are not defined for a graph that fails its
// check, and a reader must not show them. A graph that passes and has no links
// has an empty, non-nil list.
func (c Checker) CheckLinks(g *Graph) ([]Finding, []Link) {
	r := c.check(g)
	SortFindings(r.findings)
	if HasErrors(r.findings) {
		return r.findings, nil
	}
	return r.findings, deriveLinks(r.loaded)
}

// check runs the checks and returns the run that holds the findings (unsorted)
// and the counts the tests read.
func (c Checker) check(g *Graph) *run {
	if c.Models == nil {
		c.Models = HCLReader{}
	}
	if c.Noticed == nil {
		c.Noticed = map[string]bool{}
	}
	r := &run{c: c, local: g, other: c.Resolve, pins: map[string]string{}, unreadable: map[string]bool{}, valueIndex: map[*Concept]map[string][]Value{}, parents: map[*Concept]parentEntry{}, chains: map[*Concept]chainInfo{}}
	if r.other == nil {
		r.other = func(repo, _ string) (*Graph, error) {
			return nil, fmt.Errorf("meaning://%s is not available: no graph was supplied", repo)
		}
	}
	if len(g.Files) == 0 {
		r.add(g.Dir, 0, RuleNoFiles, Error, "no %s file", "*"+FileSuffix)
	}
	if formats, firstFile := formatsOf(g); len(formats) > 1 {
		// The files of one graph refer to each other by bare id, so they change
		// format together.
		second := firstFile[formats[1]]
		r.err(second, second.Root.Field("format").lineOr(1), RuleFormatMixed, "%s", formatMixed(formats, firstFile))
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
	return r
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
		r.err(f, f.ParseErr.Line, f.ParseErr.Rule, "%s", f.ParseErr.Message)
		return
	}
	if f.Format == Draft1 {
		r.add(f.Path, f.Root.Field("format").lineOr(1), RuleEarlierFormat, Warning,
			"the file is in %s, the earlier format; it is read in full. Write %s with meaninggraph rewrite --write (a file made by a generator is changed in its generator)", Draft1, Draft2)
	}
	problems := fileSchemaProblems(f, r.c.schemaFor(Draft1), r.c.schemaFor(Draft2))
	for _, p := range problems {
		r.err(f, f.Root.At(p.path), RuleSchema, "schema: %s", p)
	}
	if KnownFormat(f.Format) {
		for _, w := range formatWords(f) {
			r.err(f, w.line, RuleFormatWord, "%s", w.message)
		}
	}
	if len(problems) > 0 {
		return
	}
	if names, line := earlierRoleNames(f); f.Format == Draft2 && len(names) > 0 {
		r.add(f.Path, line, RuleEarlierRoleName, Warning,
			"the role names entity and foreign-key are the earlier spellings of instances and reference (this file uses %s)", strings.Join(names, " and "))
	}
	seen := map[string]bool{}
	for _, s := range f.Sources {
		if seen[s.ID] {
			r.err(f, s.Line, RuleDuplicateSource, "source %s is declared twice", s.ID)
		}
		seen[s.ID] = true
	}
	models, failed := r.loadModels(f)
	r.loaded = append(r.loaded, loadedFile{file: f, models: models})
	for _, c := range f.Concepts {
		r.checkConcept(f, c, seen)
		r.checkBindings(f, c, "concept "+c.ID, models, failed)
	}
}

func (r *run) checkConcept(f *File, c *Concept, sources map[string]bool) {
	label := "concept " + c.ID
	if c.Of != "" {
		if owner, ok := r.lookup(f, c.lineOf("of"), label+" of", c.Of); ok && !isEntityLike(owner.concept) {
			r.err(f, c.lineOf("of"), RuleTargetKind, "%s: of names %s, which is %s, not %s", label, c.Of, an(owner.concept.Kind), entityLikeWords(c.format))
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
		if found, ok := r.lookup(f, c.lineOf(key), label+" "+key, target); ok && !isEntityLike(found.concept) {
			r.err(f, c.lineOf(key), RuleTargetKind, "%s: %s names %s, which is %s, not %s", label, key, target, an(found.concept.Kind), entityLikeWords(c.format))
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
		allowed := extendsKinds[kindOf(c)]
		if !contains(allowed, kindOf(parent.concept)) {
			r.err(f, line, RuleExtendsKind, "%s: %s cannot extend %s, which is %s; extends means \"is a kind of\", and %s may extend only %s",
				label, an(c.Kind), c.Extends, an(parent.concept.Kind), an(c.Kind), strings.Join(wordsOf(allowed, c.format), " or "))
		}
	}
	// The chain of extends, through any graph, comes back to a concept it has passed.
	if path, cyclic := r.extendsCycle(node{concept: c, graph: r.local}); cyclic {
		r.err(f, line, RuleExtendsCycle, "%s: extends forms a cycle (%s)", label, path)
	}
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
	named := r.valuesNamed(entity.concept)[lower(c.Unit)]
	switch {
	case len(named) == 1 && named[0].Retired:
		r.add(f.Path, c.lineOf("unit"), RuleRetiredValue, Warning, "%s: unit %q names the value %s of %s, which is retired",
			label, c.Unit, named[0].ID, domain.concept.UnitsOf)
	case len(named) != 1:
		found := "none"
		if len(named) > 0 {
			ids := make([]string, len(named))
			for i, v := range named {
				ids[i] = v.ID
			}
			found = strings.Join(ids, ", ")
		}
		r.err(f, c.lineOf("unit"), RuleUnit, "%s: unit %q must name exactly one value of %s (units-of), but names %s",
			label, c.Unit, domain.concept.UnitsOf, found)
	}
}

// valuesNamed indexes the values of an entity concept by the lower-cased words
// (labels, aliases and codes) that name them, in the order of the values. It
// is built once per concept: every unit that names a value of it reads the
// index, so the work stays linear however many units there are.
func (r *run) valuesNamed(entity *Concept) map[string][]Value {
	if index, done := r.valueIndex[entity]; done {
		return index
	}
	index := map[string][]Value{}
	for _, v := range entity.Values {
		named := map[string]bool{}
		for _, w := range v.words(true) {
			if word := lower(w); !named[word] {
				named[word] = true
				index[word] = append(index[word], v)
			}
		}
	}
	r.valueIndex[entity] = index
	return index
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
		if in, ok := r.lookup(f, line, label+" measure.inputs", ref); ok && !contains(measureInputs, kindOf(in.concept)) {
			r.err(f, line, RuleMeasureInput, "%s: measure.inputs names %s, which is %s; a measure is computed from %s and measures only", label, ref, an(in.concept.Kind), pluralPropertyWord(c.format))
		}
	}
	for _, ref := range c.Measure.Dimensions {
		if d, ok := r.lookup(f, line, label+" measure.dimensions", ref); ok && !contains(measureDimensions, kindOf(d.concept)) {
			r.err(f, line, RuleMeasureDimension, "%s: measure.dimensions names %s, which is %s; a measure is grouped by dimensions or %s only", label, ref, an(d.concept.Kind), pluralPropertyWord(c.format))
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
