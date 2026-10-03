package meaning

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// HCLReader reads a ModelSpec module from its HCL source. It is a port of the
// parser of the Node reference checker (scripts/lib/modelspec.mjs of
// github.com/meaninggraph/core), and accepts exactly what ModelSpec v0 HCL
// allows there: named blocks, and attributes whose values are strings,
// numbers, booleans or lists of those. Expressions, interpolation and
// map-style values are refused rather than guessed.
//
// It is a stand-in. Once the ModelSpec library (github.com/modelspec-org/cli,
// package pkg/modelspec) is released, a ModelReader built on it replaces this
// one; see ModelReader.
type HCLReader struct{}

// ReadModel reads and parses the file at path.
func (HCLReader) ReadModel(fsys FS, path string) (*Model, error) {
	data, err := fsys.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFileBytes {
		return nil, fmt.Errorf("the model is %d bytes; at most %d are read", len(data), MaxFileBytes)
	}
	blocks, err := parseHCL(string(data))
	if err != nil {
		return nil, err
	}
	return buildModel(blocks)
}

type token struct {
	kind  string // one of {}[]=, or "string", "number", "ident"
	value any    // string, float64 or string
	line  int
}

var (
	hclNumber = regexp.MustCompile(`^-?[0-9]+(?:\.[0-9]+)?`)
	hclIdent  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*`)
)

func hclErrorf(line int, format string, args ...any) error {
	return fmt.Errorf("line %d: %s", line, fmt.Sprintf(format, args...))
}

func tokenize(text string) ([]token, error) {
	var tokens []token
	line := 1
	for i := 0; i < len(text); {
		c := text[i]
		switch {
		case c == '\n':
			line++
			i++
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case c == '#' || strings.HasPrefix(text[i:], "//"):
			i += lineLength(text[i:])
		case strings.HasPrefix(text[i:], "/*"):
			end := strings.Index(text[i+2:], "*/")
			if end < 0 {
				return nil, hclErrorf(line, "unterminated comment")
			}
			end += i + 2
			line += strings.Count(text[i:end], "\n")
			i = end + 2
		case strings.IndexByte("{}[]=,", c) >= 0:
			tokens = append(tokens, token{kind: string(c), line: line})
			i++
		case c == '"':
			value, next, err := hclString(text, i+1, line)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, token{kind: "string", value: value, line: line})
			i = next
		default:
			if number := hclNumber.FindString(text[i:]); number != "" {
				f, _ := strconv.ParseFloat(number, 64)
				tokens = append(tokens, token{kind: "number", value: f, line: line})
				i += len(number)
			} else if ident := hclIdent.FindString(text[i:]); ident != "" {
				tokens = append(tokens, token{kind: "ident", value: ident, line: line})
				i += len(ident)
			} else {
				return nil, hclErrorf(line, "unexpected character %q", string(c))
			}
		}
	}
	return tokens, nil
}

// lineLength is the length of the first line of text, without its newline.
func lineLength(text string) int {
	if end := strings.IndexByte(text, '\n'); end >= 0 {
		return end
	}
	return len(text)
}

var hclEscapes = map[byte]byte{'n': '\n', 't': '\t', '"': '"', '\\': '\\'}

// hclString reads a string whose opening quote is just before i.
func hclString(text string, i, line int) (string, int, error) {
	var sb strings.Builder
	for i >= len(text) || text[i] != '"' {
		switch {
		case i >= len(text) || text[i] == '\n':
			return "", 0, hclErrorf(line, "unterminated string")
		case strings.HasPrefix(text[i:], "${"):
			return "", 0, hclErrorf(line, "string interpolation is not ModelSpec v0")
		case text[i] == '\\':
			next := byte(0)
			if i+1 < len(text) {
				next = text[i+1]
			}
			escaped, ok := hclEscapes[next]
			if !ok {
				return "", 0, hclErrorf(line, "unsupported escape \\%s", string(next))
			}
			sb.WriteByte(escaped)
			i += 2
		default:
			sb.WriteByte(text[i])
			i++
		}
	}
	return sb.String(), i + 1, nil
}

// hclBlock is a named block: type "name" { attributes... nested blocks... }.
type hclBlock struct {
	typ, name  string
	line       int
	attributes map[string]any
	blocks     []*hclBlock
}

type hclParser struct {
	tokens []token
	p      int
}

func (h *hclParser) peek() *token {
	if h.p < len(h.tokens) {
		return &h.tokens[h.p]
	}
	return nil
}

func (h *hclParser) peekIs(kind string) bool {
	t := h.peek()
	return t != nil && t.kind == kind
}

func (h *hclParser) expect(kind string) (token, error) {
	t := h.peek()
	if t == nil {
		return token{}, fmt.Errorf("line EOF: expected %s, found end of file", kind)
	}
	if t.kind != kind {
		found := t.kind
		if t.value != nil {
			found = fmt.Sprint(t.value)
		}
		return token{}, hclErrorf(t.line, "expected %s, found %s", kind, found)
	}
	h.p++
	return *t, nil
}

func (h *hclParser) value() (any, error) {
	t := h.peek()
	if t == nil {
		return nil, fmt.Errorf("unexpected end of file in a value")
	}
	h.p++
	switch {
	case t.kind == "string" || t.kind == "number":
		return t.value, nil
	case t.kind == "ident" && (t.value == "true" || t.value == "false"):
		return t.value == "true", nil
	case t.kind == "[":
		return h.list(t.line)
	case t.kind == "{":
		return nil, hclErrorf(t.line, "map-style values are not ModelSpec v0 syntax; use named blocks")
	}
	shown := t.kind
	if t.value != nil {
		shown = fmt.Sprint(t.value)
	}
	return nil, hclErrorf(t.line, "%s is not a literal (expressions are not ModelSpec v0)", shown)
}

func (h *hclParser) list(line int) ([]any, error) {
	list := []any{}
	for !h.peekIs("]") {
		item, err := h.value()
		if err != nil {
			return nil, err
		}
		if _, nested := item.([]any); nested {
			return nil, hclErrorf(line, "nested lists are not ModelSpec v0")
		}
		list = append(list, item)
		if !h.peekIs(",") {
			break
		}
		h.p++
	}
	_, err := h.expect("]")
	return list, err
}

// maxHCLDepth is how deep blocks may nest: an entity holds properties and a
// property holds none, so ModelSpec v0 needs two levels. The reference checker
// refuses deeper blocks too, as blocks that cannot contain blocks.
const maxHCLDepth = 2

// body parses attributes and blocks until the closing token (empty: end of
// file); depth is the number of blocks around it.
func (h *hclParser) body(closing string, depth int) (*hclBlock, error) {
	out := &hclBlock{attributes: map[string]any{}}
	for h.peek() != nil && h.peek().kind != closing {
		name, err := h.expect("ident")
		if err != nil {
			return nil, err
		}
		if h.peekIs("=") {
			h.p++
			if _, dup := out.attributes[name.value.(string)]; dup {
				return nil, hclErrorf(name.line, "duplicate attribute %s", name.value)
			}
			v, err := h.value()
			if err != nil {
				return nil, err
			}
			out.attributes[name.value.(string)] = v
			continue
		}
		label, err := h.expect("string")
		if err != nil {
			return nil, err
		}
		if _, err := h.expect("{"); err != nil {
			return nil, err
		}
		if depth >= maxHCLDepth {
			return nil, hclErrorf(name.line, "blocks nested more than %d deep are not ModelSpec v0 (%s %q is inside a block of a block)", maxHCLDepth, name.value, label.value)
		}
		inner, err := h.body("}", depth+1)
		if err != nil {
			return nil, err
		}
		if _, err := h.expect("}"); err != nil {
			return nil, err
		}
		inner.typ, inner.name, inner.line = name.value.(string), label.value.(string), name.line
		out.blocks = append(out.blocks, inner)
	}
	return out, nil
}

func parseHCL(text string) ([]*hclBlock, error) {
	tokens, err := tokenize(text)
	if err != nil {
		return nil, err
	}
	h := &hclParser{tokens: tokens}
	doc, err := h.body("", 0)
	if err != nil {
		return nil, err
	}
	if len(doc.attributes) > 0 {
		return nil, fmt.Errorf("top-level attributes are not ModelSpec v0")
	}
	return doc.blocks, nil
}

// buildModel is the part of the Node converter (toModelspecJson) the checks
// read: entities with their key and properties. Components and enums are
// validated the way the converter does, so that the same files are refused.
func buildModel(blocks []*hclBlock) (*Model, error) {
	model := &Model{Entities: map[string]*Entity{}}
	declared := map[string]bool{}
	for _, block := range blocks {
		kind := block.typ
		switch kind {
		case "entity", "component", "enum":
		default:
			return nil, hclErrorf(block.line, "top-level %s blocks are not supported by this converter (entity, component, enum)", block.typ)
		}
		if isPrototypeName(block.name) {
			return nil, prototypeNameError(block.line, kind, block.name)
		}
		if declared[kind+" "+block.name] {
			return nil, hclErrorf(block.line, "duplicate %s %q", kind, block.name)
		}
		declared[kind+" "+block.name] = true
		switch kind {
		case "entity":
			entity, err := buildEntity(block)
			if err != nil {
				return nil, err
			}
			model.Entities[block.name] = entity
		case "component":
			if _, err := members(block, "field"); err != nil {
				return nil, err
			}
		default:
			if len(block.blocks) > 0 {
				return nil, hclErrorf(block.line, "enum %q cannot contain blocks", block.name)
			}
		}
	}
	return model, nil
}

func buildEntity(block *hclBlock) (*Entity, error) {
	for name := range block.attributes {
		if name != "key" && name != "use" {
			return nil, hclErrorf(block.line, "unsupported entity attribute %s", name)
		}
	}
	properties, err := members(block, "property")
	if err != nil {
		return nil, err
	}
	entity := &Entity{Properties: map[string]Property{}}
	if key := block.attributes["key"]; truthy(key) {
		list, ok := key.([]any)
		if !ok {
			return nil, hclErrorf(block.line, "entity %q key must be a list of property names", block.name)
		}
		for _, k := range list {
			if s, ok := k.(string); ok {
				entity.Key = append(entity.Key, s)
			}
		}
	}
	for name, attrs := range properties {
		p := Property{Reference: truthy(attrs["entity"])}
		p.Type, _ = attrs["type"].(string)
		p.Entity, _ = attrs["entity"].(string)
		entity.Properties[name] = p
	}
	return entity, nil
}

// members collects the child blocks of the given type of an entity or
// component: no other block type and no repeated name (the parser has refused
// blocks inside them already).
func members(block *hclBlock, memberType string) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	for _, child := range block.blocks {
		if child.typ != memberType {
			return nil, hclErrorf(child.line, "%s %q cannot contain a %s block (this converter supports %s)", block.typ, block.name, child.typ, memberType)
		}
		if isPrototypeName(child.name) {
			return nil, prototypeNameError(child.line, child.typ, child.name)
		}
		if _, dup := out[child.name]; dup {
			return nil, hclErrorf(child.line, "duplicate %s %q in %s %q", child.typ, child.name, block.typ, block.name)
		}
		out[child.name] = child.attributes
	}
	return out, nil
}

// truthy is JavaScript's truthiness of a parsed attribute value.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case string:
		return x != ""
	case float64:
		return x != 0
	case bool:
		return x
	}
	return true
}

// prototypeNames are the names of the properties of JavaScript's
// Object.prototype. The reference checker keeps the entities, properties,
// components, fields and enums of a model in plain JavaScript objects and asks
// `name in object`, so it takes any of these names for one that was declared
// twice, and refuses the model.
var prototypeNames = []string{
	"__defineGetter__", "__defineSetter__", "__lookupGetter__", "__lookupSetter__", "__proto__",
	"constructor", "hasOwnProperty", "isPrototypeOf", "propertyIsEnumerable", "toLocaleString", "toString", "valueOf",
}

// PrototypeNames lists the names that the reference checker refuses in a model
// (see the findings of HCLReader), sorted.
func PrototypeNames() []string { return slices.Clone(prototypeNames) }

func isPrototypeName(name string) bool { return slices.Contains(prototypeNames, name) }

func prototypeNameError(line int, kind, name string) error {
	return hclErrorf(line, "%s %q has the name of a property of JavaScript's Object.prototype; the reference checker (github.com/meaninggraph/core) refuses a model that declares such a name, so rename it (the names are %s)", kind, name, strings.Join(prototypeNames, ", "))
}
