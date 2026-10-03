package meaning

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/meaninggraph/cli/internal/memfs"
)

// permissive accepts every document, so that a test can reach the rules that
// only matter once the schema has let something through.
type permissive struct{}

func (permissive) Validate(any) error { return nil }

const head = "format: meaning/draft-1\nid: demo\nname: Demo\ndescription: d\n"

// cn writes a concept: id, kind and the rest of its fields (starting with a comma).
func cn(id, kind, rest string) string {
	return fmt.Sprintf("  - {id: %s, kind: %s, labels: {en: %s}, description: d%s}\n", id, kind, id, rest)
}

func doc(concepts ...string) string { return head + "concepts:\n" + strings.Join(concepts, "") }

type testCase struct {
	name    string
	files   map[string]string
	checker Checker
	// rules are the rules of the findings expected, sorted; messages are
	// substrings that must each appear in some finding.
	rules    []string
	messages []string
}

func graphOf(t *testing.T, files map[string]string, address string) *Graph {
	t.Helper()
	g, err := LoadDir(memfs.New(files), "/g")
	if err != nil {
		t.Fatal(err)
	}
	g.Address = address
	return g
}

func ruleList(findings []Finding) []string {
	var rules []string
	for _, f := range findings {
		rules = append(rules, f.Rule)
	}
	slices.Sort(rules)
	return rules
}

func runCases(t *testing.T, cases []testCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			files := map[string]string{"/g/LICENSE": "x"}
			for name, content := range tc.files {
				files["/g/"+name] = content
			}
			findings := tc.checker.Check(graphOf(t, files, ""))
			if got := ruleList(findings); !slices.Equal(got, tc.rules) {
				t.Fatalf("rules = %v, want %v\n%v", got, tc.rules, findings)
			}
			for _, want := range tc.messages {
				if !slices.ContainsFunc(findings, func(f Finding) bool { return strings.Contains(f.Message, want) }) {
					t.Errorf("no finding contains %q:\n%v", want, findings)
				}
			}
		})
	}
}

// dependency is a graph other graphs reference, served by a resolver that
// ignores pins' content (as the CLI does).
func dependency(t *testing.T, address, text string) Resolver {
	t.Helper()
	g := graphOf(t, map[string]string{"/g/dep.meaning.yaml": text}, address)
	return func(repo, pin string) (*Graph, error) {
		if repo != address {
			return nil, errors.New("meaning://" + repo + " is not available")
		}
		return g, nil
	}
}

const pin = "?ref=cb97dbcd9e951b00e7d46cb2e0c4e120c24c8db7"

func TestCleanGraph(t *testing.T) {
	t.Parallel()
	currency := cn("currency", "entity", `, values: [{id: usd, labels: {en: US dollar}, aliases: {en: [USD]}, codes: {iso: USD}}, {id: eur, labels: {en: euro}, codes: {iso: EUR}}]`)
	runCases(t, []testCase{{
		name: "a graph that uses every relation",
		files: map[string]string{
			"a.meaning.yaml": doc(
				cn("country", "entity", `, values: [{id: us, labels: {en: United States, ru: США}, aliases: {en: [USA]}}, {id: no, labels: {en: Norway}}]`),
				currency,
				cn("invoice", "entity", ""),
				cn("big-invoice", "entity", ", extends: invoice"),
				cn("billing-country", "attribute", ", of: invoice, values-of: country"),
				cn("origin-country", "dimension", ", of: invoice, extends: billing-country"),
				cn("amount", "attribute", ", of: invoice, units-of: currency, unit: USD"),
				cn("euro-amount", "attribute", ", extends: amount, unit: euro"),
				cn("revenue", "measure", ", of: invoice, source: ws, measure: {formula: sum, aggregation: sum, inputs: [amount], dimensions: [billing-country, origin-country]}"),
				cn("margin", "measure", ", extends: revenue, measure: {formula: x, aggregation: none, inputs: [revenue]}"),
				cn("avg-revenue", "measure", ", extends: revenue, measure: {formula: x}"),
			) + "sources:\n  - {id: ws, provider: p, dataset: d}\n",
		},
	}})
}

