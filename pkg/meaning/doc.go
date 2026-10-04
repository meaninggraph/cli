// Package meaning reads and checks meaning files (the *.meaning.yaml files of
// github.com/meaninggraph/core and of the repositories that use it): it parses
// them, validates them against the embedded schema, resolves references and
// extends across graphs, checks bindings against ModelSpec models, and returns a
// typed Graph and a sorted list of Findings. It has no command framework, never
// exits the process, never touches the network, and reads files only through FS.
//
// # The YAML reader
//
// ParseYAML is the strict YAML reader the checker uses, and it is exported for
// other tools that need to read a meaning file the way the checker does: the
// same bytes give the same data, or an error, in both. It reads a defined subset
// of YAML 1.2 (see the comment on the YAML subset in yaml.go and the README),
// never half-reads a file outside it, and on a file inside it returns the data
// that the reference checker's parser (the yaml package of npm, as
// meaninggraph/core calls it) reads from the same text. That is tested, with
// recorded values of the reference parser for thousands of documents and a
// mutation run that compares the two parsers on every mutated file, not proved.
//
// Its limits are enforced by ParseYAML itself, not by its caller: at most
// MaxFileBytes bytes of text, at most MaxYAMLDepth levels of nesting (block and
// flow together), keys of at most 1024 bytes, one document, UTF-8 only,
// string keys only, no repeated key, no anchor, alias, merge key or tag, no
// keep chomping or indentation indicator on a block scalar, and tabs only where
// they are text (an error that says where: Line, and for a tab Column). The file
// is read in one pass over lines whose work grows with the size of the input,
// and the quadratic shapes (a long plain scalar, a long run of comment lines)
// are linear. A refused file is a *SyntaxError with a stable Rule; it is a
// refusal of the subset and not always a YAML error, since some files are
// refused that the reference parser reads (the README lists them).
//
// ParseYAML returns an error, nil for a file it reads. A refused file's error is
// a *SyntaxError, reached with errors.As:
//
//	node, err := meaning.ParseYAML(data)
//	var refused *meaning.SyntaxError
//	if errors.As(err, &refused) {
//		// refused.Rule is the stable rule id ("yaml-tab", "yaml-key", ...),
//		// refused.Line the 1-based line (0 when not known) and refused.Column the
//		// 1-based column in characters (set for a refused tab, else 0); the
//		// message already begins with the column of a tab.
//	}
//
// The returned Node holds scalars as the YAML 1.2 core schema reads them
// (Node.Value gives nil, string, bool, json.Number, []any and map[string]any),
// and every node has the line it starts on.
package meaning
