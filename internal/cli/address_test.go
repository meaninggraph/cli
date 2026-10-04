package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/meaninggraph/cli/internal/memfs"
	"github.com/meaninggraph/cli/pkg/meaning"
)

// graphsOf reads the graphs of a --format json run, by their paths.
func graphsOf(t *testing.T, got result) map[string]graphReport {
	t.Helper()
	var report jsonReport
	if err := json.Unmarshal([]byte(got.stdout), &report); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, got.stdout)
	}
	graphs := map[string]graphReport{}
	for _, g := range report.Graphs {
		graphs[g.label()] = g
	}
	return graphs
}

// Each of --address's forms: one per checked path, spelled any way the path is,
// for a directory and for the files named together.
func TestCheckAnAddressForEachCheckedPath(t *testing.T) {
	t.Parallel()
	self := func(address string) string {
		return file(concept("a", "entity", ""), concept("b", "entity", ", extends: 'meaning://"+address+"/a'"))
	}
	files := map[string]string{
		"/a/a.meaning.yaml": self("github.com/org/a"),
		"/b/b.meaning.yaml": self("github.com/org/b"),
		"/f/f.meaning.yaml": self("github.com/org/f"),
	}
	for _, tc := range []struct {
		name string
		args []string
		code int
		want []string // parts of the output
	}{
		{"an address for each directory", []string{"/a", "/b", "--address", "github.com/org/a=/a", "--address", "github.com/org/b=/b"}, ExitClean, []string{"ok: /a:", "ok: /b:"}},
		{"in the other order", []string{"/a", "/b", "--address", "github.com/org/b=/b", "--address", "github.com/org/a=/a"}, ExitClean, []string{"ok: /a:", "ok: /b:"}},
		{"a path spelled another way", []string{"/a", "/b", "--address", "github.com/org/a=/a/../a/.", "--address", "github.com/org/b=/b/"}, ExitClean, []string{"ok: /a:", "ok: /b:"}},
		{"an address for the files named together", []string{"/a", "/f/f.meaning.yaml", "--address", "github.com/org/a=/a", "--address", "github.com/org/f=/f/f.meaning.yaml"}, ExitClean, []string{"ok: /a:", "ok: /f/f.meaning.yaml:"}},
		{"a graph with no address keeps failing to find itself", []string{"/a", "/b", "--address", "github.com/org/a=/a"}, ExitFindings, []string{"ok: /a:", "failed: /b:", "meaning://github.com/org/b is not available"}},
		{"the bare form for one path", []string{"/b", "--address", "github.com/org/b"}, ExitClean, []string{"ok: /b:"}},
		{"the form with a path for one path", []string{"/b", "--address", "github.com/org/b=/b"}, ExitClean, []string{"ok: /b:"}},
	} {
		got := execute(files, append([]string{"check"}, tc.args...)...)
		if got.code != tc.code {
			t.Errorf("%s: code %d, want %d\n%+v", tc.name, got.code, tc.code, got)
		}
		for _, want := range tc.want {
			if !strings.Contains(got.stdout, want) {
				t.Errorf("%s: output lacks %q:\n%s", tc.name, want, got.stdout)
			}
		}
	}
	// The address of each graph is in its report.
	graphs := graphsOf(t, execute(files, "check", "--format", "json", "/a", "/b", "--address", "github.com/org/a=/a", "--address", "github.com/org/b=/b"))
	if graphs["/a"].Address != "github.com/org/a" || graphs["/b"].Address != "github.com/org/b" {
		t.Errorf("addresses: %+v", graphs)
	}
}

