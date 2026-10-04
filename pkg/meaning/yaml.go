package meaning

import (
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The YAML subset.
//
// A meaning file is read as a plainly defined subset of YAML 1.2, in which it
// agrees with the reference checker's YAML library (the `yaml` 2.x package of
// npm) on every file it accepts, and which it enforces before anything is
// interpreted. Anything outside the subset is refused with a finding of its own
// that says what to write instead; it is never interpreted.
//
// The subset:
//
//   - UTF-8 text, optionally starting with a byte order mark (which may not be
//     followed by an indented first line or a block sequence: the reference
//     parser counts it as a column). No NUL, no other control character, no
//     U+0085, U+2028, U+2029 or U+FEFF inside the text. Lines end in LF or CRLF
//     (no lone CR). Tabs only where they cannot change what is read: see
//     "Tabs" below.
//   - One document, optionally started by a "---" line. No directives (%YAML,
//     %TAG) and no other document markers.
//   - Block mappings and sequences (a sequence may sit at the indent of its
//     key), flow mappings and sequences (which may span lines, without
//     comments), and scalars: plain (which may continue on following, more
//     indented lines), single-quoted and double-quoted (on one line; the JSON
//     escapes, \/ and \u surrogate pairs included), and literal and folded
//     block scalars (| and >, with an optional - but no indentation indicator
//     and no keep chomping, +: the reference parser and other readers disagree
//     about the last line break of a file that ends in the scalar). A blank
//     line inside or at the end of a block scalar may not hold more spaces than
//     the text is indented by.
//   - No anchors, aliases, merge keys (<<) or tags of any kind, and no explicit
//     keys (?).
//   - String keys only: a key that YAML reads as null, a boolean or a number
//     must be quoted. No repeated key in one mapping.
//   - Numbers are decimal: an integer within the range a double holds exactly
//     (up to 2^53), or a finite float. No 0x or 0o numbers, no .inf or .nan.
//   - At most MaxFileBytes of text and MaxYAMLDepth levels of nesting.
const (
	// MaxFileBytes bounds the size of a meaning file and of a model file.
	MaxFileBytes = 8 << 20
	// MaxYAMLDepth bounds the nesting of collections in a meaning file.
	MaxYAMLDepth = 64
	// maxKeyLength is the longest key the reference parser accepts.
	maxKeyLength = 1024
	// maxExactInt is the largest integer a double holds exactly.
	maxExactInt = 1 << 53
)

// Rule identifiers of the YAML findings.
const (
	RuleYAML            = "yaml"
	RuleYAMLEncoding    = "yaml-encoding"
	RuleYAMLCharacter   = "yaml-character"
	RuleYAMLLineEnding  = "yaml-line-ending"
	RuleYAMLTab         = "yaml-tab"
	RuleYAMLDirective   = "yaml-directive"
	RuleYAMLDocuments   = "yaml-documents"
	RuleYAMLAnchor      = "yaml-anchor"
	RuleYAMLTag         = "yaml-tag"
	RuleYAMLKey         = "yaml-key"
	RuleYAMLDuplicate   = "yaml-duplicate-key"
	RuleYAMLNumber      = "yaml-number"
	RuleYAMLEscape      = "yaml-escape"
	RuleYAMLUnsupported = "yaml-unsupported"
	RuleYAMLLimit       = "yaml-limit"
)

// SyntaxError is a problem with the YAML of a file: Rule says which rule of the
// subset (or of YAML itself) it breaks, Line where, 0 when not known. A tab
// that is refused (RuleYAMLTab) also has its Column, counted in characters from
// 1, a tab being one character, and the message starts with it; for any other
// problem Column is 0.
type SyntaxError struct {
	Line    int
	Column  int
	Rule    string
	Message string
}

func (e *SyntaxError) Error() string { return e.Message }

func syntax(line int, rule, format string, args ...any) *SyntaxError {
	return &SyntaxError{Line: line, Rule: rule, Message: fmt.Sprintf(format, args...)}
}

// tabError is a yaml-tab error for the tab at byte i of the line raw, of line
// number line: a tab is invisible, so the message begins with its column.
func tabError(line int, raw string, i int, format string, args ...any) *SyntaxError {
	column := utf8.RuneCountInString(raw[:i]) + 1
	err := syntax(line, RuleYAMLTab, "column %d: "+format, append([]any{column}, args...)...)
	err.Column = column
	return err
}

// ParseYAML reads one document of the subset described above. An empty file
// is a Null node.
func ParseYAML(data []byte) (*Node, *SyntaxError) {
	node, err, _ := parseYAML(data)
	return node, err
}

// parseYAML is ParseYAML, and also returns the number of characters the tab
// bookkeeping looked at, which a test reads to show that it is one pass.
func parseYAML(data []byte) (*Node, *SyntaxError, int) {
	if len(data) > MaxFileBytes {
		return nil, syntax(0, RuleYAMLLimit, "the file is %d bytes; at most %d are read", len(data), MaxFileBytes), 0
	}
	text, mark, err := checkText(string(data))
	if err != nil {
		return nil, err, 0
	}
	r := &reader{okTab: map[int][]bool{}}
	raws := strings.Split(text, "\n")
	raws = raws[:len(raws)-1+min(1, len(strings.TrimRight(raws[len(raws)-1], "\r")))] // the text after the last line break is no line
	for i, raw := range raws {
		raw = strings.TrimSuffix(raw, "\r")
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		if strings.TrimLeft(raw, " \t") == "" {
			indent = len(raw) // blank: spaces and tabs only
		}
		comment := strings.HasPrefix(strings.TrimLeft(raw, " \t"), "#")
		r.lines = append(r.lines, yline{no: i + 1, indent: indent, raw: raw, comment: comment})
		r.original = append(r.original, raw)
	}
	if mark && len(r.lines) > 0 {
		// The reference parser counts the mark as a column of the first line: an
		// indented first line, or a block sequence that starts on it, is refused
		// there, and read differently by a reader that skips the mark.
		if first := r.lines[0]; !first.blank() && (first.indent > 0 || isSeqEntry(first.text())) && first.text()[0] != '#' {
			return nil, syntax(1, RuleYAMLEncoding, "a byte order mark is followed by an indented line or a block sequence; the reference parser counts the mark as a column, so save the file without the mark"), 0
		}
	}
	node, err := r.document()
	if err != nil {
		return nil, err, r.steps
	}
	if err := r.checkTabs(); err != nil {
		return nil, err, r.steps
	}
	return node, nil, r.steps
}

// checkText applies the rules that concern characters and line ends, and
// strips a leading byte order mark (mark says there was one). Tabs are not
// judged here but where they are read: see checkTabs.
func checkText(s string) (text string, mark bool, err *SyntaxError) {
	s, mark = strings.CutPrefix(s, "\xef\xbb\xbf")
	line := 1
	for i := 0; i < len(s); {
		c, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case c == utf8.RuneError && size == 1:
			return "", false, syntax(line, RuleYAMLEncoding, "the file is not UTF-8 text (a UTF-16 or Latin-1 file?); save it as UTF-8")
		case c == 0:
			return "", false, syntax(line, RuleYAMLEncoding, "the file holds a NUL character, so it is not UTF-8 text (a UTF-16 file?); save it as UTF-8")
		case c == '\n':
			line++
		case c == '\r':
			if !strings.HasPrefix(s[i+1:], "\n") {
				return "", false, syntax(line, RuleYAMLLineEnding, "a carriage return that is not followed by a line feed; end lines with LF or CRLF")
			}
		case c == '\t':
		case c < 0x20 || c == 0x7f || (c >= 0x80 && c <= 0x9f) || c == 0x2028 || c == 0x2029 || c == 0xfeff || c == 0xfffe || c == 0xffff:
			return "", false, syntax(line, RuleYAMLCharacter, "the character U+%04X is not allowed; write it as an escape (\\uXXXX) in a double-quoted string", c)
		}
		i += size
	}
	return s, mark, nil
}

