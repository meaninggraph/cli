package meaning

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/meaninggraph/cli/internal/memfs"
)

const goodHCL = `# a comment
// another
/* a block
   comment */
entity "Album" {
  key = ["AlbumId"]
  use = ["Audit"]
  property "AlbumId" {
    type     = "int"
    required = true
    min_len  = -1.5
  }
  property "Title" {
    type    = "string"
    pattern = "a\tb\n\"\\"
  }
  property "ArtistId" {
    entity = "Artist"
  }
}
entity "Artist" {
  key = ["ArtistId", 7, ]
  property "ArtistId" { type = "int" }
}
entity "Loose" {
  key = ""
  property "Flag" {
    type   = 5
    entity = false
  }
  property "Other" {
    entity = 0
  }
  property "Third" {
    entity = [1]
  }
}
component "Audit" {
  field "CreatedAt" { type = "datetime" }
}
enum "Genre" {
  values = ["rock", "jazz"]
}
`

func TestHCLReaderReadsAModel(t *testing.T) {
	t.Parallel()
	fsys := memfs.New(map[string]string{"/m/chinook.hcl": goodHCL})
	model, err := HCLReader{}.ReadModel(fsys, "/m/chinook.hcl")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]*Entity{
		"Album": {Key: []string{"AlbumId"}, Properties: map[string]Property{
			"AlbumId": {Type: "int"}, "Title": {Type: "string"}, "ArtistId": {Reference: true, Entity: "Artist"},
		}},
		"Artist": {Key: []string{"ArtistId"}, Properties: map[string]Property{"ArtistId": {Type: "int"}}},
		"Loose":  {Properties: map[string]Property{"Flag": {}, "Other": {}, "Third": {Reference: true}}},
	}
	if !reflect.DeepEqual(model.Entities, want) {
		t.Fatalf("entities = %+v\nwant %+v", model.Entities, want)
	}
	if _, err := (HCLReader{}).ReadModel(fsys, "/m/missing.hcl"); err == nil {
		t.Fatal("a missing file is an error")
	}
}

func TestHCLReaderDoesNotTurnAnAttributeIntoAnEntityName(t *testing.T) {
	t.Parallel()
	fsys := memfs.New(map[string]string{"/m.hcl": "entity \"true\" {\n  property \"T\" { entity = true }\n  property \"N\" { entity = 5 }\n  property \"S\" { entity = \"true\" }\n}\n"})
	model, err := HCLReader{}.ReadModel(fsys, "/m.hcl")
	if err != nil {
		t.Fatal(err)
	}
	props := model.Entities["true"].Properties
	if !props["T"].Reference || props["T"].Entity != "" || !props["N"].Reference || props["N"].Entity != "" {
		t.Fatalf("a reference whose entity is not a string names no entity: %+v %+v", props["T"], props["N"])
	}
	if !props["S"].Reference || props["S"].Entity != "true" {
		t.Fatalf("a string names the entity: %+v", props["S"])
	}
}