func TestFileLevelFindings(t *testing.T) {
	t.Parallel()
	runCases(t, []testCase{
		{name: "no files", files: map[string]string{}, rules: []string{RuleNoFiles}},
		{name: "invalid YAML", files: map[string]string{"a.meaning.yaml": "a: b\n  c: d\n"}, rules: []string{RuleYAML}},
		{name: "schema violation", files: map[string]string{"a.meaning.yaml": doc(cn("a", "thing", ""))}, rules: []string{RuleSchema}, messages: []string{"schema: /concepts/0/kind"}},
		{name: "schema violation skips the cross-concept rules", files: map[string]string{"a.meaning.yaml": doc(cn("a", "thing", ", of: nowhere"))}, rules: []string{RuleSchema}},
		{name: "duplicate concept", files: map[string]string{
			"a.meaning.yaml": doc(cn("a", "entity", "")),
			"b.meaning.yaml": doc(cn("a", "entity", "")),
		}, rules: []string{RuleDuplicateConcept}, messages: []string{"concept a is declared twice (/g/a.meaning.yaml and /g/b.meaning.yaml)"}},
		{name: "duplicate source", files: map[string]string{"a.meaning.yaml": doc(cn("a", "entity", "")) + "sources:\n  - {id: s, provider: p, dataset: d}\n  - {id: s, provider: q, dataset: e}\n"}, rules: []string{RuleDuplicateSource}},
		{name: "undeclared source", files: map[string]string{"a.meaning.yaml": doc(cn("a", "entity", ", source: nope"))}, rules: []string{RuleSource}},
		{name: "a meaning file below the root is a warning", files: map[string]string{
			"a.meaning.yaml":                 doc(cn("a", "entity", "")),
			"more/deeper/extra.meaning.yaml": doc(cn("b", "entity", "")),
			".hidden/skipped.meaning.yaml":   "x",
			"node_modules/p/x.meaning.yaml":  "x",
			"more/notes.txt":                 "x",
		}, rules: []string{RuleSubdirectoryFile}, messages: []string{"are not read below the directory of a graph"}},
	})
}

func TestReferenceRules(t *testing.T) {
	t.Parallel()
	core := dependency(t, "github.com/org/core", doc(cn("customer", "entity", ""), cn("revenue", "measure", ", measure: {formula: x, aggregation: sum}"), cn("loop", "entity", ", extends: loop")))
	withCore := Checker{Resolve: core}
	runCases(t, []testCase{
		{name: "unknown bare concept", files: map[string]string{"a.meaning.yaml": doc(cn("a", "attribute", ", of: nowhere"))}, rules: []string{RuleUnknownConcept}, messages: []string{"concept nowhere is not declared in this repository"}},
		{name: "reference that is not a reference", files: map[string]string{"a.meaning.yaml": doc(cn("a", "attribute", ", of: Bad_Ref"))}, checker: Checker{Schema: permissive{}}, rules: []string{RuleReferenceSyntax}},
		{name: "kind of an entity from another graph", files: map[string]string{"a.meaning.yaml": doc(cn("customer", "entity", ", extends: 'meaning://github.com/org/core/customer"+pin+"'"))}, checker: withCore},
		{name: "no graph supplied", files: map[string]string{"a.meaning.yaml": doc(cn("customer", "entity", ", extends: 'meaning://github.com/org/core/customer"+pin+"'"))}, rules: []string{RuleUnresolved}, messages: []string{"meaning://github.com/org/core is not available: no graph was supplied"}},
		{name: "unknown concept in another graph", files: map[string]string{"a.meaning.yaml": doc(cn("client", "entity", ", extends: 'meaning://github.com/org/core/client"+pin+"'"))}, checker: withCore, rules: []string{RuleUnknownConcept}, messages: []string{"concept client does not exist in meaning://github.com/org/core"}},
		{name: "graph that is not available", files: map[string]string{"a.meaning.yaml": doc(cn("client", "entity", ", extends: 'meaning://github.com/org/other/client"+pin+"'"))}, checker: withCore, rules: []string{RuleUnresolved}},
		{name: "two pins", files: map[string]string{"a.meaning.yaml": doc(
			cn("customer", "entity", ", extends: 'meaning://github.com/org/core/customer"+pin+"'"),
			cn("customer2", "entity", ", extends: 'meaning://github.com/org/core/customer?ref=abc'"),
		)}, checker: withCore, rules: []string{RulePinMismatch}},
		{name: "pinned and unpinned", files: map[string]string{"a.meaning.yaml": doc(
			cn("customer", "entity", ", extends: 'meaning://github.com/org/core/customer"+pin+"'"),
			cn("customer2", "entity", ", extends: 'meaning://github.com/org/core/customer'"),
		)}, checker: withCore, rules: []string{RulePinMismatch}},
		{name: "a cycle through another graph", files: map[string]string{"a.meaning.yaml": doc(cn("mine", "entity", ", extends: 'meaning://github.com/org/core/loop"+pin+"'"))}, checker: withCore, rules: []string{RuleExtendsCycle}, messages: []string{"mine -> loop -> loop"}},
		{name: "a measure of another graph aggregates", files: map[string]string{"a.meaning.yaml": doc(cn("ratio", "measure", ", extends: 'meaning://github.com/org/core/revenue"+pin+"', measure: {formula: x, inputs: ['meaning://github.com/org/core/revenue"+pin+"']}"))}, checker: withCore, rules: []string{RuleRatio}, messages: []string{"sum is inherited from revenue, state aggregation: none"}},
	})
}