type yline struct {
	no, indent int
	raw        string
	// comment says the line holds a comment and nothing else (after blanks).
	comment bool
}

// text is the line without its indent and without trailing spaces and tabs.
func (l yline) text() string { return strings.TrimRight(l.raw[l.indent:], " \t") }

func (l yline) blank() bool { return l.indent == len(l.raw) }

type reader struct {
	lines []yline
	// original holds the lines as they were read: lines[i] is rewritten when a
	// sequence entry shares its line with the mapping or sequence it starts.
	original []string
	pos      int
	depth    int
	// okTab holds, for the lines that have tabs, which columns hold a tab that
	// was read as text. See checkTabs.
	okTab map[int][]bool
	// steps counts the characters the tab bookkeeping looked at, so that a test
	// can show that it is one pass.
	steps int
}

// Tabs.
//
// The rule has no context: it is decided by the line and by what the reader is
// reading when it meets the tab, never by what came before or comes after.
// A tab is read only
//
//   - inside a double-quoted or single-quoted scalar (a key too);
//   - inside the text of a comment, between its # and its last character;
//   - between two characters that are not blanks, on a text line of a plain
//     scalar or of a block scalar (not at the start of the text, not at its end,
//     not next to a colon or a dash that would make it something else).
//
// Everywhere else a tab is refused (RuleYAMLTab): in the leading white space of
// any line (comment lines and lines of blanks included), alone on a line,
// before a #, at the end of a line outside quotes, after a colon or a dash,
// between tokens, inside a flow collection outside quotes. The parts of the
// reader that read a tab as text mark it (acceptTabs); checkTabs then refuses
// every tab that was not marked, in one pass over the file.

