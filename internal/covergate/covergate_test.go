package covergate

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

const complete = `mode: atomic
a.go:1.1,2.2 3 5
a.go:3.1,4.2 2 1
`

func TestParseMergesBlocksAcrossTestBinaries(t *testing.T) {
	t.Parallel()
	profile, err := Parse(strings.NewReader("mode: set\nb.go:1.1,2.2 4 0\nb.go:1.1,2.2 4 7\na.go:1.1,2.2 1 0\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Blocks) != 2 || profile.Blocks[0].Location != "a.go:1.1,2.2" || !profile.Blocks[1].Hit {
		t.Fatalf("blocks = %+v", profile.Blocks)
	}
	covered, total := profile.Totals()
	if covered != 4 || total != 5 {
		t.Fatalf("totals = %d of %d", covered, total)
	}
	if profile.Complete() {
		t.Fatal("a profile with an uncovered statement is not complete")
	}
	missed := profile.Uncovered()
	if len(missed) != 1 || missed[0].Location != "a.go:1.1,2.2" {
		t.Fatalf("uncovered = %+v", missed)
	}
}

func TestParseRejectsMalformedProfiles(t *testing.T) {
	t.Parallel()
	for name, text := range map[string]string{
		"fields":     "mode: set\nnot a block\n",
		"statements": "a.go:1.1,2.2 x 1\n",
		"negative":   "a.go:1.1,2.2 -1 1\n",
		"count":      "a.go:1.1,2.2 1 y\n",
		"negcount":   "a.go:1.1,2.2 1 -4\n",
		"long line":  strings.Repeat("x", 2<<20),
	} {
		if _, err := Parse(strings.NewReader(text)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestCompleteIsExactNotRounded(t *testing.T) {
	t.Parallel()
	// 9,999 of 10,000 statements is 99.99%: it must fail however it is rounded.
	profile := Profile{Blocks: []Block{{Location: "a", Statements: 9999, Hit: true}, {Location: "b", Statements: 1}}}
	if profile.Complete() {
		t.Fatal("99.99% must not pass")
	}
	if (Profile{}).Complete() {
		t.Fatal("an empty profile must not pass")
	}
	if !(Profile{Blocks: []Block{{Location: "a", Statements: 3, Hit: true}}}).Complete() {
		t.Fatal("a fully covered profile passes")
	}
	if len((Profile{Blocks: []Block{{Location: "a"}}}).Uncovered()) != 0 {
		t.Fatal("a block with no statements is never uncovered")
	}
}

func TestThresholdIsFixedAtOneHundred(t *testing.T) {
	t.Parallel()
	if RequiredPercent != 100 {
		t.Fatalf("RequiredPercent = %d; the gate must require 100", RequiredPercent)
	}
}

func opener(text string, err error) func(string) (io.ReadCloser, error) {
	return func(string) (io.ReadCloser, error) {
		if err != nil {
			return nil, err
		}
		return io.NopCloser(strings.NewReader(text)), nil
	}
}

// tree is a module "m" with two packages, a command and a nested module.
var tree = fstest.MapFS{
	"go.mod":                       {Data: []byte("// a comment\nmodule m\n\ngo 1.27\n")},
	"a.go":                         {Data: []byte("package m\n")},
	"a_test.go":                    {Data: []byte("package m\n")},
	"cmd/tool/main.go":             {Data: []byte("package main\n")},
	"internal/only_test/x_test.go": {Data: []byte("package x\n")},
	"testdata/t.go":                {Data: []byte("package t\n")},
	"vendor/v/v.go":                {Data: []byte("package v\n")},
	"_hidden/h.go":                 {Data: []byte("package h\n")},
	".hidden/h.go":                 {Data: []byte("package h\n")},
	"scripts/go.mod":               {Data: []byte("module other\n")},
	"scripts/main.go":              {Data: []byte("package main\n")},
	"docs/readme.md":               {Data: []byte("text")},
}

// recordedTree is the tree that `go list ./...` was run on, once, by hand, in a
// module "example.com/m" (Go 1.27.1, module mode): a package at the root and in
// directories of every kind the walk has a rule for.
var recordedTree = fstest.MapFS{
	"go.mod":                {Data: []byte("module example.com/m\n\ngo 1.22\n")},
	"m.go":                  {Data: []byte("package m\n")},
	"pkg/a/a.go":            {Data: []byte("package a\n")},
	"node_modules/x/x.go":   {Data: []byte("package x\n")},
	"a/node_modules/n/n.go": {Data: []byte("package n\n")},
	"vendor/v/v.go":         {Data: []byte("package v\n")},
	"a/vendor/v/v.go":       {Data: []byte("package v\n")},
	"testdata/t/t.go":       {Data: []byte("package t\n")},
	"a/testdata/t/t.go":     {Data: []byte("package t\n")},
	"_x/a/a.go":             {Data: []byte("package a\n")},
	"a/_u/u.go":             {Data: []byte("package u\n")},
	".x/a/a.go":             {Data: []byte("package a\n")},
	"nested/nested.go":      {Data: []byte("package nested\n")},
	"nested/go.mod":         {Data: []byte("module example.com/nested\n")},
	"nested/deeper/d.go":    {Data: []byte("package deeper\n")},
	"a/b/c/c.go":            {Data: []byte("package c\n")},
	"cmd/tool/main.go":      {Data: []byte("package main\n")},
	"onlytest/x_test.go":    {Data: []byte("package onlytest\n")},
	"Upper/Case/c.go":       {Data: []byte("package c\n")},
	"dot.dir/p/p.go":        {Data: []byte("package p\n")},
	"x.go/xx.go":            {Data: []byte("package xx\n")},
	"empty/.keep":           {Data: []byte("")},
	"docs/readme.md":        {Data: []byte("text")},
}

// recordedGoList is what `go list ./...` printed for recordedTree. It lists
// node_modules and a directory that holds only a test file; it does not list
// vendor, testdata, _x, .x, the nested module and what is below them.
var recordedGoList = []string{
	"example.com/m",
	"example.com/m/Upper/Case",
	"example.com/m/a/b/c",
	"example.com/m/a/node_modules/n",
	"example.com/m/cmd/tool",
	"example.com/m/dot.dir/p",
	"example.com/m/node_modules/x",
	"example.com/m/onlytest",
	"example.com/m/pkg/a",
	"example.com/m/x.go",
}

// The gate's packages are the recorded go list answer, but for the directory
// that holds only a test file (it has no statement, so nothing to be missing).
func TestTheGateWalksTheTreeAsGoListDoes(t *testing.T) {
	t.Parallel()
	got, err := ModulePackages(recordedTree)
	var want []string
	for _, pkg := range recordedGoList {
		if pkg != "example.com/m/onlytest" {
			want = append(want, pkg)
		}
	}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("packages\n%v\nwant (the recorded go list answer without the test-only package)\n%v\n%v", got, want, err)
	}
	// Both ways, as a guard against the list and the tree drifting apart: every
	// walked directory is in the recorded answer, test-only ones included.
	_, dirs, err := walkPackages(recordedTree)
	if err != nil {
		t.Fatal(err)
	}
	var walked []string
	for _, dir := range dirs {
		walked = append(walked, path.Join("example.com/m", dir.path))
	}
	slices.Sort(walked)
	if !slices.Equal(walked, recordedGoList) {
		t.Fatalf("walked\n%v\nwant the recorded go list answer\n%v", walked, recordedGoList)
	}
}

// The gate refuses every TestMain, in whatever form.
func TestTheGateFindsEveryTestMain(t *testing.T) {
	t.Parallel()
	file := func(body string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte("package p\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\n" + body)}
	}
	module := &fstest.MapFile{Data: []byte("module m\n")}
	tests := []struct {
		name  string
		files fstest.MapFS
		want  []string
	}{
		{"none", fstest.MapFS{"go.mod": module, "p/p_test.go": file("func TestX(t *testing.T) {}\n")}, nil},
		{"one that exits before the tests run", fstest.MapFS{"go.mod": module, "p/p_test.go": file("func TestMain(m *testing.M) { os.Exit(0) }\n")}, []string{"p/p_test.go"}},
		{"one that runs them and then exits 0", fstest.MapFS{"go.mod": module, "p/p_test.go": file("func TestMain(m *testing.M) {\n\tm.Run()\n\tos.Exit(0)\n}\n")}, []string{"p/p_test.go"}},
		{"one that passes the result on", fstest.MapFS{"go.mod": module, "p/p_test.go": file("func TestMain(m *testing.M) { os.Exit(m.Run()) }\n")}, []string{"p/p_test.go"}},
		{"in a package of the root and in a test-only directory", fstest.MapFS{"go.mod": module, "r_test.go": file("func TestMain(m *testing.M) { os.Exit(0) }\n"), "q/q_test.go": file("func TestMain(m *testing.M) { os.Exit(0) }\n")}, []string{"q/q_test.go", "r_test.go"}},
		{"in a directory that holds a source file", fstest.MapFS{"go.mod": module, "p/p.go": &fstest.MapFile{Data: []byte("package p\n")}, "p/p_test.go": file("func TestMain(m *testing.M) { os.Exit(0) }\n")}, []string{"p/p_test.go"}},
		{"in node_modules, which go test walks", fstest.MapFS{"go.mod": module, "node_modules/x/x.go": &fstest.MapFile{Data: []byte("package x\n")}, "node_modules/x/x_test.go": file("func TestMain(m *testing.M) { os.Exit(0) }\n")}, []string{"node_modules/x/x_test.go"}},
		{"not in testdata, vendor, a nested module or a hidden directory", fstest.MapFS{"go.mod": module, "testdata/t_test.go": file("func TestMain(m *testing.M) {}\n"), "vendor/v/v_test.go": file("func TestMain(m *testing.M) {}\n"), "n/go.mod": module, "n/n_test.go": file("func TestMain(m *testing.M) {}\n"), ".h/h_test.go": file("func TestMain(m *testing.M) {}\n")}, nil},
		{"a method or a function of another name is no TestMain", fstest.MapFS{"go.mod": module, "p/p_test.go": file("type T struct{}\n\nfunc (T) TestMain(m *testing.M) {}\n\nfunc TestMainX(m *testing.M) {}\n\nfunc testMain() { _ = os.Args }\n")}, nil},
		{"a non-test file with a TestMain is not read", fstest.MapFS{"go.mod": module, "p/p.go": file("func TestMain(m *testing.M) { os.Exit(0) }\n")}, nil},
	}
	for _, tc := range tests {
		got, err := TestMains(tc.files)
		if err != nil || !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v, %v; want %v", tc.name, got, err, tc.want)
		}
	}
	for name, files := range map[string]fs.FS{
		"no go.mod":                  fstest.MapFS{},
		"a file that does not parse": fstest.MapFS{"go.mod": module, "p/p_test.go": &fstest.MapFile{Data: []byte("package")}},
		"an unreadable file":         unreadableFS{fstest.MapFS{"go.mod": module, "p/p_test.go": file("")}},
		"an unreadable tree":         unlistableTree{tree},
	} {
		if got, err := TestMains(files); err == nil {
			t.Errorf("%s: %v, want an error", name, got)
		}
	}
}

// unreadableFS lists its files but cannot read the test file.
type unreadableFS struct{ fstest.MapFS }

func (u unreadableFS) ReadFile(name string) ([]byte, error) {
	if strings.HasSuffix(name, "_test.go") {
		return nil, errors.New("denied")
	}
	return u.MapFS.ReadFile(name)
}

func TestModulePackagesFindsWhatGoListFinds(t *testing.T) {
	t.Parallel()
	got, err := ModulePackages(tree)
	if err != nil || !slices.Equal(got, []string{"m", "m/cmd/tool"}) {
		t.Fatalf("packages = %v, %v", got, err)
	}
	if _, err := ModulePackages(fstest.MapFS{}); err == nil {
		t.Fatal("a tree without go.mod has no packages")
	}
	if _, err := ModulePackages(fstest.MapFS{"go.mod": {Data: []byte("go 1.27\n")}}); err == nil {
		t.Fatal("a go.mod without a module line has no packages")
	}
	quoted, err := ModulePackages(fstest.MapFS{"go.mod": {Data: []byte("module \"q\"\n")}, "x.go": {Data: []byte("package q\n")}})
	if err != nil || !slices.Equal(quoted, []string{"q"}) {
		t.Fatalf("quoted module = %v, %v", quoted, err)
	}
	if _, err := ModulePackages(brokenTree{tree}); err == nil {
		t.Fatal("an unreadable tree is an error")
	}
	if _, err := ModulePackages(unlistableTree{tree}); err == nil {
		t.Fatal("a directory that cannot be listed is an error")
	}
}

// withTestFile is tree with another content for a_test.go.
func withTestFile(base fstest.MapFS, content string) fstest.MapFS {
	files := fstest.MapFS{}
	for name, file := range base {
		files[name] = file
	}
	files["a_test.go"] = &fstest.MapFile{Data: []byte(content)}
	return files
}

// brokenTree cannot be examined at its root: the walk reports an error.
type brokenTree struct{ fstest.MapFS }

func (b brokenTree) Stat(name string) (fs.FileInfo, error) {
	if name == "." {
		return nil, errors.New("denied")
	}
	return b.MapFS.Stat(name)
}

// unlistableTree opens "docs" as a directory that cannot be read.
type unlistableTree struct{ fstest.MapFS }

func (u unlistableTree) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == "docs" {
		return nil, errors.New("cannot list")
	}
	return u.MapFS.ReadDir(name)
}