func TestHCLReaderRefusesNamesOfObjectPrototype(t *testing.T) {
	t.Parallel()
	names := PrototypeNames()
	if len(names) != 12 || !isPrototypeName("constructor") || !isPrototypeName("__proto__") || isPrototypeName("Constructor") {
		t.Fatalf("names = %v", names)
	}
	names[0] = "changed"
	if PrototypeNames()[0] == "changed" {
		t.Fatal("PrototypeNames returns a copy")
	}
	for _, name := range PrototypeNames() {
		for what, text := range map[string]string{
			"entity":    "entity \"" + name + "\" {\n}\n",
			"component": "component \"" + name + "\" {\n}\n",
			"enum":      "enum \"" + name + "\" {\n}\n",
			"property":  "entity \"E\" {\n  property \"" + name + "\" { type = \"int\" }\n}\n",
			"field":     "component \"C\" {\n  field \"" + name + "\" { type = \"int\" }\n}\n",
		} {
			fsys := memfs.New(map[string]string{"/m.hcl": text})
			_, err := HCLReader{}.ReadModel(fsys, "/m.hcl")
			if err == nil || !strings.Contains(err.Error(), what+" \""+name+"\" has the name of a property of JavaScript's Object.prototype") || !strings.Contains(err.Error(), "the reference checker") {
				t.Errorf("%s %s: error = %v", what, name, err)
			}
		}
		// The reference parser asks `name in attributes` for the attribute names of
		// every block, so a prototype name is refused there too, wherever the block is.
		for where, text := range map[string]string{
			"a property":  "entity \"E\" {\n  property \"p\" {\n    type = \"int\"\n    " + name + " = \"x\"\n  }\n}\n",
			"a field":     "component \"C\" {\n  field \"f\" { " + name + " = 1 }\n}\n",
			"a component": "component \"C\" { " + name + " = 1 }\n",
			"an enum":     "enum \"E\" { " + name + " = true }\n",
			"an entity":   "entity \"E\" { " + name + " = true }\n",
			"the file":    name + " = 1\n",
		} {
			fsys := memfs.New(map[string]string{"/m.hcl": text})
			_, err := HCLReader{}.ReadModel(fsys, "/m.hcl")
			if err == nil || !strings.Contains(err.Error(), "attribute \""+name+"\" has the name of a property of JavaScript's Object.prototype") {
				t.Errorf("%s as an attribute of %s: error = %v", name, where, err)
			}
		}
	}
	// A name that only looks like one is an ordinary attribute.
	ok := memfs.New(map[string]string{"/m.hcl": "entity \"E\" {\n  property \"p\" {\n    type = \"int\"\n    Constructor = 1\n    to_string = 2\n  }\n}\n"})
	if _, err := (HCLReader{}).ReadModel(ok, "/m.hcl"); err != nil {
		t.Fatalf("error = %v", err)
	}
}

func TestHCLReaderLimits(t *testing.T) {
	t.Parallel()
	read := func(text string) error {
		_, err := (HCLReader{}).ReadModel(memfs.New(map[string]string{"/m.hcl": text}), "/m.hcl")
		return err
	}
	// The size limit: exactly the limit is read, one byte more is refused, and
	// the message says what the limit is.
	if err := read("entity \"E\" {}" + strings.Repeat(" ", MaxFileBytes-len("entity \"E\" {}"))); err != nil {
		t.Fatalf("a file of exactly %d bytes: %v", MaxFileBytes, err)
	}
	if err := read(strings.Repeat(" ", MaxFileBytes+1)); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("at most %d", MaxFileBytes)) {
		t.Fatalf("a file one byte over the limit: %v", err)
	}
	// Two levels of blocks (an entity and its properties) are what ModelSpec v0
	// has: they are read. A block inside the block of a property is the third
	// level, refused on its own line, however many blocks follow it.
	if err := read("entity \"E\" {\n  property \"p\" {\n    type = \"int\"\n  }\n}\n"); err != nil {
		t.Fatalf("two levels: %v", err)
	}
	for _, blocks := range []int{1, 2_000} {
		err := read("entity \"E\" {\n  property \"p\" {\n" + strings.Repeat("x \"y\" {\n", blocks))
		if err == nil || !strings.Contains(err.Error(), `line 3: blocks nested more than 2 deep are not ModelSpec v0 (x "y" is inside a block of a block)`) {
			t.Fatalf("%d blocks inside a property: %v", blocks, err)
		}
	}
}

