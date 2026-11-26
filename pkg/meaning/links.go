package meaning

import (
	"cmp"
	"slices"
	"strings"
)

// Link is one fact of the form "this record type, or this field, holds this
// concept in this role" (FORMAT.md of github.com/meaninggraph/core, "Derived
// links"). A written link comes from a binding line; a derived link is computed
// from the model and is never written. The JSON form is the canonical one of the
// format: the role is always in the current words (instances, reference), and
// derived is present on a derived link only.
type Link struct {
	// Concept is the id of the concept.
	Concept string `json:"concept"`
	// Model is the record type, as modelspec:///module.Name.
	Model string `json:"model"`
	// Field is the field of the record type; it is empty for the role instances.
	Field string `json:"field,omitempty"`
	// Role is what the record type or the field holds: instances, identifier,
	// display-name, reference or value.
	Role string `json:"role"`
	// Note and Match are those of the binding line that wrote the link.
	Note  string `json:"note,omitempty"`
	Match string `json:"match,omitempty"`
	// Derived marks a link that a reader worked out from the model.
	Derived bool `json:"derived,omitempty"`
}

// linkKey is what identifies a link: the concept, the module, the record type,
// the field (empty for instances) and the role, in the current words.
type linkKey struct{ concept, module, record, field, role string }

// loadedFile is a meaning file that passed the schema, with the models it lists.
type loadedFile struct {
	file   *File
	models map[string]*Model
}

// deriveLinks computes the written and the derived links of a graph, sorted by
// concept id, then model, then field (absent first), then role, comparing bytes.
// The rule is FORMAT.md's: for a field that refers to a record type, each
// concept bound to that record type with role instances applies to the field,
// with role reference; in the same way the key of a record type bound to a
// concept identifies that concept. A written line for the same five parts takes
// the place of the derived link and brings its note. The graph is expected to
// pass the check: for one that does not, the result is not defined.
func deriveLinks(files []loadedFile) []Link {
	links := map[linkKey]Link{}
	type instance struct {
		concept, module, record string
		models                  map[string]*Model
	}
	var instances []instance
	link := func(k linkKey, extra Link) Link {
		extra.Concept, extra.Model, extra.Field, extra.Role = k.concept, "modelspec:///"+k.module+"."+k.record, k.field, k.role
		return extra
	}
	// Written links, in file order; a line that repeats the five parts of an
	// earlier one adds nothing.
	for _, lf := range files {
		for _, c := range lf.file.Concepts {
			for _, b := range c.Bindings {
				ref, ok := ParseModelRef(b.Model)
				if !ok || ref.Repo != "" {
					continue
				}
				role := roleOf(b)
				k := linkKey{c.ID, ref.Module, ref.Name, "", role}
				if role != "instances" {
					k.field = b.Member(lf.file.Format)
				}
				if _, written := links[k]; !written {
					links[k] = link(k, Link{Note: b.Note, Match: b.Match})
				}
				if role == "instances" {
					instances = append(instances, instance{c.ID, ref.Module, ref.Name, lf.models})
				}
			}
		}
	}
	// Derived links: the references to a record type bound with role instances,
	// and its key.
	derived := func(k linkKey) {
		if _, taken := links[k]; !taken {
			links[k] = link(k, Link{Derived: true})
		}
	}
	for _, in := range instances {
		model := in.models[in.module]
		if model == nil {
			continue
		}
		for name, record := range model.Entities {
			for field, p := range record.Properties {
				if p.Entity == in.record {
					derived(linkKey{in.concept, in.module, name, field, "reference"})
				}
			}
		}
		if record, ok := model.Entities[in.record]; ok {
			for _, key := range record.Key {
				if _, declared := record.Properties[key]; declared {
					derived(linkKey{in.concept, in.module, in.record, key, "identifier"})
				}
			}
		}
	}
	out := make([]Link, 0, len(links))
	for _, l := range links {
		out = append(out, l)
	}
	slices.SortFunc(out, func(a, b Link) int {
		// Strings compare as bytes (UTF-8), which is what the format asks for.
		return cmp.Or(
			strings.Compare(a.Concept, b.Concept),
			strings.Compare(a.Model, b.Model),
			strings.Compare(a.Field, b.Field),
			strings.Compare(a.Role, b.Role),
		)
	})
	return out
}