func TestSelfAddress(t *testing.T) {
	t.Parallel()
	files := map[string]string{"/g/a.meaning.yaml": doc(
		cn("person", "entity", ""),
		cn("employee", "entity", ", extends: 'meaning://github.com/org/me/person'"),
		cn("pinned", "entity", ", extends: 'meaning://github.com/org/me/person"+pin+"'"),
	)}
	got := Checker{}.Check(graphOf(t, files, "github.com/org/me"))
	if rules := ruleList(got); !slices.Equal(rules, []string{RuleSelfPin}) {
		t.Fatalf("rules = %v\n%v", rules, got)
	}
	// Without its address the graph cannot find itself.
	got = Checker{}.Check(graphOf(t, files, ""))
	if rules := ruleList(got); !slices.Equal(rules, []string{RulePinMismatch, RuleUnresolved, RuleUnresolved}) {
		t.Fatalf("rules = %v\n%v", rules, got)
	}
}

func TestKindRules(t *testing.T) {
	t.Parallel()
	runCases(t, []testCase{
		{name: "of names a measure", files: map[string]string{"a.meaning.yaml": doc(cn("m", "measure", ", measure: {formula: x, aggregation: sum}"), cn("a", "attribute", ", of: m"))}, rules: []string{RuleTargetKind}, messages: []string{"of names m, which is a measure, not an entity"}},
		{name: "an entity extends a measure", files: map[string]string{"a.meaning.yaml": doc(cn("m", "measure", ", measure: {formula: x}"), cn("e", "entity", ", extends: m"))}, rules: []string{RuleExtendsKind}, messages: []string{"an entity cannot extend m, which is a measure"}},
		{name: "an attribute extends an entity", files: map[string]string{"a.meaning.yaml": doc(cn("e", "entity", ""), cn("a", "attribute", ", extends: e"))}, rules: []string{RuleExtendsKind}, messages: []string{"an attribute may extend only"}},
		{name: "extends a cycle", files: map[string]string{"a.meaning.yaml": doc(cn("a", "entity", ", extends: b"), cn("b", "entity", ", extends: a"))}, rules: []string{RuleExtendsCycle, RuleExtendsCycle}, messages: []string{"(a -> b -> a)"}},
		{name: "values-of names a measure", files: map[string]string{"a.meaning.yaml": doc(cn("m", "measure", ", measure: {formula: x}"), cn("a", "attribute", ", values-of: m"))}, rules: []string{RuleTargetKind}},
		{name: "units-of names an attribute", files: map[string]string{"a.meaning.yaml": doc(cn("x", "attribute", ""), cn("a", "attribute", ", units-of: x"))}, rules: []string{RuleTargetKind}},
		{name: "inputs must be attributes or measures", files: map[string]string{"a.meaning.yaml": doc(cn("e", "entity", ""), cn("m", "measure", ", measure: {formula: x, inputs: [e]}"))}, rules: []string{RuleMeasureInput}},
		{name: "dimensions must be dimensions or attributes", files: map[string]string{"a.meaning.yaml": doc(cn("e", "entity", ""), cn("m", "measure", ", measure: {formula: x, dimensions: [e]}"))}, rules: []string{RuleMeasureDimension}},
		{name: "unknown input", files: map[string]string{"a.meaning.yaml": doc(cn("m", "measure", ", measure: {formula: x, inputs: [nope]}"))}, rules: []string{RuleUnknownConcept}},
	})
}

