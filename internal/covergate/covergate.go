// Package covergate decides whether a Go cover profile covers every
// statement. It is the repository's coverage gate: exact (covered statements
// are compared with total statements, never a rounded percentage) and without
// any input that lowers the bar.
package covergate

import (
	"bufio"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
)

// RequiredPercent is the share of statements that must be covered. It is a
// constant on purpose: no flag, environment variable or argument changes it,
// and a test fails if it is anything other than 100.
const RequiredPercent = 100

// Block is one statement block of a cover profile, merged across the test
// binaries that reported it.
type Block struct {
	Location   string
	Statements int
	Hit        bool
}

// Profile is a parsed cover profile.
type Profile struct {
	Blocks []Block
}

// Parse reads a cover profile (any mode). Repeated blocks, as when several test
// binaries cover the same package, are merged: a block is hit when any report
// of it was hit.
func Parse(r io.Reader) (Profile, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	merged := map[string]*Block{}
	var order []string
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || (line == 1 && strings.HasPrefix(text, "mode:")) {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) != 3 {
			return Profile{}, fmt.Errorf("line %d: expected \"location statements count\", got %q", line, text)
		}
		statements, err := strconv.Atoi(fields[1])
		if err != nil || statements < 0 {
			return Profile{}, fmt.Errorf("line %d: bad statement count %q", line, fields[1])
		}
		count, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || count < 0 {
			return Profile{}, fmt.Errorf("line %d: bad hit count %q", line, fields[2])
		}
		block, seen := merged[fields[0]]
		if !seen {
			block = &Block{Location: fields[0], Statements: statements}
			merged[fields[0]] = block
			order = append(order, fields[0])
		}
		block.Hit = block.Hit || count > 0
	}
	if err := scanner.Err(); err != nil {
		return Profile{}, err
	}
	sort.Strings(order)
	profile := Profile{}
	for _, key := range order {
		profile.Blocks = append(profile.Blocks, *merged[key])
	}
	return profile, nil
}

// Totals returns the number of covered and of all statements.
func (p Profile) Totals() (covered, total int) {
	for _, block := range p.Blocks {
		total += block.Statements
		if block.Hit {
			covered += block.Statements
		}
	}
	return covered, total
}

// Uncovered returns the blocks that were never executed, in profile order.
func (p Profile) Uncovered() []Block {
	var missed []Block
	for _, block := range p.Blocks {
		if !block.Hit && block.Statements > 0 {
			missed = append(missed, block)
		}
	}
	return missed
}

// Complete reports whether the covered statements reach RequiredPercent of the
// total, in exact integer arithmetic. A profile with no statements is never
// complete: an empty profile means the tests measured nothing.
func (p Profile) Complete() bool {
	covered, total := p.Totals()
	return total > 0 && covered*100 >= total*RequiredPercent
}

// packageDir is a directory of the module that `go list ./...` lists as a
// package: its files, as the walk found them.
type packageDir struct {
	path  string // the directory, relative to the module root ("." for the root)
	files []fs.DirEntry
}

// walkPackages finds the directories `go list ./...` lists in module mode. That
// is every directory below the module root that holds a Go file (a test file
// included), except a directory whose name starts with . or _, testdata and
// vendor (and everything below them), and a directory that has a go.mod of its
// own, which is another module. Not node_modules, which go list does not skip.
// It reads the file tree, so the gate and its tests start no process. The rule
// was recorded from `go list ./...` of Go 1.27.1 on a tree that holds each of
// these cases, and the test of this package builds the same tree.
func walkPackages(fsys fs.FS) (module string, dirs []packageDir, err error) {
	mod, err := fs.ReadFile(fsys, "go.mod")
	if err != nil {
		return "", nil, err
	}
	for _, line := range strings.Split(string(mod), "\n") {
		if name, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			module = strings.Trim(strings.TrimSpace(name), "\"")
			break
		}
	}
	if module == "" {
		return "", nil, errors.New("go.mod names no module")
	}
	err = fs.WalkDir(fsys, ".", func(dir string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		if dir != "." {
			base := path.Base(dir)
			if strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_") || base == "testdata" || base == "vendor" {
				return fs.SkipDir
			}
			if _, err := fs.Stat(fsys, path.Join(dir, "go.mod")); err == nil {
				return fs.SkipDir
			}
		}
		files, err := fs.ReadDir(fsys, dir)
		if err != nil {
			return err
		}
		var goFiles []fs.DirEntry
		for _, file := range files {
			if !file.IsDir() && strings.HasSuffix(file.Name(), ".go") {
				goFiles = append(goFiles, file)
			}
		}
		if len(goFiles) > 0 {
			dirs = append(dirs, packageDir{path: dir, files: goFiles})
		}
		return nil
	})
	return module, dirs, err
}

