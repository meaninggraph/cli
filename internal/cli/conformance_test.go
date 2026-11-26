package cli

import (
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/meaninggraph/cli/internal/memfs"
	"github.com/meaninggraph/cli/pkg/meaning"
)

// The conformance test. testdata/golden/node-conformance.json holds, for every
// case of the conformance suite that the reference checker of
// github.com/meaninggraph/core runs on itself (scripts/test-conformance.mjs:
// F format and words, R roles, K kinds, V value sets, X graphs of different
// formats, D derived links, S stored values), the bytes of each graph the case
// hands to the reference checker and the report it gave, recorded by
// scripts/differential/regenerate.sh at the core commit written in the file.
// This test never runs Node: it hands the same graphs to `meaninggraph check`
// and `meaninggraph links` and compares.
//
// The rule is the corpus test's: the same verdict, accept or refuse, in both
// formats. For an accepted graph the same warnings as the reference checker's
// notices earlier-format, earlier-role-name and retired-value (its fourth notice,
// unknown-value, is about stored values, which this checker does not read, and is
// left out); for a refused one only rules the reference checker reports as
// well (it goes on after the schema where this checker stops, so it may report
// more); for a graph whose links the reference checker lists, the same links.
// Where the verdicts differ, the case is in conformanceStricter with its reason,
// and a case that is listed there must differ.

