package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"testing"

	"github.com/strongo/cli-helpers/selfupdate"

	"github.com/meaninggraph/cli/internal/memfs"
	"github.com/meaninggraph/cli/pkg/meaning"
)

const head = "format: meaning/draft-1\nid: demo\nname: Demo\ndescription: d\n"

func concept(id, kind, rest string) string {
	return "  - {id: " + id + ", kind: " + kind + ", labels: {en: " + id + "}, description: d" + rest + "}\n"
}

func file(concepts ...string) string { return head + "concepts:\n" + strings.Join(concepts, "") }

const pin = "cb97dbcd9e951b00e7d46cb2e0c4e120c24c8db7"

type result struct {
	code           int
	stdout, stderr string
}

func offline() selfupdate.Config { return selfUpdateConfig() }

func execute(files map[string]string, args ...string) result {
	return executeIn(memfs.New(files), args...)
}

func executeIn(fsys meaning.FS, args ...string) result {
	var stdout, stderr bytes.Buffer
	code := Run(args, Env{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr, FS: fsys, SelfUpdate: offline, Interactive: func() bool { return false }})
	return result{code, stdout.String(), stderr.String()}
}

func TestCheckCleanDirectory(t *testing.T) {
	t.Parallel()
	got := execute(map[string]string{"/g/a.meaning.yaml": file(concept("a", "entity", ""), concept("b", "entity", ", extends: a"))}, "check", "/g")
	if got.code != ExitClean || got.stdout != "ok: /g: 2 concepts, 1 file\n" || got.stderr != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestCheckDefaultsToTheCurrentDirectory(t *testing.T) {
	t.Parallel()
	got := execute(map[string]string{"a.meaning.yaml": file(concept("a", "entity", ""))}, "check")
	if got.code != ExitClean || got.stdout != "ok: .: 1 concept, 1 file\n" {
		t.Fatalf("got %+v", got)
	}
}

func TestCheckReportsFindingsInOrder(t *testing.T) {
	t.Parallel()
	got := execute(map[string]string{
		"/g/a.meaning.yaml":     file(concept("a", "attribute", ", of: nowhere"), concept("b", "entity", ", extends: gone")),
		"/g/b.meaning.yaml":     "a: b\n  c: d\n",
		"/g/sub/c.meaning.yaml": file(concept("c", "entity", "")),
	}, "check", "/g")
	want := "/g/a.meaning.yaml:6: error: concept a of: concept nowhere is not declared in this repository [unknown-concept]\n" +
		"/g/a.meaning.yaml:7: error: concept b extends: concept gone is not declared in this repository [unknown-concept]\n" +
		"/g/b.meaning.yaml:2: error: mapping values are not allowed in this context [yaml]\n" +
		"/g/sub/c.meaning.yaml: warning: not read: a graph is the meaning files directly in its directory, so move this file there, or check its directory [subdirectory-meaning-file]\n" +
		"failed: /g: 3 errors, 1 warning, 2 concepts, 2 files\n"
	if got.code != ExitFindings || got.stdout != want || got.stderr != "" {
		t.Fatalf("got code %d\n%s\nwant\n%s", got.code, got.stdout, want)
	}
}

func TestCheckWarningsDoNotFail(t *testing.T) {
	t.Parallel()
	got := execute(map[string]string{
		"/g/a.meaning.yaml":     file(concept("a", "entity", "")),
		"/g/sub/c.meaning.yaml": file(concept("c", "entity", "")),
	}, "check", "/g")
	if got.code != ExitClean || !strings.Contains(got.stdout, "warning") || !strings.HasSuffix(got.stdout, "ok: /g: 1 concept, 1 file\n") {
		t.Fatalf("got %+v", got)
	}
}

func TestCheckNamesFilesAndDirectoriesAsSeparateGraphs(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"/d1/a.meaning.yaml": file(concept("a", "entity", "")),
		"/d2/b.meaning.yaml": file(concept("a", "entity", "")),
		"/x/f1.meaning.yaml": file(concept("f", "entity", "")),
		"/x/f2.meaning.yaml": file(concept("g", "entity", ", extends: f")),
	}
	got := execute(files, "check", "/x/f2.meaning.yaml", "/d2", "/x/f1.meaning.yaml", "/d1/", "/d2", "/d1")
	want := "ok: /d1: 1 concept, 1 file\nok: /d2: 1 concept, 1 file\nok: /x/f1.meaning.yaml /x/f2.meaning.yaml: 2 concepts, 2 files\n"
	if got.code != ExitClean || got.stdout != want {
		t.Fatalf("got %+v\nwant %q", got, want)
	}
	// One file alone cannot see the concept of its sibling.
	alone := execute(files, "check", "/x/f2.meaning.yaml")
	if alone.code != ExitFindings || !strings.Contains(alone.stdout, "concept f is not declared") {
		t.Fatalf("got %+v", alone)
	}
}

