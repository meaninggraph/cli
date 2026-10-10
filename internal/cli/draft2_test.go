package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/meaninggraph/cli/internal/memfs"
	"github.com/meaninggraph/cli/pkg/meaning"
)

// The command line over meaning/draft-2: what a report says of the two formats,
// the command links, and the schema of either format.

const shopModel = `record "Customer" {
  key = ["Id"]
  field "Id" {
    type = "int"
  }
}
record "Order" {
  key = ["Id"]
  field "Id" {
    type = "int"
  }
  field "CustomerId" {
    record = "Customer"
  }
}
`

func shop(role string) map[string]string {
	return map[string]string{
		"/shop/shop.modelspec.hcl": shopModel,
		"/shop/shop.meaning.yaml": "format: meaning/draft-2\nid: shop\nname: Shop\ndescription: d\nmodels: {shop: shop.modelspec.hcl}\nconcepts:\n" +
			concept("customer", "entity", ", bindings: [{model: 'modelspec:///shop.Customer', role: "+role+"}]") +
			concept("order", "entity", ", bindings: [{model: 'modelspec:///shop.Order', role: instances}]") +
			concept("status", "value-set", ", values: [{id: open, labels: {en: Open}}]"),
	}
}

type linksReport struct {
	Tool, Version string
	OK            bool
	Graphs        []struct {
		Format   string
		Errors   int
		Findings []meaning.Finding
		Links    []meaning.Link
	}
}

func TestLinksPrintsTheWrittenAndTheDerivedLinks(t *testing.T) {
	t.Parallel()
	got := execute(shop("instances"), "links", "/shop")
	if got.code != ExitClean || got.stderr != "" {
		t.Fatalf("got %+v", got)
	}
	var report linksReport
	if err := json.Unmarshal([]byte(got.stdout), &report); err != nil {
		t.Fatalf("%v\n%s", err, got.stdout)
	}
	if report.Tool != "meaninggraph" || !report.OK || len(report.Graphs) != 1 || report.Graphs[0].Format != meaning.Draft2 {
		t.Fatalf("report %+v", report)
	}
	want := []meaning.Link{
		{Concept: "customer", Model: "modelspec:///shop.Customer", Role: "instances"},
		{Concept: "customer", Model: "modelspec:///shop.Customer", Field: "Id", Role: "identifier", Derived: true},
		{Concept: "customer", Model: "modelspec:///shop.Order", Field: "CustomerId", Role: "reference", Derived: true},
		{Concept: "order", Model: "modelspec:///shop.Order", Role: "instances"},
		{Concept: "order", Model: "modelspec:///shop.Order", Field: "Id", Role: "identifier", Derived: true},
	}
	if len(report.Graphs[0].Links) != len(want) {
		t.Fatalf("links %+v", report.Graphs[0].Links)
	}
	for i, l := range want {
		if report.Graphs[0].Links[i] != l {
			t.Errorf("link %d = %+v, want %+v", i, report.Graphs[0].Links[i], l)
		}
	}
	// Only the derived links carry the mark.
	if strings.Count(got.stdout, `"derived": true`) != 3 {
		t.Errorf("derived marks:\n%s", got.stdout)
	}
}

func TestLinksOfAGraphWithAnErrorAreNull(t *testing.T) {
	t.Parallel()
	files := shop("instances")
	files["/shop/shop.meaning.yaml"] = strings.Replace(files["/shop/shop.meaning.yaml"], "role: instances}]", "role: identifier, field: Nope}]", 1)
	got := execute(files, "links", "/shop")
	if got.code != ExitFindings || !strings.Contains(got.stdout, `"links": null`) || !strings.Contains(got.stdout, `"rule": "binding-model"`) || !strings.Contains(got.stdout, `"ok": false`) {
		t.Fatalf("got %+v", got)
	}
	// A graph with no link at all has the empty list.
	got = execute(map[string]string{"/g/a.meaning.yaml": file(concept("a", "entity", ""))}, "links", "/g")
	if got.code != ExitClean || !strings.Contains(got.stdout, `"links": []`) {
		t.Fatalf("got %+v", got)
	}
	// A pin that cannot be verified is a warning, and the links stay.
	pinned := map[string]string{
		"/core/core.meaning.yaml": file(concept("customer", "entity", "")),
		"/mine/a.meaning.yaml":    file(concept("customer", "entity", ", extends: 'meaning://github.com/org/core/customer?ref="+pin+"'")),
	}
	got = execute(pinned, "links", "/mine", "--graph", "github.com/org/core=/core")
	if got.code != ExitClean || !strings.Contains(got.stdout, `"links": []`) || !strings.Contains(got.stdout, `"rule": "pin-not-verified"`) {
		t.Fatalf("a pin that cannot be verified is a warning: %+v", got)
	}
}

