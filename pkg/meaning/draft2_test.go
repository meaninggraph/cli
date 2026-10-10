package meaning

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// The tests of meaning/draft-2: its schema, its words, the rules that name a
// kind, the findings that are about the two formats, and the links. The
// conformance cases of the reference checker are compared in internal/cli.

const head2 = "format: meaning/draft-2\nid: demo\nname: Demo\ndescription: d\n"

func doc2(concepts ...string) string { return head2 + "concepts:\n" + strings.Join(concepts, "") }

func bound2(concepts ...string) map[string]string {
	return map[string]string{"a.meaning.yaml": head2 + modelsLine + "concepts:\n" + strings.Join(concepts, "")}
}

// bind2 writes a binding in the words of meaning/draft-2: the key field.
func bind2(model, role, field string) string {
	out := fmt.Sprintf("{model: 'modelspec:///%s', role: %s", model, role)
	if field != "" {
		out += ", field: " + field
	}
	return out + "}"
}

// inG puts the files of a test in the directory /g.
func inG(files map[string]string) map[string]string {
	out := map[string]string{}
	for name, content := range files {
		out["/g/"+name] = content
	}
	return out
}

func values2(ids ...string) string {
	var out []string
	for _, id := range ids {
		out = append(out, fmt.Sprintf("{id: %s, labels: {en: %s}}", id, id))
	}
	return ", values: [" + strings.Join(out, ", ") + "]"
}

func TestTheEmbeddedSchemasAreTheOnesRecorded(t *testing.T) {
	t.Parallel()
	for _, format := range []string{Draft1, Draft2} {
		fields := map[string]string{}
		for _, line := range strings.Split(strings.TrimSpace(SchemaSourceFor(format)), "\n") {
			key, value, _ := strings.Cut(line, ": ")
			fields[key] = value
		}
		sum := sha256.Sum256(SchemaJSONFor(format))
		if got := hex.EncodeToString(sum[:]); got != fields["sha256"] {
			t.Errorf("%s: the embedded schema has sha256 %s, its source records %s", format, got, fields["sha256"])
		}
		if fields["repository"] != "https://github.com/meaninggraph/core" || SchemaCommitFor(format) != fields["commit"] || len(fields["commit"]) != 40 {
			t.Errorf("%s: source %v, commit %q", format, fields, SchemaCommitFor(format))
		}
	}
	if SchemaSourceFor(Draft1) != SchemaSource() || SchemaCommitFor(Draft1) != SchemaCommit() || !slices.Equal(SchemaJSONFor(Draft1), SchemaJSON()) {
		t.Error("the schema of Draft1 is the default one")
	}
	if got := SchemaJSONFor(Draft2); strings.Contains(string(got), `"const": "meaning/draft-1"`) || !strings.Contains(string(got), `"const": "meaning/draft-2"`) {
		t.Error("the schema of Draft2 is not for meaning/draft-2")
	}
	if SchemaJSONFor("meaning/draft-3") != nil || SchemaSourceFor("meaning/draft-3") != "" || SchemaCommitFor("meaning/draft-3") != "" {
		t.Error("a format that is not read has no schema")
	}
	SchemaJSONFor(Draft2)[0] = '!'
	if SchemaJSONFor(Draft2)[0] == '!' {
		t.Error("SchemaJSONFor must not hand out the embedded bytes")
	}
	if !KnownFormat(Draft1) || !KnownFormat(Draft2) || KnownFormat("") || KnownFormat("meaning/draft-3") {
		t.Error("KnownFormat")
	}
}

func TestTheSchemaOfDraft2(t *testing.T) {
	t.Parallel()
	schema := Draft2Schema()
	minimal := doc2(cn("thing", "entity", ""))
	if got := problemsOf(t, schema, minimal); len(got) != 0 {
		t.Fatalf("a minimal draft-2 file is refused: %v", got)
	}
	tests := map[string]struct{ text, want string }{
		"the earlier format":        {strings.Replace(minimal, "draft-2", "draft-1", 1), "/format"},
		"kind attribute":            {doc2(cn("a", "attribute", "")), "/concepts/0/kind"},
		"a value set needs values":  {doc2(cn("v", "value-set", "")), "values"},
		"values on an entity":       {doc2(cn("v", "entity", values2("a"))), "/concepts/0"},
		"complete on an entity":     {doc2(cn("v", "entity", ", complete: true")), "/concepts/0"},
		"binding key property":      {doc2(cn("a", "entity", ", bindings: [{model: 'modelspec:///m.A', role: identifier, property: Id}]")), "/concepts/0/bindings/0"},
		"retired is a boolean":      {doc2(cn("v", "value-set", ", values: [{id: a, labels: {en: a}, retired: soon}]")), "retired"},
		"a value set has no values": {doc2(cn("v", "value-set", ", values-of: x"+values2("a"))), "/concepts/0"},
	}
	for name, tc := range tests {
		if got := problemsOf(t, schema, tc.text); len(got) == 0 || !strings.Contains(strings.Join(got, "\n"), tc.want) {
			t.Errorf("%s: problems = %v, want one about %q", name, got, tc.want)
		}
	}
	for name, text := range map[string]string{
		"a complete value set":      doc2(cn("v", "value-set", ", complete: true"+values2("a", "b"))),
		"a retired value":           doc2(cn("v", "value-set", ", values: [{id: a, labels: {en: a}, retired: true}]")),
		"field and instances":       doc2(cn("a", "entity", ", bindings: [{model: 'modelspec:///m.A', role: instances}, {model: 'modelspec:///m.A', role: identifier, field: Id}]")),
		"the earlier role names":    doc2(cn("a", "entity", ", bindings: [{model: 'modelspec:///m.A', role: entity}, {model: 'modelspec:///m.A', role: foreign-key, field: Id}]")),
		"a property of a value set": doc2(cn("v", "value-set", values2("a")), cn("p", "property", ", values-of: v")),
	} {
		if got := problemsOf(t, schema, text); len(got) != 0 {
			t.Errorf("%s is refused: %v", name, got)
		}
	}
}

