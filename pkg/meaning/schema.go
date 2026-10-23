package meaning

import (
	"bytes"
	_ "embed" // the schema is embedded in the binary
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// The two formats of a meaning file, as a file writes them on its format line.
const (
	// Draft1 is the earlier format: the kind attribute, the binding key
	// property, and a list of values on an entity, an attribute or a dimension.
	Draft1 = "meaning/draft-1"
	// Draft2 is the format with the kind property, the binding key field and the
	// kind value-set.
	Draft2 = "meaning/draft-2"
)

//go:embed meaning.schema.json
var schemaJSON []byte

//go:embed meaning.schema.source
var schemaSource string

//go:embed meaning.draft-2.schema.json
var schemaDraft2JSON []byte

//go:embed meaning.draft-2.schema.source
var schemaDraft2Source string

// SchemaJSON returns meaning.schema.json of github.com/meaninggraph/core, the
// schema of format meaning/draft-1, as it is embedded. SchemaSource says which
// commit it was taken from; scripts/check-schema-drift.sh compares both with a
// checkout of core. SchemaJSONFor gives the schema of either format.
func SchemaJSON() []byte { return slices.Clone(schemaJSON) }

// SchemaSource records where SchemaJSON comes from: repository, commit, path
// and sha256 of the file, one "key: value" per line.
func SchemaSource() string { return schemaSource }

// SchemaJSONFor returns the embedded schema of a format, Draft1 or Draft2, as
// the bytes of the file of github.com/meaninggraph/core it was taken from
// (meaning.schema.json and meaning.draft-2.schema.json). It is nil for any
// other format.
func SchemaJSONFor(format string) []byte {
	switch format {
	case Draft1:
		return SchemaJSON()
	case Draft2:
		return slices.Clone(schemaDraft2JSON)
	}
	return nil
}

// SchemaSourceFor records where SchemaJSONFor(format) comes from, in the form
// of SchemaSource; it is empty for a format that is neither Draft1 nor Draft2.
func SchemaSourceFor(format string) string {
	switch format {
	case Draft1:
		return schemaSource
	case Draft2:
		return schemaDraft2Source
	}
	return ""
}

// SchemaCommitFor is the commit of github.com/meaninggraph/core the schema of a
// format was taken from.
func SchemaCommitFor(format string) string { return commitOf(SchemaSourceFor(format)) }

// KnownFormat reports whether a format line names a format that is read.
func KnownFormat(format string) bool { return format == Draft1 || format == Draft2 }

// Validator checks a value against the meaning-file schema. *jsonschema.Schema
// is one; tests supply others.
type Validator interface {
	Validate(v any) error
}

// SchemaCommit is the commit of github.com/meaninggraph/core the embedded
// schema was taken from.
func SchemaCommit() string { return commitOf(schemaSource) }

func commitOf(source string) string {
	for _, line := range strings.Split(source, "\n") {
		if value, ok := strings.CutPrefix(line, "commit: "); ok {
			return value
		}
	}
	return ""
}

// DefaultSchema returns the schema of Draft1 embedded in the binary, compiled
// once.
func DefaultSchema() Validator { return embeddedSchema() }

// Draft2Schema returns the schema of Draft2 embedded in the binary, compiled
// once.
func Draft2Schema() Validator { return embeddedDraft2Schema() }

var embeddedSchema = sync.OnceValue(func() *jsonschema.Schema {
	return must(compileSchema("meaning.schema.json", schemaJSON))
})

var embeddedDraft2Schema = sync.OnceValue(func() *jsonschema.Schema {
	return must(compileSchema("meaning.draft-2.schema.json", schemaDraft2JSON))
})

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// compileSchema compiles a JSON Schema document (draft 2020-12), known by name,
// with format assertions switched on, as the reference checker's Ajv does, and
// with the reference checker's rule for format uri.
func compileSchema(name string, data []byte) (*jsonschema.Schema, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	compiler.RegisterFormat(&jsonschema.Format{Name: "uri", Validate: func(v any) error {
		if s, ok := v.(string); ok && !isURI(s) {
			return errors.New("is not a valid uri")
		}
		return nil
	}})
	compiler.UseLoader(staticLoader{doc})
	return compiler.Compile(name)
}

// staticLoader serves the one schema document, whatever address is asked for.
type staticLoader struct{ doc any }

func (l staticLoader) Load(string) (any, error) { return l.doc, nil }

type schemaProblem struct {
	path    []string
	message string
}

// schemaProblems validates a document and lists what the schema rejects: the
// innermost causes only, each with the place in the document it is about.
func schemaProblems(v Validator, doc *Node) []schemaProblem {
	err := v.Validate(doc.Value())
	if err == nil {
		return nil
	}
	var invalid *jsonschema.ValidationError
	if !errors.As(err, &invalid) {
		return []schemaProblem{{message: err.Error()}}
	}
	return causes(invalid)
}

var printer = message.NewPrinter(language.English)

func causes(e *jsonschema.ValidationError) []schemaProblem {
	if len(e.Causes) == 0 {
		return []schemaProblem{{path: e.InstanceLocation, message: e.ErrorKind.LocalizedString(printer)}}
	}
	var out []schemaProblem
	for _, cause := range e.Causes {
		out = append(out, causes(cause)...)
	}
	return out
}

func (p schemaProblem) String() string {
	return fmt.Sprintf("/%s %s", strings.Join(p.path, "/"), p.message)
}
