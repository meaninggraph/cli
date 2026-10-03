package meaning

// Model is what a meaning file's bindings need to know about a ModelSpec
// module: its entities, their keys and their properties.
type Model struct {
	Entities map[string]*Entity
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
	// Entity is the entity a reference property points at, empty otherwise.
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