// What cannot be said is refused, with exit 2 and a message that says what to write.
func TestCheckRefusesAddressesThatContradict(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"/a/a.meaning.yaml": file(concept("a", "entity", "")),
		"/b/b.meaning.yaml": file(concept("b", "entity", "")),
		"/f/f.meaning.yaml": file(concept("f", "entity", "")),
	}
	const x, y = "github.com/org/x", "github.com/org/y"
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"two addresses for one path", []string{"/a", "--address", x + "=/a", "--address", y + "=/a"}, "--address " + y + "=/a and an earlier --address both name the graph at /a (" + x + ")"},
		{"two addresses for one path, spelled differently", []string{"/a", "--address", x + "=/a", "--address", y + "=/a/."}, "both name the graph at /a"},
		{"the same address twice for one path", []string{"/a", "--address", x + "=/a", "--address", x + "=/a"}, "both name the graph at /a"},
		{"two addresses for the files named together", []string{"/f/f.meaning.yaml", "/f/g.meaning.yaml", "--address", x + "=/f/f.meaning.yaml", "--address", y + "=/f/g.meaning.yaml"}, "both name the graph at /f/f.meaning.yaml /f/g.meaning.yaml"},
		{"one address for two paths", []string{"/a", "/b", "--address", x + "=/a", "--address", x + "=/b"}, "gives the address " + x + " to the graph at /b, but it is the address of the graph at /a"},
		{"an address for a path that is not checked", []string{"/a", "--address", x + "=/b"}, "--address " + x + "=/b: /b is not one of the paths being checked"},
		{"an address for a file of a directory that is checked", []string{"/a", "--address", x + "=/a/a.meaning.yaml"}, "is not one of the paths being checked"},
		{"a bare address for several graphs", []string{"/a", "/b", "--address", x}, "--address " + x + " names no path, but 2 graphs are being checked: write --address " + x + "=<path>, once for each path"},
		{"a bare address and another", []string{"/a", "--address", x, "--address", y + "=/a"}, "--address " + x + " names no path, but there are other --address flags"},
		{"a bare address twice", []string{"/a", "--address", x, "--address", y}, "but there are other --address flags"},
		{"an address that is not an address", []string{"/a", "--address", "nope=/a"}, `invalid --address "nope=/a"`},
		{"--graph and --address for one directory", []string{"/a", "--address", x + "=/a", "--graph", y + "=/a"}, "--graph " + y + "=/a gives the graph at /a the address " + y + ", but another flag gave it the address " + x},
		{"--address and --graph for one directory, in the other order", []string{"/a", "--graph", y + "=/a", "--address", x + "=/a"}, "--graph " + y + "=/a gives the graph at /a the address " + y + ", but another flag gave it the address " + x},
		{"an address of --graph given to another graph", []string{"/a", "/b", "--address", x + "=/a", "--graph", x + "=/b"}, "--graph " + x + "=/b gives the address " + x + " to the graph at /b, but it is the address of the graph at /a"},
		{"a directory supplied under two addresses", []string{"/a", "--graph", x + "=/a", "--graph", y + "=/a"}, "/a is given with --graph under 2 addresses (" + x + ", " + y + "): a graph has one"},
	} {
		files["/f/g.meaning.yaml"] = file(concept("g", "entity", ""))
		got := execute(files, append([]string{"check"}, tc.args...)...)
		if got.code != ExitUsage || got.stdout != "" || !strings.HasPrefix(got.stderr, "meaninggraph: ") || !strings.Contains(got.stderr, tc.want) {
			t.Errorf("%s: got %+v\nwant a message with %q", tc.name, got, tc.want)
		}
	}
	// The same address given twice, by --address and --graph for one directory, is no contradiction.
	if got := execute(files, "check", "/a", "--address", x+"=/a", "--graph", x+"=/a"); got.code != ExitClean {
		t.Errorf("one address from two flags: %+v", got)
	}
}

// A path that cannot be made absolute is an error where an --address names it,
// and where --graph supplies a directory next to a checked one.
func TestCheckReportsAddressPathsThatCannotBeMadeAbsolute(t *testing.T) {
	t.Parallel()
	files := memfs.New(map[string]string{"/a/a.meaning.yaml": file(concept("a", "entity", "")), "/bad/b.meaning.yaml": file(concept("b", "entity", ""))})
	abs := func(path string) (string, error) {
		if strings.HasPrefix(path, "/bad") {
			return "", errors.New("no working directory")
		}
		return path, nil
	}
	for _, args := range [][]string{
		{"check", "/a", "--address", "github.com/org/x=/bad"},
		{"check", "/a", "--graph", "github.com/org/x=/bad"},
	} {
		var stdout, stderr bytes.Buffer
		code := Run(args, Env{Stdout: &stdout, Stderr: &stderr, FS: files, Abs: abs, SelfUpdate: offline})
		if code != ExitUsage || stderr.String() != "meaninggraph: no working directory\n" {
			t.Errorf("%v: code %d stderr %q", args, code, stderr.String())
		}
	}
}

// One run that checks a graph and the dependency supplied for it gives each graph
// the findings its own run gives: the consumer's with --graph, and the dependency
// checked alone, with the address that --graph names for the directory.
func TestCheckAGraphAndItsDependencyInOneRun(t *testing.T) {
	t.Parallel()
	other := strings.Repeat("0", 40)
	files := map[string]string{
		// the dependency: a self reference that resolves with its address, a concept that extends one that is not there, and a checkout at the pin
		"/core/core.meaning.yaml": file(concept("customer", "entity", ""), concept("client", "entity", ", extends: 'meaning://github.com/org/core/customer'"), concept("orphan", "entity", ", extends: nope")),
		"/core/.git/HEAD":         pin + "\n",
		// a warning in each graph: a meaning file below the directory of the graph
		"/core/sub/x.meaning.yaml": file(concept("x", "entity", "")),
		"/mine/sub/y.meaning.yaml": file(concept("y", "entity", "")),
		// the consumer: one good reference, one to a concept the dependency lacks, and a pin that no checkout has
		"/mine/a.meaning.yaml": file(concept("buyer", "entity", ", extends: 'meaning://github.com/org/core/customer?ref="+pin+"'"), concept("lost", "entity", ", extends: 'meaning://github.com/org/core/missing?ref="+pin+"'"), concept("self", "entity", ", extends: 'meaning://github.com/org/me/buyer'")),
		// a second consumer, whose pin does not match the checkout
		"/stale/a.meaning.yaml": file(concept("buyer", "entity", ", extends: 'meaning://github.com/org/core/customer?ref="+other+"'")),
	}
	const core, me = "github.com/org/core", "github.com/org/me"
	mine := execute(files, "check", "--format", "json", "/mine", "--address", me, "--graph", core+"=/core")
	alone := execute(files, "check", "--format", "json", "/core", "--address", core)
	both := execute(files, "check", "--format", "json", "/mine", "/core", "--address", me+"=/mine", "--graph", core+"=/core")
	if mine.code != ExitFindings || alone.code != ExitFindings || both.code != ExitFindings {
		t.Fatalf("codes %d %d %d\n%s\n%s", mine.code, alone.code, both.code, mine.stdout, alone.stdout)
	}
	one, two, together := graphsOf(t, mine)["/mine"], graphsOf(t, alone)["/core"], graphsOf(t, both)
	if len(together) != 2 || len(one.Findings) == 0 || len(two.Findings) == 0 || one.Warnings == 0 || two.Warnings == 0 {
		t.Fatalf("graphs %+v; the runs found %d and %d", together, len(one.Findings), len(two.Findings))
	}
	if !reflect.DeepEqual(together["/mine"], one) || !reflect.DeepEqual(together["/core"], two) {
		t.Fatalf("the findings of the two runs and of one run differ:\nmine  %+v\nalone %+v\nboth  %+v", one, two, together)
	}
	if together["/core"].Address != core || together["/mine"].Address != me {
		t.Errorf("addresses: %+v", together)
	}
	// The stale pin is still an error, found while the dependency is checked too.
	stale := execute(files, "check", "/stale", "/core", "--address", me+"=/stale", "--graph", core+"=/core")
	if stale.code != ExitFindings || !strings.Contains(stale.stdout, "pin-checkout-mismatch") {
		t.Errorf("a pin that does not match: %+v", stale)
	}
}

