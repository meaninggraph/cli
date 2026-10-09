package meaning

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ModelRef is a parsed modelspec:// reference to an entity.
type ModelRef struct {
	// Repo is {host}/{org}/{repo} for a model of another repository, empty
	// for one of the same repository.
	Repo, Module, Name, Pin string
}

// ParseModelRef parses modelspec:///{module}.{Entity} (same repository) or
// modelspec://{host}/{org}/{repo}/{module}.{Entity}.
func ParseModelRef(ref string) (ModelRef, bool) {
	m := modelRefPattern.FindStringSubmatch(ref)
	if m == nil {
		return ModelRef{}, false
	}
	return ModelRef{Repo: m[1], Module: m[2], Name: m[3], Pin: m[4]}, true
}

// loadModels reads the modules a file's `models` names, relative to the file.
// The first module that cannot be read is reported, and the bindings of the
// file are then not checked against models: the reference checker loses all
// the modules at that point, which would make every binding "not listed".
func (r *run) loadModels(f *File) (models map[string]*Model, failed bool) {
	models = map[string]*Model{}
	for _, module := range sortedKeys(f.Models) {
		relative := f.Models[module]
		var model *Model
		var err error
		if strings.HasSuffix(relative, "/") {
			err = fmt.Errorf("%s ends in a slash, so it names a directory, not a model file", relative)
		} else {
			path := filepath.Join(filepath.Dir(f.Path), relative)
			model, err = r.local.fsModels(r.c.Models, path)
			if err == nil {
				r.noticeEarlierSpelling(path, model)
			}
		}
		if err != nil {
			r.err(f, f.Root.Field("models").Line, RuleModels, "models: module %s: %v", module, err)
			return nil, true
		}
		models[module] = model
	}
	return models, false
}

// noticeEarlierSpelling reports, once per model file and as a warning, that the
// file uses the earlier ModelSpec spellings. They are read as the words that
// replaced them, so the notice never fails a check.
func (r *run) noticeEarlierSpelling(path string, model *Model) {
	if model.Earlier.Count == 0 || r.noticed[path] {
		return
	}
	if r.noticed == nil {
		r.noticed = map[string]bool{}
	}
	r.noticed[path] = true
	what := "an earlier spelling"
	if model.Earlier.Count > 1 {
		what = fmt.Sprintf("%d earlier spellings", model.Earlier.Count)
	}
	r.add(path, model.Earlier.Line, RuleEarlierSpelling, Warning,
		"the model uses %s: entity, property and entity = are the earlier spellings of record, field and record =, and are read as those; modelspec rewrite --write %q rewrites the file", what, path)
}

// fsModels reads a model through the file system the graph was loaded from.
func (g *Graph) fsModels(reader ModelReader, path string) (*Model, error) {
	if g.fs == nil {
		return nil, errors.New("the graph was not loaded from a file system, so models cannot be read")
	}
	return reader.ReadModel(g.fs, path)
}

// entityBindings are the entities whose rows are instances of a concept (role entity).
func entityBindings(c *Concept) []ModelRef {
	var out []ModelRef
	for _, b := range c.Bindings {
		if ref, ok := ParseModelRef(b.Model); ok && b.Role == "entity" {
			out = append(out, ref)
		}
	}
	return out
}

func names(refs []ModelRef) []string {
	var out []string
	for _, ref := range refs {
		out = append(out, ref.Name)
	}
	return out
}

// checkBindings checks every binding of a concept against the models of its file.
func (r *run) checkBindings(f *File, c *Concept, label string, models map[string]*Model, modelsFailed bool) {
	entities := entityBindings(c)
	if len(entities) > 1 {
		r.err(f, c.lineOf("bindings"), RuleEntityBindings, "%s: has %d entity bindings (%v); a concept binds one entity", label, len(entities), names(entities))
	}
	for _, b := range c.Bindings {
		ref, ok := ParseModelRef(b.Model)
		if !ok {
			r.err(f, b.Line, RuleBindingModel, "%s: %s is not a modelspec:// reference", label, b.Model)
			continue
		}
		if ref.Repo != "" {
			r.err(f, b.Line, RuleBindingModel, "%s: %s points at another repository; this check resolves same-repository models only", label, b.Model)
			continue
		}
		if modelsFailed {
			continue // the models error says why
		}
		model, ok := models[ref.Module]
		if !ok {
			r.err(f, b.Line, RuleBindingModel, "%s: %s: module %s is not listed in models", label, b.Model, ref.Module)
			continue
		}
		entity, ok := model.Entities[ref.Name]
		if !ok {
			r.err(f, b.Line, RuleBindingModel, "%s: %s: module %s has no entity %s", label, b.Model, ref.Module, ref.Name)
			continue
		}
		if b.Property == "" {
			continue
		}
		member, ok := entity.Properties[b.Property]
		if !ok {
			r.err(f, b.Line, RuleBindingModel, "%s: %s: entity %s has no property %s", label, b.Model, ref.Name, b.Property)
			continue
		}
		r.checkRole(f, c, b, ref, entity, member, entities)
	}
}

