package meaning

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
}

// tabRefusals lists, per tab group, the documents (by name) that ParseYAML
// refuses although the reference parser reads them: the tabs that are not
// accepted. The groups that are not listed are read in full.
var tabRefusals = map[string][]string{
	"tab after colon":      {`"a:\t1\n"`, `"a:\tx\n"`, `"a: \t1\n"`, `"a:\t\"q\"\n"`, `"a:\t[x]\n"`, `"a:\t|\n  x\n"`, `"a:\t# c\n"`, `"a:\t\n  b: 1\n"`},
	"tab after dash":       {`"-\tx\n"`, `"- \tx\n"`, `"-\t\"q\"\n"`, `"-\ta: 1\n"`, `"-\t\n  x\n"`},
	"tab in flow":          {`"a: [x,\ty]\n"`, `"a: [\tx]\n"`, `"a: {k:\tv}\n"`, `"a: {k\t: v}\n"`, `"a: [x\t]\n"`, `"a: {k: v,\tl: w}\n"`, `"a: [x,\n\ty]\n"`, `"a: [\t]\n"`, `"a: {\t}\n"`},
	"tab trailing":         {`"\"k\"\t: 1\n"`, `"k\t: 1\n"`},
	"tab in plain scalars": {`"k\tk: 1\n"`, `"k\tk: v\tv\n"`, `"a: [x\ty]\n"`, `"a: {k: x\ty}\n"`, `"a: {k\tk: v}\n"`, `"a: x:\ty\n"`, `"a: -\tx\n"`, `"a: ?\tx\n"`, `"a: x\t: y\n"`},
	"tab in block text":    {`"a: |\n  x\n  \t\n  y\n"`, `"a: |\n  \t\n  x\n"`, `"a: >\n  x\n  \t\n  y\n"`, `"a: |\n  x\n  \t\n"`, `"a: |\n  x\n  \t"`, `"a: |-\n  x\n  \t\n"`},
	"tab indentation":      nil,
}

// refusalAllowed says whether ParseYAML may refuse an entry that the reference
// parser reads, with this error.
func refusalAllowed(entry goldenEntry, e *SyntaxError) bool {
	switch {
	case entry.Group == "tab after a block scalar":
		// A tab on a blank line or before a comment right after a block scalar is
		// refused, whatever the reference parser does there.
		return e.Rule == RuleYAMLTab
	case strings.HasPrefix(entry.Group, "tab "):
		return e.Rule == RuleYAMLTab && slices.Contains(tabRefusals[entry.Group], entry.Name)
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
	for _, entry := range golden.Entries {
		text := entry.Text
		if entry.Group == "corpus" {
			raw, err := os.ReadFile(filepath.Join("../..", entry.Name))
			if err != nil {
				t.Fatal(err)
			}
			text = string(raw)
		}
		counts := groups[entry.Group]
		node, syntaxErr := ParseYAML([]byte(text))
		switch {
		case entry.Refused != "":
			// The reference parser refuses it: ParseYAML must too.
			counts[1]++
			if syntaxErr == nil {
				problem(entry.Group, "ParseYAML reads what the reference parser refuses", "%s (%s)", entry.Name, entry.Refused)
			}
		case syntaxErr != nil:
			counts[1]++
			if !refusalAllowed(entry, syntaxErr) {
				problem(entry.Group, "ParseYAML refuses ("+syntaxErr.Rule+") what the reference parser reads", "%s: %s", entry.Name, syntaxErr.Message)
			}
		default:
			counts[0]++
			var got any
			encoded, _ := json.Marshal(node.Value())
			_ = json.Unmarshal(encoded, &got)
			if !sameData(got, entry.Value) {
				want, _ := json.Marshal(entry.Value)
				problem(entry.Group, "ParseYAML reads other data than the reference parser", "%s: ParseYAML %s, the reference parser %s", entry.Name, encoded, want)
			}
		}
		groups[entry.Group] = counts
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
