package meaning

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/meaninggraph/cli/internal/memfs"
)

// fakeModels serves fixed models by path; a path with no model is an error.
type fakeModels map[string]*Model

func (m fakeModels) ReadModel(_ FS, path string) (*Model, error) {
	if model, ok := m[path]; ok {
		return model, nil
	}
	return nil, errors.New("no model at " + path)
}

var chinook = &Model{Entities: map[string]*Entity{
	"Artist": {Key: []string{"ArtistId"}, Properties: map[string]Property{"ArtistId": {Type: "int"}, "Name": {Type: "string"}}},
	"Album": {Key: []string{"AlbumId"}, Properties: map[string]Property{
		"AlbumId": {Type: "int"}, "Title": {Type: "string"}, "ArtistId": {Reference: true, Entity: "Artist"}, "Sales": {Type: "decimal"},
	}},
}}

const modelsLine = "models: {chinook: m.hcl}\n"

func bound(concepts ...string) map[string]string {
	return map[string]string{"a.meaning.yaml": head + modelsLine + "concepts:\n" + strings.Join(concepts, "")}
}

func bind(model, role, property string) string {
	out := fmt.Sprintf("{model: 'modelspec:///%s', role: %s", model, role)
	if property != "" {
		out += ", property: " + property
	}
	return out + "}"
}

func bindings(items ...string) string { return ", bindings: [" + strings.Join(items, ", ") + "]" }