// ModulePackages returns the import paths of the packages of the module whose
// root is fsys that have a Go file that is not a test file, which is where
// statements are: the packages `go list ./...` lists (see walkPackages), but for
// those that hold test files only.
func ModulePackages(fsys fs.FS) ([]string, error) {
	module, dirs, err := walkPackages(fsys)
	if err != nil {
		return nil, err
	}
	var packages []string
	for _, dir := range dirs {
		for _, file := range dir.files {
			if !strings.HasSuffix(file.Name(), "_test.go") {
				packages = append(packages, path.Join(module, dir.path))
				break
			}
		}
	}
	sort.Strings(packages)
	return packages, nil
}

// TestMains returns the test files of the module that declare a TestMain, read
// from the source. The gate refuses every one: a TestMain that exits before it
// runs the tests (os.Exit(0)) leaves its package out of the profile, and one
// that runs them and then exits 0 (m.Run(); os.Exit(0)) hides a failing test and
// still leaves coverage at 100%. A file that does not parse is an error.
func TestMains(fsys fs.FS) ([]string, error) {
	_, dirs, err := walkPackages(fsys)
	if err != nil {
		return nil, err
	}
	var found []string
	fset := token.NewFileSet()
	for _, dir := range dirs {
		for _, entry := range dir.files {
			if !strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			name := path.Join(dir.path, entry.Name())
			source, err := fs.ReadFile(fsys, name)
			if err != nil {
				return nil, err
			}
			file, err := parser.ParseFile(fset, name, source, parser.SkipObjectResolution)
			if err != nil {
				return nil, fmt.Errorf("%s does not parse: %w", name, err)
			}
			for _, decl := range file.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == "TestMain" {
					found = append(found, name)
				}
			}
		}
	}
	sort.Strings(found)
	return found, nil
}

// Missing returns the packages, of those given, that contribute no statement to
// the profile: a package whose tests were not run (a TestMain that exits, a
// package without tests) is absent from a profile, and the totals of the
// others would still read 100%.
func (p Profile) Missing(packages []string) []string {
	seen := map[string]bool{}
	for _, block := range p.Blocks {
		if file, _, ok := strings.Cut(block.Location, ":"); ok && block.Statements > 0 {
			seen[path.Dir(file)] = true
		}
	}
	var missing []string
	for _, pkg := range packages {
		if !seen[pkg] {
			missing = append(missing, pkg)
		}
	}
	return missing
}

// Run is the gate command: Run(["cover.out"], ...) prints the totals and
// returns 0 when every statement is covered, 1 when any is not, 2 for a usage
// or read error. It takes exactly one argument, the profile path. root is the
// module's file tree: every package of the module must be in the profile.
func Run(args []string, stdout, stderr io.Writer, open func(string) (io.ReadCloser, error), root fs.FS) int {
	if len(args) != 1 {
		_, _ = fmt.Fprintln(stderr, "usage: covergate <cover profile>")
		return 2
	}
	file, err := open(args[0])
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "covergate: %v\n", err)
		return 2
	}
	defer func() { _ = file.Close() }()
	profile, err := Parse(file)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "covergate: %s: %v\n", args[0], err)
		return 2
	}
	packages, err := ModulePackages(root)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "covergate: the packages of the module: %v\n", err)
		return 2
	}
	testMains, err := TestMains(root)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "covergate: the test files of the module: %v\n", err)
		return 2
	}
	covered, total := profile.Totals()
	_, _ = fmt.Fprintf(stdout, "statements covered: %d of %d\n", covered, total)
	for _, file := range testMains {
		_, _ = fmt.Fprintf(stderr, "testmain: %s declares TestMain, which the gate refuses: one that exits hides its package from the profile, or hides a failing test\n", file)
	}
	missing := profile.Missing(packages)
	for _, pkg := range missing {
		_, _ = fmt.Fprintf(stderr, "missing: package %s has Go files but no statement in the profile (its tests were not run, or it has no statement)\n", pkg)
	}
	if profile.Complete() && len(missing) == 0 && len(testMains) == 0 {
		return 0
	}
	if len(testMains) > 0 {
		_, _ = fmt.Fprintf(stderr, "covergate: %d test file(s) of the module declare TestMain\n", len(testMains))
	}
	if len(missing) > 0 {
		_, _ = fmt.Fprintf(stderr, "covergate: %d package(s) of the module are not in the profile\n", len(missing))
	}
	if !profile.Complete() {
		for _, block := range profile.Uncovered() {
			_, _ = fmt.Fprintf(stderr, "uncovered: %s (%d statements)\n", block.Location, block.Statements)
		}
		_, _ = fmt.Fprintf(stderr, "covergate: %d of %d statements are not covered; %d%% is required\n", total-covered, total, RequiredPercent)
	}
	return 1
}
