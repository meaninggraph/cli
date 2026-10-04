package meaning

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
)

// The reference parser's values, recorded by scripts/differential/values.mjs in
// testdata/golden/node-values.json: for every YAML file of the corpus and for a
// generated matrix of documents (scalars in every position, block scalars with
// every ending of the file, tabs, byte order marks, line ends). ParseYAML must
// read the same data from the same text, field for field, or refuse the text
// with one of the rules the group of the document may be refused by. The test
// needs no Node; the golden file is what the reference parser said.

type goldenEntry struct {
	Group   string `json:"group"`
	Name    string `json:"name"`
	Text    string `json:"text"`
	Value   any    `json:"value"`
	Refused string `json:"refused"`
}

type goldenValues struct {
	Entries []goldenEntry `json:"entries"`
}

// refusableBy says, per group of the matrix, which rules may refuse a document
// that the reference parser reads (the documented differences). A group that is
// not listed must be read by ParseYAML whenever the reference parser reads it.
// The tab groups and the block scalar groups have narrower rules, below.
var refusableBy = map[string][]string{
	"scalar":          {RuleYAMLNumber, RuleYAMLKey, RuleYAMLTag, RuleYAMLAnchor, RuleYAMLUnsupported, RuleYAMLCharacter, RuleYAMLEscape},
	"structure":       {RuleYAMLDirective, RuleYAMLDocuments, RuleYAMLAnchor, RuleYAMLTag, RuleYAMLUnsupported, RuleYAMLEncoding, RuleYAMLCharacter, RuleYAMLNumber},
	"block-keep":      {RuleYAMLUnsupported},
	"block-indicator": {RuleYAMLUnsupported},
	"line-ends":       {RuleYAMLLineEnding},
	"bom":             {RuleYAMLEncoding, RuleYAMLUnsupported, RuleYAMLCharacter},
	// A key in a flow mapping has no limit for the reference parser, and a key of
	// many bytes but few UTF-16 units is over the limit here and not there.
	"key-limit": {RuleYAMLKey},
	// A tab in the text of a comment on a key, dash or --- line is read; what the
	// CLI refuses of these documents is a comment line between the key and a
	// scalar, or a block scalar that starts on the next line, whatever the tab.
	"tab-in-key-comment": {RuleYAMLUnsupported},
	// A tab at the end of such a comment, or before its #, is refused by the CLI
	// and read by the reference parser (stricter), or refused for the other reasons.
	"tab-at-end-of-key-comment": {RuleYAMLTab, RuleYAMLUnsupported},
}

// refusalAllowed says whether ParseYAML may refuse an entry that the reference
// parser reads, with this error. text is the document.
func refusalAllowed(entry goldenEntry, text string, e *SyntaxError) bool {
	switch {
	case entry.Group == "tab read":
		return false // every tab of these documents is in a position that is read
	case entry.Group == "tab refused":
		return e.Rule == RuleYAMLTab || (e.Rule == RuleYAMLUnsupported && strings.Contains(e.Message, "comment line between"))
	case entry.Group == "comment-lines":
		// A comment line between a key and the value below it is refused whole, so is
		// a comment inside [ ] or { } and a blank line of too many spaces in a block
		// scalar, and a tab outside the places where one is read.
		return (e.Rule == RuleYAMLUnsupported && (strings.Contains(e.Message, "comment line between") || strings.Contains(e.Message, "comments inside [ ]") || strings.Contains(e.Message, "more spaces than the text is indented by"))) ||
			(e.Rule == RuleYAMLTab && strings.Contains(text, "\t"))
	case entry.Group == "comment-lines-collection":
		// A comment line before a block mapping or sequence is read at every column,
		// with and without a space after the #; only a tab is refused.
		return e.Rule == RuleYAMLTab && strings.Contains(text, "\t") || (e.Rule == RuleYAMLUnsupported && strings.Contains(e.Message, "more spaces than the text is indented by"))
	case entry.Group == "corpus":
		// The accepted items of the corpus are read in full; the others may be
		// refused by one of the rules (they are the refusals and the stricter items).
		return !strings.HasPrefix(strings.TrimPrefix(entry.Name, "testdata/corpus/"), "accept-")
	case entry.Group == "block-eof":
		// A blank line of more spaces than the text is indented by is text to the
		// reference parser (also the last line of the file); a blank line with a
		// tab after the indentation is text too.
		return (e.Rule == RuleYAMLUnsupported && strings.Contains(e.Message, "more spaces than the text is indented by")) ||
			(e.Rule == RuleYAMLTab && strings.Contains(entry.Name, "ending indent_tab"))
	}
	return slices.Contains(refusableBy[entry.Group], e.Rule)
}