func TestNarrowingAndUnits(t *testing.T) {
	t.Parallel()
	entities := cn("country", "entity", ", values: [{id: us, labels: {en: US}}]") +
		cn("kind-of-country", "entity", ", extends: country") +
		cn("city", "entity", ", values: [{id: a, labels: {en: A}, codes: {c: X}}, {id: b, labels: {en: B}, codes: {c: X}}]") +
		cn("base", "attribute", ", values-of: country, units-of: country")
	runCases(t, []testCase{
		{name: "narrowing to a kind of the parent's entity", files: map[string]string{"a.meaning.yaml": doc(entities, cn("narrow", "attribute", ", extends: base, values-of: kind-of-country"))}},
		{name: "changing the parent's entity", files: map[string]string{"a.meaning.yaml": doc(entities, cn("wrong", "attribute", ", extends: base, values-of: city"))}, rules: []string{RuleNarrowing}, messages: []string{"values-of city is neither country nor a kind of it"}},
		{name: "changing units-of", files: map[string]string{"a.meaning.yaml": doc(entities, cn("wrong", "attribute", ", extends: base, units-of: city"))}, rules: []string{RuleNarrowing}},
		{name: "a parent with no values-of may be extended freely", files: map[string]string{"a.meaning.yaml": doc(entities, cn("plain", "attribute", ""), cn("free", "attribute", ", extends: plain, values-of: city"))}},
		{name: "a parent that does not resolve", files: map[string]string{"a.meaning.yaml": doc(entities, cn("lost", "attribute", ", extends: nope, values-of: city"))}, rules: []string{RuleUnknownConcept}},
		{name: "an inherited domain that does not resolve", files: map[string]string{"a.meaning.yaml": doc(entities, cn("bad-base", "attribute", ", values-of: nope"), cn("child", "attribute", ", extends: bad-base, values-of: city"))}, rules: []string{RuleUnknownConcept}},
		{name: "a values-of that does not resolve", files: map[string]string{"a.meaning.yaml": doc(entities, cn("child", "attribute", ", extends: base, values-of: nope"))}, rules: []string{RuleUnknownConcept}},
		{name: "unit names no value", files: map[string]string{"a.meaning.yaml": doc(entities, cn("amount", "attribute", ", units-of: country, unit: nowhere"))}, rules: []string{RuleUnit}, messages: []string{"but names none"}},
		{name: "unit names two values", files: map[string]string{"a.meaning.yaml": doc(entities, cn("amount", "attribute", ", units-of: city, unit: X"))}, rules: []string{RuleUnit}, messages: []string{"but names a, b"}},
		{name: "unit is matched ignoring case", files: map[string]string{"a.meaning.yaml": doc(entities, cn("amount", "attribute", ", units-of: country, unit: uS"))}},
		{name: "a unit without units-of is free text", files: map[string]string{"a.meaning.yaml": doc(entities, cn("amount", "attribute", ", unit: dollars"))}},
		{name: "units-of that does not resolve", files: map[string]string{"a.meaning.yaml": doc(entities, cn("amount", "attribute", ", units-of: nope, unit: x"))}, rules: []string{RuleUnknownConcept}},
	})
}

func TestMeasureRules(t *testing.T) {
	t.Parallel()
	base := cn("revenue", "measure", ", measure: {formula: x, aggregation: sum}") + cn("share", "measure", ", measure: {formula: x, aggregation: none, inputs: [revenue]}")
	runCases(t, []testCase{
		{name: "a ratio that sums", files: map[string]string{"a.meaning.yaml": doc(base, cn("bad", "measure", ", measure: {formula: x, aggregation: sum, inputs: [revenue]}"))}, rules: []string{RuleRatio}},
		{name: "a ratio that counts", files: map[string]string{"a.meaning.yaml": doc(base, cn("bad", "measure", ", measure: {formula: x, aggregation: count, inputs: [revenue]}"))}, rules: []string{RuleRatio}},
		{name: "a ratio that averages", files: map[string]string{"a.meaning.yaml": doc(base, cn("bad", "measure", ", measure: {formula: x, aggregation: average, inputs: [revenue]}"))}, rules: []string{RuleRatio}},
		{name: "a ratio may take min and max", files: map[string]string{"a.meaning.yaml": doc(base, cn("ok", "measure", ", measure: {formula: x, aggregation: max, inputs: [revenue]}"))}},
		{name: "a ratio inherits a sum", files: map[string]string{"a.meaning.yaml": doc(base, cn("bad", "measure", ", extends: revenue, measure: {formula: x, inputs: [revenue]}"))}, rules: []string{RuleRatio}, messages: []string{"sum is inherited from revenue"}},
		{name: "a kind of a ratio inherits its input", files: map[string]string{"a.meaning.yaml": doc(base, cn("sum-share", "measure", ", extends: share, measure: {formula: x, aggregation: sum}"))}, rules: []string{RuleRatio}},
		{name: "a kind of a summing measure that is no ratio sums", files: map[string]string{"a.meaning.yaml": doc(base, cn("online", "measure", ", extends: revenue, measure: {formula: x}"))}},
		{name: "a measure without aggregation means none", files: map[string]string{"a.meaning.yaml": doc(cn("m", "measure", ", measure: {formula: x}"))}},
		{name: "a cycle of measures does not loop", files: map[string]string{"a.meaning.yaml": doc(
			cn("m1", "measure", ", extends: m2, measure: {formula: x, aggregation: sum}"),
			cn("m2", "measure", ", extends: m1, measure: {formula: x}"))}, rules: []string{RuleExtendsCycle, RuleExtendsCycle}},
		{name: "a measure that extends an entity", files: map[string]string{"a.meaning.yaml": doc(
			cn("e", "entity", ""),
			cn("m", "measure", ", extends: e, measure: {formula: x, aggregation: sum}"))}, rules: []string{RuleExtendsKind}},
		{name: "a ratio input that is an attribute is no ratio", files: map[string]string{"a.meaning.yaml": doc(cn("a", "attribute", ""), cn("m", "measure", ", measure: {formula: x, aggregation: sum, inputs: [a]}"))}},
	})
}