// mark marks the tabs of raw[from:to] of line index as read; with interior only
// the tabs between the first and the last character that is not a blank.
func (r *reader) mark(index, from, to int, interior bool) {
	raw := r.original[index]
	to = min(to, len(raw))
	if interior {
		for from < to && (raw[from] == ' ' || raw[from] == '\t') {
			from++
		}
		for to > from && (raw[to-1] == ' ' || raw[to-1] == '\t') {
			to--
		}
	}
	for col := from; col < to; col++ {
		r.steps++
		if raw[col] == '\t' {
			if r.okTab[index] == nil {
				r.okTab[index] = make([]bool, len(raw))
			}
			r.okTab[index][col] = true
		}
	}
}

// markTabs marks every tab of raw[from:to] (text inside quotes).
func (r *reader) markTabs(index, from, to int) { r.mark(index, from, to, false) }

// acceptTabs marks the tabs of t[from:to], where t is a piece of the line
// number no that ends the line (or ends it but for blanks).
func (r *reader) acceptTabs(no int, t string, from, to int, interior bool) {
	base := r.base(no, t)
	r.mark(no-1, base+from, base+to, interior)
}

// base is the byte index in line number no where t starts, t being the end of
// the line or the end of it but for blanks.
func (r *reader) base(no int, t string) int {
	raw := r.original[no-1]
	if strings.HasSuffix(raw, t) {
		return len(raw) - len(t)
	}
	return len(strings.TrimRight(raw, " \t")) - len(t)
}

// tabAt is the yaml-tab error for the tab at byte i of t, a piece of line
// number no that ends the line (or ends it but for blanks).
func (r *reader) tabAt(no int, t string, i int, format string, args ...any) *SyntaxError {
	return tabError(no, r.original[no-1], r.base(no, t)+i, format, args...)
}

// checkTabs refuses the first tab that was not read as text.
func (r *reader) checkTabs() *SyntaxError {
	for i, raw := range r.original {
		if !strings.Contains(raw, "\t") {
			continue
		}
		ok := r.okTab[i]
		for col := 0; col < len(raw); col++ {
			r.steps++
			if raw[col] == '\t' && (ok == nil || !ok[col]) {
				return tabError(i+1, raw, col, "a tab here is not read: tabs are accepted inside quotes, in a comment after the #, and between two characters of plain and block text, but not in indentation, alone on a line, before a #, at the end of a line or between tokens; use spaces")
			}
		}
	}
	return nil
}

func (r *reader) eof() bool { return r.pos >= len(r.lines) }

func (r *reader) cur() yline { return r.lines[r.pos] }

// skipBlank moves to the next line that holds more than blanks and a comment.
func (r *reader) skipBlank() {
	for !r.eof() && (r.cur().blank() || r.cur().comment) {
		if raw := r.original[r.pos]; r.cur().comment {
			r.mark(r.pos, strings.IndexByte(raw, '#'), len(raw), true) // the text of the comment
		}
		r.pos++
	}
}

func nullAt(line int) *Node { return &Node{Kind: Null, Line: line} }

func (r *reader) enter(line int) *SyntaxError {
	if r.depth++; r.depth > MaxYAMLDepth {
		return syntax(line, RuleYAMLLimit, "collections are nested more than %d levels deep", MaxYAMLDepth)
	}
	return nil
}

func (r *reader) leave() { r.depth-- }

func isMarker(raw, marker string) bool { return raw == marker || strings.HasPrefix(raw, marker+" ") }