func TestCheckJSON(t *testing.T) {
	t.Parallel()
	got := execute(map[string]string{
		"/ok/a.meaning.yaml":  file(concept("a", "entity", "")),
		"/bad/a.meaning.yaml": file(concept("a", "entity", ", of: nowhere")),
	}, "check", "--format", "json", "/bad", "/ok")
	if got.code != ExitFindings {
		t.Fatalf("code = %d", got.code)
	}
	var report struct {
		Tool, Version string
		Schema        struct {
			Format     string
			CoreCommit string `json:"core_commit"`
		}
		OK     bool
		Graphs []struct {
			Paths    []string
			Files    int
			Concepts int
			Errors   int
			Findings []meaning.Finding
		}
	}
	if err := json.Unmarshal([]byte(got.stdout), &report); err != nil {
		t.Fatalf("%v\n%s", err, got.stdout)
	}
	if report.Tool != "meaninggraph" || report.Version == "" || report.Schema.Format != "meaning/draft-1" || report.Schema.CoreCommit != meaning.SchemaCommit() || report.OK {
		t.Fatalf("report = %+v", report)
	}
	if len(report.Graphs) != 2 || report.Graphs[0].Paths[0] != "/bad" || report.Graphs[0].Errors != 1 || len(report.Graphs[0].Findings) != 1 {
		t.Fatalf("graphs = %+v", report.Graphs)
	}
	f := report.Graphs[0].Findings[0]
	if f.Rule != meaning.RuleUnknownConcept || f.Severity != meaning.Error || f.Line != 6 || f.File != "/bad/a.meaning.yaml" {
		t.Fatalf("finding = %+v", f)
	}
	if !strings.Contains(got.stdout, `"findings": []`) {
		t.Fatalf("a clean graph lists no findings as []:\n%s", got.stdout)
	}
}

func TestCheckResolvesOtherGraphsFromSuppliedDirectories(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"/core/core.meaning.yaml": file(concept("customer", "entity", "")),
		"/mine/a.meaning.yaml":    file(concept("customer", "entity", ", extends: 'meaning://github.com/org/core/customer?ref="+pin+"'")),
		"/bare/a.meaning.yaml":    file(concept("customer", "entity", ", extends: 'meaning://github.com/org/core/customer'")),
		"/sick/core.meaning.yaml": "a: b\n  c: d\n",
	}
	got := execute(files, "check", "/mine", "--graph", "github.com/org/core=/core")
	want := "/mine: info: meaning://github.com/org/core?ref=" + pin + " was read from the directory given with --graph; the pin is not verified offline [pin-not-verified]\nok: /mine: 1 concept, 1 file\n"
	if got.code != ExitClean || got.stdout != want {
		t.Fatalf("got %+v\nwant %q", got, want)
	}
	// Not supplied: an error, with the way out in the message.
	missing := execute(files, "check", "/mine")
	if missing.code != ExitFindings || !strings.Contains(missing.stdout, "no local copy of it was supplied, and nothing is fetched") {
		t.Fatalf("got %+v", missing)
	}
	// An unpinned reference is refused as the reference checker refuses it.
	unpinned := execute(files, "check", "/bare", "--graph", "github.com/org/core=/core")
	if unpinned.code != ExitFindings || !strings.Contains(unpinned.stdout, "needs a ?ref= pin") {
		t.Fatalf("got %+v", unpinned)
	}
	// A supplied graph that is not YAML cannot be read.
	sick := execute(files, "check", "/mine", "--graph", "github.com/org/core=/sick")
	if sick.code != ExitFindings || !strings.Contains(sick.stdout, "cannot be read: /sick/core.meaning.yaml") {
		t.Fatalf("got %+v", sick)
	}
}