func TestBindings(t *testing.T) {
	t.Parallel()
	models := fakeModels{"/g/m.hcl": chinook}
	checker := Checker{Models: models, Schema: permissive{}}
	artist := cn("artist", "entity", bindings(bind("chinook.Artist", "entity", ""), bind("chinook.Artist", "identifier", "ArtistId"), bind("chinook.Artist", "display-name", "Name"), bind("chinook.Album", "foreign-key", "ArtistId")))
	runCases(t, []testCase{
		{name: "a fully bound entity", checker: checker, files: bound(artist)},
		{name: "a bound attribute and measure", checker: checker, files: bound(artist,
			cn("title", "attribute", bindings(bind("chinook.Album", "value", "Title"))),
			cn("sales", "measure", ", measure: {formula: x}"+bindings(bind("chinook.Album", "value", "Sales"))),
			cn("album-artist", "attribute", ", values-of: artist"+bindings(bind("chinook.Album", "foreign-key", "ArtistId"))),
			cn("sub-artist", "entity", ", extends: artist"+bindings(bind("chinook.Artist", "entity", ""))),
			cn("featured", "attribute", ", extends: album-artist"+bindings(bind("chinook.Album", "foreign-key", "ArtistId"))),
		)},
		{name: "two entity bindings", checker: checker, files: bound(cn("a", "entity", bindings(bind("chinook.Artist", "entity", ""), bind("chinook.Album", "entity", "")))), rules: []string{RuleEntityBindings}, messages: []string{"has 2 entity bindings ([Artist Album])"}},
		{name: "not a modelspec reference", checker: checker, files: bound(cn("a", "entity", ", bindings: [{model: nope, role: entity}]")), rules: []string{RuleBindingModel}, messages: []string{"nope is not a modelspec:// reference"}},
		{name: "another repository", checker: checker, files: bound(cn("a", "entity", ", bindings: [{model: 'modelspec://github.com/o/r/chinook.Artist', role: entity}]")), rules: []string{RuleBindingModel}, messages: []string{"points at another repository"}},
		{name: "unlisted module", checker: checker, files: bound(cn("a", "entity", bindings(bind("music.Artist", "entity", "")))), rules: []string{RuleBindingModel}, messages: []string{"module music is not listed in models"}},
		{name: "missing entity", checker: checker, files: bound(cn("a", "entity", bindings(bind("chinook.Artists", "entity", "")))), rules: []string{RuleBindingModel}, messages: []string{"module chinook has no entity Artists"}},
		{name: "missing property", checker: checker, files: bound(cn("a", "attribute", bindings(bind("chinook.Artist", "value", "Nme")))), rules: []string{RuleBindingModel}, messages: []string{"entity Artist has no property Nme"}},
		{name: "identifier with no entity binding", checker: checker, files: bound(cn("a", "entity", bindings(bind("chinook.Artist", "identifier", "ArtistId")))), rules: []string{RuleBindingRole}, messages: []string{"has no entity binding, so it cannot be checked"}},
		{name: "identifier on another entity", checker: checker, files: bound(cn("a", "entity", bindings(bind("chinook.Artist", "entity", ""), bind("chinook.Album", "identifier", "AlbumId")))), rules: []string{RuleBindingRole}, messages: []string{"is bound to the entity Artist; the property must be on that entity"}},
		{name: "identifier with two entity bindings is checked by the count only", checker: checker, files: bound(cn("a", "entity", bindings(bind("chinook.Artist", "entity", ""), bind("chinook.Album", "entity", ""), bind("chinook.Album", "identifier", "AlbumId")))), rules: []string{RuleEntityBindings}},
		{name: "identifier outside the key", checker: checker, files: bound(cn("a", "entity", bindings(bind("chinook.Album", "entity", ""), bind("chinook.Album", "identifier", "Title")))), rules: []string{RuleBindingRole}, messages: []string{"is not in the key of Album [AlbumId]"}},
		{name: "display-name that is not a string", checker: checker, files: bound(cn("a", "entity", bindings(bind("chinook.Album", "entity", ""), bind("chinook.Album", "display-name", "AlbumId")))), rules: []string{RuleBindingRole}, messages: []string{"is an int, not a string"}},
		{name: "display-name that is a reference", checker: checker, files: bound(cn("a", "entity", bindings(bind("chinook.Album", "entity", ""), bind("chinook.Album", "display-name", "ArtistId")))), rules: []string{RuleBindingRole}, messages: []string{"is a reference to Artist, not a string"}},
		{name: "value on a reference", checker: checker, files: bound(cn("a", "attribute", bindings(bind("chinook.Album", "value", "ArtistId")))), rules: []string{RuleBindingRole}, messages: []string{"bind it with role foreign-key"}},
		{name: "foreign-key on a plain property", checker: checker, files: bound(artist, cn("a", "attribute", ", values-of: artist"+bindings(bind("chinook.Album", "foreign-key", "Title")))), rules: []string{RuleBindingRole}, messages: []string{"is not a reference (it is a string)"}},
		{name: "foreign-key attribute without values-of", checker: checker, files: bound(artist, cn("a", "attribute", bindings(bind("chinook.Album", "foreign-key", "ArtistId")))), rules: []string{RuleBindingRole}, messages: []string{"needs values-of"}},
		{name: "foreign-key to an unresolvable values-of", checker: checker, files: bound(artist, cn("a", "attribute", ", values-of: nope"+bindings(bind("chinook.Album", "foreign-key", "ArtistId")))), rules: []string{RuleUnknownConcept}},
		{name: "foreign-key target with no entity binding", checker: checker, files: bound(cn("artist", "entity", ""), cn("a", "attribute", ", values-of: artist"+bindings(bind("chinook.Album", "foreign-key", "ArtistId")))), rules: []string{RuleBindingRole}, messages: []string{"artist has no entity binding in this repository"}},
		{name: "foreign-key to the wrong entity", checker: checker, files: bound(cn("album", "entity", bindings(bind("chinook.Album", "entity", ""))), cn("a", "attribute", ", values-of: album"+bindings(bind("chinook.Album", "foreign-key", "ArtistId")))), rules: []string{RuleBindingRole}, messages: []string{"references Artist, but the instances of album are [Album] rows"}},
		{name: "foreign-key on an entity to another module's entity", checker: checker, files: bound(cn("album", "entity", bindings(bind("chinook.Album", "entity", ""), bind("chinook.Album", "foreign-key", "ArtistId")))), rules: []string{RuleBindingRole}},
	})
}

func TestBindingsToAnotherGraphsEntityCannotBeChecked(t *testing.T) {
	t.Parallel()
	core := dependency(t, "github.com/org/core", doc(cn("artist", "entity", bindings(bind("chinook.Artist", "entity", "")))))
	files := bound(cn("a", "attribute", ", values-of: 'meaning://github.com/org/core/artist"+pin+"'"+bindings(bind("chinook.Album", "foreign-key", "ArtistId"))))
	checker := Checker{Models: fakeModels{"/g/m.hcl": chinook}, Schema: permissive{}, Resolve: core}
	runCases(t, []testCase{{name: "a values-of entity of another graph", checker: checker, files: files, rules: []string{RuleBindingRole}, messages: []string{"has no entity binding in this repository"}}})
}