func (r *reader) document() (*Node, *SyntaxError) {
	for _, l := range r.lines {
		if strings.HasPrefix(l.raw, "%") {
			return nil, syntax(l.no, RuleYAMLDirective, "a directive (%%YAML or %%TAG); remove it")
		}
	}
	r.skipBlank()
	if r.eof() {
		return nullAt(1), nil
	}
	line := r.cur().no
	if first := r.cur(); isMarker(first.raw, "---") {
		rest := strings.TrimSpace(strings.TrimPrefix(first.raw, "---"))
		if rest != "" && !strings.HasPrefix(rest, "#") {
			return nil, syntax(first.no, RuleYAMLDocuments, "text on the --- line; start the document on the next line")
		}
		r.acceptTabs(first.no, rest, 0, len(rest), true) // the text of a comment after the marker
		r.pos++
	}
	for _, l := range r.lines[r.pos:] {
		if isMarker(l.raw, "---") || isMarker(l.raw, "...") {
			return nil, syntax(l.no, RuleYAMLDocuments, "a document marker (--- or ...); a meaning file is one document, optionally started by a --- line")
		}
	}
	r.skipBlank()
	if r.eof() {
		return nullAt(line), nil
	}
	node, err := r.block(-1)
	if err != nil {
		return nil, err
	}
	r.skipBlank()
	if !r.eof() {
		return nil, syntax(r.cur().no, RuleYAML, "this line is indented differently from the lines before it")
	}
	return node, nil
}

func isSeqEntry(t string) bool { return t == "-" || strings.HasPrefix(t, "- ") }

// dashTab says that the text starts like a sequence entry whose dash is
// followed by a tab, which the subset does not read.
func dashTab(t string) bool { return strings.HasPrefix(t, "-\t") }

// dashTabError is the error for the tab of "-<tab>" at the start of the text t
// of line no.
func (r *reader) dashTabError(no int, t string) *SyntaxError {
	return r.tabAt(no, t, 1, "a tab after a dash is not accepted; use a space")
}

// nested reads the value that follows a key or a dash with nothing after it but
// a comment (comment, possibly empty): a block on the next lines, more indented
// than p (or, after a key, a sequence at p itself when seqSame), else null.
func (r *reader) nested(no, p int, seqSame bool, comment string) (*Node, *SyntaxError) {
	r.acceptTabs(no, comment, 0, len(comment), true) // the text of a comment on the line of the key or dash
	from := r.pos
	r.skipBlank()
	if r.eof() {
		return nullAt(no), nil
	}
	l := r.cur()
	switch {
	case l.indent > p:
		if comment := r.commentBetween(from); comment > 0 && !r.startsCollection(l) {
			// The reference parser's lexer lowers the indentation of what follows
			// a comment line in this place (by the comment's layout), and the lines
			// of the scalar that follows are read as part of it or refused.
			return nil, syntax(comment, RuleYAMLUnsupported, "a comment line between a key (or a dash) and the value that starts on a later line (line %d) is not supported; move this comment above the key", l.no)
		}
		return r.block(p)
	case seqSame && l.indent == p && isSeqEntry(l.text()):
		return r.parseSeq(p)
	}
	return nullAt(no), nil
}

// commentBetween is the number of the first comment line among the lines from
// line index from up to the current one, 0 when there is none.
func (r *reader) commentBetween(from int) int {
	for _, l := range r.lines[from:r.pos] {
		if l.comment {
			return l.no
		}
	}
	return 0
}

// startsCollection says that a line starts a block sequence or a block mapping,
// rather than a scalar (a malformed entry counts: the collection reports it).
func (r *reader) startsCollection(l yline) bool {
	t := l.text()
	return isSeqEntry(t) || dashTab(t) || r.isKeyLine(t, l.no)
}

// block reads the node that starts at the current line, whose indent is more
// than p.
func (r *reader) block(p int) (*Node, *SyntaxError) {
	l := r.cur()
	if err := r.enter(l.no); err != nil {
		return nil, err
	}
	defer r.leave()
	t := l.text()
	if dashTab(t) {
		return nil, r.dashTabError(l.no, t)
	}
	if isSeqEntry(t) {
		return r.parseSeq(l.indent)
	}
	if _, _, ok, err := r.splitKey(t, l.no); err != nil || ok {
		if err != nil {
			return nil, err
		}
		return r.parseMap(l.indent)
	}
	r.pos++
	return r.value(t, l.no, p, false)
}

