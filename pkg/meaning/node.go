package meaning

import (
	"encoding/json"
	"strconv"
)

// Kind is the kind of value a Node holds.
type Kind int

// The kinds of YAML value a meaning file may hold.
const (
	Null Kind = iota
	String
	Number
	Bool
	Map
	Seq
)

// Node is a parsed YAML value with the line it starts on. Scalars are
// resolved the way the Node reference checker's YAML library resolves them
// (YAML 1.2 core schema): only true/false are booleans, and yes/no/on/off and
// dates are strings. See ParseYAML for the subset of YAML that is read.
type Node struct {
	Kind Kind
	Line int
	// Text is the value of a scalar: the string itself, or the canonical
	// text of a number or a boolean.
	Text string
	// Keys are the keys of a Map in document order.
	Keys []string
	// Fields are the values of a Map by key.
	Fields map[string]*Node
	// Items are the elements of a Seq.
	Items []*Node
}

// Field returns the value of a key of a Map, or nil.
func (n *Node) Field(key string) *Node {
	if n == nil || n.Kind != Map {
		return nil
	}
	return n.Fields[key]
}

// Str returns the value of a String node.
func (n *Node) Str() (string, bool) {
	if n == nil || n.Kind != String {
		return "", false
	}
	return n.Text, true
}

// Value converts the node to the values a JSON Schema validator reads:
// nil, string, bool, json.Number, []any and map[string]any.
func (n *Node) Value() any {
	switch n.Kind {
	case String:
		return n.Text
	case Number:
		return json.Number(n.Text)
	case Bool:
		return n.Text == "true"
	case Map:
		m := make(map[string]any, len(n.Keys))
		for _, key := range n.Keys {
			m[key] = n.Fields[key].Value()
		}
		return m
	case Seq:
		items := make([]any, len(n.Items))
		for i, item := range n.Items {
			items[i] = item.Value()
		}
		return items
	}
	return nil
}

// At returns the line of the value at a JSON pointer's tokens, or of the
// deepest node on the way to it that exists.
func (n *Node) At(path []string) int {
	line := n.Line
	for _, token := range path {
		switch n.Kind {
		case Map:
			n = n.Fields[token]
		case Seq:
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(n.Items) {
				return line
			}
			n = n.Items[index]
		}
		if n == nil {
			return line
		}
		line = n.Line
	}
	return line
}