func TestCheckAddressLetsAGraphReferenceItself(t *testing.T) {
	t.Parallel()
	files := map[string]string{"/g/a.meaning.yaml": file(concept("a", "entity", ""), concept("b", "entity", ", extends: 'meaning://github.com/org/me/a'"))}
	if got := execute(files, "check", "/g", "--address", "github.com/org/me"); got.code != ExitClean {
		t.Fatalf("got %+v", got)
	}
	if got := execute(files, "check", "/g"); got.code != ExitFindings {
		t.Fatalf("got %+v", got)
	}
	got := execute(files, "check", "/g", "/g/a.meaning.yaml", "--address", "github.com/org/me")
	if got.code != ExitUsage || !strings.Contains(got.stderr, "--address names one graph, but 2 graphs are being checked") {
		t.Fatalf("got %+v", got)
	}
}

func TestCheckUniversalProfile(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"/c/a.meaning.yaml": head + "license: CC0-1.0\nconcepts:\n" + concept("a", "entity", ""),
		"/c/LICENSE":        "x",
		"/f/a.meaning.yaml": head + "license: CC0-1.0\nconcepts:\n" + concept("a", "entity", ""),
	}
	if got := execute(files, "check", "/c", "--profile", "universal"); got.code != ExitClean {
		t.Fatalf("got %+v", got)
	}
	if got := execute(files, "check", "/f", "--profile", "universal"); got.code != ExitFindings || !strings.Contains(got.stdout, "LICENSE is missing") {
		t.Fatalf("got %+v", got)
	}
	got := execute(files, "check", "/f/a.meaning.yaml", "--profile", "universal")
	if got.code != ExitUsage || !strings.Contains(got.stderr, "name its directory, not files") {
		t.Fatalf("got %+v", got)
	}
}

func TestCheckRefusesWrongUsage(t *testing.T) {
	t.Parallel()
	files := map[string]string{"/g/a.meaning.yaml": file(concept("a", "entity", ""))}
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"format", []string{"check", "--format", "xml", "/g"}, `invalid --format "xml"`},
		{"address", []string{"check", "--address", "nope", "/g"}, `invalid --address "nope"`},
		{"profile", []string{"check", "--profile", "strict", "/g"}, `invalid --profile "strict"`},
		{"graph without equals", []string{"check", "--graph", "github.com/o/r", "/g"}, `invalid --graph "github.com/o/r"`},
		{"graph with a bad address", []string{"check", "--graph", "core=/g", "/g"}, `invalid --graph "core=/g"`},
		{"graph without a directory", []string{"check", "--graph", "github.com/o/r=", "/g"}, `invalid --graph "github.com/o/r="`},
		{"graph twice", []string{"check", "--graph", "github.com/o/r=/g", "--graph", "github.com/o/r=/g", "/g"}, "--graph github.com/o/r is given twice"},
		{"graph directory missing", []string{"check", "--graph", "github.com/o/r=/nowhere", "/g"}, "--graph github.com/o/r: "},
		{"path missing", []string{"check", "/nowhere"}, "meaninggraph: "},
		{"unknown flag", []string{"check", "--nope"}, "unknown flag: --nope"},
		{"unknown command", []string{"nope"}, `unknown command "nope"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := execute(files, tc.args...)
			if got.code != ExitUsage || got.stdout != "" || !strings.HasPrefix(got.stderr, "meaninggraph: ") || !strings.Contains(got.stderr, tc.want) {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

type brokenDirs struct{ meaning.FS }

func (brokenDirs) ReadDir(string) ([]fs.DirEntry, error) { return nil, errors.New("denied") }

type brokenFiles struct{ meaning.FS }

func (brokenFiles) ReadFile(string) ([]byte, error) { return nil, errors.New("denied") }

func TestCheckReportsFilesThatCannotBeRead(t *testing.T) {
	t.Parallel()
	mem := memfs.New(map[string]string{"/g/a.meaning.yaml": file(concept("a", "entity", ""))})
	for name, fsys := range map[string]meaning.FS{"directory": brokenDirs{mem}, "file": brokenFiles{mem}} {
		path := "/g"
		if name == "file" {
			path = "/g/a.meaning.yaml"
		}
		got := executeIn(fsys, "check", path)
		if got.code != ExitUsage || got.stderr != "meaninggraph: denied\n" {
			t.Errorf("%s: got %+v", name, got)
		}
	}
	// A directory that lists but whose file cannot be read.
	if got := executeIn(brokenFiles{mem}, "check", "/g"); got.code != ExitUsage {
		t.Errorf("got %+v", got)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed pipe") }

func TestCheckReportsAnOutputThatCannotBeWritten(t *testing.T) {
	t.Parallel()
	mem := memfs.New(map[string]string{"/g/a.meaning.yaml": file(concept("a", "entity", ""))})
	for _, format := range []string{"text", "json"} {
		var stderr bytes.Buffer
		code := Run([]string{"check", "--format", format, "/g"}, Env{Stdout: failingWriter{}, Stderr: &stderr, FS: mem, SelfUpdate: offline})
		if code != ExitUsage || stderr.String() != "meaninggraph: closed pipe\n" {
			t.Errorf("%s: code %d stderr %q", format, code, stderr.String())
		}
	}
}

func TestRootCommandHelpAndVersion(t *testing.T) {
	t.Parallel()
	help := execute(nil)
	if help.code != ExitClean || !strings.Contains(help.stdout, "check") || !strings.Contains(help.stdout, "self-update") {
		t.Fatalf("help = %+v", help)
	}
	for _, args := range [][]string{{"--version"}, {"version"}} {
		got := execute(nil, args...)
		if got.code != ExitClean || got.stdout == "" {
			t.Fatalf("%v = %+v", args, got)
		}
	}
	var out bytes.Buffer
	if code := Run([]string{"version", "--json"}, Env{Stdout: &out, Stderr: io.Discard, SelfUpdate: offline}); code != ExitClean || !strings.Contains(out.String(), `"`) {
		t.Fatalf("version --json = %d %q", code, out.String())
	}
}