func (r *reader) parseMap(ind int) (*Node, *SyntaxError) {
	node := &Node{Kind: Map, Line: r.cur().no, Fields: map[string]*Node{}}
	for {
		r.skipBlank()
		if r.eof() || r.cur().indent < ind {
			return node, nil
		}
		l := r.cur()
		t := l.text()
		switch {
		case l.indent > ind:
			return nil, syntax(l.no, RuleYAML, "this line is indented more than the other entries of its mapping")
		case dashTab(t):
			return nil, r.dashTabError(l.no, t)
		case isSeqEntry(t):
			return nil, syntax(l.no, RuleYAML, "a sequence entry (-) where a \"key: value\" entry of the mapping is expected")
		}
		key, rest, ok, err := r.splitKey(t, l.no)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, syntax(l.no, RuleYAML, "expected \"key: value\" here; a key is followed by a colon and a space")
		}
		if _, dup := node.Fields[key]; dup {
			return nil, syntax(l.no, RuleYAMLDuplicate, "the key %q is repeated in one mapping", key)
		}
		r.pos++
		value, err := r.afterIndicator(rest, l.no, ind, true)
		if err != nil {
			return nil, err
		}
		node.Keys = append(node.Keys, key)
		node.Fields[key] = value
	}
}

// afterIndicator reads what follows "key:" or "-" on its line: a value on the
// same line, or a nested block on the lines below.
func (r *reader) afterIndicator(rest string, no, p int, seqSame bool) (*Node, *SyntaxError) {
	rest = strings.TrimLeft(rest, " ")
	if rest == "" || rest[0] == '#' {
		return r.nested(no, p, seqSame, rest)
	}
	return r.value(rest, no, p, true)
}

func (r *reader) parseSeq(ind int) (*Node, *SyntaxError) {
	node := &Node{Kind: Seq, Line: r.cur().no}
	for {
		r.skipBlank()
		if r.eof() || r.cur().indent < ind {
			return node, nil
		}
		l := r.cur()
		t := l.text()
		switch {
		case l.indent > ind:
			return nil, syntax(l.no, RuleYAML, "this line is indented more than the other entries of its sequence")
		case dashTab(t):
			return nil, r.dashTabError(l.no, t)
		case !isSeqEntry(t):
			return node, nil
		}
		afterDash := t[1:]
		rest := strings.TrimLeft(afterDash, " ")
		col := ind + 1 + len(afterDash) - len(rest)
		var item *Node
		var err *SyntaxError
		switch {
		case rest == "" || rest[0] == '#':
			r.pos++
			item, err = r.nested(l.no, ind, false, rest)
		case isSeqEntry(rest) || r.isKeyLine(rest, l.no):
			// A sequence or a mapping that starts on the dash's line: read the
			// rest of the line as a line of its own, at its own column.
			r.lines[r.pos] = yline{no: l.no, indent: col, raw: strings.Repeat(" ", col) + rest}
			item, err = r.block(ind)
		default:
			r.pos++
			item, err = r.value(rest, l.no, ind, true)
		}
		if err != nil {
			return nil, err
		}
		node.Items = append(node.Items, item)
	}
}

// isKeyLine reports whether the text starts a "key: value" entry (a malformed
// one counts: its error is reported by the mapping).
func (r *reader) isKeyLine(t string, no int) bool {
	_, _, ok, err := r.splitKey(t, no)
	return ok || err != nil
}

// splitKey splits "key: rest". ok is false when the text is not a key line.
func (r *reader) splitKey(t string, no int) (key, rest string, ok bool, err *SyntaxError) {
	switch t[0] {
	case '"', '\'':
		value, next, qerr := scanQuoted(t, 0, no)
		if qerr != nil {
			return "", "", false, nil
		}
		after := strings.TrimLeft(t[next:], " ")
		if strings.HasPrefix(after, "\t") && strings.HasPrefix(strings.TrimLeft(after, " \t"), ":") {
			return "", "", false, r.tabAt(no, t, len(t)-len(after), "a tab between a key and its colon is not accepted; use a space or none")
		}
		if !strings.HasPrefix(after, ":") {
			return "", "", false, nil
		}
		if len(after) > 1 && after[1] == '\t' {
			return "", "", false, r.tabAt(no, t, len(t)-len(after)+1, "a tab after a colon is not accepted; use a space")
		}
		if len(after) > 1 && after[1] != ' ' {
			return "", "", false, nil
		}
		r.acceptTabs(no, t, 1, next-1, false)
		return value, after[1:], true, keyProblem(value, no)
	case '&', '*':
		return "", "", false, syntax(no, RuleYAMLAnchor, "anchors and aliases (& and *) are not supported; write the value out")
	case '!':
		return "", "", false, syntax(no, RuleYAMLTag, "tags (!) are not supported; write the value plainly")
	case '[', '{', ']', '}', ',', '#', '|', '>', '%', '@', '`':
		return "", "", false, nil
	case '?':
		if len(t) == 1 || t[1] == ' ' {
			return "", "", false, syntax(no, RuleYAMLUnsupported, "explicit keys (?) are not supported; write \"key: value\"")
		}
	}
	for i := 0; i < len(t); i++ {
		switch {
		case (t[i] == ' ' || t[i] == '\t') && i+1 < len(t) && t[i+1] == '#':
			return "", "", false, nil
		case t[i] == ':' && i+1 < len(t) && t[i+1] == '\t':
			return "", "", false, r.tabAt(no, t, i+1, "a tab after a colon is not accepted; use a space")
		case t[i] == ':' && (i+1 == len(t) || t[i+1] == ' '):
			key = strings.TrimRight(t[:i], " ")
			kind, _, perr := resolvePlain(key, no)
			if perr != nil {
				return "", "", false, perr
			}
			if kind != String {
				return "", "", false, syntax(no, RuleYAMLKey, "the key %s is not read as a string (YAML reads it as %s); put it in quotes", describeKey(key), kindName(kind))
			}
			return key, t[i+1:], true, keyProblem(key, no)
		}
	}
	return "", "", false, nil
}