func TestFilesOfEitherFormatAreValidatedAgainstTheirOwnSchema(t *testing.T) {
	t.Parallel()
	d1 := cn("a", "attribute", "")
	d2 := cn("a", "property", "")
	runCases(t, []testCase{
		{name: "a draft-1 file", files: map[string]string{"a.meaning.yaml": doc(d1)}},
		{name: "a draft-2 file", files: map[string]string{"a.meaning.yaml": doc2(d2)}},
		{name: "kind property in draft 1", files: map[string]string{"a.meaning.yaml": doc(d2)}, rules: []string{RuleFormatWord, RuleSchema}, messages: []string{"concept a: format-word: kind property belongs to meaning/draft-2; in meaning/draft-1 it is written attribute"}},
		{name: "kind attribute in draft 2", files: map[string]string{"a.meaning.yaml": doc2(d1)}, rules: []string{RuleFormatWord, RuleSchema}, messages: []string{"concept a: format-word: kind attribute is written property in meaning/draft-2"}},
		{name: "kind value-set in draft 1", files: map[string]string{"a.meaning.yaml": doc(cn("v", "value-set", values2("a")))}, rules: []string{RuleFormatWord, RuleSchema}, messages: []string{"concept v: format-word: kind value-set belongs to meaning/draft-2"}},
		{name: "complete in draft 1", files: map[string]string{"a.meaning.yaml": doc(cn("e", "entity", ", complete: true"+values2("a")))}, rules: []string{RuleFormatWord, RuleSchema}, messages: []string{"concept e: format-word: complete belongs to meaning/draft-2"}},
		{name: "retired in draft 1", files: map[string]string{"a.meaning.yaml": doc(cn("e", "entity", ", values: [{id: a, labels: {en: a}}, {id: b, labels: {en: b}, retired: true}]"))}, rules: []string{RuleFormatWord, RuleSchema}, messages: []string{"concept e: value b: format-word: retired belongs to meaning/draft-2"}},
		{name: "values on an entity in draft 2", files: map[string]string{"a.meaning.yaml": doc2(cn("e", "entity", values2("a")))}, rules: []string{RuleFormatWord, RuleSchema}, messages: []string{"values belongs on a concept of kind value-set; name that concept with values-of"}},
		{name: "field in draft 1", files: map[string]string{"a.meaning.yaml": head + modelsLine + "concepts:\n" + cn("a", "entity", bindings(bind2("chinook.Artist", "identifier", "ArtistId")))}, rules: []string{RuleFormatWord, RuleSchema, RuleSchema}, messages: []string{"the binding key field belongs to meaning/draft-2; in meaning/draft-1 it is written property"}},
		{name: "property in draft 2", files: map[string]string{"a.meaning.yaml": head2 + modelsLine + "concepts:\n" + cn("a", "entity", bindings(bind("chinook.Artist", "identifier", "ArtistId")))}, rules: []string{RuleFormatWord, RuleSchema, RuleSchema}, messages: []string{"the binding key property is written field in meaning/draft-2"}},
		{name: "both keys in draft 2", files: map[string]string{"a.meaning.yaml": head2 + modelsLine + "concepts:\n" + cn("a", "entity", ", bindings: [{model: 'modelspec:///chinook.Artist', role: identifier, property: ArtistId, field: ArtistId}]")}, rules: []string{RuleFormatWord, RuleSchema}, messages: []string{"has both property: and field:; meaning/draft-2 writes field"}},
		{name: "both keys in draft 1", files: map[string]string{"a.meaning.yaml": head + modelsLine + "concepts:\n" + cn("a", "entity", ", bindings: [{model: 'modelspec:///chinook.Artist', role: identifier, property: ArtistId, field: ArtistId}]")}, rules: []string{RuleFormatWord, RuleSchema}, messages: []string{"has both property: and field:; meaning/draft-1 writes property"}},
	})
}

