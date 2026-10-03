package covergate

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
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
		{"complete", []string{"c.out"}, opener(complete, nil), 0, "statements covered: 5 of 5\n", ""},
		{"missing", []string{"c.out"}, opener(complete+"b.go:1.1,2.2 1 0\n", nil), 1, "statements covered: 5 of 6\n", "uncovered: b.go:1.1,2.2 (1 statements)"},
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
			if code := Run(tc.args, &stdout, &stderr, tc.open); code != tc.code {
				t.Fatalf("code = %d, want %d (stderr %q)", code, tc.code, stderr.String())
			}
			if stdout.String() != tc.stdout || !strings.Contains(stderr.String(), tc.stderr) {
				t.Fatalf("stdout %q stderr %q", stdout.String(), stderr.String())
			}
		})
	}
}