func TestLinksTakesTheFlagsOfCheckAndAlwaysAnswersInJSON(t *testing.T) {
	t.Parallel()
	got := execute(shop("instances"), "links", "/shop", "--format", "json")
	if got.code != ExitUsage || !strings.Contains(got.stderr, "unknown flag: --format") {
		t.Fatalf("links has no --format: %+v", got)
	}
	var failure struct {
		OK    bool
		Error string
	}
	if json.Unmarshal([]byte(got.stdout), &failure) != nil || failure.OK || failure.Error == "" {
		t.Fatalf("a script that reads stdout gets JSON on every path: %q", got.stdout)
	}
	got = execute(shop("instances"), "links", "/shop", "--profile", "other")
	if !strings.Contains(got.stderr, `invalid --profile "other"`) || !strings.Contains(got.stdout, `"ok": false`) {
		t.Fatalf("got %+v", got)
	}
	var stderr bytes.Buffer
	if code := Run([]string{"links", "/shop"}, Env{Stdout: failingWriter{}, Stderr: &stderr, FS: memfs.New(shop("instances")), Abs: fakeAbs, SelfUpdate: offline}); code != ExitUsage || !strings.Contains(stderr.String(), "closed pipe") {
		t.Fatalf("code %d stderr %q", code, stderr.String())
	}
}

func TestACheckReportsTheFormatOfEachGraph(t *testing.T) {
	t.Parallel()
	files := shop("instances")
	files["/old/a.meaning.yaml"] = strings.Replace(file(concept("a", "entity", "")), "draft-2", "draft-1", 1)
	files["/mixed/a.meaning.yaml"] = strings.Replace(file(concept("a", "entity", "")), "draft-2", "draft-1", 1)
	files["/mixed/b.meaning.yaml"] = file(concept("b", "entity", ""))
	files["/none/a.meaning.yaml"] = "a: b\n"
	got := execute(files, "check", "--format", "json", "/shop", "/old", "/mixed", "/none")
	var report struct {
		Schema struct {
			Format     string
			CoreCommit string `json:"core_commit"`
			Schemas    []struct {
				Format     string
				CoreCommit string `json:"core_commit"`
			}
		}
		Graphs []struct {
			Paths    []string
			Format   string
			Warnings int
			Findings []meaning.Finding
		}
	}
	if err := json.Unmarshal([]byte(got.stdout), &report); err != nil {
		t.Fatalf("%v\n%s", err, got.stdout)
	}
	// The keys of the report before draft 2 are what they were; the schemas of each format are listed beside them.
	if report.Schema.Format != meaning.Draft1 || report.Schema.CoreCommit != meaning.SchemaCommit() || len(report.Schema.Schemas) != 2 ||
		report.Schema.Schemas[0].Format != meaning.Draft1 || report.Schema.Schemas[1].Format != meaning.Draft2 || report.Schema.Schemas[1].CoreCommit != meaning.SchemaCommitFor(meaning.Draft2) {
		t.Fatalf("schema %+v", report.Schema)
	}
	formats := map[string]string{}
	for _, g := range report.Graphs {
		formats[g.Paths[0]] = g.Format
	}
	if formats["/shop"] != meaning.Draft2 || formats["/old"] != meaning.Draft1 || formats["/mixed"] != "" || formats["/none"] != "" {
		t.Errorf("formats %v", formats)
	}
	if !strings.Contains(got.stdout, `"rule": "format-mixed"`) {
		t.Errorf("no format-mixed:\n%s", got.stdout)
	}
	if strings.Contains(got.stdout, `"format": ""`) {
		t.Errorf("a graph with no format has no key for it:\n%s", got.stdout)
	}
}

func TestACheckOfAnEarlierFormatFileWarnsAndPasses(t *testing.T) {
	t.Parallel()
	text := strings.Replace(file(concept("a", "attribute", "")), "draft-2", "draft-1", 1)
	got := execute(map[string]string{"/g/a.meaning.yaml": text}, "check", "/g")
	want := "/g/a.meaning.yaml:1: warning: the file is in meaning/draft-1, the earlier format; it is read in full. Write meaning/draft-2 with meaninggraph rewrite --write (a file made by a generator is changed in its generator) [earlier-format]\n" +
		"ok: /g: 1 concept, 1 file, 1 warning\n"
	if got.code != ExitClean || got.stdout != want || got.stderr != "" {
		t.Fatalf("got %+v\nwant %q", got, want)
	}
}

func TestSchemaPrintsEitherFormatsSchemaByteForByte(t *testing.T) {
	t.Parallel()
	file, err := os.ReadFile("../../pkg/meaning/meaning.draft-2.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	got := execute(nil, "schema", "--draft", "2")
	if got.code != ExitClean || got.stdout != string(file) || got.stdout != string(meaning.SchemaJSONFor(meaning.Draft2)) {
		t.Fatalf("code %d, %d bytes, the file has %d", got.code, len(got.stdout), len(file))
	}
	if first := execute(nil, "schema", "--draft", "1"); first.stdout != string(meaning.SchemaJSON()) || first.stdout != execute(nil, "schema").stdout {
		t.Fatalf("--draft 1 is the default")
	}
	if got.stdout == execute(nil, "schema").stdout {
		t.Fatal("the two formats have one schema")
	}
	source := execute(nil, "schema", "--draft", "2", "--source")
	if source.code != ExitClean || source.stdout != meaning.SchemaCommitFor(meaning.Draft2)+"\n" {
		t.Fatalf("got %+v", source)
	}
	if bad := execute(nil, "schema", "--draft", "3"); bad.code != ExitUsage || bad.stdout != "" || !strings.Contains(bad.stderr, "invalid --draft 3: expected 1 or 2") {
		t.Fatalf("got %+v", bad)
	}
}