func TestAFileWithNoKnownFormatGetsTheOneFindingAndNothingElse(t *testing.T) {
	t.Parallel()
	defects := cn("thing", "entity", ", extends: ghost, source: undeclared")
	for name, text := range map[string]string{
		"another identifier":        strings.Replace(doc(defects), "draft-1", "draft-3", 1),
		"no format":                 strings.Replace(doc(defects), "format: meaning/draft-1\n", "", 1),
		"a format that is not text": strings.Replace(doc(defects), "meaning/draft-1", "[1]", 1),
	} {
		g := graphOf(t, map[string]string{"/g/a.meaning.yaml": text}, "")
		findings := Checker{}.Check(g)
		if len(findings) != 1 || findings[0].Rule != RuleSchema || findings[0].Message != "schema: /format must be meaning/draft-1 or meaning/draft-2" {
			t.Errorf("%s: %v", name, findings)
		}
		if g.Format() != "" {
			t.Errorf("%s: format %q", name, g.Format())
		}
	}
	// A file that is not a mapping is refused by the schema, as before.
	findings := Checker{}.Check(graphOf(t, map[string]string{"/g/a.meaning.yaml": "- a\n"}, ""))
	if len(findings) != 1 || findings[0].Rule != RuleSchema || !strings.Contains(findings[0].Message, "object") {
		t.Errorf("a list: %v", findings)
	}
	// Its concepts stay in the graph, so another file's reference to one resolves.
	g := graphOf(t, map[string]string{
		"/g/a.meaning.yaml": strings.Replace(doc(cn("thing", "entity", "")), "draft-1", "draft-3", 1),
		"/g/b.meaning.yaml": doc(cn("other", "entity", ", extends: thing")),
	}, "")
	if rules := ruleList(withoutEarlierFormat(Checker{}.Check(g))); !slices.Equal(rules, []string{RuleSchema}) {
		t.Errorf("rules %v", rules)
	}
}

func TestEarlierFormat(t *testing.T) {
	t.Parallel()
	g := graphOf(t, map[string]string{"/g/a.meaning.yaml": doc(cn("a", "entity", "")), "/g/b.meaning.yaml": doc(cn("b", "entity", ""))}, "")
	findings := Checker{}.Check(g)
	if len(findings) != 2 {
		t.Fatalf("one notice for each file of the earlier format: %v", findings)
	}
	for i, f := range findings {
		if f.Rule != RuleEarlierFormat || f.Severity != Warning || f.Line != 1 || f.File != fmt.Sprintf("/g/%c.meaning.yaml", 'a'+i) ||
			!strings.Contains(f.Message, "the file is in meaning/draft-1, the earlier format; it is read in full") || !strings.Contains(f.Message, "meaninggraph rewrite --write") {
			t.Errorf("finding %d: %+v", i, f)
		}
	}
	if HasErrors(findings) || g.Format() != Draft1 {
		t.Errorf("the notice is no error; format %q", g.Format())
	}
	if got := (Checker{}).Check(graphOf(t, map[string]string{"/g/a.meaning.yaml": doc2(cn("a", "entity", ""))}, "")); len(got) != 0 {
		t.Errorf("a draft-2 file gets no notice: %v", got)
	}
}

func TestEarlierRoleNames(t *testing.T) {
	t.Parallel()
	checker := Checker{Models: fakeModels{"/g/m.hcl": chinook}}
	artist := cn("artist", "entity", bindings(bind2("chinook.Artist", "entity", ""), bind2("chinook.Artist", "identifier", "ArtistId"), bind2("chinook.Album", "foreign-key", "ArtistId")))
	album := cn("album", "entity", bindings(bind2("chinook.Album", "instances", "")))
	findings := checker.Check(graphOf(t, inG(bound2(artist, album)), ""))
	if len(findings) != 1 || findings[0].Rule != RuleEarlierRoleName || findings[0].Severity != Warning || findings[0].Line != 7 ||
		!strings.Contains(findings[0].Message, "the role names entity and foreign-key are the earlier spellings of instances and reference (this file uses entity and foreign-key)") {
		t.Errorf("one notice for the file: %v", findings)
	}
	clean := cn("artist", "entity", bindings(bind2("chinook.Artist", "instances", ""), bind2("chinook.Artist", "identifier", "ArtistId")))
	if got := checker.Check(graphOf(t, inG(bound2(clean)), "")); len(got) != 0 {
		t.Errorf("the current names get no notice: %v", got)
	}
}

func TestFilesOfOneGraphAreInOneFormat(t *testing.T) {
	t.Parallel()
	g := graphOf(t, map[string]string{"/g/a.meaning.yaml": doc(cn("one", "entity", "")), "/g/b.meaning.yaml": doc2(cn("two", "entity", ""))}, "")
	findings := Checker{}.Check(g)
	var mixed []Finding
	for _, f := range findings {
		if f.Rule == RuleFormatMixed {
			mixed = append(mixed, f)
		}
	}
	if len(mixed) != 1 || mixed[0].Severity != Error || mixed[0].File != "/g/b.meaning.yaml" || mixed[0].Line != 1 ||
		!strings.Contains(mixed[0].Message, "change format together, and these are in meaning/draft-1 (/g/a.meaning.yaml) and meaning/draft-2 (/g/b.meaning.yaml)") {
		t.Fatalf("format-mixed: %v", findings)
	}
	if g.Format() != "" {
		t.Errorf("a graph in two formats has none: %q", g.Format())
	}
	// Each file alone is fine.
	for _, text := range []string{doc(cn("one", "entity", "")), doc2(cn("two", "entity", ""))} {
		if rules := ruleList(withoutEarlierFormat(Checker{}.Check(graphOf(t, map[string]string{"/g/a.meaning.yaml": text}, "")))); len(rules) != 0 {
			t.Errorf("rules %v", rules)
		}
	}
}