func TestOSEnv(t *testing.T) {
	t.Parallel()
	env := OSEnv()
	if env.Stdin == nil || env.Stdout == nil || env.Stderr == nil || env.FS == nil || env.SelfUpdate == nil {
		t.Fatalf("env = %+v", env)
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// releases serves a fixed GitHub releases listing, so that no test touches the network.
func releases(status int, body string) func() selfupdate.Config {
	return func() selfupdate.Config {
		cfg := selfUpdateConfig()
		cfg.CurrentVersion = "0.1.0"
		cfg.HTTPClient = &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
		})}
		return cfg
	}
}

func TestSelfUpdateCheck(t *testing.T) {
	t.Parallel()
	run := func(update func() selfupdate.Config, args ...string) result {
		var stdout, stderr bytes.Buffer
		code := Run(args, Env{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr, SelfUpdate: update, Interactive: func() bool { return false }})
		return result{code, stdout.String(), stderr.String()}
	}
	newer := run(releases(200, `[{"tag_name":"v0.2.0"}]`), "self-update", "--check")
	if newer.code != ExitClean || !strings.Contains(newer.stdout, "0.2.0") {
		t.Fatalf("newer = %+v", newer)
	}
	same := run(releases(200, `[{"tag_name":"v0.1.0"}]`), "update", "--check", "--format", "json")
	if same.code != ExitClean || !strings.Contains(same.stdout, "0.1.0") {
		t.Fatalf("same = %+v", same)
	}
	failed := run(releases(500, "boom"), "self-update", "--check")
	if failed.code != ExitUsage || !strings.Contains(failed.stderr, "meaninggraph: ") {
		t.Fatalf("failed = %+v", failed)
	}
}

func TestSelfUpdateConfigNamesTheReleases(t *testing.T) {
	t.Parallel()
	cfg := selfUpdateConfig()
	if cfg.Repository != "meaninggraph/cli" || cfg.BinaryName != "meaninggraph" || cfg.CurrentVersion != info.Version || len(cfg.Managers) != 0 {
		t.Fatalf("cfg = %+v", cfg)
	}
}
