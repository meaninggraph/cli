// Package covergate decides whether a Go cover profile covers every
// statement. It is the repository's coverage gate: exact (covered statements
// are compared with total statements, never a rounded percentage) and without
// any input that lowers the bar.
package covergate

import (
	"bufio"
	"fmt"
	"io"
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

// Run is the gate command: Run(["cover.out"], ...) prints the totals and
// returns 0 when every statement is covered, 1 when any is not, 2 for a usage
// or read error. It takes exactly one argument, the profile path.
func Run(args []string, stdout, stderr io.Writer, open func(string) (io.ReadCloser, error)) int {
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
	covered, total := profile.Totals()
	_, _ = fmt.Fprintf(stdout, "statements covered: %d of %d\n", covered, total)
	if profile.Complete() {
		return 0
	}
	for _, block := range profile.Uncovered() {
		_, _ = fmt.Fprintf(stderr, "uncovered: %s (%d statements)\n", block.Location, block.Statements)
	}
	_, _ = fmt.Fprintf(stderr, "covergate: %d of %d statements are not covered; %d%% is required\n", total-covered, total, RequiredPercent)
	return 1
}
