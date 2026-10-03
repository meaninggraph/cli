package meaning

import (
	"strings"
)

// blockScalar reads a literal (|) or folded (>) scalar. The header is the text
// from the indicator on; the lines that follow are its content, indented more
// than p. Keep chomping (+) and indentation indicators are outside the subset.
func (r *reader) blockScalar(header string, no, p int) (*Node, *SyntaxError) {
	i, chomp := 1, byte(0)
	if i < len(header) && (header[i] == '-' || header[i] == '+') {
		if header[i] == '+' {
			return nil, syntax(no, RuleYAMLUnsupported, "keep chomping (%c+) is not supported, because readers disagree about the last line break of the file; use |, >, |- or >-", header[0])
		}
		chomp = header[i]
		i++
	}
	if i < len(header) && header[i] >= '0' && header[i] <= '9' {
		return nil, syntax(no, RuleYAMLUnsupported, "an indentation indicator (a digit after %c) is not supported; indent the text by two spaces", header[0])
	}
	if err := r.endOfLine(header[i:], no); err != nil {
		return nil, err
	}
	first, indent := r.pos, -1
	for ; !r.eof(); r.pos++ {
		l := r.cur()
		if !l.blank() {
			if indent < 0 && l.indent > p {
				indent = l.indent
			}
			if indent < 0 || l.indent < indent {
				break
			}
		}
		r.inBlock[r.pos] = true
	}
	// The tabs of a line of text are text. A blank line that holds a tab is
	// refused (its tabs are never marked): the reference parser reads it as text
	// when it follows the indentation and refuses it when it does not. A blank
	// line of more spaces than the text is indented by is text at the end of the
	// file, and read differently from the lines around it elsewhere.
	lines := make([]string, r.pos-first)
	for k := range lines {
		l := r.lines[first+k]
		switch {
		case !l.blank():
			lines[k] = l.raw[indent:]
			r.markTabs(first+k, indent, len(l.raw))
		case indent >= 0 && len(l.raw) > indent && strings.TrimLeft(l.raw, " ") == "":
			return nil, syntax(l.no, RuleYAMLUnsupported, "a blank line inside a block scalar holds more spaces than the text is indented by; remove the spaces")
		}
	}
	return &Node{Kind: String, Line: no, Text: blockText(lines, header[0] == '>', chomp)}, nil
}

// blockText joins the content lines of a block scalar: a literal keeps its
// line breaks, a folded one turns single breaks into spaces; a stripping scalar
// (chomp is '-') has no break at its end, a clipped one has one.
func blockText(lines []string, folded bool, chomp byte) string {
	// Blank lines at the end are not part of a clipped or stripped scalar.
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	text := strings.Join(lines, "\n")
	if folded {
		text = fold(lines)
	}
	if chomp == '-' || len(lines) == 0 {
		return text
	}
	return text + "\n"
}

// fold joins lines the way a folded scalar does: lines that start with a
// non-space are joined by one space, or by one line break per empty line
// between them; a line that starts with a space keeps its line breaks.
func fold(lines []string) string {
	var sb strings.Builder
	prevNormal, empties, first := false, 0, true
	for _, line := range lines {
		if line == "" {
			empties++
			continue
		}
		normal := line[0] != ' ' && line[0] != '\t' // a line that starts with a blank keeps its breaks
		switch {
		case first:
			sb.WriteString(strings.Repeat("\n", empties))
		case normal && prevNormal && empties == 0:
			sb.WriteByte(' ')
		case normal && prevNormal:
			sb.WriteString(strings.Repeat("\n", empties))
		default:
			sb.WriteString(strings.Repeat("\n", empties+1))
		}
		sb.WriteString(line)
		prevNormal, empties, first = normal, 0, false
	}
	return sb.String()
}

// flowValue reads a flow collection that starts on the line before the current
// one, at the end of text t, and may continue on the lines after it; lines
// that continue it must be indented more than p.
func (r *reader) flowValue(t string, no, p int) (node *Node, err *SyntaxError) {
	li := r.pos - 1
	f := &flow{r: r, li: li, ci: len(strings.TrimRight(r.lines[li].raw, " \t")) - len(t), p: p, start: no}
	// The collection is read by recursive descent, and a problem is raised with
	// a panic that is caught here, which keeps the error handling out of every
	// step of the descent.
	defer func() {
		if recovered := recover(); recovered != nil {
			node, err = nil, recovered.(flowFailure).err
		}
	}()
	node = f.value()
	r.pos = f.li + 1
	return node, r.endOfLine(f.r.lines[f.li].raw[f.ci:], f.r.lines[f.li].no)
}