func TestValueRules(t *testing.T) {
	t.Parallel()
	runCases(t, []testCase{
		{name: "value declared twice", files: map[string]string{"a.meaning.yaml": doc(cn("c", "entity", ", values: [{id: a, labels: {en: A}}, {id: a, labels: {en: B}}]"))}, rules: []string{RuleDuplicateValue}},
		{name: "a word naming two values", files: map[string]string{"a.meaning.yaml": doc(cn("c", "entity", ", values: [{id: a, labels: {en: A}, aliases: {en: [Shared]}}, {id: b, labels: {en: B}, aliases: {en: [shared]}}]"))}, rules: []string{RuleDuplicateValue}, messages: []string{`"shared" names both a and b`}},
		{name: "across languages", files: map[string]string{"a.meaning.yaml": doc(cn("c", "entity", ", values: [{id: a, labels: {en: A}}, {id: b, labels: {en: B, ru: a}}]"))}, rules: []string{RuleDuplicateValue}},
		{name: "a value may repeat its own word", files: map[string]string{"a.meaning.yaml": doc(cn("c", "entity", ", values: [{id: a, labels: {en: A, ru: A}, aliases: {en: [a]}}]"))}},
		{name: "case folding follows Unicode, final sigma included", files: map[string]string{"a.meaning.yaml": doc(cn("c", "entity", ", values: [{id: a, labels: {en: ΟΔΟΣ}}, {id: b, labels: {en: οδος}}]"))}, rules: []string{RuleDuplicateValue}},
		{name: "a medial sigma is not a final sigma", files: map[string]string{"a.meaning.yaml": doc(cn("c", "entity", ", values: [{id: a, labels: {en: ΟΔΟΣ}}, {id: b, labels: {en: οδοσ}}]"))}},
	})
}

func TestGraphResolver(t *testing.T) {
	t.Parallel()
	good := graphOf(t, map[string]string{"/g/a.meaning.yaml": doc(cn("a", "entity", ""))}, "github.com/org/dep")
	bad := graphOf(t, map[string]string{"/g/a.meaning.yaml": "a: b\n  c: d\n"}, "github.com/org/bad")
	resolve := GraphResolver(map[string]*Graph{"github.com/org/dep": good, "github.com/org/bad": bad})
	if g, err := resolve("github.com/org/dep", "abc"); err != nil || g != good {
		t.Fatalf("resolve = %v, %v", g, err)
	}
	for _, tc := range []struct{ repo, pin, want string }{
		{"github.com/org/nope", "abc", "no local copy"},
		{"github.com/org/dep", "", "needs a ?ref= pin"},
		{"github.com/org/bad", "abc", "cannot be read: /g/a.meaning.yaml: a colon followed by a space"},
	} {
		if _, err := resolve(tc.repo, tc.pin); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("resolve(%s, %s) error = %v, want %q", tc.repo, tc.pin, err, tc.want)
		}
	}
}

func TestLoadDirAndLoadFiles(t *testing.T) {
	t.Parallel()
	fsys := memfs.New(map[string]string{
		"/g/b.meaning.yaml":     doc(cn("b", "entity", "")),
		"/g/a.meaning.yaml":     doc(cn("a", "entity", "")),
		"/g/notes.txt":          "x",
		"/g/sub/c.meaning.yaml": doc(cn("c", "entity", "")),
	})
	g, err := LoadDir(fsys, "/g")
	if err != nil || len(g.Files) != 2 || g.Files[0].Path != "/g/a.meaning.yaml" || g.Dir != "/g" || g.hasLicense {
		t.Fatalf("LoadDir = %+v, %v", g, err)
	}
	if _, err := LoadDir(fsys, "/missing"); err == nil {
		t.Fatal("a missing directory is an error")
	}
	if _, err := LoadFiles(fsys, []string{"/g/a.meaning.yaml", "/g/missing.meaning.yaml"}); err == nil {
		t.Fatal("a missing file is an error")
	}
	both, err := LoadFiles(fsys, []string{"/g/b.meaning.yaml", "/g/sub/c.meaning.yaml"})
	if err != nil || len(both.Concepts) != 2 || both.Dir != "" {
		t.Fatalf("LoadFiles = %+v, %v", both, err)
	}
}

func TestParseConceptRef(t *testing.T) {
	t.Parallel()
	for ref, want := range map[string]ConceptRef{
		"country":         {ID: "country"},
		"billing-country": {ID: "billing-country"},
		"meaning://github.com/meaninggraph/core/country":   {Repo: "github.com/meaninggraph/core", ID: "country"},
		"meaning://github.com/a/b/c/d/country?ref=abc.1/x": {Repo: "github.com/a/b/c/d", ID: "country", Pin: "abc.1/x"},
	} {
		if got, ok := ParseConceptRef(ref); !ok || got != want {
			t.Errorf("ParseConceptRef(%q) = %+v, %v", ref, got, ok)
		}
	}
	for _, ref := range []string{"", "Country", "line-2", "meaning://country", "meaning://github.com/country", "meaning://github.com/a/b/country?ref=", "http://x/y/z/a"} {
		if _, ok := ParseConceptRef(ref); ok {
			t.Errorf("ParseConceptRef(%q) must fail", ref)
		}
	}
}

