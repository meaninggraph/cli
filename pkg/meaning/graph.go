package meaning

import (
	"path/filepath"
	"slices"
	"strings"
)

// FileSuffix ends the name of every meaning file.
const FileSuffix = ".meaning.yaml"

// Graph is a set of meaning files read as one repository of concepts, as the
// format defines it: a file is only packaging, and a concept id is unique
// across all of them.
type Graph struct {
	// Address is the graph's own address, {host}/{org}/{repo} (for example
	// github.com/meaninggraph/core), or empty when it is not known. A
	// meaning:// reference to it from inside it is a reference to itself.
	Address string
	// Dir is the directory the files were read from by LoadDir, else empty.
	Dir   string
	Files []*File
	// hasLicense says whether Dir holds a LICENSE.
	hasLicense bool
	// Concepts maps a concept id to its first declaration.
	Concepts map[string]*Entry
	// duplicates are declarations of an id that an earlier one already took.
	duplicates []duplicate
}

type duplicate struct{ first, second *Entry }

// Entry is a concept with the file it is declared in.
type Entry struct {
	Concept *Concept
	File    *File
}

// File is one meaning file.
type File struct {
	// Path is the path as it was given.
	Path string
	// Root is the parsed document; nil when it could not be parsed.
	Root *Node
	// ParseErr is why Root is nil.
	ParseErr *SyntaxError

	ID, Name, License string
	// Models maps a module short name to the path of its source, relative to
	// the file.
	Models   map[string]string
	Sources  []Source
	Concepts []*Concept
}

// Source is an entry of a file's sources.
type Source struct {
	ID   string
	Line int
}

// Concept is a concept of a meaning file, decoded leniently: a field of the
// wrong type is left empty, because the schema check reports it.
type Concept struct {
	ID, Kind                       string
	Of, Extends, ValuesOf, UnitsOf string
	Unit, Source                   string
	Labels                         map[string]string
	Synonyms                       map[string][]string
	Values                         []Value
	Measure                        *Measure
	Bindings                       []Binding
	Line                           int
	node                           *Node
}

// Value is a known value of a concept.
type Value struct {
	ID      string
	Labels  map[string]string
	Aliases map[string][]string
	Codes   map[string]string
}

// Measure is the measure part of a concept of kind measure.
type Measure struct {
	Formula, Aggregation string
	Inputs, Dimensions   []string
}

// Binding binds a concept to an entity or property of a model.
type Binding struct {
	Model, Property, Role, Match, Note string
	Line                               int
}

// lineOf is the line of a field of the concept, or of the concept.
func (c *Concept) lineOf(key string) int {
	return c.node.Field(key).lineOr(c.Line)
}

// LoadDir reads the *.meaning.yaml files directly in dir as one graph. A file
// in a subdirectory is not part of the graph. A file that is not valid YAML is
// kept with its ParseErr; only a directory that cannot be read is an error.
func LoadDir(fsys FS, dir string) (*Graph, error) {
	entries, err := fsys.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var paths []string
	license := false
	for _, entry := range entries {
		license = license || entry.Name() == "LICENSE"
		if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), FileSuffix) {
			paths = append(paths, filepath.Join(dir, entry.Name()))
		}
	}
	g, err := LoadFiles(fsys, paths)
	if err != nil {
		return nil, err
	}
	g.Dir, g.hasLicense = dir, license
	return g, nil
}

// LoadFiles reads the given files as one graph, in path order.
func LoadFiles(fsys FS, paths []string) (*Graph, error) {
	g := &Graph{Concepts: map[string]*Entry{}}
	for _, path := range slices.Sorted(slices.Values(paths)) {
		data, err := fsys.ReadFile(path)
		if err != nil {
			return nil, err
		}
		g.add(decodeFile(path, data))
	}
	return g, nil
}

func (g *Graph) add(f *File) {
	g.Files = append(g.Files, f)
	for _, c := range f.Concepts {
		entry := &Entry{Concept: c, File: f}
		if first, taken := g.Concepts[c.ID]; taken {
			g.duplicates = append(g.duplicates, duplicate{first: first, second: entry})
			continue
		}
		g.Concepts[c.ID] = entry
	}
}

func decodeFile(path string, data []byte) *File {
	f := &File{Path: path}
	root, err := ParseYAML(data)
	if err != nil {
		f.ParseErr = err
		return f
	}
	f.Root = root
	f.ID, f.Name, f.License = root.text("id"), root.text("name"), root.text("license")
	f.Models = root.Field("models").strings()
	for _, s := range root.Field("sources").items() {
		f.Sources = append(f.Sources, Source{ID: s.text("id"), Line: s.Line})
	}
	for _, c := range root.Field("concepts").items() {
		f.Concepts = append(f.Concepts, decodeConcept(c))
	}
	return f
}

func decodeConcept(n *Node) *Concept {
	c := &Concept{
		ID: n.text("id"), Kind: n.text("kind"),
		Of: n.text("of"), Extends: n.text("extends"), ValuesOf: n.text("values-of"), UnitsOf: n.text("units-of"),
		Unit: n.text("unit"), Source: n.text("source"),
		Labels: n.Field("labels").strings(), Synonyms: n.Field("synonyms").wordLists(),
		Line: n.Line, node: n,
	}
	for _, v := range n.Field("values").items() {
		c.Values = append(c.Values, Value{
			ID: v.text("id"), Labels: v.Field("labels").strings(),
			Aliases: v.Field("aliases").wordLists(), Codes: v.Field("codes").strings(),
		})
	}
	if m := n.Field("measure"); m != nil {
		c.Measure = &Measure{
			Formula: m.text("formula"), Aggregation: m.text("aggregation"),
			Inputs: m.Field("inputs").list(), Dimensions: m.Field("dimensions").list(),
		}
	}
	for _, b := range n.Field("bindings").items() {
		c.Bindings = append(c.Bindings, Binding{
			Model: b.text("model"), Property: b.text("property"), Role: b.text("role"),
			Match: b.text("match"), Note: b.text("note"), Line: b.Line,
		})
	}
	return c
}

// text is the string value of a key, or "".
func (n *Node) text(key string) string {
	s, _ := n.Field(key).Str()
	return s
}

// items are the elements of a Seq that are maps; anything else is skipped.
func (n *Node) items() []*Node {
	if n == nil || n.Kind != Seq {
		return nil
	}
	var out []*Node
	for _, item := range n.Items {
		if item.Kind == Map {
			out = append(out, item)
		}
	}
	return out
}

// list is the strings of a Seq of strings.
func (n *Node) list() []string {
	if n == nil || n.Kind != Seq {
		return nil
	}
	var out []string
	for _, item := range n.Items {
		if s, ok := item.Str(); ok {
			out = append(out, s)
		}
	}
	return out
}

// strings is a Map of strings.
func (n *Node) strings() map[string]string {
	if n == nil || n.Kind != Map {
		return nil
	}
	out := map[string]string{}
	for _, key := range n.Keys {
		if s, ok := n.Fields[key].Str(); ok {
			out[key] = s
		}
	}
	return out
}

// wordLists is a Map of lists of strings.
func (n *Node) wordLists() map[string][]string {
	if n == nil || n.Kind != Map {
		return nil
	}
	out := map[string][]string{}
	for _, key := range n.Keys {
		out[key] = n.Fields[key].list()
	}
	return out
}