func TestKindsAndRelationsOfDraft2(t *testing.T) {
	t.Parallel()
	vs := func(id, rest string) string { return cn(id, "value-set", values2("a", "b")+rest) }
	runCases(t, []testCase{
		{name: "every relation", files: map[string]string{"a.meaning.yaml": doc2(
			cn("invoice", "entity", ""),
			cn("status", "value-set", values2("open", "shut")+", complete: true"),
			cn("flag", "value-set", ", extends: status"+values2("on")),
			cn("kind-of-status", "entity", ", extends: status"),
			cn("shape", "value-set", ", extends: invoice"+values2("a")),
			cn("total", "property", ", of: invoice"),
			cn("state", "property", ", of: status, values-of: status"),
			cn("area", "dimension", ", extends: total"),
			cn("sum", "measure", ", measure: {formula: x, inputs: [total], dimensions: [area, total]}"),
		)}},
		{name: "a property extends an entity", files: map[string]string{"a.meaning.yaml": doc2(cn("e", "entity", ""), cn("p", "property", ", extends: e"))}, rules: []string{RuleExtendsKind}, messages: []string{"a property cannot extend e, which is an entity; extends means \"is a kind of\", and a property may extend only property or dimension"}},
		{name: "an entity extends a property", files: map[string]string{"a.meaning.yaml": doc2(cn("p", "property", ""), cn("e", "entity", ", extends: p"))}, rules: []string{RuleExtendsKind}, messages: []string{"an entity cannot extend p, which is a property; extends means \"is a kind of\", and an entity may extend only entity or value-set"}},
		{name: "a measure extends a value set", files: map[string]string{"a.meaning.yaml": doc2(vs("v", ""), cn("m", "measure", ", extends: v, measure: {formula: x}"))}, rules: []string{RuleExtendsKind}},
		{name: "of names a property", files: map[string]string{"a.meaning.yaml": doc2(cn("q", "property", ""), cn("p", "property", ", of: q"))}, rules: []string{RuleTargetKind}, messages: []string{"of names q, which is a property, not an entity or a value-set"}},
		{name: "values-of and units-of name a property", files: map[string]string{"a.meaning.yaml": doc2(cn("q", "property", ""), cn("p", "property", ", values-of: q, units-of: q"))}, rules: []string{RuleTargetKind, RuleTargetKind}, messages: []string{"values-of names q, which is a property, not an entity or a value-set"}},
		{name: "an input that is a value set", files: map[string]string{"a.meaning.yaml": doc2(vs("v", ""), cn("m", "measure", ", measure: {formula: x, inputs: [v]}"))}, rules: []string{RuleMeasureInput}, messages: []string{"a measure is computed from properties and measures only"}},
		{name: "a dimension that is a value set", files: map[string]string{"a.meaning.yaml": doc2(vs("v", ""), cn("m", "measure", ", measure: {formula: x, dimensions: [v]}"))}, rules: []string{RuleMeasureDimension}, messages: []string{"a measure is grouped by dimensions or properties only"}},
		{name: "narrowing to an entity that extends the value set", files: map[string]string{"a.meaning.yaml": doc2(vs("v", ""), cn("e", "entity", ", extends: v"), cn("parent", "property", ", values-of: v"), cn("child", "property", ", extends: parent, values-of: e"))}},
		{name: "narrowing to an unrelated value set", files: map[string]string{"a.meaning.yaml": doc2(vs("v", ""), vs("w", ""), cn("parent", "property", ", values-of: v"), cn("child", "property", ", extends: parent, values-of: w"))}, rules: []string{RuleNarrowing}},
		{name: "a unit that names a value of a value set", files: map[string]string{"a.meaning.yaml": doc2(
			cn("currency", "value-set", ", values: [{id: usd, labels: {en: US dollar}, codes: {iso: USD}}]"), cn("money", "measure", ", measure: {formula: x}, units-of: currency, unit: usd"))}},
		{name: "a unit that names no value", files: map[string]string{"a.meaning.yaml": doc2(
			cn("currency", "value-set", ", values: [{id: usd, labels: {en: US dollar}}]"), cn("money", "measure", ", measure: {formula: x}, units-of: currency, unit: eur"))}, rules: []string{RuleUnit}},
		{name: "a unit that names two values", files: map[string]string{"a.meaning.yaml": doc2(
			cn("currency", "value-set", ", values: [{id: a, labels: {en: A}, aliases: {en: [X]}}, {id: b, labels: {en: B}, codes: {k: X}}]"), cn("money", "measure", ", measure: {formula: x}, units-of: currency, unit: x"))}, rules: []string{RuleUnit}, messages: []string{"but names a, b"}},
		{name: "a retired value and a live one with one label", files: map[string]string{"a.meaning.yaml": doc2(cn("v", "value-set", ", values: [{id: a, labels: {en: Same}, retired: true}, {id: b, labels: {en: same}}]"))}, rules: []string{RuleDuplicateValue}},
	})
	// The notice for a unit that names a retired value is a warning, not an error.
	g := graphOf(t, map[string]string{"/g/a.meaning.yaml": doc2(
		cn("currency", "value-set", ", values: [{id: usd, labels: {en: US dollar}, codes: {iso: USD}, retired: true}]"), cn("money", "measure", ", measure: {formula: x}, units-of: currency, unit: USD"))}, "")
	findings := Checker{}.Check(g)
	if len(findings) != 1 || findings[0].Rule != RuleRetiredValue || findings[0].Severity != Warning || !strings.Contains(findings[0].Message, `unit "USD" names the value usd of currency, which is retired`) {
		t.Errorf("retired unit: %v", findings)
	}
}

