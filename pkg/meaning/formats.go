package meaning

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// Both formats are read through one vocabulary (FORMAT.md of
// github.com/meaninggraph/core, "Reading both formats"). A file is validated
// against the schema of its own format, and the earlier words are then read as
// the current ones: the kind attribute as property, the binding key property as
// field, the role entity as instances and the role foreign-key as reference.
// After that one set of rules serves both formats.

// Rule identifiers of the findings that the second format added. They are as
// stable as the others: scripts may match on them.
const (
	// RuleFormatMixed: the files of one graph are in two formats.
	RuleFormatMixed = "format-mixed"
	// RuleFormatWord: a word that belongs to the other format.
	RuleFormatWord = "format-word"
	// RuleEarlierFormat: a file is in the earlier format (a warning).
	RuleEarlierFormat = "earlier-format"
	// RuleEarlierRoleName: a Draft2 file uses entity or foreign-key (a warning).
	RuleEarlierRoleName = "earlier-role-name"
	// RuleRetiredValue: a unit names a retired value (a warning).
	RuleRetiredValue = "retired-value"
)

// formatError is what a file with no known format is refused with.
const formatError = "must be meaning/draft-1 or meaning/draft-2"

// kindOf is a concept's kind in the current words. The kinds property and
// value-set are read only in a file that says Draft2: in any other file they
// are no kind this check knows ("" is returned), as they were before draft 2,
// and the concept matches no rule that names a kind. The earlier kind attribute
// is read as property.
func kindOf(c *Concept) string {
	switch {
	case c.Kind == "attribute":
		return "property"
	case (c.Kind == "property" || c.Kind == "value-set") && c.format != Draft2:
		return ""
	}
	return c.Kind
}

// isEntityLike says whether a concept stands where an entity stands: an entity,
// or a value set.
func isEntityLike(c *Concept) bool {
	k := kindOf(c)
	return k == "entity" || k == "value-set"
}

// entityLikeWords names what of, values-of and units-of may name, in the words
// of the file that makes the reference.
func entityLikeWords(format string) string {
	if format == Draft2 {
		return "an entity or a value-set"
	}
	return "an entity"
}

// roleOf is a binding's role in the current words.
func roleOf(b Binding) string {
	switch b.Role {
	case "entity":
		return "instances"
	case "foreign-key":
		return "reference"
	}
	return b.Role
}

// referenceWord is the role of a reference in the words of a format.
func referenceWord(format string) string {
	if format == Draft2 {
		return "reference"
	}
	return "foreign-key"
}

// instancesWord is the role of the rows of a record type in the words of a
// format.
func instancesWord(format string) string {
	if format == Draft2 {
		return "instances"
	}
	return "entity"
}

// Member is the name of the field of a record type that a binding names, under
// the key that the file's format uses (field in Draft2, property otherwise), and
// else under the other key, in a file that the schema refuses.
func (b Binding) Member(format string) string {
	if format == Draft2 {
		return cmp.Or(b.Field, b.Property)
	}
	return cmp.Or(b.Property, b.Field)
}

// extendsKinds says which kinds a concept of a kind may extend, in the current
// words: extends means "is a kind of". A property and a dimension are both a
// property of an entity (a dimension is one that answers are grouped by), so
// they may extend each other; an entity and a value set are one class here.
var extendsKinds = map[string][]string{
	"entity":    {"entity", "value-set"},
	"value-set": {"value-set", "entity"},
	"property":  {"property", "dimension"},
	"dimension": {"dimension", "property"},
	"measure":   {"measure"},
}

// What a measure may be computed from, and what it may be grouped by, in the
// current words.
var (
	measureInputs     = []string{"property", "measure"}
	measureDimensions = []string{"dimension", "property"}
)

// wordsOf writes kinds in the words of a format: Draft1 has no value-set and
// calls a property an attribute.
func wordsOf(kinds []string, format string) []string {
	if format == Draft2 {
		return kinds
	}
	var out []string
	for _, kind := range kinds {
		switch kind {
		case "value-set":
		case "property":
			out = append(out, "attribute")
		default:
			out = append(out, kind)
		}
	}
	return out
}

// pluralPropertyWord is the plural of the kind of a property in the words of a
// format.
func pluralPropertyWord(format string) string {
	if format == Draft2 {
		return "properties"
	}
	return "attributes"
}

// schemaFor is the validator of a file: of the format it says, or the Draft1
// validator for a file that says none (the format is then refused before the
// validator is asked anything but whether the document is a mapping).
func (c Checker) schemaFor(format string) Validator {
	if format == Draft2 {
		if c.SchemaDraft2 != nil {
			return c.SchemaDraft2
		}
		return Draft2Schema()
	}
	if c.Schema != nil {
		return c.Schema
	}
	return DefaultSchema()
}