func TestModelsThatCannotBeRead(t *testing.T) {
	t.Parallel()
	files := map[string]string{"a.meaning.yaml": head + "models: {chinook: m.hcl, other: o.hcl}\nconcepts:\n" + cn("a", "entity", bindings(bind("chinook.Artist", "entity", "")))}
	runCases(t, []testCase{
		{name: "the first unreadable model loses all of them", files: files, checker: Checker{Models: fakeModels{"/g/m.hcl": chinook}}, rules: []string{RuleModels}, messages: []string{"models: module other: no model at /g/o.hcl"}},
		{name: "a model is read relative to its file", files: files, checker: Checker{Models: fakeModels{"/g/m.hcl": chinook, "/g/o.hcl": chinook}}},
		{name: "a model with no bindings still has to be readable", files: map[string]string{"a.meaning.yaml": head + modelsLine + "concepts:\n" + cn("a", "entity", "")}, checker: Checker{Models: fakeModels{}}, rules: []string{RuleModels}},
	})
}

func TestBindingsAgainstARealHCLModel(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"/g/sub/a.meaning.yaml": head + "models: {chinook: ../m.hcl}\nconcepts:\n" + cn("artist", "entity", bindings(bind("chinook.Artist", "entity", ""), bind("chinook.Artist", "identifier", "ArtistId"))),
		"/g/m.hcl":              "entity \"Artist\" {\n key = [\"ArtistId\"]\n property \"ArtistId\" { type = \"int\" }\n}\n",
	}
	g, err := LoadFiles(memfs.New(files), []string{"/g/sub/a.meaning.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	// The model is in the earlier spelling: that is reported, as a warning, and nothing else is.
	got := withoutEarlierFormat((Checker{}).Check(g))
	if len(got) != 1 || got[0].Rule != RuleEarlierSpelling || got[0].Severity != Warning || got[0].File != "/g/m.hcl" || got[0].Line != 1 || HasErrors(got) {
		t.Fatalf("findings = %v", got)
	}
	for _, want := range []string{"2 earlier spellings", `modelspec rewrite --write "/g"`, "--module <name>=<file>", "entity for record, property for field, entity = for record ="} {
		if !strings.Contains(got[0].Message, want) {
			t.Errorf("the notice %q lacks %q", got[0].Message, want)
		}
	}
}

func TestBindingsAgainstARealHCLModelInTheCurrentSpelling(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"/g/sub/a.meaning.yaml": head + "models: {chinook: ../m.hcl}\nconcepts:\n" + cn("artist", "entity", bindings(bind("chinook.Artist", "entity", ""), bind("chinook.Artist", "identifier", "ArtistId"))),
		"/g/m.hcl":              "record \"Artist\" {\n key = [\"ArtistId\"]\n field \"ArtistId\" { type = \"int\" }\n}\n",
	}
	g, err := LoadFiles(memfs.New(files), []string{"/g/sub/a.meaning.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if got := withoutEarlierFormat((Checker{}).Check(g)); len(got) != 0 {
		t.Fatalf("findings = %v", got)
	}
}

func TestTheEarlierSpellingIsReportedOncePerModelFile(t *testing.T) {
	t.Parallel()
	earlier := "entity \"Artist\" {\n key = [\"ArtistId\"]\n property \"ArtistId\" { type = \"int\" }\n}\n"
	artist := cn("artist", "entity", bindings(bind("chinook.Artist", "entity", "")))
	files := map[string]string{
		"/g/a.meaning.yaml": head + "models: {chinook: m.hcl, again: m.hcl, other: o.hcl}\nconcepts:\n" + artist,
		"/g/b.meaning.yaml": head + "models: {chinook: m.hcl}\nconcepts:\n" + cn("artist2", "entity", bindings(bind("chinook.Artist", "entity", ""))),
		"/g/m.hcl":          earlier,
		"/g/o.hcl":          earlier,
	}
	g, err := LoadFiles(memfs.New(files), []string{"/g/a.meaning.yaml", "/g/b.meaning.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	got := withoutEarlierFormat((Checker{}).Check(g))
	var notices []string
	for _, f := range got {
		if f.Rule != RuleEarlierSpelling {
			t.Errorf("unexpected finding %+v", f)
		}
		notices = append(notices, f.File)
	}
	if strings.Join(notices, " ") != "/g/m.hcl /g/o.hcl" {
		t.Fatalf("notices for %v", notices)
	}
	if g := withoutEarlierFormat((Checker{Models: fakeModels{"/g/m.hcl": {Earlier: EarlierSpelling{Count: 1, Line: 4}, Entities: chinook.Entities}}}).Check(mustLoad(t, map[string]string{
		"/g/a.meaning.yaml": head + modelsLine + "concepts:\n" + cn("a", "entity", ""),
	}, "/g/a.meaning.yaml"))); len(g) != 1 || g[0].Line != 4 || !strings.Contains(g[0].Message, "an earlier spelling (") {
		t.Fatalf("a single spelling: %v", g)
	}
}

func mustLoad(t *testing.T, files map[string]string, paths ...string) *Graph {
	t.Helper()
	g, err := LoadFiles(memfs.New(files), paths)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestParseModelRef(t *testing.T) {
	t.Parallel()
	got, ok := ParseModelRef("modelspec:///chinook.Invoice")
	if !ok || got != (ModelRef{Module: "chinook", Name: "Invoice"}) {
		t.Fatalf("got %+v, %v", got, ok)
	}
	got, ok = ParseModelRef("modelspec://github.com/o/r/chinook.Invoice?ref=abc")
	if !ok || got != (ModelRef{Repo: "github.com/o/r", Module: "chinook", Name: "Invoice", Pin: "abc"}) {
		t.Fatalf("got %+v, %v", got, ok)
	}
	for _, ref := range []string{"", "modelspec://chinook.Invoice", "modelspec:///chinook", "modelspec:///1x.Invoice", "model:///a.B"} {
		if _, ok := ParseModelRef(ref); ok {
			t.Errorf("ParseModelRef(%q) must fail", ref)
		}
	}
}

func TestModelsThatAreDirectoriesOrHaveFailedAreNotCascaded(t *testing.T) {
	t.Parallel()
	artist := cn("artist", "entity", bindings(bind("chinook.Artist", "entity", ""), bind("chinook.Artist", "identifier", "ArtistId")))
	two := cn("two", "entity", bindings(bind("chinook.Artist", "entity", ""), bind("chinook.Album", "entity", "")))
	models := fakeModels{"/g/m.hcl": chinook}
	runCases(t, []testCase{
		{name: "a path with a trailing slash", checker: Checker{Models: models, Schema: permissive{}},
			files: map[string]string{"a.meaning.yaml": head + "models: {chinook: m.hcl/}\nconcepts:\n" + artist},
			rules: []string{RuleModels}, messages: []string{"module chinook: m.hcl/ ends in a slash"}},
		{name: "a model that cannot be read does not make every binding unlisted, but a count is still a count", checker: Checker{Models: fakeModels{}, Schema: permissive{}},
			files: map[string]string{"a.meaning.yaml": head + modelsLine + "concepts:\n" + artist + two},
			rules: []string{RuleEntityBindings, RuleModels}},
		{name: "a binding to another repository is still reported", checker: Checker{Models: fakeModels{}, Schema: permissive{}},
			files: map[string]string{"a.meaning.yaml": head + modelsLine + "concepts:\n" + cn("a", "entity", ", bindings: [{model: nope, role: entity}]")},
			rules: []string{RuleBindingModel, RuleModels}},
	})
	if (Property{Entity: "Artist"}).IsReference() != true || (Property{Reference: true}).IsReference() != true || (Property{Type: "int"}).IsReference() {
		t.Fatal("IsReference")
	}
}

func TestAForeignKeyToAnEntityThatIsNotNamedByAString(t *testing.T) {
	t.Parallel()
	// The reference checker compares the entity a property references with the
	// entity's name; `entity = true` names no entity, so it cannot match one called "true".
	odd := &Model{Entities: map[string]*Entity{
		"true": {Key: []string{"Id"}, Properties: map[string]Property{"Id": {Type: "int"}, "T": {Reference: true}}},
	}}
	checker := Checker{Models: fakeModels{"/g/m.hcl": odd}, Schema: permissive{}}
	runCases(t, []testCase{{
		name: "a reference that names no entity", checker: checker,
		files: map[string]string{"a.meaning.yaml": head + modelsLine + "concepts:\n" +
			cn("t", "entity", bindings(bind("chinook.true", "entity", ""))) +
			cn("ref", "attribute", ", values-of: t"+bindings(bind("chinook.true", "foreign-key", "T")))},
		rules: []string{RuleBindingRole}, messages: []string{"references an entity that is not named by a string, but the instances of t are [true] rows"},
	}, {
		name: "a reference that names no entity, to an entity with no binding", checker: checker,
		files: map[string]string{"a.meaning.yaml": head + modelsLine + "concepts:\n" +
			cn("t", "entity", "") +
			cn("ref", "attribute", ", values-of: t"+bindings(bind("chinook.true", "foreign-key", "T")))},
		rules: []string{RuleBindingRole}, messages: []string{"cannot be checked that an entity that is not named by a string holds its instances"},
	}, {
		name: "a value on a reference that names no entity", checker: checker,
		files: map[string]string{"a.meaning.yaml": head + modelsLine + "concepts:\n" +
			cn("v", "attribute", bindings(bind("chinook.true", "value", "T")))},
		rules: []string{RuleBindingRole}, messages: []string{"has role value but is a reference"},
	}})
}
