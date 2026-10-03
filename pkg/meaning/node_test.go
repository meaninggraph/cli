package meaning

import (
	"encoding/json"
	"reflect"
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