// fileSchemaProblems validates a file against the schema of its own format. A
// mapping with no known format gets the one finding that its format is refused,
// and nothing else is applied to it: the words of a file with no known format
// belong to no format.
func fileSchemaProblems(f *File, draft1, draft2 Validator) []schemaProblem {
	if f.Root.Kind == Map && !KnownFormat(f.Format) {
		return []schemaProblem{{path: []string{"format"}, message: formatError}}
	}
	v := draft1
	if f.Format == Draft2 {
		v = draft2
	}
	return schemaProblems(v, f.Root)
}

// formatWord is a word that belongs to the other format.
type formatWord struct {
	line    int
	message string
}

// formatWords lists, for a file that says Draft1 or Draft2, the words of the
// other format. The schema of the file's format refuses them as well; the
// finding says which format the word belongs to.
func formatWords(f *File) []formatWord {
	var found []formatWord
	other := Draft1
	if f.Format == Draft1 {
		other = Draft2
	}
	add := func(line int, where, format string, args ...any) {
		found = append(found, formatWord{line, fmt.Sprintf("%s: format-word: %s", where, fmt.Sprintf(format, args...))})
	}
	for _, c := range f.Concepts {
		where := "concept " + c.ID
		if f.Format == Draft1 {
			if c.Kind == "property" {
				add(c.lineOf("kind"), where, "kind property belongs to %s; in %s it is written attribute", other, f.Format)
			}
			if c.Kind == "value-set" {
				add(c.lineOf("kind"), where, "kind value-set belongs to %s", other)
			}
			if c.node.Field("complete") != nil {
				add(c.lineOf("complete"), where, "complete belongs to %s", other)
			}
			for i, v := range c.node.Field("values").items() {
				if v.Field("retired") != nil {
					add(v.Field("retired").Line, where+": value "+c.Values[i].ID, "retired belongs to %s", other)
				}
			}
		} else {
			if c.Kind == "attribute" {
				add(c.lineOf("kind"), where, "kind attribute is written property in %s", f.Format)
			}
			if c.node.Field("values") != nil && c.Kind != "value-set" {
				add(c.lineOf("values"), where, "values belongs on a concept of kind value-set; name that concept with values-of")
			}
		}
		for _, b := range c.node.Field("bindings").items() {
			at := where + ": binding " + b.text("model")
			hasProperty, hasField := b.Field("property") != nil, b.Field("field") != nil
			switch {
			case hasProperty && hasField:
				writes := "property"
				if f.Format == Draft2 {
					writes = "field"
				}
				add(b.Line, at, "the binding has both property: and field:; %s writes %s", f.Format, writes)
			case f.Format == Draft1 && hasField:
				add(b.Line, at, "the binding key field belongs to %s; in %s it is written property", other, f.Format)
			case f.Format == Draft2 && hasProperty:
				add(b.Line, at, "the binding key property is written field in %s", f.Format)
			}
		}
	}
	return found
}

// earlierRoleNames lists, in order of first use and once each, the earlier role
// names (entity, foreign-key) that the bindings of a file use, and the line of
// the first binding that uses one.
func earlierRoleNames(f *File) (names []string, line int) {
	for _, c := range f.Concepts {
		for _, b := range c.Bindings {
			if (b.Role == "entity" || b.Role == "foreign-key") && !slices.Contains(names, b.Role) {
				if names == nil {
					line = b.Line
				}
				names = append(names, b.Role)
			}
		}
	}
	return names, line
}

// formatsOf lists the known formats that the files of a graph say, in order of
// first use, each with the first file that says it.
func formatsOf(g *Graph) (formats []string, firstFile map[string]*File) {
	firstFile = map[string]*File{}
	for _, f := range g.Files {
		if f.ParseErr == nil && KnownFormat(f.Format) && firstFile[f.Format] == nil {
			firstFile[f.Format] = f
			formats = append(formats, f.Format)
		}
	}
	return formats, firstFile
}

// Format is the format of a graph: the format its files say when they say one
// and the same, and "" when no file says a known format or the files say two.
func (g *Graph) Format() string {
	formats, _ := formatsOf(g)
	if len(formats) != 1 {
		return ""
	}
	return formats[0]
}

// formatMixed describes a graph whose files say two formats, for a finding.
func formatMixed(formats []string, firstFile map[string]*File) string {
	parts := make([]string, len(formats))
	for i, format := range formats {
		parts[i] = fmt.Sprintf("%s (%s)", format, firstFile[format].Path)
	}
	return "the meaning files of one graph change format together, and these are in " + strings.Join(parts, " and ")
}