func describeKey(key string) string {
	if key == "" {
		return "(empty)"
	}
	return strconv.Quote(key)
}

func kindName(k Kind) string {
	switch k {
	case Null:
		return "null"
	case Bool:
		return "a boolean"
	}
	return "a number"
}

// keyProblem checks a key's text: not too long, not a merge key.
func keyProblem(key string, no int) *SyntaxError {
	switch {
	case len(key) > maxKeyLength:
		return syntax(no, RuleYAMLKey, "a key of %d characters; the longest key read is %d", len(key), maxKeyLength)
	case key == "<<":
		return syntax(no, RuleYAMLAnchor, "merge keys (<<) are not supported; write the entries out")
	}
	return nil
}

// value reads a value that starts on a line of its own text (after a key or a
// dash). p is the indent of the collection that holds it.
func (r *reader) value(t string, no, p int, onIndicatorLine bool) (*Node, *SyntaxError) {
	switch t[0] {
	case '|', '>':
		if !onIndicatorLine {
			return nil, syntax(no, RuleYAMLUnsupported, "a block scalar (%c) must start on the line of its key or dash", t[0])
		}
		return r.blockScalar(t, no, p)
	case '[', '{':
		return r.flowValue(t, no, p)
	case '"', '\'':
		return r.quotedValue(t, no)
	case '&', '*':
		return nil, syntax(no, RuleYAMLAnchor, "anchors and aliases (& and *) are not supported; write the value out")
	case '!':
		return nil, syntax(no, RuleYAMLTag, "tags (!) are not supported; write the value plainly")
	case '%', '@', '`', ',', ']', '}':
		return nil, syntax(no, RuleYAML, "a value cannot start with %q; put it in quotes", t[0])
	case '-', '?', ':':
		if len(t) > 1 && t[1] == '\t' {
			return nil, r.tabAt(no, t, 1, "a tab after %q is not accepted; use a space", t[0])
		}
		if len(t) == 1 || t[1] == ' ' {
			return nil, syntax(no, RuleYAML, "%q followed by a space cannot start a value here; put the value in quotes", t[0])
		}
	}
	return r.plainValue(t, no, p)
}

// plainValue reads a plain scalar, which may continue on the following lines
// when they are indented more than p.
func (r *reader) plainValue(t string, no, p int) (*Node, *SyntaxError) {
	first, commented, err := r.plainLine(t, no)
	if err != nil {
		return nil, err
	}
	var text strings.Builder // built only when the scalar continues: appending to a string would be quadratic
	multiline := false
	for !commented {
		// Blank lines inside a plain scalar are line breaks in its value; they
		// belong to it only when a line that continues it follows.
		next, blanks := r.pos, 0
		for next < len(r.lines) && r.lines[next].blank() {
			next++
			blanks++
		}
		if next >= len(r.lines) {
			break
		}
		l := r.lines[next]
		if l.indent <= p || l.comment {
			break
		}
		ct := l.text()
		if strings.ContainsRune("[]{},&*!|>'\"%@`", rune(ct[0])) {
			return nil, syntax(l.no, RuleYAML, "a plain value that continues on a new line cannot continue with %q; put the value in quotes or use a block scalar (> or |)", ct[0])
		}
		var seg string
		if seg, commented, err = r.plainLine(ct, l.no); err != nil {
			return nil, err
		}
		if !multiline {
			text.WriteString(first)
		}
		if blanks == 0 {
			text.WriteByte(' ')
		} else {
			text.WriteString(strings.Repeat("\n", blanks))
		}
		text.WriteString(seg)
		multiline = true
		r.pos = next + 1
	}
	if multiline {
		return &Node{Kind: String, Line: no, Text: text.String()}, nil
	}
	kind, resolved, err := resolvePlain(first, no)
	if err != nil {
		return nil, err
	}
	return &Node{Kind: kind, Line: no, Text: resolved}, nil
}