// A concept of a kind that its file's format does not have is no kind this check
// knows, whatever the schema said of the file.
func TestKindsAFormatDoesNotHaveAreNoKind(t *testing.T) {
	t.Parallel()
	checker := Checker{Schema: permissive{}}
	runCases(t, []testCase{
		{name: "an entity extends a value set in draft 1", checker: checker, files: map[string]string{"a.meaning.yaml": doc(cn("v", "value-set", values2("a")), cn("e", "entity", ", extends: v"))}, rules: []string{RuleExtendsKind, RuleFormatWord}},
		{name: "a property extends a property in draft 1", checker: checker, files: map[string]string{"a.meaning.yaml": doc(cn("p", "property", ""), cn("q", "property", ", extends: p"))}, rules: []string{RuleExtendsKind, RuleFormatWord, RuleFormatWord}},
		{name: "of names a value set in draft 1", checker: checker, files: map[string]string{"a.meaning.yaml": doc(cn("v", "value-set", values2("a")), cn("a", "attribute", ", of: v"))}, rules: []string{RuleFormatWord, RuleTargetKind}},
	})
}

func TestKindsAcrossTheTwoFormats(t *testing.T) {
	t.Parallel()
	d2 := doc2(cn("status-list", "value-set", values2("a")), cn("some-property", "property", ""))
	d1 := doc(cn("old-attribute", "attribute", ""), cn("old-list", "entity", values2("a")))
	ref := func(id string) string { return "'meaning://github.com/org/other/" + id + pin + "'" }
	against := func(supplied string, local string) []Finding {
		g := graphOf(t, map[string]string{"/g/a.meaning.yaml": local}, "")
		return withoutEarlierFormat(Checker{Resolve: dependency(t, "github.com/org/other", supplied)}.Check(g))
	}
	for name, tc := range map[string]struct{ supplied, local string }{
		"a draft-1 attribute whose values-of names a draft-2 value set": {d2, doc(cn("a", "attribute", ", values-of: "+ref("status-list")))},
		"a draft-1 attribute extends a draft-2 property":                {d2, doc(cn("a", "attribute", ", extends: "+ref("some-property")))},
		"a draft-1 entity extends a draft-2 value set":                  {d2, doc(cn("e", "entity", ", extends: "+ref("status-list")))},
		"a draft-1 measure takes a draft-2 property as input":           {d2, doc(cn("m", "measure", ", measure: {formula: x, inputs: ["+ref("some-property")+"]}"))},
		"a draft-2 property extends a draft-1 attribute":                {d1, doc2(cn("p", "property", ", extends: "+ref("old-attribute")))},
		"a draft-2 property takes the values of a draft-1 entity":       {d1, doc2(cn("p", "property", ", values-of: "+ref("old-list")))},
	} {
		if got := against(tc.supplied, tc.local); len(got) != 0 {
			t.Errorf("%s: %v", name, got)
		}
	}
	got := against(d2, doc(cn("m", "measure", ", measure: {formula: x, inputs: ["+ref("status-list")+"]}")))
	if len(got) != 1 || got[0].Rule != RuleMeasureInput || !strings.Contains(got[0].Message, "a measure is computed from attributes and measures only") {
		t.Errorf("a draft-1 measure takes a value set: %v, in the words of the file it is about", got)
	}
	// A supplied graph is validated against the schema of the format of each of its files.
	bad := graphOf(t, map[string]string{"/g/dep.meaning.yaml": doc2(cn("stray", "attribute", ""))}, "github.com/org/other")
	got = Checker{Resolve: GraphResolver(map[string]*Graph{"github.com/org/other": bad})}.Check(graphOf(t, map[string]string{"/g/a.meaning.yaml": doc(cn("a", "attribute", ", extends: "+ref("stray")))}, ""))
	if rules := ruleList(withoutEarlierFormat(got)); !slices.Equal(rules, []string{RuleUnresolved}) {
		t.Errorf("a supplied draft-2 file with kind attribute: %v", got)
	}
}

