// Package covergate decides whether a Go cover profile covers every
// statement. It is the repository's coverage gate: exact (covered statements
// are compared with total statements, never a rounded percentage) and without
// any input that lowers the bar.
package covergate

import (
	"bufio"
	"errors"
	"fmt"
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

// ModulePackages returns the import paths of the packages of the module whose
// root is fsys: the directories that hold a Go file that is not a test file,
// found the way `go list ./...` finds them (a directory that starts with . or _,
// testdata, vendor and node_modules are skipped, and so is a directory that has
// its own go.mod, which is another module). It reads the file tree, so the gate
// and its tests start no process.
func ModulePackages(fsys fs.FS) ([]string, error) {
	mod, err := fs.ReadFile(fsys, "go.mod")
	if err != nil {
		return nil, err
	}
	module := ""
	for _, line := range strings.Split(string(mod), "\n") {
		if name, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			module = strings.Trim(strings.TrimSpace(name), "\"")
			break
		}
	}
	if module == "" {
		return nil, errors.New("go.mod names no module")
	}
	var packages []string
	err = fs.WalkDir(fsys, ".", func(dir string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		if dir != "." {
			base := path.Base(dir)
			if strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_") || base == "testdata" || base == "vendor" || base == "node_modules" {
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
		for _, file := range files {
			if name := file.Name(); !file.IsDir() && strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
				packages = append(packages, path.Join(module, dir))
				break
			}
		}
		return nil
	})
	sort.Strings(packages)
	return packages, err
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
	covered, total := profile.Totals()
	_, _ = fmt.Fprintf(stdout, "statements covered: %d of %d\n", covered, total)
	missing := profile.Missing(packages)
	for _, pkg := range missing {
		_, _ = fmt.Fprintf(stderr, "missing: package %s has Go files but no statement in the profile (were its tests run?)\n", pkg)
	}
	if profile.Complete() && len(missing) == 0 {
		return 0
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