// plainLine is the text of a line of a plain scalar: it ends at a comment (a #
// after a blank) and may not hold ": ", which would start a mapping. The tabs
// of the text are read as part of the value when they cannot be taken for
// anything else (see plainTabsFine), and those of a comment are the comment's.
func (r *reader) plainLine(t string, no int) (text string, commented bool, err *SyntaxError) {
	whole := t
	if i := commentStart(t); i >= 0 {
		t, commented = strings.TrimRight(t[:i], " \t"), true
		r.acceptTabs(no, whole, i, len(whole), true) // the text of the comment, not the blanks before it
	}
	if strings.Contains(t, ": ") || strings.HasSuffix(t, ":") {
		return "", false, syntax(no, RuleYAML, "a colon followed by a space inside a plain value would start a mapping; put the value in quotes")
	}
	if plainTabsFine(t) {
		r.acceptTabs(no, whole, 0, len(t), true)
	}
	return t, commented, nil
}

// commentStart is the index of the # that starts a comment in t: one that
// follows a space or a tab. -1 when there is none.
func commentStart(t string) int {
	for i := 1; i < len(t); i++ {
		if t[i] == '#' && (t[i-1] == ' ' || t[i-1] == '\t') {
			return i
		}
	}
	return -1
}

// plainTabsFine says whether the tabs in the text of a plain scalar are text:
// not at the start (that is the blank after an indicator), not next to a colon
// (a mapping), not after a dash or a question mark that starts the text (a
// sequence entry or an explicit key).
func plainTabsFine(t string) bool {
	if t == "" || t[0] == '\t' || strings.Contains(t, ":\t") {
		return false
	}
	return len(t) < 2 || t[1] != '\t' || !strings.ContainsRune("-?:", rune(t[0]))
}

var (
	yamlInt   = regexp.MustCompile(`^[-+]?[0-9]+$`)
	yamlFloat = regexp.MustCompile(`^[-+]?(?:\.[0-9]+|[0-9]+(?:\.[0-9]*)?)(?:[eE][-+]?[0-9]+)?$`)
	yamlNon   = regexp.MustCompile(`^(?:[-+]?\.(?:inf|Inf|INF)|\.(?:nan|NaN|NAN))$`)
	yamlBase  = regexp.MustCompile(`^(?:0x[0-9a-fA-F]+|0o[0-7]+)$`)
)

// resolvePlain resolves a plain scalar the way the YAML 1.2 core schema does.
func resolvePlain(text string, no int) (Kind, string, *SyntaxError) {
	switch {
	case text == "" || text == "~" || text == "null" || text == "Null" || text == "NULL":
		return Null, "", nil
	case text == "true" || text == "True" || text == "TRUE":
		return Bool, "true", nil
	case text == "false" || text == "False" || text == "FALSE":
		return Bool, "false", nil
	case yamlInt.MatchString(text):
		n, _ := new(big.Int).SetString(text, 10)
		if n.CmpAbs(big.NewInt(maxExactInt)) > 0 {
			return 0, "", syntax(no, RuleYAMLNumber, "the integer %s is larger than 2^53, the largest a double holds exactly; put it in quotes if it is not a number", text)
		}
		return Number, n.String(), nil
	case yamlBase.MatchString(text):
		return 0, "", syntax(no, RuleYAMLNumber, "%s is a hexadecimal or octal number; write it in decimal, or put it in quotes", text)
	case yamlNon.MatchString(text):
		return 0, "", syntax(no, RuleYAMLNumber, "%s is not a finite number; put it in quotes", text)
	case yamlFloat.MatchString(text):
		f, _ := strconv.ParseFloat(text, 64)
		if math.IsInf(f, 0) {
			return 0, "", syntax(no, RuleYAMLNumber, "%s is not a finite number; put it in quotes", text)
		}
		return Number, strconv.FormatFloat(f, 'g', -1, 64), nil
	}
	return String, text, nil
}

