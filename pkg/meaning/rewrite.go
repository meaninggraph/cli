package meaning

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
)

// ValuesConcept is a concept of a meaning/draft-1 file that carries values. In
// meaning/draft-2 a list of values stands on a concept of kind value-set and
// nowhere else, and whether a list on an entity, an attribute or a dimension
// becomes a value set, or moves to one, is a decision about the concept: a
// rewrite lists such a concept and does not change it.
type ValuesConcept struct {
	ID, Kind string
	// Line is the line of the values.
	Line int
}

// Rewritten is a meaning file written in meaning/draft-2.
type Rewritten struct {
	// Text is the rewritten file; it is the file itself when nothing changed.
	Text []byte
	// Format, Kinds, Keys and Roles count what was changed: the format line, the
	// kinds attribute (now property), the binding keys property (now field) and
	// the role names entity and foreign-key (now instances and reference).
	Format, Kinds, Keys, Roles int
	// Values lists the concepts that carry values and were left as they are.
	Values []ValuesConcept
}

// Changed says whether the rewrite changed the file.
func (r Rewritten) Changed() bool { return r.Format+r.Kinds+r.Keys+r.Roles > 0 }

// ErrRewrite is wrapped by the error of a file that cannot be rewritten.
var ErrRewrite = errors.New("cannot be rewritten")

// RewriteDraft2 writes a hand-written meaning/draft-1 file in meaning/draft-2:
// the format line, the kind attribute (property), the binding key property
// (field) and the role names entity and foreign-key (instances and reference).
// It changes those words in place and nothing else, comments and layout
// included; a file in meaning/draft-2 is returned as it is. A concept that
// carries values is listed in Rewritten.Values and left alone.
//
// It edits a line only where it finds the word once, in the form the formats
// write it, and it reads its own result: the file it returns holds the data of
// the file it was given, with those words changed and no other difference. A
// file that this cannot be shown for is an error (it wraps ErrRewrite) and there
// is no result: the file is changed by hand.
func RewriteDraft2(data []byte) (*Rewritten, error) {
	root, err, _ := parseYAML(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrRewrite, err.Message)
	}
	f := decodeFile("", data)
	if f.Format == Draft2 {
		return &Rewritten{Text: data}, nil
	}
	if f.Format != Draft1 {
		return nil, fmt.Errorf("%w: the file says no format that is read (%s)", ErrRewrite, formatError)
	}
	lines := bytes.SplitAfter(data, []byte("\n"))
	result := &Rewritten{}
	var failure error
	edit := func(line int, count *int, word, pattern, replacement string) {
		if failure != nil {
			return
		}
		re := regexp.MustCompile(pattern)
		text := ""
		if line >= 1 && line <= len(lines) {
			text = string(lines[line-1])
		}
		if found := re.FindAllStringIndex(text, -1); len(found) != 1 {
			failure = fmt.Errorf("%w: line %d does not hold %s exactly once in the form it is written in a file; edit it by hand", ErrRewrite, line, word)
			return
		}
		lines[line-1] = []byte(re.ReplaceAllString(text, replacement)) // a line was found to hold the word
		*count++
	}
	edit(root.Field("format").Line, &result.Format, "the format line", `(\bformat:\s*['"]?)meaning/draft-1\b`, "${1}"+Draft2)
	for _, c := range f.Concepts {
		if c.Kind == "attribute" {
			edit(c.lineOf("kind"), &result.Kinds, "kind: attribute", `(\bkind:\s*['"]?)attribute\b`, "${1}property")
		}
		if c.node.Field("values") != nil {
			result.Values = append(result.Values, ValuesConcept{ID: c.ID, Kind: c.Kind, Line: c.lineOf("values")})
		}
		for _, b := range c.node.Field("bindings").items() {
			if b.Field("property") != nil {
				edit(b.Field("property").Line, &result.Keys, "the binding key property", `\bproperty:`, "field:")
			}
			switch b.text("role") {
			case "entity":
				edit(b.Field("role").Line, &result.Roles, "role: entity", `(\brole:\s*['"]?)entity\b`, "${1}instances")
			case "foreign-key":
				edit(b.Field("role").Line, &result.Roles, "role: foreign-key", `(\brole:\s*['"]?)foreign-key\b`, "${1}reference")
			}
		}
	}
	if failure != nil {
		return nil, failure
	}
	result.Text = bytes.Join(lines, nil)
	again, err, _ := parseYAML(result.Text)
	if err != nil || !reflect.DeepEqual(inDraft2Words(root.Value()), again.Value()) {
		return nil, fmt.Errorf("%w: the file read after the rewrite does not hold the data of the file before it, with those words changed; edit the file by hand", ErrRewrite)
	}
	return result, nil
}

// inDraft2Words is the data of a meaning/draft-1 file with the words that
// RewriteDraft2 changes, changed.
func inDraft2Words(v any) any {
	doc := v.(map[string]any) // a file that says a format is a mapping
	doc["format"] = Draft2
	concepts, _ := doc["concepts"].([]any)
	for _, item := range concepts {
		concept, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if concept["kind"] == "attribute" {
			concept["kind"] = "property"
		}
		bindings, _ := concept["bindings"].([]any)
		for _, item := range bindings {
			if binding, ok := item.(map[string]any); ok {
				if p, has := binding["property"]; has {
					delete(binding, "property")
					binding["field"] = p
				}
				switch binding["role"] {
				case "entity":
					binding["role"] = "instances"
				case "foreign-key":
					binding["role"] = "reference"
				}
			}
		}
	}
	return doc
}

// Describe says in a line what a rewrite changed.
func (r Rewritten) Describe() string {
	var parts []string
	add := func(n int, what string) {
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, what))
		}
	}
	add(r.Format, "format line")
	add(r.Kinds, "kind attribute")
	add(r.Keys, "binding key property")
	add(r.Roles, "role name")
	return strings.Join(parts, ", ")
}