// flowFailure is the panic that carries a problem out of a flow collection.
type flowFailure struct{ err *SyntaxError }

// flow reads flow collections ([...] and {...}) across lines.
type flow struct {
	r      *reader
	li, ci int
	p      int
	depth  int
	// start is the line the outermost collection starts on.
	start int
}

func (f *flow) no() int { return f.r.lines[f.li].no }

func (f *flow) raw() string { return f.r.lines[f.li].raw }

func (f *flow) fail(rule, format string, args ...any) {
	panic(flowFailure{syntax(f.no(), rule, format, args...)})
}

// quoted reads a quoted scalar at the current position.
func (f *flow) quoted() string {
	value, next, err := scanQuoted(f.raw(), f.ci, f.no())
	if err != nil {
		panic(flowFailure{err})
	}
	f.r.markTabs(f.li, f.ci+1, next-1)
	f.ci = next
	return value
}

// skipSpace moves over spaces and line breaks to the next token. The lines that
// continue a collection must be indented more than the collection's parent.
func (f *flow) skipSpace() {
	for {
		raw := f.raw()
		for f.ci < len(raw) && raw[f.ci] == ' ' {
			f.ci++
		}
		if strings.TrimLeft(raw[f.ci:], " \t") == "" {
			f.ci = len(raw) // blanks, tabs included, at the end of the line
		}
		if f.ci < len(raw) {
			break
		}
		if f.li+1 >= len(f.r.lines) {
			panic(flowFailure{syntax(f.start, RuleYAML, "the flow collection that starts here is not closed before the end of the file")})
		}
		f.li++
		f.ci = 0
		// A line that continues a collection is indented more than what holds it,
		// except that the bracket that closes the outermost collection may stand
		// at that indent.
		next := f.r.lines[f.li]
		closes := !next.blank() && next.indent == f.p && f.depth == 1 && (next.raw[next.indent] == ']' || next.raw[next.indent] == '}')
		if !next.blank() && next.indent <= f.p && !closes {
			f.fail(RuleYAML, "a line that continues a [ ] or { } collection must be indented more than the line it started on")
		}
	}
	if f.raw()[f.ci] == '#' {
		f.fail(RuleYAMLUnsupported, "comments inside [ ] or { } are not supported; move the comment out of the collection")
	}
}

func (f *flow) value() *Node {
	f.skipSpace()
	raw := f.raw()
	switch c := raw[f.ci]; c {
	case '[':
		return f.seq()
	case '{':
		return f.mapping()
	case '"', '\'':
		line := f.no()
		return &Node{Kind: String, Line: line, Text: f.quoted()}
	case '&', '*':
		f.fail(RuleYAMLAnchor, "anchors and aliases (& and *) are not supported; write the value out")
	case '!':
		f.fail(RuleYAMLTag, "tags (!) are not supported; write the value plainly")
	case ',', ']', '}', '|', '>', '%', '@', '`':
		f.fail(RuleYAML, "unexpected %q here; put a value that starts with it in quotes", c)
	}
	text := f.plain()
	if text == "" {
		f.fail(RuleYAML, "a value cannot start with %q here; put it in quotes", raw[f.ci])
	}
	kind, resolved, err := resolvePlain(text, f.no())
	if err != nil {
		panic(flowFailure{err})
	}
	return &Node{Kind: kind, Line: f.no(), Text: resolved}
}

// plain reads a plain scalar of a flow collection: it ends at a flow indicator,
// at a colon that is followed by a space, the end of the line or a flow
// indicator, and at a comment.
func (f *flow) plain() string {
	raw := f.raw()
	j := f.ci
	for j < len(raw) {
		c := raw[j]
		if strings.IndexByte(",[]{}", c) >= 0 || (c == ' ' && j+1 < len(raw) && raw[j+1] == '#') {
			break
		}
		if c == ':' && (j+1 == len(raw) || strings.IndexByte(" ,[]{}", raw[j+1]) >= 0) {
			break
		}
		j++
	}
	text := strings.TrimRight(raw[f.ci:j], " ")
	if text == "" {
		return ""
	}
	if strings.Contains(text, "\t") {
		f.fail(RuleYAMLTab, "a tab inside [ ] or { } is accepted in quotes only; use spaces")
	}
	if first := text[0]; (first == '-' || first == '?' || first == ':') && (len(text) == 1 || text[1] == ' ') {
		return ""
	}
	f.ci = j
	return text
}

