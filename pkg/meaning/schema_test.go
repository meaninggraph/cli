package meaning

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"testing"
)

const minimalFile = `format: meaning/draft-1
id: demo
name: Demo
description: A demo.
concepts:
  - id: thing
    kind: entity
    labels: {en: Thing}
    description: A thing.
`

func problemsOf(t *testing.T, v Validator, text string) []string {
	t.Helper()
	var out []string
	for _, p := range schemaProblems(v, parse(t, text)) {
		out = append(out, p.String())
	}
	return out
}

func TestEmbeddedSchemaMatchesItsRecordedSource(t *testing.T) {
	t.Parallel()
	fields := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(SchemaSource()), "\n") {
		key, value, _ := strings.Cut(line, ": ")
		fields[key] = value
	}
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(fields["commit"]) {
		t.Errorf("commit = %q, want a full commit id", fields["commit"])
	}
	if fields["repository"] != "https://github.com/meaninggraph/core" || fields["path"] != "meaning.schema.json" {
		t.Errorf("source = %v", fields)
	}
	if SchemaCommit() != fields["commit"] {
		t.Errorf("SchemaCommit() = %q", SchemaCommit())
	}
	sum := sha256.Sum256(SchemaJSON())
	if got := hex.EncodeToString(sum[:]); got != fields["sha256"] {
		t.Errorf("embedded schema sha256 = %s, but meaning.schema.source records %s", got, fields["sha256"])
	}
}

func TestDefaultSchemaAcceptsAMinimalFileAndRefusesBrokenOnes(t *testing.T) {
	t.Parallel()
	schema := DefaultSchema()
	if got := problemsOf(t, schema, minimalFile); len(got) != 0 {
		t.Fatalf("a minimal file is refused: %v", got)
	}
	tests := map[string]struct{ text, want string }{
		"missing format":   {strings.Replace(minimalFile, "format: meaning/draft-1\n", "", 1), "format"},
		"wrong format":     {strings.Replace(minimalFile, "draft-1", "draft-2", 1), "/format"},
		"unknown key":      {minimalFile + "extra: 1\n", "extra"},
		"bad kind":         {strings.Replace(minimalFile, "kind: entity", "kind: thing", 1), "/concepts/0/kind"},
		"bad id":           {strings.Replace(minimalFile, "id: thing", "id: Thing", 1), "/concepts/0/id"},
		"no concepts":      {strings.Replace(minimalFile, "concepts:", "concepts: []\nx:", 1), "concepts"},
		"wrong yaml type":  {strings.Replace(minimalFile, "name: Demo", "name: 12", 1), "/name"},
		"null description": {strings.Replace(minimalFile, "description: A demo.", "description:", 1), "/description"},
		"bad uri":          {minimalFile + "sources:\n  - {id: s, provider: p, dataset: d, url: not a uri}\n", "/sources/0/url"},
		"root not a map":   {"- a\n", "object"},
	}
	for name, tc := range tests {
		got := problemsOf(t, schema, tc.text)
		if len(got) == 0 || !strings.Contains(strings.Join(got, "\n"), tc.want) {
			t.Errorf("%s: problems = %v, want one about %q", name, got, tc.want)
		}
	}
	ok := minimalFile + "sources:\n  - {id: s, provider: p, dataset: d, url: 'https://example.com/a?b=c#d', year: 2025}\n"
	if got := problemsOf(t, schema, ok); len(got) != 0 {
		t.Errorf("a valid uri and an integer year are refused: %v", got)
	}
	if got := problemsOf(t, schema, minimalFile+"sources:\n  - {id: s, provider: p, dataset: d, year: 2025.5}\n"); len(got) == 0 {
		t.Error("a fractional year is refused by the schema")
	}
}

func TestURIFormatAgreesWithTheReferenceChecker(t *testing.T) {
	t.Parallel()
	// Verdicts of ajv-formats 3.0.1 for the same strings (see scripts/differential).
	valid := []string{"https://example.com", "http://a.b/c?d=e#f", "urn:isbn:0451450523", "mailto:a@b.co", "https://[::1]:8080/x", "file:///etc/hosts"}
	invalid := []string{"not a uri", "example.com", "", "https://exa mple.com", "//example.com", "/path/only", "http://a.com/a b", "1http://x.y"}
	for _, s := range valid {
		if !isURI(s) {
			t.Errorf("isURI(%q) = false", s)
		}
	}
	for _, s := range invalid {
		if isURI(s) {
			t.Errorf("isURI(%q) = true", s)
		}
	}
}

type failing struct{ err error }

func (f failing) Validate(any) error { return f.err }

func TestSchemaProblemsReportsAnyValidatorError(t *testing.T) {
	t.Parallel()
	got := schemaProblems(failing{errors.New("boom")}, parse(t, "a: 1"))
	if len(got) != 1 || got[0].String() != "/ boom" {
		t.Fatalf("problems = %v", got)
	}
	if got := schemaProblems(failing{}, parse(t, "a: 1")); got != nil {
		t.Fatalf("no error, no problems; got %v", got)
	}
}

func TestCompileSchemaRefusesBadSchemas(t *testing.T) {
	t.Parallel()
	if _, err := compileSchema("test.json", []byte("{not json")); err == nil {
		t.Error("invalid JSON must fail")
	}
	if _, err := compileSchema("test.json", []byte(`{"type": 5}`)); err == nil {
		t.Error("an invalid schema must fail")
	}
	if _, err := compileSchema("test.json", []byte(`{"type": "object"}`)); err != nil {
		t.Errorf("a valid schema compiles: %v", err)
	}
}

func TestMustPanicsOnError(t *testing.T) {
	t.Parallel()
	if must(5, nil) != 5 {
		t.Fatal("must returns the value")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("must(err) must panic")
		}
	}()
	must(0, errors.New("boom"))
}

func TestCommitOfASource(t *testing.T) {
	t.Parallel()
	if got := commitOf("repository: x\ncommit: abc\npath: y\n"); got != "abc" {
		t.Fatalf("commit = %q", got)
	}
	if got := commitOf("repository: x\n"); got != "" {
		t.Fatalf("no commit line, no commit; got %q", got)
	}
}

func TestSchemaAccessorsReturnCopies(t *testing.T) {
	t.Parallel()
	first := SchemaJSON()
	first[0] = '!'
	if SchemaJSON()[0] == '!' {
		t.Fatal("SchemaJSON must not hand out the embedded bytes")
	}
}