// checkRole checks that a property fits the role a binding gives it.
func (r *run) checkRole(f *File, c *Concept, b Binding, ref ModelRef, entity *Entity, member Property, entities []ModelRef) {
	at := fmt.Sprintf("concept %s: %s.%s", c.ID, ref.Name, b.Property)
	fail := func(format string, args ...any) { r.roleFailure(f, b, at, format, args...) }
	// identifier and display-name describe the rows of the concept's own entity.
	if b.Role == "identifier" || b.Role == "display-name" {
		switch {
		case len(entities) == 0:
			fail("has role %s, but %s has no entity binding, so it cannot be checked which entity the property must sit on", b.Role, c.ID)
		case len(entities) == 1 && !sameEntity(entities[0], ref):
			fail("has role %s, but %s is bound to the entity %s; the property must be on that entity", b.Role, c.ID, entities[0].Name)
		}
	}
	switch b.Role {
	case "identifier":
		if !contains(entity.Key, b.Property) {
			fail("has role identifier but is not in the key of %s %v", ref.Name, entity.Key)
		}
	case "display-name":
		if member.Type != "string" {
			fail("has role display-name but is %s, not a string", describe(member))
		}
	case "value":
		if member.IsReference() {
			fail("has role value but is %s; bind it with role foreign-key", describe(member))
		}
	case "foreign-key":
		r.checkForeignKey(f, c, b, ref, member, at)
	}
}

func (r *run) roleFailure(f *File, b Binding, at, format string, args ...any) {
	r.err(f, b.Line, RuleBindingRole, "%s %s", at, fmt.Sprintf(format, args...))
}

// sameEntity compares repository, module and entity name; a pin is not part of it.
func sameEntity(a, b ModelRef) bool {
	return a.Repo == b.Repo && a.Module == b.Module && a.Name == b.Name
}

func describe(p Property) string {
	switch {
	case p.Entity != "":
		return "a reference to " + p.Entity
	case p.IsReference():
		return "a reference"
	}
	return an(p.Type)
}

// checkForeignKey: the reference must point at the entity whose rows are the
// instances of this concept (an entity) or of its values-of entity.
func (r *run) checkForeignKey(f *File, c *Concept, b Binding, ref ModelRef, member Property, at string) {
	fail := func(format string, args ...any) { r.roleFailure(f, b, at, format, args...) }
	if !member.IsReference() {
		fail("has role foreign-key but is not a reference (it is %s)", an(member.Type))
		return
	}
	target := node{concept: c, graph: r.local}
	if c.Kind != "entity" {
		domain, ok := r.inherited(c, r.local, func(x *Concept) string { return x.ValuesOf })
		if !ok {
			fail("has role foreign-key, so %s needs values-of: the entity its references point at", c.ID)
			return
		}
		if target, ok = r.resolveConcept(domain.concept.ValuesOf, domain.graph); !ok {
			return // an unresolvable values-of is reported elsewhere
		}
	}
	// Only this repository's own bindings name models this check can read.
	var expected []ModelRef
	if target.graph == r.local {
		expected = entityBindings(target.concept)
	}
	if len(expected) == 0 {
		fail("has role foreign-key, but %s has no entity binding in this repository, so it cannot be checked that %s holds its instances; bind %s (or a concept of this repository that extends it) to its entity", target.concept.ID, describeTarget(member), target.concept.ID)
		return
	}
	for _, e := range expected {
		if e.Module == ref.Module && e.Name == member.Entity {
			return
		}
	}
	fail("references %s, but the instances of %s are %v rows", describeTarget(member), target.concept.ID, names(expected))
}

// describeTarget names the entity a reference property points at.
func describeTarget(p Property) string {
	if p.Entity != "" {
		return p.Entity
	}
	return "an entity that is not named by a string"
}