// tally is what the test counts, and pins: a golden file that is emptied or
// shrunk, or a reader that refuses more, changes it.
type tally struct {
	Documents          int            // in the golden file
	ReadByBoth         int            // read by the reference parser and by ParseYAML, compared by value
	RefusedByReference int            // refused by the reference parser (ParseYAML refuses them too)
	RefusedByRule      map[string]int // read by the reference parser and refused by ParseYAML, by rule
}

// wantTally is the count the golden file and the reader give today.
var wantTally = tally{
	Documents:          14074,
	ReadByBoth:         7163,
	RefusedByReference: 1511,
	RefusedByRule: map[string]int{
		"yaml": 2, "yaml-anchor": 3, "yaml-character": 12, "yaml-directive": 3, "yaml-documents": 2, "yaml-encoding": 7,
		"yaml-key": 365, "yaml-limit": 1, "yaml-number": 89, "yaml-tab": 1923, "yaml-tag": 3, "yaml-unsupported": 2990,
	},
}

func TestParseYAMLReadsWhatTheReferenceParserReads(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../testdata/golden/node-values.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden goldenValues
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	problems := map[string][]string{} // by group and kind: a few examples of each are shown
	problem := func(group, kind, format string, args ...any) {
		key := group + ": " + kind
		problems[key] = append(problems[key], fmt.Sprintf(format, args...))
	}
	groups := map[string][2]int{}
	got := tally{RefusedByRule: map[string]int{}}
	for _, entry := range golden.Entries {
		got.Documents++
		text := entry.Text
		if entry.Group == "corpus" {
			raw, err := os.ReadFile(filepath.Join("../..", entry.Name))
			if err != nil {
				t.Fatal(err)
			}
			text = string(raw)
		}
		counts := groups[entry.Group]
		node, syntaxErr := parseSyntax([]byte(text))
		switch {
		case entry.Refused != "":
			// The reference parser refuses it: ParseYAML must too.
			counts[1]++
			got.RefusedByReference++
			if syntaxErr == nil {
				problem(entry.Group, "ParseYAML reads what the reference parser refuses", "%s (%s)", entry.Name, entry.Refused)
			}
		case syntaxErr != nil:
			counts[1]++
			got.RefusedByRule[syntaxErr.Rule]++
			if !refusalAllowed(entry, text, syntaxErr) {
				problem(entry.Group, "ParseYAML refuses ("+syntaxErr.Rule+") what the reference parser reads", "%s: %s", entry.Name, syntaxErr.Message)
			}
		default:
			counts[0]++
			got.ReadByBoth++
			if entry.Group == "tab refused" {
				problem(entry.Group, "ParseYAML reads a document with a tab that is refused", "%s", entry.Name)
			}
			var read any
			encoded, _ := json.Marshal(node.Value())
			_ = json.Unmarshal(encoded, &read)
			if !sameData(read, entry.Value) {
				want, _ := json.Marshal(entry.Value)
				problem(entry.Group, "ParseYAML reads other data than the reference parser", "%s: ParseYAML %s, the reference parser %s", entry.Name, encoded, want)
			}
		}
		groups[entry.Group] = counts
	}
	if !reflect.DeepEqual(got, wantTally) {
		t.Errorf("the golden file and the reader give\n%#v\nwant\n%#v\n(a smaller count means the golden file lost documents or the reader refuses more; change wantTally on purpose)", got, wantTally)
	}
	keys := make([]string, 0, len(problems))
	for key := range problems {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		list := problems[key]
		t.Errorf("%s (%d documents), for example:\n  %s", key, len(list), strings.Join(list[:min(len(list), 4)], "\n  "))
	}
	if os.Getenv("VALUES_REPORT") != "" {
		names := make([]string, 0, len(groups))
		for name := range groups {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			t.Logf("%-28s read by both %5d  refused by ParseYAML or the reference %5d", name, groups[name][0], groups[name][1])
		}
	}
}

// sameData compares decoded JSON values; numbers are compared as numbers.
func sameData(a, b any) bool {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for key, value := range x {
			other, present := y[key]
			if !present || !sameData(value, other) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !sameData(x[i], y[i]) {
				return false
			}
		}
		return true
	}
	return a == b
}
