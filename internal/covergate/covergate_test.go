package covergate

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
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
	"node_modules/n/n.go":          {Data: []byte("package n\n")},
	"_hidden/h.go":                 {Data: []byte("package h\n")},
	".hidden/h.go":                 {Data: []byte("package h\n")},
	"scripts/go.mod":               {Data: []byte("module other\n")},
	"scripts/main.go":              {Data: []byte("package main\n")},
	"docs/readme.md":               {Data: []byte("text")},
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
			if tc.name == "no module" {
				root = fstest.MapFS{}
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