type conformanceFinding struct {
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

type conformanceCall struct {
	Address string            `json:"address"`
	Files   map[string]string `json:"files"`
	Graphs  []struct {
		Address string            `json:"address"`
		Pin     string            `json:"pin"`
		Files   map[string]string `json:"files"`
	} `json:"graphs"`
	ReferenceOnly string `json:"reference_only"`
	Derive        bool   `json:"derive"`
	Reference     struct {
		Problems []conformanceFinding `json:"problems"`
		Notices  []conformanceFinding `json:"notices"`
		Links    []map[string]any     `json:"links"`
	} `json:"reference"`
}

type conformanceGolden struct {
	NodeChecker struct {
		Repository string `json:"repository"`
		Commit     string `json:"commit"`
	} `json:"node_checker"`
	Cases map[string]struct {
		Title string            `json:"title"`
		Calls []conformanceCall `json:"calls"`
	} `json:"cases"`
}

// conformanceStricter lists the cases where this checker refuses a graph that the
// reference checker accepts, each with the reason. They are the differences the README lists.
var conformanceStricter = map[string]string{
	"X-10": "this checker validates every graph it is given to read against its schema, and the reference checker does not (README, a supplied graph that is not itself valid): the supplied graph holds kind: attribute in a meaning/draft-2 file",
	"D-21": "a model whose key is not a list (key = 5, key = \"Id\") is refused here, where the reference checker reads it as a model with no key (README, a model whose entity key is a string)",
}

// conformanceNotApplicable lists the cases that hand no graph to the checker
// that is compared here, with the reason.
var conformanceNotApplicable = map[string]string{
	"S-":         "stored values: this checker reads no data (README, what it does not check)",
	"interface-": "the interface of the reference checker's functions, which this checker does not have",
	"effective":  "effectiveValues of the reference checker, which this checker does not have",
}

func notApplicable(key string) (string, bool) {
	for prefix, why := range conformanceNotApplicable {
		if strings.HasPrefix(key, prefix) {
			return why, true
		}
	}
	return "", false
}

func TestConformanceAgreesWithTheReferenceChecker(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	var golden conformanceGolden
	readJSON(t, filepath.Join(root, "testdata", "golden", "node-conformance.json"), &golden)
	for _, format := range []string{meaning.Draft1, meaning.Draft2} {
		if golden.NodeChecker.Repository != "https://github.com/meaninggraph/core" || golden.NodeChecker.Commit != meaning.SchemaCommitFor(format) {
			t.Fatalf("the conformance cases were recorded at %s %s, but the embedded schema of %s is from %s", golden.NodeChecker.Repository, golden.NodeChecker.Commit, format, meaning.SchemaCommitFor(format))
		}
	}
	if len(golden.Cases) < 120 {
		t.Fatalf("%d cases recorded; the suite of the reference checker has more", len(golden.Cases))
	}
	replayed := 0
	differs := map[string]bool{}
	for _, key := range slices.Sorted(maps.Keys(golden.Cases)) {
		c := golden.Cases[key]
		if len(c.Calls) == 0 {
			if _, ok := notApplicable(key); !ok {
				t.Errorf("%s (%s): no graph recorded, and the case is not listed as one that hands none", key, c.Title)
			}
			continue
		}
		for n, call := range c.Calls {
			if call.ReferenceOnly != "" {
				continue
			}
			replayed++
			t.Run(fmt.Sprintf("%s.%d", key, n), func(t *testing.T) {
				differs[key] = conformanceReplay(t, key, call) || differs[key]
			})
		}
	}
	if replayed < 150 {
		t.Errorf("%d graphs replayed; the suite of the reference checker has more", replayed)
	}
	for key, reason := range conformanceStricter {
		if !differs[key] {
			t.Errorf("%s is listed as a case where this checker is stricter (%s), but no graph of it differs", key, reason)
		}
	}
}

// conformanceReplay checks one graph of a case, named by the case and the
// position of the graph in it.
func conformanceReplay(t *testing.T, key string, call conformanceCall) (differs bool) {
	t.Helper()
	files := map[string]string{}
	for file, content := range call.Files {
		files["/g/"+file] = content
	}
	args := []string{"check", "--format", "json", "/g"}
	if call.Address != "" {
		args = append(args, "--address", call.Address)
	}
	for i, graph := range call.Graphs {
		for file, content := range graph.Files {
			files[fmt.Sprintf("/dep%d/%s", i, file)] = content
		}
		args = append(args, "--graph", fmt.Sprintf("%s=/dep%d", graph.Address, i))
	}
	got := executeIn(memfs.New(files), args...)
	var report struct {
		Graphs []struct {
			Findings []meaning.Finding `json:"findings"`
		} `json:"graphs"`
	}
	if err := json.Unmarshal([]byte(got.stdout), &report); err != nil || len(report.Graphs) == 0 {
		t.Fatalf("code %d: %v\n%s%s", got.code, err, got.stdout, got.stderr)
	}
	var errorRules, warningRules []string
	for _, g := range report.Graphs {
		for _, f := range g.Findings {
			switch f.Severity {
			case meaning.Error:
				errorRules = append(errorRules, f.Rule)
			case meaning.Warning:
				if slices.Contains([]string{meaning.RuleEarlierFormat, meaning.RuleEarlierRoleName, meaning.RuleRetiredValue}, f.Rule) {
					warningRules = append(warningRules, f.Rule)
				}
			}
		}
	}
	refCount := len(call.Reference.Problems)
	reference := map[bool]string{true: "accept", false: "refuse"}[refCount == 0]
	verdict := map[int]string{ExitClean: "accept", ExitFindings: "refuse"}[got.code]
	_, listed := conformanceStricter[key]
	switch {
	case verdict == "":
		t.Fatalf("exit code %d: %s", got.code, got.stderr)
	case verdict != reference && !listed:
		t.Errorf("this checker says %s, the reference checker %s: %v\n%s", verdict, reference, call.Reference.Problems, got.stdout)
	case verdict == "accept" && reference == "refuse":
		t.Errorf("this checker accepts what the reference checker refuses: %v", call.Reference.Problems)
	}
	var referenceRules []string
	for _, p := range call.Reference.Problems {
		referenceRules = append(referenceRules, p.Rule)
	}
	if verdict == "refuse" && reference == "refuse" && !listed {
		for _, rule := range errorRules {
			// The rules of the YAML subset are this checker's own, and so is the rule
			// that a graph it is given to read must itself be valid (README): the
			// reference checker reads such a graph unchecked and finds the fault
			// it makes with another rule.
			own := strings.HasPrefix(rule, "yaml") || (rule == meaning.RuleUnresolved && len(call.Graphs) > 0)
			if !own && !slices.Contains(referenceRules, rule) {
				t.Errorf("this checker reports %s, which the reference checker does not (it reports %v)", rule, referenceRules)
			}
		}
	}
	if verdict == "accept" && reference == "accept" {
		var want []string
		for _, n := range call.Reference.Notices {
			if n.Rule != "unknown-value" { // a stored value: this checker reads none
				want = append(want, n.Rule)
			}
		}
		slices.Sort(want)
		slices.Sort(warningRules)
		if !slices.Equal(want, warningRules) {
			t.Errorf("warnings %v, the reference checker's notices %v", warningRules, want)
		}
	}
	if call.Derive {
		conformanceLinks(t, files, args, call, listed)
	}
	return verdict != reference
}

// conformanceLinks compares the links of a graph: those of `links` and those the
// reference checker listed (none, when it found a problem).
func conformanceLinks(t *testing.T, files map[string]string, checkArgs []string, call conformanceCall, listed bool) {
	t.Helper()
	args := []string{"links"}
	for i := 1; i < len(checkArgs); i++ {
		if checkArgs[i] == "--format" {
			i++
			continue
		}
		args = append(args, checkArgs[i])
	}
	got := executeIn(memfs.New(files), args...)
	var report struct {
		Graphs []struct {
			Links []map[string]any `json:"links"`
		} `json:"graphs"`
	}
	if err := json.Unmarshal([]byte(got.stdout), &report); err != nil || len(report.Graphs) != 1 {
		t.Fatalf("links: code %d: %v\n%s%s", got.code, err, got.stdout, got.stderr)
	}
	want := call.Reference.Links
	if len(call.Reference.Problems) > 0 || listed {
		want = nil // no links are defined for a graph with a problem (null), and this checker refuses a graph of a listed case
	}
	if !reflect.DeepEqual(report.Graphs[0].Links, want) {
		t.Errorf("links\n got %v\nwant %v", report.Graphs[0].Links, want)
	}
}