func TestMissingFindsAPackageThatIsAbsentFromTheProfile(t *testing.T) {
	t.Parallel()
	profile := Profile{Blocks: []Block{
		{Location: "m/a.go:1.1,2.2", Statements: 3, Hit: true},
		{Location: "m/cmd/tool/main.go:1.1,2.2", Statements: 0, Hit: true},
		{Location: "no colon", Statements: 1, Hit: true},
	}}
	if got := profile.Missing([]string{"m", "m/cmd/tool", "m/other"}); !slices.Equal(got, []string{"m/cmd/tool", "m/other"}) {
		t.Fatalf("missing = %v: a package with no statements counts as missing", got)
	}
	if got := profile.Missing(nil); got != nil {
		t.Fatalf("missing = %v", got)
	}
}

// whole is a profile that holds both packages of tree.
const whole = "mode: atomic\nm/a.go:1.1,2.2 3 1\nm/cmd/tool/main.go:1.1,2.2 2 1\n"

func TestRun(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		args   []string
		open   func(string) (io.ReadCloser, error)
		code   int
		stdout string
		stderr string
	}{
		{"complete", []string{"c.out"}, opener(whole, nil), 0, "statements covered: 5 of 5\n", ""},
		{"uncovered", []string{"c.out"}, opener(whole+"m/b.go:1.1,2.2 1 0\n", nil), 1, "statements covered: 5 of 6\n", "uncovered: m/b.go:1.1,2.2 (1 statements)"},
		// What a TestMain that exits 0 does: the package is gone from the profile,
		// the rest is complete, and the totals alone would read 100%.
		{"a package missing from the profile", []string{"c.out"}, opener("mode: atomic\nm/a.go:1.1,2.2 3 1\n", nil), 1, "statements covered: 3 of 3\n", "package m/cmd/tool has Go files but no statement in the profile"},
		{"no module", []string{"c.out"}, opener(whole, nil), 2, "", "the packages of the module: open go.mod"},
		{"a TestMain", []string{"c.out"}, opener(whole, nil), 1, "statements covered: 5 of 5\n", "testmain: a_test.go declares TestMain"},
		{"a test file that does not parse", []string{"c.out"}, opener(whole, nil), 2, "", "the test files of the module: a_test.go does not parse"},
		{"no arguments", nil, opener("", nil), 2, "", "usage: covergate"},
		{"two arguments", []string{"a", "b"}, opener("", nil), 2, "", "usage: covergate"},
		{"unreadable", []string{"c.out"}, opener("", errors.New("boom")), 2, "", "covergate: boom"},
		{"malformed", []string{"c.out"}, opener("junk\n", nil), 2, "", "covergate: c.out: line 1"},
		{"empty", []string{"c.out"}, opener("mode: set\n", nil), 1, "statements covered: 0 of 0\n", "0 of 0 statements are not covered"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			root := fs.FS(tree)
			switch tc.name {
			case "no module":
				root = fstest.MapFS{}
			case "a TestMain":
				root = withTestFile(tree, "package m\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestMain(m *testing.M) {\n\tm.Run()\n\tos.Exit(0)\n}\n")
			case "a test file that does not parse":
				root = withTestFile(tree, "package")
			}
			if code := Run(tc.args, &stdout, &stderr, tc.open, root); code != tc.code {
				t.Fatalf("code = %d, want %d (stderr %q)", code, tc.code, stderr.String())
			}
			if stdout.String() != tc.stdout || !strings.Contains(stderr.String(), tc.stderr) {
				t.Fatalf("stdout %q stderr %q", stdout.String(), stderr.String())
			}
		})
	}
}