func TestHCLReaderRefusesWhatTheReferenceParserRefuses(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, text, want string }{
		{"unterminated comment", "/* never closed", "line 1: unterminated comment"},
		{"unexpected character", "entity \"A\" { @ }", `line 1: unexpected character "@"`},
		{"a lone slash", "/ x", "unexpected character"},
		{"a lone minus", "a = -", "unexpected character"},
		{"unterminated string", "a = \"abc\nb", "line 1: unterminated string"},
		{"string at end of file", "a = \"abc", "unterminated string"},
		{"interpolation", `a = "x${y}"`, "string interpolation is not ModelSpec v0"},
		{"unsupported escape", `a = "\q"`, `unsupported escape \q`},
		{"escape at end of file", `a = "\`, "unsupported escape"},
		{"expected ident", "= 1", "line 1: expected ident, found ="},
		{"expected ident at end", "entity \"A\" {", "line EOF: expected }, found end of file"},
		{"expected string", "entity {", "expected string, found {"},
		{"expected string at end", "entity", "line EOF: expected string, found end of file"},
		{"expected brace", "entity \"A\" x", "expected {, found x"},
		{"expected closing brace", "entity \"A\" { a = 1", "expected }, found end of file"},
		{"value at end of file", "a =", "unexpected end of file in a value"},
		{"nested list", "a = [[1]]", "line 1: nested lists are not ModelSpec v0"},
		{"list not closed", "a = [1", "line EOF: expected ], found end of file"},
		{"list of nothing", "a = [", "unexpected end of file in a value"},
		{"list followed by garbage", "a = [1 2]", "expected ], found 2"},
		{"map value", "a = {", "map-style values are not ModelSpec v0 syntax"},
		{"expression", "a = foo", "foo is not a literal"},
		{"punctuation as value", "a = ]", "] is not a literal"},
		{"top-level attribute", "a = 1", "top-level attributes are not ModelSpec v0"},
		{"duplicate attribute", "entity \"A\" {\n key = [\"x\"]\n key = [\"y\"]\n}", "line 3: duplicate attribute key"},
		{"unsupported block", "table \"A\" { }", "top-level table blocks are not supported"},
		{"unsupported entity attribute", "entity \"A\" { color = \"red\" }", "unsupported entity attribute color"},
		{"entity with another block", "entity \"A\" {\n field \"f\" { }\n}", `entity "A" cannot contain a field block`},
		{"component with another block", "component \"A\" {\n property \"f\" { }\n}", `component "A" cannot contain a property block`},
		{"property with blocks", "entity \"A\" {\n property \"p\" {\n  property \"q\" { }\n }\n}", "nested more than 2 deep"},
		{"duplicate property", "entity \"A\" {\n property \"p\" { }\n property \"p\" { }\n}", `duplicate property "p" in entity "A"`},
		{"duplicate entity", "entity \"A\" { }\nentity \"A\" { }", `duplicate entity "A"`},
		{"duplicate component", "component \"A\" { }\ncomponent \"A\" { }", `duplicate component "A"`},
		{"duplicate enum", "enum \"A\" { }\nenum \"A\" { }", `duplicate enum "A"`},
		{"enum with blocks", "enum \"A\" {\n x \"y\" { }\n}", `enum "A" cannot contain blocks`},
		{"key is not a list", "entity \"A\" { key = \"Id\" }", `entity "A" key must be a list of property names`},
		{"bad component field", "component \"A\" {\n field \"f\" {\n  x \"y\" { }\n }\n}", "nested more than 2 deep"},
		{"bad entity inside", "entity \"A\" { property \"p\" { @ } }", "unexpected character"},
		{"bad value inside a block", "entity \"A\" { key = foo }", "foo is not a literal"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fsys := memfs.New(map[string]string{"/m.hcl": tc.text})
			_, err := HCLReader{}.ReadModel(fsys, "/m.hcl")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestTruthyFollowsJavaScript(t *testing.T) {
	t.Parallel()
	for v, want := range map[string]bool{"nil": false, "empty": false, "text": true, "zero": false, "one": true, "false": false, "true": true, "list": true} {
		got := truthy(map[string]any{"nil": nil, "empty": "", "text": "x", "zero": 0.0, "one": 1.0, "false": false, "true": true, "list": []any{}}[v])
		if got != want {
			t.Errorf("truthy(%s) = %v", v, got)
		}
	}
}

func TestHCLReaderReadsACommentAtTheEndOfAFile(t *testing.T) {
	t.Parallel()
	fsys := memfs.New(map[string]string{"/m.hcl": "entity \"A\" { } // trailing, no newline"})
	model, err := HCLReader{}.ReadModel(fsys, "/m.hcl")
	if err != nil || len(model.Entities) != 1 {
		t.Fatalf("model = %+v, %v", model, err)
	}
}