func TestLowerFollowsJavaScript(t *testing.T) {
	t.Parallel()
	// Computed with Node: "ΟΔΟΣ".toLowerCase() === "οδος", "İ".toLowerCase() === "i\u0307".
	if lower("ΟΔΟΣ") != "οδος" || lower("İ") != "i\u0307" || lower("ÀB") != "àb" {
		t.Fatalf("lower: %q %q", lower("ΟΔΟΣ"), lower("İ"))
	}
	if an("entity") != "an entity" || an("measure") != "a measure" || an("") != "a " {
		t.Fatal("an")
	}
}

func TestUniversalProfile(t *testing.T) {
	t.Parallel()
	universal := Checker{Profile: ProfileUniversal}
	cc0 := func(concepts ...string) string {
		return head + "license: CC0-1.0\nconcepts:\n" + strings.Join(concepts, "")
	}
	runCases(t, []testCase{
		{name: "a clean universal graph", checker: universal, files: map[string]string{"a.meaning.yaml": cc0(cn("person", "entity", ", synonyms: {en: [human]}"), cn("employee", "entity", ", extends: person, synonyms: {en: [human, staff]}"))}},
		{name: "licence missing", checker: universal, files: map[string]string{"a.meaning.yaml": doc(cn("a", "entity", ""))}, rules: []string{RuleUniversal}, messages: []string{"license must be CC0-1.0"}},
		{name: "licence wrong", checker: universal, files: map[string]string{"a.meaning.yaml": head + "license: MIT\nconcepts:\n" + cn("a", "entity", "")}, rules: []string{RuleUniversal}},
		{name: "models", checker: universal, files: map[string]string{"a.meaning.yaml": head + "license: CC0-1.0\nmodels: {x: x.hcl}\nconcepts:\n" + cn("a", "entity", "")}, rules: []string{RuleModels, RuleUniversal}},
		{name: "bindings", checker: universal, files: map[string]string{"a.meaning.yaml": cc0(cn("a", "entity", ", bindings: [{model: 'modelspec:///x.A', role: entity}]"))}, rules: []string{RuleBindingModel, RuleUniversal}, messages: []string{"concept a: bindings belong"}},
		{name: "a meaning file below the root is an error", checker: universal, files: map[string]string{
			"a.meaning.yaml":      cc0(cn("a", "entity", "")),
			"more/b.meaning.yaml": cc0(cn("b", "entity", "")),
		}, rules: []string{RuleSubdirectoryFile}},
		{name: "a file that is not YAML is skipped", checker: universal, files: map[string]string{"a.meaning.yaml": "a: b\n  c: d\n"}, rules: []string{RuleYAML}},
		{name: "one word names two concepts", checker: universal, files: map[string]string{"a.meaning.yaml": cc0(cn("person", "entity", ", synonyms: {en: [People]}"), cn("population", "measure", ", measure: {formula: x}, synonyms: {en: [people]}"))}, rules: []string{RuleAmbiguousWord}, messages: []string{`"people" (en) is also a word of concept person`}},
		{name: "a language with synonyms but no label counts", checker: universal, files: map[string]string{"a.meaning.yaml": cc0(cn("a", "entity", ", synonyms: {de: [Leute]}"), cn("b", "entity", ", synonyms: {de: [leute]}"))}, rules: []string{RuleAmbiguousWord}},
		{name: "the same word in two languages is two words", checker: universal, files: map[string]string{"a.meaning.yaml": cc0(cn("a", "entity", ", synonyms: {de: [Rat]}"), cn("b", "entity", ", synonyms: {fr: [rat]}"))}},
		{name: "a concept may repeat its own word", checker: universal, files: map[string]string{"a.meaning.yaml": cc0(cn("a", "entity", ", synonyms: {en: [a, A]}"))}},
		{name: "a duplicate declaration is reported once", checker: universal, files: map[string]string{"a.meaning.yaml": cc0(cn("a", "entity", ""), cn("a", "entity", ""))}, rules: []string{RuleDuplicateConcept}},
	})
	// A graph read from a list of files has no directory: no LICENSE rule applies.
	fsys := memfs.New(map[string]string{"/g/a.meaning.yaml": cc0(cn("a", "entity", ""))})
	g, err := LoadFiles(fsys, []string{"/g/a.meaning.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if got := universal.Check(g); len(got) != 0 {
		t.Fatalf("findings = %v", got)
	}
	// Without a LICENSE in the directory the rule fires.
	dir, err := LoadDir(memfs.New(map[string]string{"/d/a.meaning.yaml": cc0(cn("a", "entity", ""))}), "/d")
	if err != nil {
		t.Fatal(err)
	}
	if got := universal.Check(dir); len(got) != 1 || got[0].Message != "LICENSE is missing" {
		t.Fatalf("findings = %v", got)
	}
}

type brokenDirs struct{ FS }

func (brokenDirs) ReadDir(string) ([]fs.DirEntry, error) { return nil, errors.New("denied") }

func TestUnreadableSubdirectoriesAreReported(t *testing.T) {
	t.Parallel()
	found, unreadable := subdirectoryFiles(brokenDirs{}, "/g", "")
	if found != nil || len(unreadable) != 1 || unreadable[0] != "/g" {
		t.Fatalf("got %v, %v", found, unreadable)
	}
}

type brokenFiles struct{ FS }

func (brokenFiles) ReadFile(string) ([]byte, error) { return nil, errors.New("denied") }

func TestLoadDirReportsAFileThatCannotBeRead(t *testing.T) {
	t.Parallel()
	fsys := brokenFiles{memfs.New(map[string]string{"/g/a.meaning.yaml": "x"})}
	if _, err := LoadDir(fsys, "/g"); err == nil {
		t.Fatal("an unreadable file is an error")
	}
}

func TestGraphResolverRefusesASuppliedGraphThatIsNotValid(t *testing.T) {
	t.Parallel()
	bad := map[string]string{
		"unknown kind":      doc(cn("a", "thing", "")),
		"a null concept":    head + "concepts:\n  - ~\n",
		"a number label":    head + "concepts:\n  - {id: a, kind: entity, labels: {en: 5}, description: d}\n",
		"missing format":    strings.Replace(doc(cn("a", "entity", "")), "format: meaning/draft-1\n", "", 1),
		"an unknown key":    doc(cn("a", "entity", ", colour: red")),
		"an unreadable one": "a: b\n  c: d\n",
	}
	for name, text := range bad {
		g := graphOf(t, map[string]string{"/g/a.meaning.yaml": text}, "github.com/org/dep")
		resolve := GraphResolver(map[string]*Graph{"github.com/org/dep": g})
		for range 2 { // the second call is answered from what the first found
			if _, err := resolve("github.com/org/dep", "abc"); err == nil || !strings.Contains(err.Error(), "cannot be read: /g/a.meaning.yaml: ") {
				t.Errorf("%s: error = %v", name, err)
			}
		}
	}
	good := graphOf(t, map[string]string{"/g/a.meaning.yaml": doc(cn("a", "entity", ""))}, "github.com/org/dep")
	if _, err := GraphResolver(map[string]*Graph{"github.com/org/dep": good})("github.com/org/dep", "abc"); err != nil {
		t.Fatal(err)
	}
}

func chainOf(n int, extra string) string {
	var sb strings.Builder
	sb.WriteString(head + "concepts:\n")
	sb.WriteString("  - {id: c0, kind: entity, labels: {en: c0}, description: d}\n")
	for i := 1; i < n; i++ {
		fmt.Fprintf(&sb, "  - {id: c%d, kind: entity, labels: {en: c%d}, description: d, extends: c%d}\n", i, i, i-1)
	}
	sb.WriteString(extra)
	return sb.String()
}

func TestLongExtendsChainsAreCheckedInLinearTime(t *testing.T) {
	t.Parallel()
	start := time.Now()
	files := map[string]string{"/g/a.meaning.yaml": chainOf(5000, "")}
	if got := (Checker{}).Check(graphOf(t, files, "")); len(got) != 0 {
		t.Fatalf("findings = %v", got[:min(3, len(got))])
	}
	// The same chain closed into a cycle: every concept reports it, in a short message.
	files["/g/a.meaning.yaml"] = strings.Replace(chainOf(5000, ""), "  - {id: c0, kind: entity, labels: {en: c0}, description: d}", "  - {id: c0, kind: entity, labels: {en: c0}, description: d, extends: c4999}", 1)
	got := (Checker{}).Check(graphOf(t, files, ""))
	if len(got) != 5000 || got[0].Rule != RuleExtendsCycle || len(got[0].Message) > 300 || !strings.Contains(got[0].Message, " -> ... -> ") {
		t.Fatalf("%d findings, first %+v", len(got), got[0])
	}
	if took := time.Since(start); took > 20*time.Second {
		t.Fatalf("5,000-concept chains took %v; checking them must not be quadratic or worse", took)
	}
}

func TestCyclesAreReportedFromEveryConceptThatLeadsIn(t *testing.T) {
	t.Parallel()
	runCases(t, []testCase{
		{name: "a chain that runs into a cycle", files: map[string]string{"a.meaning.yaml": doc(
			cn("tail", "entity", ", extends: b"), cn("b", "entity", ", extends: c"), cn("c", "entity", ", extends: b"))},
			rules: []string{RuleExtendsCycle, RuleExtendsCycle, RuleExtendsCycle}, messages: []string{"(tail -> b -> c -> b)", "(b -> c -> b)", "(c -> b -> c)"}},
		{name: "a child declared before its parent", files: map[string]string{"a.meaning.yaml": doc(
			cn("child", "entity", ", extends: parent"), cn("parent", "entity", ", extends: root"), cn("root", "entity", ""))}},
		{name: "a parent declared before its child", files: map[string]string{"a.meaning.yaml": doc(
			cn("root", "entity", ""), cn("parent", "entity", ", extends: root"), cn("child", "entity", ", extends: parent"))}},
		{name: "a concept that extends itself", files: map[string]string{"a.meaning.yaml": doc(cn("a", "entity", ", extends: a"))}, rules: []string{RuleExtendsCycle}, messages: []string{"(a -> a)"}},
	})
}

func TestCaseFoldingFollowsTheReferenceChecker(t *testing.T) {
	t.Parallel()
	// Unicode 16 case pairs, which the reference checker's Node folds: a value
	// that has one of the pair as a label and another that has the other.
	for _, pair := range [][2]string{{`Ɤ`, `ɤ`}, {`Ꟍ`, `ꟍ`}, {`Ꟛ`, `ꟛ`}, {`\U00010D50`, `\U00010D70`}} {
		runCases(t, []testCase{{
			name:  pair[0] + " and " + pair[1],
			files: map[string]string{"a.meaning.yaml": doc(cn("c", "entity", `, values: [{id: v1, labels: {en: "`+pair[0]+`"}}, {id: v2, labels: {en: "`+pair[1]+`"}}]`))},
			rules: []string{RuleDuplicateValue}, messages: []string{"names both v1 and v2"},
		}})
	}
}

func TestSymbolicLinksAndUnreadableDirectoriesAreReported(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"/g/a.meaning.yaml":     doc(cn("a", "entity", "")),
		"/g/link.meaning.yaml":  "x",
		"/g/sub/b.meaning.yaml": doc(cn("b", "entity", "")),
	}
	fsys := dirFails{FS: memfs.New(files, "/g/link.meaning.yaml"), fail: "/g/sub"}
	g, err := LoadDir(fsys, "/g")
	if err != nil {
		t.Fatal(err)
	}
	got := Checker{}.Check(g)
	if rules := ruleList(got); !slices.Equal(rules, []string{RuleSymlink, RuleUnreadableDir}) {
		t.Fatalf("findings = %v", got)
	}
	for _, f := range got {
		if f.Severity != Warning {
			t.Errorf("%+v must be a warning", f)
		}
	}
	// Under the universal profile an unreadable directory is an error, as in the reference checker.
	g.hasLicense = true
	g.Files[0].Root.Fields["license"] = &Node{Kind: String, Text: "CC0-1.0"}
	g.Files[0].License = "CC0-1.0"
	var severity Severity
	for _, f := range (Checker{Profile: ProfileUniversal}).Check(g) {
		if f.Rule == RuleUnreadableDir {
			severity = f.Severity
		}
	}
	if severity != Error {
		t.Fatalf("severity = %q", severity)
	}
	// Only links: nothing to check, and the finding says there is nothing.
	only := dirFails{FS: memfs.New(map[string]string{"/g/link.meaning.yaml": "x"}, "/g/link.meaning.yaml")}
	g, err = LoadDir(only, "/g")
	if err != nil {
		t.Fatal(err)
	}
	if rules := ruleList(Checker{}.Check(g)); !slices.Equal(rules, []string{RuleNoFiles, RuleSymlink}) {
		t.Fatalf("rules = %v", rules)
	}
}

// dirFails makes the listing of one directory fail.
type dirFails struct {
	FS
	fail string
}

func (d dirFails) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == d.fail {
		return nil, errors.New("denied")
	}
	return d.FS.ReadDir(name)
}

func TestAGraphWithoutAFileSystemCannotReadModels(t *testing.T) {
	t.Parallel()
	files := map[string]string{"/g/a.meaning.yaml": head + "models: {m: m.hcl}\nconcepts:\n" + cn("a", "entity", "")}
	g := graphOf(t, files, "")
	g.fs, g.Dir = nil, ""
	got := Checker{}.Check(g)
	if rules := ruleList(got); !slices.Equal(rules, []string{RuleModels}) || !strings.Contains(got[0].Message, "was not loaded from a file system") {
		t.Fatalf("findings = %v", got)
	}
}
