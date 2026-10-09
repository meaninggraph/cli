package meaning

// Model is what a meaning file's bindings need to know about a ModelSpec
// module: its entities, their keys and their properties.
type Model struct {
	Entities map[string]*Entity
	// Earlier says which earlier spellings the model file uses (entity, property
	// and entity = for record, field and record =). They are read as the words
	// they were replaced by; this is how a check can report them.
	Earlier EarlierSpelling
}

// EarlierSpelling counts the uses of the earlier ModelSpec spellings in a model
// file. The zero value says the file uses none.
type EarlierSpelling struct {
	// Count is the number of earlier spellings the file uses.
	Count int
	// Line is the 1-based line of the first of them.
	Line int
}

// Entity is an entity of a ModelSpec module.
type Entity struct {
	// Key is the names of the properties that form the entity's key.
	Key        []string
	Properties map[string]Property
}

// Property is a property of an entity.
type Property struct {
	// Type is the ModelSpec type ("string", "int", ...), empty for a
	// reference to another entity.
	Type string
	// Reference says the property is a reference to an entity, in the sense of
	// the reference checker: its `entity` attribute is set.
	Reference bool
	// Entity is the name of the entity a reference property points at. It is
	// empty when the property is no reference, or when the attribute is not a
	// string (and so names no entity).
	Entity string
}

// ModelReader reads the model of a module from the file a meaning file's
// `models` names. The single implementation today is HCLReader, which reads the
// subset of ModelSpec HCL that the Node reference checker reads.
//
// ModelReader is the seam for the ModelSpec library: once github.com/modelspec-org/cli
// (package pkg/modelspec) has a release, a ModelReader that calls it replaces
// HCLReader and nothing else in this package changes.
type ModelReader interface {
	ReadModel(fsys FS, path string) (*Model, error)
}

// IsReference reports whether the property is a reference to an entity.
func (p Property) IsReference() bool { return p.Reference || p.Entity != "" }