func TestRolesOfDraft2(t *testing.T) {
	t.Parallel()
	checker := Checker{Models: fakeModels{"/g/m.hcl": chinook}}
	artist := cn("artist", "entity", bindings(bind2("chinook.Artist", "instances", ""), bind2("chinook.Artist", "identifier", "ArtistId"), bind2("chinook.Artist", "display-name", "Name"), bind2("chinook.Album", "reference", "ArtistId")))
	runCases(t, []testCase{
		{name: "a fully bound entity", checker: checker, files: bound2(artist)},
		{name: "a property that refers to its values-of concept", checker: checker, files: bound2(artist, cn("album-artist", "property", ", values-of: artist"+bindings(bind2("chinook.Album", "reference", "ArtistId"))))},
		{name: "instances on two record types, under both names", checker: checker, files: bound2(cn("a", "entity", bindings(bind2("chinook.Artist", "entity", ""), bind2("chinook.Album", "instances", "")))), rules: []string{RuleEarlierRoleName, RuleEntityBindings}, messages: []string{"has 2 instances bindings ([Artist Album])"}},
		{name: "a missing field", checker: checker, files: bound2(cn("a", "property", bindings(bind2("chinook.Artist", "value", "Nme")))), rules: []string{RuleBindingModel}, messages: []string{"entity Artist has no property Nme"}},
		{name: "value on a reference", checker: checker, files: bound2(cn("a", "property", bindings(bind2("chinook.Album", "value", "ArtistId")))), rules: []string{RuleBindingRole}, messages: []string{"bind it with role reference"}},
		{name: "reference on a plain field", checker: checker, files: bound2(artist, cn("a", "property", ", values-of: artist"+bindings(bind2("chinook.Album", "reference", "Title")))), rules: []string{RuleBindingRole}, messages: []string{"has role reference but is not a reference (it is a string)"}},
		{name: "reference without values-of", checker: checker, files: bound2(artist, cn("a", "property", bindings(bind2("chinook.Album", "reference", "ArtistId")))), rules: []string{RuleBindingRole}, messages: []string{"has role reference, so a needs values-of"}},
		{name: "reference to the wrong record type", checker: checker, files: bound2(cn("album", "entity", bindings(bind2("chinook.Album", "instances", ""))), cn("a", "property", ", values-of: album"+bindings(bind2("chinook.Album", "reference", "ArtistId")))), rules: []string{RuleBindingRole}, messages: []string{"references Artist, but the instances of album are [Album] rows"}},
		{name: "reference whose target has no instances binding", checker: checker, files: bound2(cn("artist", "entity", ""), cn("a", "property", ", values-of: artist"+bindings(bind2("chinook.Album", "reference", "ArtistId")))), rules: []string{RuleBindingRole}, messages: []string{"has role reference, but artist has no entity binding in this repository"}},
		{name: "the earlier role names keep their words in a message", checker: checker, files: bound2(artist, cn("a", "property", ", values-of: artist"+bindings(bind2("chinook.Album", "foreign-key", "Title")))), rules: []string{RuleBindingRole, RuleEarlierRoleName}, messages: []string{"has role foreign-key but is not a reference"}},
	})
}

func TestASchemaCanBeGivenForEachFormat(t *testing.T) {
	t.Parallel()
	// A file that the schema of its format refuses is read when the schema given for that format lets it through.
	text := doc2(cn("a", "attribute", ""))
	if rules := ruleList(Checker{SchemaDraft2: permissive{}}.Check(graphOf(t, map[string]string{"/g/a.meaning.yaml": text}, ""))); !slices.Equal(rules, []string{RuleFormatWord}) {
		t.Errorf("rules %v", rules)
	}
	if rules := ruleList(Checker{Schema: permissive{}}.Check(graphOf(t, map[string]string{"/g/a.meaning.yaml": text}, ""))); !slices.Equal(rules, []string{RuleFormatWord, RuleSchema}) {
		t.Errorf("the schema of draft 1 is not asked about a draft-2 file: %v", rules)
	}
}

func TestMemberNamesTheFieldUnderTheKeyOfTheFormat(t *testing.T) {
	t.Parallel()
	b := Binding{Property: "P", Field: "F"}
	if b.Member(Draft1) != "P" || b.Member(Draft2) != "F" || b.Member("") != "P" {
		t.Errorf("both keys: %q %q", b.Member(Draft1), b.Member(Draft2))
	}
	if (Binding{Field: "F"}).Member(Draft1) != "F" || (Binding{Property: "P"}).Member(Draft2) != "P" || (Binding{}).Member(Draft2) != "" {
		t.Error("the other key is read in a file the schema refuses")
	}
	if roleOf(Binding{Role: "entity"}) != "instances" || roleOf(Binding{Role: "foreign-key"}) != "reference" || roleOf(Binding{Role: "value"}) != "value" {
		t.Error("roleOf")
	}
}

