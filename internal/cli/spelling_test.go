package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/meaninggraph/cli/pkg/meaning"
)

// testdata/spelling holds the cases of ModelSpec's renamed words (record, field
// and record = for entity, property and entity =). The Node reference checker
// that testdata/corpus is compared with reads only the earlier spelling, so
// these cases are not in that corpus and have no recorded verdict of it; they
// are checked against what ModelSpec's own reference CLI says of the models.
//
// A case is a directory with an item.json and one directory per variant, each
// a graph of one meaning file and its model. A case with an "earlier" and a
// "current" variant says the same thing twice: "current" is what
// `modelspec rewrite --write` makes of the model of "earlier", and the two
// must give the same findings, apart from the notice about the earlier
// spelling.

type spellingItem struct {
	Description string   `json:"description"`
	Expect      string   `json:"expect"`
	Rule        string   `json:"rule"`
	Message     string   `json:"message"`
	Warns       []string `json:"warns"`
}

type spellingReport struct {
	Graphs []struct {
		Files    int
		Concepts int
		Errors   int
		Warnings int
		Findings []meaning.Finding
	}
}

func checkSpelling(t *testing.T, dir string) (int, spellingReport) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run([]string{"check", "--format", "json", dir}, Env{Stdout: &stdout, Stderr: &stderr, FS: meaning.OSFS{}, Abs: filepath.Abs, SelfUpdate: offline})
	var report spellingReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("%s: %v\n%s%s", dir, err, stdout.String(), stderr.String())
	}
	return code, report
}

func TestSpellingCases(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "testdata", "spelling")
	cases, err := os.ReadDir(root)
	if err != nil || len(cases) == 0 {
		t.Fatalf("no cases: %v", err)
	}
	for _, c := range cases {
		t.Run(c.Name(), func(t *testing.T) {
			t.Parallel()
			dir := filepath.Join(root, c.Name())
			var item spellingItem
			readJSON(t, filepath.Join(dir, "item.json"), &item)
			variants, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			seen := map[string][]meaning.Finding{}
			for _, v := range variants {
				if !v.IsDir() {
					continue
				}
				code, report := checkSpelling(t, filepath.Join(dir, v.Name()))
				findings := report.Graphs[0].Findings
				wantCode := map[string]int{"accept": ExitClean, "refuse": ExitFindings}[item.Expect]
				if code != wantCode {
					t.Errorf("%s: exit code %d, the item expects %s: %+v", v.Name(), code, item.Expect, findings)
				}
				var notices, rules, messages []string
				var rest []meaning.Finding
				for _, f := range findings {
					switch {
					case f.Rule == meaning.RuleEarlierSpelling:
						notices = append(notices, f.Message)
						if f.Severity != meaning.Warning || !strings.HasSuffix(f.File, "shop.modelspec.hcl") || f.Line < 1 || !strings.Contains(f.Message, "modelspec rewrite --write") {
							t.Errorf("%s: the notice is not a warning about the model that names the command: %+v", v.Name(), f)
						}
					case f.Severity == meaning.Error:
						rules, messages = append(rules, f.Rule), append(messages, f.Message)
						fallthrough
					default:
						f.File = strings.TrimPrefix(f.File, filepath.Join(dir, v.Name())+string(filepath.Separator))
						rest = append(rest, f)
					}
				}
				if want := slices.Contains(item.Warns, v.Name()); (len(notices) == 1) != want || len(notices) > 1 {
					t.Errorf("%s: %d notices, but warns = %v", v.Name(), len(notices), item.Warns)
				}
				if item.Rule != "" && !slices.Contains(rules, item.Rule) {
					t.Errorf("%s: no error with rule %s: %+v", v.Name(), item.Rule, findings)
				}
				if item.Message != "" && !strings.Contains(strings.Join(messages, "\n"), item.Message) {
					t.Errorf("%s: no error message with %q: %v", v.Name(), item.Message, messages)
				}
				if item.Expect == "accept" && len(rules) > 0 {
					t.Errorf("%s: errors %v", v.Name(), messages)
				}
				seen[v.Name()] = rest
			}
			if earlier, ok := seen["earlier"]; ok {
				if current, ok := seen["current"]; ok && !slices.EqualFunc(earlier, current, func(a, b meaning.Finding) bool { return a == b }) {
					t.Errorf("the earlier spelling and its rewrite do not give the same findings:\nearlier %+v\ncurrent %+v", earlier, current)
				}
			}
		})
	}
}

// A check reads a model file once however many meaning files list it, and
// reports its earlier spelling once.
func TestSpellingNoticeIsOncePerModelFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	model := "entity \"Customer\" {\n  key = [\"CustomerId\"]\n  property \"CustomerId\" { type = \"int\" }\n}\n"
	meaningFile := func(id string) string {
		return "format: meaning/draft-1\nid: " + id + "\nname: N\ndescription: d\nmodels: {shop: shop.modelspec.hcl}\nconcepts:\n" +
			"  - {id: customer-" + id + ", kind: entity, labels: {en: customer-" + id + "}, description: d, bindings: [{model: 'modelspec:///shop.Customer', role: entity}]}\n"
	}
	for name, content := range map[string]string{"shop.modelspec.hcl": model, "a.meaning.yaml": meaningFile("a"), "b.meaning.yaml": meaningFile("b")} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"check", dir}, Env{Stdout: &stdout, Stderr: &stderr, FS: meaning.OSFS{}, Abs: filepath.Abs, SelfUpdate: offline})
	out := stdout.String()
	if code != ExitClean || strings.Count(out, "[deprecated-spelling]") != 1 || !strings.Contains(out, ", 1 warning\n") {
		t.Fatalf("code %d\n%s%s", code, out, stderr.String())
	}
}
