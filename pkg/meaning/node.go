package meaning

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"regexp"
	"strconv"

	"go.yaml.in/yaml/v3"
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
// (YAML 1.2 core schema): only true/false are booleans, yes/no/on/off and
// dates are strings, and an anchor's content is copied where it is aliased.
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

// SyntaxError is a YAML problem in a file, with the line when it is known.
type SyntaxError struct {
	Line    int
	Message string
}

func (e *SyntaxError) Error() string { return e.Message }

// maxNodes bounds the size of a document after aliases are expanded.
const maxNodes = 1 << 20

var yamlLine = regexp.MustCompile(`^(?:yaml: )?(?:line (\d+): )?`)

// ParseYAML parses one YAML document. An empty file is a Null node. Several
// documents, duplicate keys, keys that are not scalars, tags other than
// !!str, and numbers that are not finite are refused.
func ParseYAML(data []byte) (*Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); errors.Is(err, io.EOF) {
		return &Node{Kind: Null, Line: 1}, nil
	} else if err != nil {
		return nil, syntaxError(err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, &SyntaxError{Line: extra.Line, Message: "the file holds more than one YAML document"}
	}
	budget := maxNodes
	return convert(doc.Content[0], &budget)
}

func syntaxError(err error) *SyntaxError {
	text := err.Error()
	loc := yamlLine.FindStringSubmatch(text)
	line, _ := strconv.Atoi(loc[1])
	return &SyntaxError{Line: line, Message: text[len(loc[0]):]}
}

func convert(n *yaml.Node, budget *int) (*Node, error) {
	if *budget--; *budget < 0 {
		return nil, &SyntaxError{Line: n.Line, Message: "the document is too large once aliases are expanded"}
	}
	switch n.Kind {
	case yaml.AliasNode:
		return convert(n.Alias, budget)
	case yaml.MappingNode:
		return convertMap(n, budget)
	case yaml.SequenceNode:
		out := &Node{Kind: Seq, Line: n.Line}
		for _, item := range n.Content {
			child, err := convert(item, budget)
			if err != nil {
				return nil, err
			}
			out.Items = append(out.Items, child)
		}
		return out, nil
	}
	return convertScalar(n)
}

func convertMap(n *yaml.Node, budget *int) (*Node, error) {
	out := &Node{Kind: Map, Line: n.Line, Fields: map[string]*Node{}}
	for i := 0; i < len(n.Content); i += 2 {
		keyNode := n.Content[i]
		for keyNode.Kind == yaml.AliasNode {
			keyNode = keyNode.Alias
		}
		if keyNode.Kind != yaml.ScalarNode {
			return nil, &SyntaxError{Line: keyNode.Line, Message: "a mapping key must be a scalar"}
		}
		key := keyNode.Value
		if _, seen := out.Fields[key]; seen {
			return nil, &SyntaxError{Line: keyNode.Line, Message: fmt.Sprintf("the key %q is repeated in one mapping", key)}
		}
		value, err := convert(n.Content[i+1], budget)
		if err != nil {
			return nil, err
		}
		out.Keys = append(out.Keys, key)
		out.Fields[key] = value
	}
	return out, nil
}

var (
	yamlInt   = regexp.MustCompile(`^[-+]?[0-9]+$`)
	yamlOct   = regexp.MustCompile(`^0o[0-7]+$`)
	yamlHex   = regexp.MustCompile(`^0x[0-9a-fA-F]+$`)
	yamlFloat = regexp.MustCompile(`^[-+]?(?:\.[0-9]+|[0-9]+(?:\.[0-9]*)?)(?:[eE][-+]?[0-9]+)?$`)
	yamlInf   = regexp.MustCompile(`^[-+]?\.(?:inf|Inf|INF)$|^\.(?:nan|NaN|NAN)$`)
)

func convertScalar(n *yaml.Node) (*Node, error) {
	out := &Node{Line: n.Line, Kind: String, Text: n.Value}
	if n.Style&yaml.TaggedStyle != 0 {
		if n.ShortTag() != "!!str" {
			return nil, &SyntaxError{Line: n.Line, Message: fmt.Sprintf("the tag %s is not supported; write the value plainly", n.Tag)}
		}
		return out, nil
	}
	if n.Style&(yaml.SingleQuotedStyle|yaml.DoubleQuotedStyle|yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		return out, nil
	}
	text := n.Value
	switch {
	case text == "" || text == "~" || text == "null" || text == "Null" || text == "NULL":
		out.Kind, out.Text = Null, ""
	case text == "true" || text == "True" || text == "TRUE":
		out.Kind, out.Text = Bool, "true"
	case text == "false" || text == "False" || text == "FALSE":
		out.Kind, out.Text = Bool, "false"
	case yamlInt.MatchString(text):
		out.Kind, out.Text = Number, bigText(text, 10)
	case yamlOct.MatchString(text):
		out.Kind, out.Text = Number, bigText(text[2:], 8)
	case yamlHex.MatchString(text):
		out.Kind, out.Text = Number, bigText(text[2:], 16)
	case yamlInf.MatchString(text):
		return nil, &SyntaxError{Line: n.Line, Message: fmt.Sprintf("%s is not a finite number", text)}
	case yamlFloat.MatchString(text):
		f, _ := strconv.ParseFloat(text, 64)
		if math.IsInf(f, 0) {
			return nil, &SyntaxError{Line: n.Line, Message: fmt.Sprintf("%s is not a finite number", text)}
		}
		out.Kind, out.Text = Number, strconv.FormatFloat(f, 'g', -1, 64)
	}
	return out, nil
}

// bigText writes an integer of any size in decimal, without a sign of +.
func bigText(digits string, base int) string {
	n, _ := new(big.Int).SetString(digits, base)
	return n.String()
}