// ---- links --------------------------------------------------------------------------------------------------------------

var shop = &Model{Entities: map[string]*Entity{
	"Customer": {Key: []string{"Id"}, Properties: map[string]Property{"Id": {Type: "int"}, "Name": {Type: "string"}}},
	"Order":    {Key: []string{"Id"}, Properties: map[string]Property{"Id": {Type: "int"}, "CustomerId": {Reference: true, Entity: "Customer"}, "Status": {Type: "string"}}},
}}

func linksOf(t *testing.T, model *Model, files map[string]string) ([]Finding, []Link) {
	t.Helper()
	for name, content := range files {
		files[name] = strings.Replace(content, "models: {chinook: m.hcl}", "models: {shop: m.hcl}", 1)
	}
	return Checker{Models: fakeModels{"/g/m.hcl": model}}.CheckLinks(graphOf(t, inG(files), ""))
}

func link(concept, record, field, role string, derived bool) Link {
	l := Link{Concept: concept, Model: "modelspec:///shop." + record, Field: field, Role: role, Derived: derived}
	return l
}

func TestDerivedLinks(t *testing.T) {
	t.Parallel()
	customer := cn("customer", "entity", bindings(bind2("shop.Customer", "instances", "")))
	order := cn("order", "entity", bindings(bind2("shop.Order", "instances", "")))
	base := []Link{
		link("customer", "Customer", "", "instances", false),
		link("customer", "Customer", "Id", "identifier", true),
		link("customer", "Order", "CustomerId", "reference", true),
		link("order", "Order", "", "instances", false),
		link("order", "Order", "Id", "identifier", true),
	}
	only := func(links []Link, concept string) []Link {
		return slices.DeleteFunc(slices.Clone(links), func(l Link) bool { return l.Concept != concept })
	}
	tests := []struct {
		name  string
		model *Model
		files map[string]string
		want  []Link
	}{
		{name: "nothing written", files: bound2(customer, order), want: base},
		{name: "the earlier format and role names give the same list", files: map[string]string{"a.meaning.yaml": head + modelsLine + "concepts:\n" +
			cn("customer", "entity", bindings(bind("shop.Customer", "entity", ""))) + cn("order", "entity", bindings(bind("shop.Order", "entity", "")))}, want: base},
		{name: "no concept bound to the target derives nothing for the reference", files: bound2(order), want: only(base, "order")},
		{name: "two concepts bound to one record type both apply", files: bound2(customer, order, cn("buyer", "entity", bindings(bind2("shop.Customer", "instances", "")))),
			want: append([]Link{link("buyer", "Customer", "", "instances", false), link("buyer", "Customer", "Id", "identifier", true), link("buyer", "Order", "CustomerId", "reference", true)}, base...)},
		{name: "a written line takes the place of the derived one and brings its note", files: bound2(cn("customer", "entity", bindings(bind2("shop.Customer", "instances", ""), "{model: 'modelspec:///shop.Order', role: reference, field: CustomerId, note: Who placed it.}")), order),
			want: []Link{base[0], base[1], {Concept: "customer", Model: "modelspec:///shop.Order", Field: "CustomerId", Role: "reference", Note: "Who placed it."}, base[3], base[4]}},
		{name: "a repeated line adds nothing, and the first line's note stands", files: bound2(cn("customer", "entity", bindings(bind2("shop.Customer", "instances", ""), "{model: 'modelspec:///shop.Customer', role: identifier, field: Id, note: first}", "{model: 'modelspec:///shop.Customer', role: identifier, field: Id, note: second}")), order),
			want: []Link{base[0], {Concept: "customer", Model: "modelspec:///shop.Customer", Field: "Id", Role: "identifier", Note: "first"}, base[2], base[3], base[4]}},
		{name: "a line keeps its match", files: bound2(customer, order, cn("status", "property", bindings("{model: 'modelspec:///shop.Order', role: value, field: Status, match: codes.k, note: By code.}"))),
			want: append(slices.Clone(base), Link{Concept: "status", Model: "modelspec:///shop.Order", Field: "Status", Role: "value", Match: "codes.k", Note: "By code."})},
		{name: "another role on the same field and concept does not replace the derived one", model: &Model{Entities: map[string]*Entity{
			"Customer": {Key: []string{"Id"}, Properties: map[string]Property{"Id": {Type: "string"}}}, "Order": shop.Entities["Order"]}}, files: bound2(cn("customer", "entity", bindings(bind2("shop.Customer", "instances", ""), bind2("shop.Customer", "display-name", "Id"))), order),
			want: []Link{base[0], link("customer", "Customer", "Id", "display-name", false), base[1], base[2], base[3], base[4]}},
		{name: "a key of several fields gives one link for each, and a name the record does not declare none", model: &Model{Entities: map[string]*Entity{
			"Line": {Key: []string{"OrderId", "LineNo", "Ghost"}, Properties: map[string]Property{"OrderId": {Reference: true, Entity: "Line"}, "LineNo": {Type: "int"}}}}},
			files: bound2(cn("line", "entity", bindings(bind2("shop.Line", "instances", "")))),
			want:  []Link{link("line", "Line", "", "instances", false), link("line", "Line", "LineNo", "identifier", true), link("line", "Line", "OrderId", "identifier", true), link("line", "Line", "OrderId", "reference", true)}},
		{name: "a record type with no key gets no identifier", model: &Model{Entities: map[string]*Entity{"Note": {Properties: map[string]Property{"Text": {Type: "string"}}}}},
			files: bound2(cn("note", "entity", bindings(bind2("shop.Note", "instances", "")))), want: []Link{link("note", "Note", "", "instances", false)}},
		{name: "a reference written with a module name derives nothing", model: &Model{Entities: map[string]*Entity{
			"Customer": shop.Entities["Customer"], "Order": {Properties: map[string]Property{"CustomerId": {Reference: true, Entity: "shop.Customer"}}}}},
			files: bound2(customer), want: []Link{base[0], base[1]}},
		{name: "a graph with no models map and no bindings has the empty list", files: map[string]string{"a.meaning.yaml": doc2(cn("a", "entity", ""))}, want: []Link{}},
		{name: "a binding to another repository is not a link", files: bound2(cn("a", "entity", ", bindings: [{model: 'modelspec://github.com/o/r/shop.Customer', role: instances}]")), want: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			model := tc.model
			if model == nil {
				model = shop
			}
			findings, links := linksOf(t, model, tc.files)
			if tc.want == nil {
				if links != nil || !HasErrors(findings) {
					t.Fatalf("no list is defined for a graph with an error: %v %v", links, findings)
				}
				return
			}
			if HasErrors(findings) {
				t.Fatalf("findings: %v", findings)
			}
			if !slices.EqualFunc(links, tc.want, func(a, b Link) bool { return a == b }) {
				t.Errorf("links\n got %+v\nwant %+v", links, tc.want)
			}
		})
	}
}

