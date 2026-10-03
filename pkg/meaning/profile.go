package meaning

import (
	"path/filepath"
	"strings"
)

// hiddenFiles reports meaning files below the root of the graph's directory:
// they are never read, so they would be silently dead. A file is an error
// under the universal profile, which the reference checker fails on, and
// otherwise a warning. So is a directory that cannot be read, since a file in
// it would be missed, and a meaning file that is a symbolic link.
func (r *run) hiddenFiles() {
	severity := Warning
	if r.c.Profile == ProfileUniversal {
		severity = Error
	}
	found, unreadable := subdirectoryFiles(r.local.fs, r.local.Dir, "")
	for _, path := range found {
		r.add(path, 0, RuleSubdirectoryFile, severity, "meaning files are not read below the directory of a graph: a graph is the meaning files directly in its directory, so move this file there, or check its directory")
	}
	for _, path := range unreadable {
		r.add(path, 0, RuleUnreadableDir, severity, "this directory cannot be read, so meaning files in it, if any, are not checked")
	}
	for _, path := range r.local.symlinks {
		r.add(path, 0, RuleSymlink, Warning, "this meaning file is a symbolic link, so it is not part of the graph; meaning files must be regular files")
	}
}

// subdirectoryFiles lists the *.meaning.yaml files below dir (those in dir itself
// are not listed), skipping directories whose name starts with "." and
// node_modules, and the directories that cannot be read.
func subdirectoryFiles(fsys FS, dir, prefix string) (found, unreadable []string) {
	entries, err := fsys.ReadDir(filepath.Join(dir, prefix))
	if err != nil {
		return nil, []string{filepath.Join(dir, prefix)}
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || name == "node_modules" {
			continue
		}
		path := filepath.Join(prefix, name)
		switch {
		case entry.IsDir():
			f, u := subdirectoryFiles(fsys, dir, path)
			found, unreadable = append(found, f...), append(unreadable, u...)
		case prefix != "" && strings.HasSuffix(name, FileSuffix):
			found = append(found, filepath.Join(dir, path))
		}
	}
	return found, unreadable
}

// universal applies the rules of a repository of universal concepts.
func (r *run) universal() {
	g := r.local
	if g.Dir != "" && !g.hasLicense {
		r.add(g.Dir, 0, RuleUniversal, Error, "LICENSE is missing")
	}
	for _, f := range g.Files {
		if f.ParseErr != nil {
			continue
		}
		if f.License != "CC0-1.0" {
			r.err(f, f.Root.Field("license").lineOr(f.Root.Line), RuleUniversal, "license must be CC0-1.0")
		}
		if f.Root.Field("models") != nil {
			r.err(f, f.Root.Field("models").Line, RuleUniversal, "models belong in a dataset repository, not in the universal concepts")
		}
		for _, c := range f.Concepts {
			if len(c.Bindings) > 0 {
				r.err(f, c.lineOf("bindings"), RuleUniversal, "concept %s: bindings belong in a dataset repository, not in the universal concepts", c.ID)
			}
		}
	}
	r.oneWordOneConcept()
}

// lineOr is the line of the node, or def when it is nil.
func (n *Node) lineOr(def int) int {
	if n == nil {
		return def
	}
	return n.Line
}

// MaxAmbiguousWords bounds how many words that name two concepts the universal
// profile lists: when graphs of thousands of concepts share one word, the pairs
// would number in the millions. The first MaxAmbiguousWords are listed, and a
// finding of the same rule says that the others are not.
const MaxAmbiguousWords = 1000

// oneWordOneConcept: a label or synonym that two concepts share in a language
// is ambiguous, unless one concept is a kind of the other. A language counts
// when the concept has a label or synonyms in it.
func (r *run) oneWordOneConcept() {
	owners := map[string][]*Concept{}
	for _, f := range r.local.Files {
		for _, c := range f.Concepts {
			if r.local.Concepts[c.ID].Concept != c {
				continue // a duplicate declaration is reported on its own
			}
			r.ownWords(f, c, owners)
		}
	}
}

func (r *run) ownWords(f *File, c *Concept, owners map[string][]*Concept) {
	languages := map[string]bool{}
	for lang := range c.Labels {
		languages[lang] = true
	}
	for lang := range c.Synonyms {
		languages[lang] = true
	}
	for _, lang := range sortedKeys(languages) {
		words := append([]string{c.Labels[lang]}, c.Synonyms[lang]...)
		for _, word := range words {
			if word == "" {
				continue
			}
			key := lang + ":" + lower(word)
			for _, other := range owners[key] {
				if r.ambiguous > MaxAmbiguousWords {
					return
				}
				if !r.related(c, other) {
					if r.ambiguous++; r.ambiguous > MaxAmbiguousWords {
						r.err(f, c.Line, RuleAmbiguousWord, "more than %d words name two concepts; the others are not listed (fix these first)", MaxAmbiguousWords)
						return
					}
					r.err(f, c.Line, RuleAmbiguousWord, "concept %s: %q (%s) is also a word of concept %s; one word must name one concept", c.ID, word, lang, other.ID)
				}
			}
			owners[key] = append(owners[key], c)
		}
	}
}

// related reports whether one concept is a kind of the other, or the same.
func (r *run) related(a, b *Concept) bool {
	return r.kindOf(a, b) || r.kindOf(b, a)
}

func (r *run) kindOf(c, ancestor *Concept) bool {
	return seenConcept(r.lineage(c, r.local), ancestor)
}
