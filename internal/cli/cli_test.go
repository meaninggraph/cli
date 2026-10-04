package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/strongo/buildinfo"
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

// fakeAbs makes a path absolute against /work without asking the host.
func fakeAbs(path string) (string, error) {
	if strings.HasPrefix(path, "/") {
		return path, nil
	}
	return "/work/" + path, nil
}

func execute(files map[string]string, args ...string) result {
	return executeIn(memfs.New(files), args...)
}

func executeIn(fsys meaning.FS, args ...string) result {
	var stdout, stderr bytes.Buffer
	code := Run(args, Env{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr, FS: fsys, Abs: fakeAbs, SelfUpdate: offline, Interactive: func() bool { return false }})
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
		"/g/b.meaning.yaml:2: error: a colon followed by a space inside a plain value would start a mapping; put the value in quotes [yaml]\n" +
		"/g/sub/c.meaning.yaml: warning: meaning files are not read below the directory of a graph: a graph is the meaning files directly in its directory, so move this file there, or check its directory [subdirectory-meaning-file]\n" +
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
	if got.code != ExitClean || !strings.Contains(got.stdout, "warning") || !strings.HasSuffix(got.stdout, "ok: /g: 1 concept, 1 file, 1 warning\n") {
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
	want := "/mine: warning: meaning://github.com/org/core?ref=" + pin + " was read from /core, which cannot be verified as that version (it is not a git checkout (it has no .git)); the pin is trusted, not checked [pin-not-verified]\nok: /mine: 1 concept, 1 file, 1 warning\n"
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
	if got.code != ExitUsage || !strings.Contains(got.stderr, "--address github.com/org/me names no path, but 2 graphs are being checked: write --address github.com/org/me=<path>") {
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
		{"address with an empty path", []string{"check", "--address", "github.com/o/r=", "/g"}, `invalid --address "github.com/o/r="`},
		{"address with a path and no address", []string{"check", "--address", "=/g", "/g"}, `invalid --address "=/g"`},
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
		code := Run([]string{"check", "--format", format, "/g"}, Env{Stdout: failingWriter{}, Stderr: &stderr, FS: mem, Abs: fakeAbs, SelfUpdate: offline})
		if code != ExitUsage || stderr.String() != "meaninggraph: closed pipe\n" {
			t.Errorf("%s: code %d stderr %q", format, code, stderr.String())
		}
	}
}

func TestRootCommandHelpAndVersion(t *testing.T) {
	t.Parallel()
	help := execute(nil)
	if help.code != ExitClean || !strings.Contains(help.stdout, "check") || !strings.Contains(help.stdout, "schema") || !strings.Contains(help.stdout, "self-update") {
		t.Fatalf("help = %+v", help)
	}
	if got := execute(nil, "--version"); got.code != ExitClean || got.stdout != info.Short()+"\n" || got.stderr != "" {
		t.Fatalf("--version = %+v, want %q", got, info.Short())
	}
	if got := execute(nil, "version"); got.code != ExitClean || got.stdout != info.Long()+"\n" || !strings.HasPrefix(got.stdout, "meaninggraph ") {
		t.Fatalf("version = %+v, want %q", got, info.Long())
	}
	var parsed buildinfo.VersionJSON
	got := execute(nil, "version", "--json")
	if err := json.Unmarshal([]byte(got.stdout), &parsed); err != nil || got.code != ExitClean || parsed != info.JSON() || parsed.Version == "" {
		t.Fatalf("version --json = %+v (%v), want %+v", got, err, info.JSON())
	}
}

func TestOSEnv(t *testing.T) {
	t.Parallel()
	env := OSEnv()
	if env.Stdin != os.Stdin || env.Stdout != os.Stdout || env.Stderr != os.Stderr {
		t.Fatalf("the streams are the process's own: %+v", env)
	}
	if _, ok := env.FS.(meaning.OSFS); !ok {
		t.Fatalf("FS = %T, want the host file system", env.FS)
	}
	abs, err := env.Abs("sub/dir")
	if err != nil || !filepath.IsAbs(abs) || !strings.HasSuffix(abs, filepath.Join("sub", "dir")) {
		t.Fatalf("Abs = %q, %v", abs, err)
	}
	if cfg := env.SelfUpdate(); cfg.Repository != "meaninggraph/cli" || cfg.BinaryName != "meaninggraph" || cfg.CurrentVersion != info.Version {
		t.Fatalf("SelfUpdate() = %+v", cfg)
	}
	if env.Interactive != nil {
		t.Fatal("the terminal check is the library's own")
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
	type check struct {
		Current, Latest, Verdict string
	}
	read := func(r result) check {
		var c check
		if err := json.Unmarshal([]byte(r.stdout), &c); err != nil {
			t.Fatalf("not JSON: %v\n%s", err, r.stdout)
		}
		return c
	}
	newer := run(releases(200, `[{"tag_name":"v0.2.0"}]`), "self-update", "--check", "--format", "json")
	same := run(releases(200, `[{"tag_name":"v0.1.0"}]`), "update", "--check", "--format", "json")
	older := run(releases(200, `[{"tag_name":"v0.0.9"}]`), "update", "--check", "--format", "json")
	if newer.code != ExitClean || same.code != ExitClean || older.code != ExitClean {
		t.Fatalf("--check never fails the command, whatever it finds: %+v %+v %+v", newer, same, older)
	}
	n, s, o := read(newer), read(same), read(older)
	if n.Current != "0.1.0" || n.Latest != "0.2.0" || s.Latest != "0.1.0" || o.Latest != "0.0.9" {
		t.Fatalf("versions: %+v %+v %+v", n, s, o)
	}
	if n.Verdict == s.Verdict || s.Verdict == o.Verdict || n.Verdict == o.Verdict || n.Verdict == "" {
		t.Fatalf("verdicts must tell the three apart: %+v %+v %+v", n, s, o)
	}
	text := run(releases(200, `[{"tag_name":"v0.2.0"}]`), "self-update", "--check")
	if text.code != ExitClean || !strings.Contains(text.stdout, "0.2.0") || !strings.Contains(text.stdout, "update available: 0.1.0") {
		t.Fatalf("text = %+v", text)
	}
	failed := run(releases(500, "boom"), "self-update", "--check")
	if failed.code != ExitUsage || failed.stdout != "" || !strings.HasPrefix(failed.stderr, "meaninggraph: ") {
		t.Fatalf("failed = %+v", failed)
	}
	failedJSON := run(releases(500, "boom"), "self-update", "--check", "--format=json")
	var parsed struct {
		OK    bool
		Error string
	}
	if err := json.Unmarshal([]byte(failedJSON.stdout), &parsed); err != nil || failedJSON.code != ExitUsage || parsed.OK || !strings.Contains(parsed.Error, "500") {
		t.Fatalf("failed with --format json = %+v (%v)", failedJSON, err)
	}
}

func TestSelfUpdateConfigNamesTheReleases(t *testing.T) {
	t.Parallel()
	cfg := selfUpdateConfig()
	if cfg.Repository != "meaninggraph/cli" || cfg.BinaryName != "meaninggraph" || cfg.CurrentVersion != info.Version || len(cfg.Managers) != 0 {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestCheckVerifiesPinsAgainstGitCheckouts(t *testing.T) {
	t.Parallel()
	other := strings.Repeat("0", 40)
	files := map[string]string{
		"/core/core.meaning.yaml":    file(concept("customer", "entity", "")),
		"/mine/a.meaning.yaml":       file(concept("customer", "entity", ", extends: 'meaning://github.com/org/core/customer?ref="+pin+"'")),
		"/core/.git/HEAD":            "ref: refs/heads/main\n",
		"/core/.git/refs/heads/main": pin + "\n",
	}
	got := execute(files, "check", "/mine", "--graph", "github.com/org/core=/core")
	if got.code != ExitClean || got.stdout != "ok: /mine: 1 concept, 1 file\n" {
		t.Fatalf("a checkout at the pinned commit is silent: %+v", got)
	}
	files["/core/.git/refs/heads/main"] = other + "\n"
	got = execute(files, "check", "/mine", "--graph", "github.com/org/core=/core")
	want := "/mine: error: meaning://github.com/org/core is pinned at " + pin + ", but /core, given with --graph, is a checkout of " + other + "; check out the pinned version there [pin-checkout-mismatch]\n" +
		"failed: /mine: 1 error, 0 warnings, 1 concept, 1 file\n"
	if got.code != ExitFindings || got.stdout != want {
		t.Fatalf("a checkout at another commit is an error: %+v\nwant %q", got, want)
	}
}

// A pin that is a branch or a tag name (FORMAT.md allows them) is looked up in
// the checkout's refs, so that a checkout of that branch or tag is not a
// mismatch.
func TestCheckVerifiesPinsThatAreBranchesAndTags(t *testing.T) {
	t.Parallel()
	other := strings.Repeat("0", 40)
	check := func(ref string, git map[string]string) result {
		files := map[string]string{
			"/core/core.meaning.yaml": file(concept("customer", "entity", "")),
			"/mine/a.meaning.yaml":    file(concept("customer", "entity", ", extends: 'meaning://github.com/org/core/customer?ref="+ref+"'")),
		}
		for name, content := range git {
			files["/core/.git/"+name] = content
		}
		return execute(files, "check", "/mine", "--graph", "github.com/org/core=/core")
	}
	for _, tc := range []struct {
		name, ref string
		git       map[string]string
		code      int
		contains  []string
	}{
		{"a checkout of the branch", "main", map[string]string{"HEAD": "ref: refs/heads/main\n", "refs/heads/main": other}, ExitClean, []string{"ok: /mine: 1 concept, 1 file\n"}},
		{"a checkout at the commit of the branch", "main", map[string]string{"HEAD": pin, "refs/heads/main": pin}, ExitClean, []string{"ok: /mine: 1 concept, 1 file\n"}},
		{"a checkout at the commit of the branch of the remote", "main", map[string]string{"HEAD": pin, "refs/remotes/origin/main": pin}, ExitClean, []string{"ok: /mine: 1 concept, 1 file\n"}},
		{"a checkout at the commit of a tag", "v1.2.0", map[string]string{"HEAD": pin, "refs/tags/v1.2.0": pin}, ExitClean, []string{"ok: /mine: 1 concept, 1 file\n"}},
		{"a checkout at the commit an annotated packed tag peels to", "v1", map[string]string{"HEAD": pin, "packed-refs": other + " refs/tags/v1\n^" + pin + "\n"}, ExitClean, []string{"ok: /mine"}},
		{"a checkout at another commit than an annotated packed tag", "v1", map[string]string{"HEAD": other, "packed-refs": other + " refs/tags/v1\n^" + pin + "\n"}, ExitFindings, []string{"pin-checkout-mismatch", "refs/tags/v1 is at " + pin}},
		{"a checkout at another commit than the branch", "main", map[string]string{"HEAD": other, "refs/heads/main": pin}, ExitFindings, []string{"pin-checkout-mismatch", "is a checkout of " + other + ", and refs/heads/main is at " + pin}},
		{"a checkout at another commit than the branches", "main", map[string]string{"HEAD": other, "refs/heads/main": pin, "refs/remotes/origin/main": strings.Repeat("1", 40)}, ExitFindings, []string{"refs/heads/main is at " + pin + ", refs/remotes/origin/main is at " + strings.Repeat("1", 40)}},
		{"a loose tag that may be annotated", "v1", map[string]string{"HEAD": other, "refs/tags/v1": pin}, ExitClean, []string{"pin-not-verified", "may be annotated"}},
		{"a name the checkout does not have", "nope", map[string]string{"HEAD": pin}, ExitClean, []string{"pin-not-verified", `"nope" is a branch or tag name, which can move, and the checkout has no such ref`}},
		{"a pin in upper case hexadecimal is a name", strings.ToUpper(pin), map[string]string{"HEAD": pin}, ExitClean, []string{"pin-not-verified"}},
		{"a pin that climbs out of the refs", "a/../b", map[string]string{"HEAD": pin}, ExitClean, []string{"pin-not-verified", "is not a valid branch or tag name"}},
		{"a branch name with a trailing slash", "main/", map[string]string{"HEAD": pin, "refs/heads/main": pin}, ExitClean, []string{"pin-not-verified", "is not a valid branch or tag name"}},
		{"a branch name that starts with a dot segment", "./main", map[string]string{"HEAD": pin, "refs/heads/main": pin}, ExitClean, []string{"pin-not-verified", "is not a valid branch or tag name"}},
		{"a packed tag that may be annotated, in a file that is not fully peeled", "v1", map[string]string{"HEAD": other, "packed-refs": "# pack-refs with: peeled\n" + pin + " refs/tags/v1\n"}, ExitClean, []string{"pin-not-verified", "may be annotated"}},
		{"a packed tag of a fully peeled file is a commit", "v1", map[string]string{"HEAD": pin, "packed-refs": "# pack-refs with: peeled fully-peeled sorted\n" + pin + " refs/tags/v1\n"}, ExitClean, []string{"ok: /mine"}},
		{"a packed tag of a fully peeled file at another commit is a mismatch", "v1", map[string]string{"HEAD": other, "packed-refs": "# pack-refs with: peeled fully-peeled sorted\n" + pin + " refs/tags/v1\n"}, ExitFindings, []string{"pin-checkout-mismatch"}},
		{"a big ref file is not read", "big", map[string]string{"HEAD": pin, "refs/heads/big": strings.Repeat("x", 5000)}, ExitClean, []string{"pin-not-verified", "larger than"}},
	} {
		got := check(tc.ref, tc.git)
		if got.code != tc.code {
			t.Errorf("%s: code = %d, want %d:\n%s", tc.name, got.code, tc.code, got.stdout)
		}
		for _, want := range tc.contains {
			if !strings.Contains(got.stdout, want) {
				t.Errorf("%s: output lacks %q:\n%s", tc.name, want, got.stdout)
			}
		}
	}
}

func TestCheckWarnsAboutAGraphThatNothingRefersTo(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"/core/core.meaning.yaml": file(concept("customer", "entity", "")),
		"/mine/a.meaning.yaml":    file(concept("a", "entity", "")),
		"/cased/a.meaning.yaml":   file(concept("customer", "entity", ", extends: 'meaning://github.com/org/Core/customer?ref="+pin+"'")),
	}
	got := execute(files, "check", "/mine", "--graph", "github.com/org/core=/core")
	want := "/mine: warning: the graph github.com/org/core was given with --graph, but no file refers to it [unused-graph]\nok: /mine: 1 concept, 1 file, 1 warning\n"
	if got.code != ExitClean || got.stdout != want {
		t.Fatalf("got %+v\nwant %q", got, want)
	}
	got = execute(files, "check", "/cased", "--graph", "github.com/org/core=/core")
	if got.code != ExitFindings || !strings.Contains(got.stdout, "meaning://github.com/org/Core is not available") ||
		!strings.Contains(got.stdout, "the files refer to github.com/org/Core, which differs only by case (addresses are compared exactly)") {
		t.Fatalf("got %+v", got)
	}
	// A graph that one of the checked graphs uses is used.
	got = execute(files, "check", "/mine", "/cased", "--graph", "github.com/org/Core=/core")
	if got.code != ExitClean || strings.Contains(got.stdout, "unused-graph") {
		t.Fatalf("got %+v", got)
	}
}

func TestCheckReadsOneDirectoryOnceWhicheverWayItIsWritten(t *testing.T) {
	t.Parallel()
	// The in-memory file system has no working directory: the same files are there twice.
	files := map[string]string{"/work/model/a.meaning.yaml": file(concept("a", "entity", "")), "model/a.meaning.yaml": file(concept("a", "entity", ""))}
	got := execute(files, "check", "model", "/work/model", "./model", "model/", "/work/model/../model")
	if got.code != ExitClean || got.stdout != "ok: model: 1 concept, 1 file\n" {
		t.Fatalf("got %+v", got)
	}
	got = execute(files, "check", "/work/model/a.meaning.yaml", "model/a.meaning.yaml")
	if got.code != ExitClean || got.stdout != "ok: /work/model/a.meaning.yaml: 1 concept, 1 file\n" {
		t.Fatalf("got %+v", got)
	}
}

func TestCheckRefusesAnEmptyPath(t *testing.T) {
	t.Parallel()
	files := map[string]string{"/work/a.meaning.yaml": file(concept("a", "entity", "")), "model/a.meaning.yaml": file(concept("a", "entity", ""))}
	for _, args := range [][]string{{"check", ""}, {"check", "model", ""}} {
		got := execute(files, args...)
		if got.code != ExitUsage || got.stdout != "" || !strings.Contains(got.stderr, "an empty path was given") {
			t.Errorf("%v: %+v", args, got)
		}
	}
}

func TestCheckGivesJSONOnEveryExitTwoPath(t *testing.T) {
	t.Parallel()
	files := map[string]string{"/g/a.meaning.yaml": file(concept("a", "entity", ""))}
	paths := []struct {
		name string
		args []string
		want string
	}{
		{"a path that is not there", []string{"check", "--format", "json", "/nowhere"}, "nowhere"},
		{"an empty path", []string{"check", "--format", "json", ""}, "an empty path"},
		{"a bad profile", []string{"check", "--format=json", "--profile", "strict", "/g"}, "invalid --profile"},
		{"a bad address", []string{"check", "--format=json", "--address", "nope", "/g"}, "invalid --address"},
		{"a bad graph", []string{"check", "--format", "json", "--graph", "nope", "/g"}, "invalid --graph"},
		{"an unknown flag", []string{"check", "--format=json", "--nope"}, "unknown flag"},
		{"an unknown command", []string{"nope", "--format", "json"}, "unknown command"},
		{"files with the universal profile", []string{"check", "--format=json", "--profile", "universal", "/g/a.meaning.yaml"}, "name its directory"},
	}
	for _, tc := range paths {
		got := execute(files, tc.args...)
		var parsed struct {
			Tool, Version string
			OK            bool
			Error         string
		}
		if err := json.Unmarshal([]byte(got.stdout), &parsed); err != nil {
			t.Errorf("%s: stdout is not JSON: %v\n%q", tc.name, err, got.stdout)
			continue
		}
		if got.code != ExitUsage || parsed.Tool != "meaninggraph" || parsed.Version != info.Version || parsed.OK || !strings.Contains(parsed.Error, tc.want) {
			t.Errorf("%s: %+v / %+v", tc.name, got, parsed)
		}
		if !strings.HasPrefix(got.stderr, "meaninggraph: ") || !strings.Contains(got.stderr, tc.want) {
			t.Errorf("%s: the message goes to stderr as well: %q", tc.name, got.stderr)
		}
	}
	// Without --format json stdout stays empty, as before.
	if got := execute(files, "check", "/nowhere"); got.stdout != "" || got.code != ExitUsage {
		t.Fatalf("got %+v", got)
	}
	// A --format that is not json is not a request for JSON.
	if got := execute(files, "check", "--format", "text", "/nowhere"); got.stdout != "" {
		t.Fatalf("got %+v", got)
	}
	if wantsJSON([]string{"--format"}) || !wantsJSON([]string{"x", "--format=json"}) {
		t.Fatal("wantsJSON")
	}
}

func TestCheckReportsAPathThatCannotBeMadeAbsolute(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	env := Env{Stdout: &stdout, Stderr: &stderr, FS: memfs.New(map[string]string{"a/a.meaning.yaml": file(concept("a", "entity", ""))}),
		Abs: func(string) (string, error) { return "", errors.New("no working directory") }, SelfUpdate: offline}
	if code := Run([]string{"check", "a"}, env); code != ExitUsage || stderr.String() != "meaninggraph: no working directory\n" {
		t.Fatalf("code %d stderr %q", code, stderr.String())
	}
}
