package meaning

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func parse(t *testing.T, text string) *Node {
	t.Helper()
	n, err := ParseYAML([]byte(text))
	if err != nil {
		t.Fatalf("ParseYAML(%q): %v", text, err)
	}
	return n
}

func TestParseYAMLResolvesScalarsLikeTheReferenceChecker(t *testing.T) {
	t.Parallel()
	doc := parse(t, strings.Join([]string{
		"null1: ~", "null2: null", "null3:", "null4: NULL",
		"yes: yes", "on: on", "tru: True", "fal: FALSE",
		"dec: 840", "plus: +5", "neg: -3", "zeros: 007", "oct: 0o17", "hex: 0xFF", "big: 123456789012345678901234567890",
		"flt: 1.5", "exp: 1e3", "dot: .5", "trail: 1.",
		"date: 2025-01-01", "under: 1_000", "legacyoct: 0777", "str: '840'", "dq: \"true\"",
		"lit: |", "  text", "tagged: !!str 5", "word: hello",
		"alias: &a hello", "ref: *a",
	}, "\n")+"\n")
	want := map[string]any{
		"null1": nil, "null2": nil, "null3": nil, "null4": nil,
		"yes": "yes", "on": "on", "tru": true, "fal": false,
		"dec": json.Number("840"), "plus": json.Number("5"), "neg": json.Number("-3"), "zeros": json.Number("7"),
		"oct": json.Number("15"), "hex": json.Number("255"), "big": json.Number("123456789012345678901234567890"),
		"flt": json.Number("1.5"), "exp": json.Number("1000"), "dot": json.Number("0.5"), "trail": json.Number("1"),
		"date": "2025-01-01", "under": "1_000", "legacyoct": json.Number("777"), "str": "840", "dq": "true",
		"lit": "text\n", "tagged": "5", "word": "hello", "alias": "hello", "ref": "hello",
	}
	if got := doc.Value(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Value() =\n%#v\nwant\n%#v", got, want)
	}
}

func TestParseYAMLStructure(t *testing.T) {
	t.Parallel()
	doc := parse(t, "a:\n  - x\n  - b: 1\nc: d\n")
	if doc.Kind != Map || !reflect.DeepEqual(doc.Keys, []string{"a", "c"}) || doc.Line != 1 {
		t.Fatalf("doc = %+v", doc)
	}
	if s, ok := doc.Field("c").Str(); !ok || s != "d" {
		t.Fatal("Field/Str")
	}
	if _, ok := doc.Field("a").Str(); ok {
		t.Fatal("a sequence is no string")
	}
	if doc.Field("missing") != nil || doc.Field("a").Field("x") != nil {
		t.Fatal("Field of a missing key or of a non-map is nil")
	}
	var none *Node
	if none.Field("a") != nil {
		t.Fatal("Field of nil is nil")
	}
	if _, ok := none.Str(); ok {
		t.Fatal("Str of nil")
	}
	if got := doc.Value(); !reflect.DeepEqual(got, map[string]any{"a": []any{"x", map[string]any{"b": json.Number("1")}}, "c": "d"}) {
		t.Fatalf("Value() = %#v", got)
	}
}

func TestParseYAMLEmptyInputIsNull(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"", "# only a comment\n", "---\n", "--- # nothing\n...\n"} {
		n, err := ParseYAML([]byte(text))
		if err != nil || n.Kind != Null || n.Value() != nil {
			t.Errorf("ParseYAML(%q) = %+v, %v", text, n, err)
		}
	}
}

func TestParseYAMLRefusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, text string
		line       int
		message    string
	}{
		{"syntax", "a: b\n  c: d\n", 2, "mapping values"},
		{"tab", "a:\n\t- b\n", 2, "cannot start any token"},
		{"two documents", "a: 1\n---\nb: 2\n", 2, "more than one"},
		//, "a: 1\n---\n[\n", 0, "more than one"},
		{"repeated key", "a: 1\nb: 2\na: 3\n", 3, `"a" is repeated`},
		{"complex key", "? [a]\n: b\n", 1, "must be a scalar"},
		{"tag", "a: !!int 5\n", 1, "!!int is not supported"},
		{"custom tag", "a: !x 5\n", 1, "!x is not supported"},
		{"inf", "a: .inf\n", 1, "not a finite number"},
		{"neg inf", "a: -.INF\n", 1, "not a finite number"},
		{"nan", "a: .nan\n", 1, "not a finite number"},
		{"overflow", "a: 1e999\n", 1, "not a finite number"},
		{"bad escape", "a: \"\\q\"\n", 0, "escape"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseYAML([]byte(tc.text))
			var syntax *SyntaxError
			if !errors.As(err, &syntax) {
				t.Fatalf("err = %v, want a *SyntaxError", err)
			}
			if !strings.Contains(syntax.Error(), tc.message) {
				t.Errorf("message = %q, want it to contain %q", syntax.Message, tc.message)
			}
			if tc.line != 0 && syntax.Line != tc.line {
				t.Errorf("line = %d, want %d", syntax.Line, tc.line)
			}
		})
	}
}

func TestParseYAMLAliasesAreCopiedAndBounded(t *testing.T) {
	t.Parallel()
	doc := parse(t, "base: &b {x: 1}\nuse: *b\n<<: *b\n")
	if !reflect.DeepEqual(doc.Keys, []string{"base", "use", "<<"}) {
		t.Fatalf("merge keys are plain keys, got %v", doc.Keys)
	}
	if doc.Field("use").Field("x") == nil {
		t.Fatal("an alias is replaced by its content")
	}
	keyed := parse(t, "k: &k name\n*k : v\n")
	if keyed.Field("name") == nil {
		t.Fatal("an alias may be a key")
	}
	// A "billion laughs" document expands past the bound and is refused.
	laughs := "a: &a [x, x, x, x]\nb: &b [*a, *a, *a, *a]\nc: [*b, *b, *b, *b]\n"
	if _, err := parseYAML([]byte(laughs), 50); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("err = %v, want the size bound", err)
	}
	if _, err := parseYAML([]byte(laughs), maxNodes); err != nil {
		t.Fatalf("the same document is fine under the real bound: %v", err)
	}
}

func TestAtFindsTheLineOfAPath(t *testing.T) {
	t.Parallel()
	doc := parse(t, "a:\n  - x\n  - b: 1\n    c: 2\nd: e\n")
	tests := []struct {
		path []string
		line int
	}{
		{nil, 1},
		{[]string{"a"}, 2},
		{[]string{"a", "1", "c"}, 4},
		{[]string{"d"}, 5},
		{[]string{"missing"}, 1},
		{[]string{"a", "9"}, 2},
		{[]string{"a", "x"}, 2},
		{[]string{"a", "-1"}, 2},
		{[]string{"d", "deeper"}, 5},
	}
	for _, tc := range tests {
		if got := doc.At(tc.path); got != tc.line {
			t.Errorf("At(%v) = %d, want %d", tc.path, got, tc.line)
		}
	}
}