// A directory that is checked and is also supplied with --graph is that graph:
// it has that address with no --address, and is checked once.
func TestCheckAGraphThatIsAlsoSuppliedHasThatAddressAndIsCheckedOnce(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"/core/core.meaning.yaml": file(concept("customer", "entity", ""), concept("client", "entity", ", extends: 'meaning://github.com/org/core/customer'")),
		"/mine/a.meaning.yaml":    file(concept("buyer", "entity", ", extends: 'meaning://github.com/org/core/customer?ref="+pin+"'")),
	}
	// /core/. and /core/../core are the directory that --graph names.
	got := execute(files, "check", "--format", "json", "/mine", "/core/.", "--graph", "github.com/org/core=/core/../core")
	graphs := graphsOf(t, got)
	if got.code != ExitClean || len(graphs) != 2 || graphs["/core"].Address != "github.com/org/core" || graphs["/mine"].Address != "" {
		t.Fatalf("got %+v\n%+v", got, graphs)
	}
	var report jsonReport
	_ = json.Unmarshal([]byte(got.stdout), &report)
	for _, g := range report.Graphs {
		for _, f := range g.Findings {
			if f.Severity == meaning.Error {
				t.Errorf("a finding: %+v", f)
			}
		}
	}
	// Without --graph the directory has no address, and its reference to itself is not found.
	if bare := execute(files, "check", "/core"); bare.code != ExitFindings {
		t.Errorf("no address: %+v", bare)
	}
	// Files named together are not a directory that --graph supplies.
	if got := execute(files, "check", "/core/core.meaning.yaml", "--graph", "github.com/org/core=/core"); got.code != ExitFindings {
		t.Errorf("a file next to a supplied directory has no address: %+v", got)
	}
}

// A supplied directory that is also checked is in use: it gets no unused-graph
// warning that it would not have in a run of its own, and every graph has, warnings
// included, the findings of its own run.
func TestCheckAGraphThatIsAlsoSuppliedIsNotUnused(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"/b/b.meaning.yaml": file(concept("b", "entity", "")),
		"/d/d.meaning.yaml": file(concept("d", "entity", "")),
		"/e/e.meaning.yaml": file(concept("e", "entity", "")),
	}
	const b = "github.com/org/b"
	alone := execute(files, "check", "/b", "--graph", b+"=/b")
	if alone.code != ExitClean || strings.Contains(alone.stdout, "unused-graph") {
		t.Fatalf("a graph that is checked and supplied: %+v", alone)
	}
	separate := graphsOf(t, execute(files, "check", "--format", "json", "/b", "--address", b))
	separate["/d"] = graphsOf(t, execute(files, "check", "--format", "json", "/d"))["/d"]
	together := graphsOf(t, execute(files, "check", "--format", "json", "/b", "/d", "--graph", b+"=/b"))
	if len(together) != 2 || !reflect.DeepEqual(together["/b"], separate["/b"]) || !reflect.DeepEqual(together["/d"], separate["/d"]) {
		t.Fatalf("one run and two runs differ:\n%+v\n%+v", together, separate)
	}
	// A supplied directory that is not checked, and that nothing refers to, still warns.
	if got := execute(files, "check", "/b", "--graph", b+"=/b", "--graph", "github.com/org/e=/e"); !strings.Contains(got.stdout, "the graph github.com/org/e was given with --graph, but no file refers to it") || strings.Contains(got.stdout, "github.com/org/b was given") {
		t.Fatalf("got %+v", got)
	}
}
