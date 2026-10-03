package meaning

import (
	"cmp"
	"slices"
)

// Severity says how serious a finding is. Only errors make a check fail.
type Severity string

// The severities.
const (
	// Error means the files are not valid meaning files; the check fails.
	Error Severity = "error"
	// Warning means something is likely wrong but the Node reference checker
	// accepts it; the check does not fail.
	Warning Severity = "warning"
	// Info states a fact the reader should know, such as which directory
	// stood in for another graph.
	Info Severity = "info"
)

// Finding is one thing a check found.
type Finding struct {
	// File is the path of the file the finding is about, as it was given.
	File string `json:"file"`
	// Line is the 1-based line, or 0 when it is not known.
	Line int `json:"line,omitempty"`
	// Rule is a stable identifier of the rule that was broken.
	Rule     string   `json:"rule"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

// SortFindings orders findings by file, line, rule, severity and message, so
// that the same files always give the same list.
func SortFindings(findings []Finding) {
	slices.SortStableFunc(findings, func(a, b Finding) int {
		return cmp.Or(
			cmp.Compare(a.File, b.File),
			cmp.Compare(a.Line, b.Line),
			cmp.Compare(a.Rule, b.Rule),
			cmp.Compare(a.Severity, b.Severity),
			cmp.Compare(a.Message, b.Message),
		)
	})
}

// HasErrors reports whether any finding is an error.
func HasErrors(findings []Finding) bool {
	return slices.ContainsFunc(findings, func(f Finding) bool { return f.Severity == Error })
}