func TestLinksAreSortedByBytes(t *testing.T) {
	t.Parallel()
	model := &Model{Entities: map[string]*Entity{
		"Customer": {Key: []string{"alpha", "Zeta"}, Properties: map[string]Property{"alpha": {Type: "int"}, "Zeta": {Type: "int"}}},
		"Zed":      {Properties: map[string]Property{"CustomerId": {Reference: true, Entity: "Customer"}}},
		"alpha":    {Properties: map[string]Property{"CustomerId": {Reference: true, Entity: "Customer"}}},
	}}
	_, links := linksOf(t, model, bound2(cn("customer", "entity", bindings(bind2("shop.Customer", "instances", "")))))
	var got []string
	for _, l := range links {
		got = append(got, l.Model[len("modelspec:///shop."):]+"."+l.Field+" "+l.Role)
	}
	want := []string{"Customer. instances", "Customer.Zeta identifier", "Customer.alpha identifier", "Zed.CustomerId reference", "alpha.CustomerId reference"}
	if !slices.Equal(got, want) {
		t.Errorf("capital letters come first, as bytes: %v", got)
	}
}

func TestNoLinksForAGraphThatFailsItsCheck(t *testing.T) {
	t.Parallel()
	files := bound2(
		cn("customer", "entity", bindings(bind2("shop.Customer", "instances", ""))),
		cn("order", "entity", bindings(bind2("shop.Order", "instances", ""), bind2("shop.Order", "reference", "CustomerId"))),
	)
	findings, links := linksOf(t, shop, files)
	if links != nil || !HasErrors(findings) || findings[0].Rule != RuleBindingRole {
		t.Errorf("a reference written under the wrong concept: %v %v", findings, links)
	}
	findings, links = Checker{Models: fakeModels{}}.CheckLinks(graphOf(t, inG(bound2(cn("customer", "entity", ""))), ""))
	if links != nil || len(findings) != 1 || findings[0].Rule != RuleModels {
		t.Errorf("a model that cannot be read: %v %v", findings, links)
	}
	// A module the models map does not list gives no links of its own.
	findings, links = linksOf(t, shop, map[string]string{"a.meaning.yaml": head2 + "concepts:\n" + cn("customer", "entity", bindings(bind2("shop.Customer", "instances", "")))})
	if links != nil || !HasErrors(findings) {
		t.Errorf("an unlisted module: %v %v", findings, links)
	}
}

// deriveLinks does not need the check to have passed: it reads what it is given.
func TestDeriveLinksSkipsAModuleWithNoModel(t *testing.T) {
	t.Parallel()
	g := graphOf(t, inG(bound2(
		cn("customer", "entity", bindings(bind2("shop.Customer", "instances", ""))),
		cn("lost", "entity", ", bindings: [{model: nope, role: instances}]"),
	)), "")
	links := deriveLinks([]loadedFile{{file: g.Files[0], models: map[string]*Model{}}})
	if len(links) != 1 || links[0].Role != "instances" || links[0].Derived {
		t.Errorf("only the written link: %+v", links)
	}
}
