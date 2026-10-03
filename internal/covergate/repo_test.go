package covergate

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// The coverage gate measures what the tests run. Code behind a build constraint
// is not run by every test run, and the gate would not see it, so no Go file of
// the repository may have a constraint: not in its first lines, and not in its
// name (_linux.go, _arm64.go, _windows_amd64_test.go ...).

var (
	knownOS   = []string{"aix", "android", "darwin", "dragonfly", "freebsd", "hurd", "illumos", "ios", "js", "linux", "nacl", "netbsd", "openbsd", "plan9", "solaris", "wasip1", "windows", "zos"}
	knownArch = []string{"386", "amd64", "amd64p32", "arm", "armbe", "arm64", "arm64be", "loong64", "mips", "mipsle", "mips64", "mips64le", "mips64p32", "mips64p32le", "ppc", "ppc64", "ppc64le", "riscv", "riscv64", "s390", "s390x", "sparc", "sparc64", "wasm"}
)

// constraintProblems lists the Go files of fsys that carry a build constraint.
func constraintProblems(fsys fs.FS) []string {
	var problems []string
	err := fs.WalkDir(fsys, ".", func(name string, entry fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case entry.IsDir() && (name == ".git" || name == "node_modules"):
			return fs.SkipDir
		case entry.IsDir() || !strings.HasSuffix(name, ".go"):
			return nil
		}
		stem := strings.TrimSuffix(strings.TrimSuffix(path.Base(name), ".go"), "_test")
		parts := strings.Split(stem, "_")
		if n := len(parts); n > 1 && (slices.Contains(knownOS, parts[n-1]) || slices.Contains(knownArch, parts[n-1])) {
			problems = append(problems, name+": the file name is a build constraint ("+parts[n-1]+")")
		}
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "package ") {
				break // constraints come before the package clause
			}
			if strings.HasPrefix(trimmed, "//go:build") || strings.HasPrefix(trimmed, "// +build") {
				problems = append(problems, name+": "+trimmed)
			}
		}
		return nil
	})
	if err != nil {
		problems = append(problems, err.Error())
	}
	return problems
}

func TestNoGoFileHasABuildConstraint(t *testing.T) {
	t.Parallel()
	if problems := constraintProblems(os.DirFS("../..")); len(problems) > 0 {
		t.Fatalf("the coverage gate cannot see code behind a build constraint:\n%s", strings.Join(problems, "\n"))
	}
}

func TestTheBuildConstraintCheckSeesEveryKind(t *testing.T) {
	t.Parallel()
	const constraint = "//go:build " + "!race\n\npackage x\n"
	files := fstest.MapFS{
		"ok.go":                       {Data: []byte("package x\n")},
		"ok_test.go":                  {Data: []byte("// a comment mentioning go:build in text\npackage x\n")},
		"notes_windows.txt":           {Data: []byte("not Go")},
		"sub/ok.go":                   {Data: []byte("package x\n")},
		"constraint.go":               {Data: []byte(constraint)},
		"old.go":                      {Data: []byte("// +build ignore\n\npackage x\n")},
		"sub/deep/constraint_test.go": {Data: []byte("\n  " + constraint)},
		"file_linux.go":               {Data: []byte("package x\n")},
		"file_arm64_test.go":          {Data: []byte("package x\n")},
		"file_windows_amd64.go":       {Data: []byte("package x\n")},
		"node_modules/x/bad_linux.go": {Data: []byte("package x\n")},
		".git/bad_linux.go":           {Data: []byte("package x\n")},
	}
	got := constraintProblems(files)
	want := []string{"constraint.go", "file_arm64_test.go", "file_linux.go", "file_windows_amd64.go", "old.go", "sub/deep/constraint_test.go"}
	var names []string
	for _, p := range got {
		names = append(names, p[:strings.Index(p, ":")])
	}
	slices.Sort(names)
	names = slices.Compact(names)
	if !slices.Equal(names, want) {
		t.Fatalf("flagged %v, want %v\n%v", names, want, got)
	}
	if got := constraintProblems(brokenFS{}); len(got) != 1 || !strings.Contains(got[0], "denied") {
		t.Fatalf("an unreadable tree is a problem: %v", got)
	}
}

// brokenFS is a file system whose root cannot be read.
type brokenFS struct{}

func (brokenFS) Open(string) (fs.File, error) {
	return nil, &fs.PathError{Op: "open", Path: ".", Err: errors.New("denied")}
}

// The gate demands a statement from every package of the repository. The file
// tree of this repository is the one it reads in CI, so it must find the
// packages that matter here and none of the nested module of the mutation run.
func TestTheGateFindsThePackagesOfThisRepository(t *testing.T) {
	t.Parallel()
	packages, err := ModulePackages(os.DirFS("../.."))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"github.com/meaninggraph/cli/pkg/meaning", "github.com/meaninggraph/cli/internal/cli", "github.com/meaninggraph/cli/cmd/meaninggraph", "github.com/meaninggraph/cli/internal/covergate"} {
		if !slices.Contains(packages, want) {
			t.Errorf("%s is not among %v", want, packages)
		}
	}
	for _, pkg := range packages {
		if strings.Contains(pkg, "scripts") || strings.Contains(pkg, "testdata") {
			t.Errorf("%s is not a package of this module", pkg)
		}
	}
}
