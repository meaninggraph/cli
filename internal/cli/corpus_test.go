package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meaninggraph/cli/pkg/meaning"
)

// The differential test. testdata/corpus holds one directory per item (a graph
// with, for a refusal, one defect), testdata/golden/node-verdicts.json holds
// what the Node reference checker of github.com/meaninggraph/core said about each
// item, recorded by scripts/differential/regenerate.sh at the core commit
// written in the file. This test never runs Node: it runs `meaninggraph check`
// over every item and compares its verdict with the recorded one.
//
// The rule: the CLI never accepts what the reference checker refuses. It may
// refuse what the reference checker accepts only where the item says why, in
// its "stricter" field; the README lists those items.

type corpusItem struct {
	Description string            `json:"description"`
	Expect      string            `json:"expect"`
	Rule        string            `json:"rule"`
	Stricter    string            `json:"stricter"`
	Address     string            `json:"address"`
	Profile     string            `json:"profile"`
	Check       string            `json:"check"`
	Graphs      map[string]string `json:"graphs"`
}

type goldenFile struct {
	NodeChecker struct {
		Repository string `json:"repository"`
		Commit     string `json:"commit"`
	} `json:"node_checker"`
	Items map[string]struct {
		Verdict  string   `json:"verdict"`
		Crashed  bool     `json:"crashed"`
		Problems []string `json:"problems"`
	} `json:"items"`
}

func readJSON(t *testing.T, path string, into any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, into); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func TestCorpusAgreesWithTheReferenceChecker(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	var golden goldenFile
	readJSON(t, filepath.Join(root, "testdata", "golden", "node-verdicts.json"), &golden)
	if golden.NodeChecker.Repository != "https://github.com/meaninggraph/core" || golden.NodeChecker.Commit != meaning.SchemaCommit() {
		t.Fatalf("the golden verdicts were recorded at %s %s, but the embedded schema is from %s", golden.NodeChecker.Repository, golden.NodeChecker.Commit, meaning.SchemaCommit())
	}
	entries, err := os.ReadDir(filepath.Join(root, "testdata", "corpus"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(golden.Items) || len(entries) < 100 {
		t.Fatalf("the corpus has %d items, the golden file %d", len(entries), len(golden.Items))
	}
	for _, entry := range entries {
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := filepath.Join(root, "testdata", "corpus", name)
			var item corpusItem
			readJSON(t, filepath.Join(dir, "item.json"), &item)
			node, ok := golden.Items[name]
			if !ok {
				t.Fatalf("no recorded verdict of the reference checker; run scripts/differential/regenerate.sh")
			}
			args := []string{"check", "--format", "json", filepath.Join(dir, item.Check)}
			for address, path := range item.Graphs {
				args = append(args, "--graph", address+"="+filepath.Join(root, path))
			}
			if item.Address != "" {
				args = append(args, "--address", item.Address)
			}
			if item.Profile != "" {
				args = append(args, "--profile", item.Profile)
			}
			var stdout, stderr bytes.Buffer
			code := Run(args, Env{Stdout: &stdout, Stderr: &stderr, FS: meaning.OSFS{}, Abs: filepath.Abs, SelfUpdate: offline})
			if code != ExitClean && code != ExitFindings {
				t.Fatalf("exit code %d: %s", code, stderr.String())
			}
			cli := map[int]string{ExitClean: "accept", ExitFindings: "refuse"}[code]
			if cli != item.Expect {
				t.Errorf("the CLI says %s, the item expects %s\n%s", cli, item.Expect, stdout.String())
			}
			if cli == "accept" && node.Verdict == "refuse" {
				t.Errorf("the CLI accepts what the reference checker refuses: %v", node.Problems)
			}
			if cli == "refuse" && node.Verdict == "accept" && item.Stricter == "" {
				t.Errorf("the CLI refuses what the reference checker accepts, and the item gives no reason")
			}
			if (cli == node.Verdict) == (item.Stricter != "") {
				t.Errorf("verdicts %s (CLI) and %s (reference), stricter = %q: a reason belongs on a difference only", cli, node.Verdict, item.Stricter)
			}
			if item.Rule != "" && !strings.Contains(findingRules(t, stdout.Bytes()), " "+item.Rule+" ") {
				t.Errorf("no error finding with rule %s:\n%s", item.Rule, stdout.String())
			}
		})
	}
}

// findingRules lists the rules of the error findings of a JSON report, space-separated.
func findingRules(t *testing.T, data []byte) string {
	t.Helper()
	var report struct {
		Graphs []struct{ Findings []meaning.Finding }
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	rules := " "
	for _, g := range report.Graphs {
		for _, f := range g.Findings {
			if f.Severity == meaning.Error {
				rules += f.Rule + " "
			}
		}
	}
	return rules
}
