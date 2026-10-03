package meaning

import (
	"bytes"
	_ "embed" // the schema is embedded in the binary
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// SchemaJSON is meaning.schema.json of github.com/meaninggraph/core, the schema
// of format meaning/draft-1. SchemaSource says which commit it was taken from;
// scripts/check-schema-drift.sh compares both with a checkout of core.
//
//go:embed meaning.schema.json
var SchemaJSON []byte

// SchemaSource records where SchemaJSON comes from: repository, commit, path
// and sha256 of the file, one "key: value" per line.
//
//go:embed meaning.schema.source
var SchemaSource string

// Validator checks a value against the meaning-file schema. *jsonschema.Schema
// is one; tests supply others.
type Validator interface {
	Validate(v any) error
}

// DefaultSchema returns the schema embedded in the binary, compiled once.
func DefaultSchema() Validator { return embeddedSchema() }

var embeddedSchema = sync.OnceValue(func() *jsonschema.Schema {
	return must(compileSchema(SchemaJSON))
})

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// compileSchema compiles a JSON Schema document (draft 2020-12) with format
// assertions switched on, as the reference checker's Ajv does, and with the
// reference checker's rule for format uri.
func compileSchema(data []byte) (*jsonschema.Schema, error) {
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
	return compiler.Compile("meaning.schema.json")
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