// afterValueFailure reports what follows a value of a flow collection where a
// comma or the closing bracket belongs: text on a line below the value is a
// plain scalar that continues there.
func (f *flow) afterValueFailure(ended int, closing byte, found byte) {
	if f.li != ended {
		f.fail(RuleYAMLUnsupported, "a plain scalar inside [ ] or { } cannot continue on the next line (it continues with %q); write it on one line or put it in quotes", found)
	}
	f.fail(RuleYAML, "expected a comma or %c after the value, found %q", closing, found)
}

func (f *flow) enter() {
	if f.depth++; f.depth+f.r.depth > MaxYAMLDepth {
		f.fail(RuleYAMLLimit, "collections are nested more than %d levels deep", MaxYAMLDepth)
	}
}

func (f *flow) seq() *Node {
	f.enter()
	defer func() { f.depth-- }()
	node := &Node{Kind: Seq, Line: f.no()}
	f.ci++
	for {
		f.skipSpace()
		if f.raw()[f.ci] == ']' {
			f.ci++
			return node
		}
		node.Items = append(node.Items, f.value())
		ended := f.li
		f.skipSpace()
		switch c := f.raw()[f.ci]; c {
		case ',':
			f.ci++
		case ']':
			f.ci++
			return node
		case ':':
			f.fail(RuleYAMLUnsupported, "\"key: value\" inside [ ] is not supported; use { } for a mapping")
		default:
			f.afterValueFailure(ended, ']', c)
		}
	}
}

func (f *flow) mapping() *Node {
	f.enter()
	defer func() { f.depth-- }()
	node := &Node{Kind: Map, Line: f.no(), Fields: map[string]*Node{}}
	f.ci++
	for {
		f.skipSpace()
		if f.raw()[f.ci] == '}' {
			f.ci++
			return node
		}
		key := f.key()
		if _, dup := node.Fields[key]; dup {
			f.fail(RuleYAMLDuplicate, "the key %q is repeated in one mapping", key)
		}
		f.skipSpace()
		if f.raw()[f.ci] != ':' {
			f.fail(RuleYAMLUnsupported, "the entry %q in { } is not followed by \": value\"; put a space after a colon that starts a value (an entry without a value is not supported)", key)
		}
		f.ci++
		f.skipSpace()
		value := nullAt(f.no())
		if c := f.raw()[f.ci]; c != ',' && c != '}' {
			value = f.value()
		}
		node.Keys = append(node.Keys, key)
		node.Fields[key] = value
		ended := f.li
		f.skipSpace()
		switch c := f.raw()[f.ci]; c {
		case ',':
			f.ci++
		case '}':
			f.ci++
			return node
		default:
			f.afterValueFailure(ended, '}', c)
		}
	}
}

// key reads a key of a flow mapping: a quoted or a plain string.
func (f *flow) key() string {
	line := f.no()
	var key string
	switch c := f.raw()[f.ci]; c {
	case '"', '\'':
		key = f.quoted()
	case '&', '*':
		f.fail(RuleYAMLAnchor, "anchors and aliases (& and *) are not supported; write the value out")
	case '!':
		f.fail(RuleYAMLTag, "tags (!) are not supported; write the value plainly")
	case '[', '{', ',', ']', '|', '>', '%', '@', '`':
		f.fail(RuleYAML, "%q cannot start a key here; put the key in quotes", c)
	default:
		key = f.plain()
		kind, _, err := resolvePlain(key, line)
		if err != nil {
			panic(flowFailure{err})
		}
		if key == "" || kind != String {
			f.fail(RuleYAMLKey, "the key %s is not read as a string (YAML reads it as %s); put it in quotes", describeKey(key), kindName(kind))
		}
	}
	if err := keyProblem(key, line); err != nil {
		panic(flowFailure{err})
	}
	return key
}
