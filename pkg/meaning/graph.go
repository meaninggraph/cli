package meaning

import (
	"io/fs"
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
	// github.com/meaninggraph/core), or empty when it is not known. Set it,
	// before the graph is checked or handed to GraphResolver, for every graph
	// whose files refer to the graph by its address: a meaning:// reference to
	// the address from inside the graph is a reference to itself, and without
	// the address it is a reference to another graph. Neither LoadDir nor
	// LoadFiles sets it.
	Address string
	// Dir is the directory the files were read from by LoadDir, else empty.
	Dir   string
	Files []*File
	// hasLicense says whether Dir holds a LICENSE.
	hasLicense bool
	// fs is the file system the graph was read from; models and the directory
	// tree around the graph are read through it.
	fs FS
	// symlinks are the meaning files of Dir that are symbolic links: they are
	// not part of the graph.
	symlinks []string
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

	// Format is the value of the file's format line, as written: Draft1,
	// Draft2, or anything else (or nothing) for a file the checker refuses.
	Format string

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
	// Complete is the key complete of a value set (Draft2): true says that the
	// list holds all the values there are.
	Complete bool
	Measure  *Measure
	Bindings []Binding
	Line     int
	node     *Node
	// format is the Format of the file the concept is declared in.
	format string
}

// Value is a known value of a concept.
type Value struct {
	ID      string
	Labels  map[string]string
	Aliases map[string][]string
	Codes   map[string]string
	// Retired is the key retired (Draft2): the value should no longer be used.
	Retired bool
}

// Measure is the measure part of a concept of kind measure.
type Measure struct {
	Formula, Aggregation string
	Inputs, Dimensions   []string
}

// Binding binds a concept to a record type or a field of a model. The member of
// the record type is written under the key property in a Draft1 file and under
// the key field in a Draft2 file; Property holds what the first says and Field
// what the second says. Member gives the one that the file uses.
type Binding struct {
	Model, Property, Field, Role, Match, Note string
	Line                                      int
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
	var paths, symlinks []string
	license := false
	for _, entry := range entries {
		license = license || entry.Name() == "LICENSE"
		switch {
		case !strings.HasSuffix(entry.Name(), FileSuffix):
		case entry.Type().IsRegular():
			paths = append(paths, filepath.Join(dir, entry.Name()))
		case entry.Type()&fs.ModeSymlink != 0:
			symlinks = append(symlinks, filepath.Join(dir, entry.Name()))
		}
	}
	g, err := LoadFiles(fsys, paths)
	if err != nil {
		return nil, err
	}
	g.Dir, g.hasLicense, g.symlinks = dir, license, symlinks
	return g, nil
}

// LoadFiles reads the given files as one graph, in path order.
func LoadFiles(fsys FS, paths []string) (*Graph, error) {
	g := &Graph{Concepts: map[string]*Entry{}, fs: fsys}
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
	root, err, _ := parseYAML(data)
	if err != nil {
		f.ParseErr = err
		return f
	}
	f.Root = root
	f.Format = root.text("format")
	f.ID, f.Name, f.License = root.text("id"), root.text("name"), root.text("license")
	f.Models = root.Field("models").strings()
	for _, s := range root.Field("sources").items() {
		f.Sources = append(f.Sources, Source{ID: s.text("id"), Line: s.Line})
	}
	for _, c := range root.Field("concepts").items() {
		concept := decodeConcept(c)
		concept.format = f.Format
		f.Concepts = append(f.Concepts, concept)
	}
	return f
}

func decodeConcept(n *Node) *Concept {
	c := &Concept{
		ID: n.text("id"), Kind: n.text("kind"),
		Of: n.text("of"), Extends: n.text("extends"), ValuesOf: n.text("values-of"), UnitsOf: n.text("units-of"),
		Unit: n.text("unit"), Source: n.text("source"),
		Labels: n.Field("labels").strings(), Synonyms: n.Field("synonyms").wordLists(),
		Complete: n.flag("complete"),
		Line:     n.Line, node: n,
	}
	for _, v := range n.Field("values").items() {
		c.Values = append(c.Values, Value{
			ID: v.text("id"), Labels: v.Field("labels").strings(),
			Aliases: v.Field("aliases").wordLists(), Codes: v.Field("codes").strings(),
			Retired: v.flag("retired"),
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
			Model: b.text("model"), Property: b.text("property"), Field: b.text("field"), Role: b.text("role"),
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

// flag is the value of a key that holds a boolean, false when it holds anything
// else or is not there.
func (n *Node) flag(key string) bool {
	v := n.Field(key)
	return v != nil && v.Kind == Bool && v.Text == "true"
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