// quotedValue reads a quoted scalar that must end its line (apart from a comment).
func (r *reader) quotedValue(t string, no int) (*Node, *SyntaxError) {
	value, next, err := scanQuoted(t, 0, no)
	if err != nil {
		return nil, err
	}
	r.acceptTabs(no, t, 1, next-1, false)
	if err := r.endOfLine(t[next:], no); err != nil {
		return nil, err
	}
	return &Node{Kind: String, Line: no, Text: value}, nil
}

// endOfLine checks that only blanks and a comment (a # after a blank) follow;
// the tabs of the blanks before the comment and of the comment are accepted.
func (r *reader) endOfLine(rest string, no int) *SyntaxError {
	trimmed := strings.TrimLeft(rest, " \t")
	if trimmed == "" {
		return nil
	}
	if trimmed[0] == '#' && len(trimmed) < len(rest) {
		r.acceptTabs(no, rest, len(rest)-len(trimmed), len(rest), true) // the text of the comment
		return nil
	}
	return syntax(no, RuleYAML, "unexpected text %q after the end of the value", firstWord(trimmed))
}

func firstWord(s string) string {
	if i := strings.IndexByte(s, ' '); i >= 0 {
		s = s[:i]
	}
	return s
}

var escapes = map[byte]string{
	'0': "\x00", 'a': "\a", 'b': "\b", 't': "\t", 'n': "\n", 'v': "\v", 'f': "\f", 'r': "\r", 'e': "\x1b",
	' ': " ", '"': "\"", '/': "/", '\\': "\\", 'N': "\u0085", '_': "\u00a0", 'L': "\u2028", 'P': "\u2029",
}

// scanQuoted reads the quoted scalar whose opening quote is s[i], on one line,
// and returns its value and the index after the closing quote.
func scanQuoted(s string, i, no int) (string, int, *SyntaxError) {
	quote := s[i]
	var sb strings.Builder
	for j := i + 1; j < len(s); j++ {
		c := s[j]
		switch {
		case c == quote && quote == '\'' && j+1 < len(s) && s[j+1] == '\'':
			sb.WriteByte('\'')
			j++
		case c == quote:
			return sb.String(), j + 1, nil
		case c == '\\' && quote == '"':
			text, used, err := unescape(s[j+1:], no)
			if err != nil {
				return "", 0, err
			}
			sb.WriteString(text)
			j += used
		default:
			sb.WriteByte(c)
		}
	}
	return "", 0, syntax(no, RuleYAMLUnsupported, "the closing %c is not on this line; a quoted value must fit on one line (use a block scalar, > or |, for longer text)", quote)
}

// unescape decodes the escape that follows a backslash and returns the text and
// the number of bytes it used.
func unescape(s string, no int) (string, int, *SyntaxError) {
	if s == "" {
		return "", 0, syntax(no, RuleYAMLUnsupported, "a backslash at the end of a line continues a quoted value on the next line; a quoted value must fit on one line")
	}
	if text, ok := escapes[s[0]]; ok {
		return text, 1, nil
	}
	digits := map[byte]int{'x': 2, 'u': 4, 'U': 8}[s[0]]
	if digits == 0 {
		return "", 0, syntax(no, RuleYAMLEscape, "the escape \\%c is not a YAML escape; write \\\\ for a backslash", s[0])
	}
	code, ok := hexCode(s[1:], digits)
	if !ok {
		return "", 0, syntax(no, RuleYAMLEscape, "\\%c must be followed by %d hexadecimal digits", s[0], digits)
	}
	used := 1 + digits
	if code >= 0xd800 && code <= 0xdbff {
		// A high surrogate: JSON writes characters beyond U+FFFF as a pair.
		low, ok := uint32(0), strings.HasPrefix(s[used:], "\\u")
		if ok {
			low, ok = hexCode(s[used+2:], 4)
		}
		if !ok || low < 0xdc00 || low > 0xdfff {
			return "", 0, syntax(no, RuleYAMLEscape, "\\u%04X is half of a surrogate pair and is not followed by its other half", code)
		}
		code, used = 0x10000+(code-0xd800)<<10+(low-0xdc00), used+6
	}
	if code > 0x10ffff || (code >= 0xdc00 && code <= 0xdfff) {
		return "", 0, syntax(no, RuleYAMLEscape, "the escape does not name a character (U+%X)", code)
	}
	return string(rune(code)), used, nil
}

// hexCode reads exactly n hexadecimal digits from the start of s.
func hexCode(s string, n int) (uint32, bool) {
	if len(s) < n {
		return 0, false
	}
	code, err := strconv.ParseUint(s[:n], 16, 32)
	return uint32(code), err == nil
}
