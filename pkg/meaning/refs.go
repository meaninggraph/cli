package meaning

import (
	"regexp"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

const conceptIDPattern = `[a-z][a-z0-9]*(?:-[a-z][a-z0-9]*)*`

var (
	bareRefPattern    = regexp.MustCompile(`^` + conceptIDPattern + `$`)
	conceptRefPattern = regexp.MustCompile(`^meaning://([A-Za-z0-9.-]+(?:/[A-Za-z0-9._-]+)+)/(` + conceptIDPattern + `)(?:\?ref=([A-Za-z0-9._/-]+))?$`)
	modelRefPattern   = regexp.MustCompile(`^modelspec://((?:[A-Za-z0-9.-]+(?:/[A-Za-z0-9._-]+)+)?)/([A-Za-z][A-Za-z0-9_]*)\.([A-Za-z][A-Za-z0-9_]*)(?:\?ref=([A-Za-z0-9._/-]+))?$`)
)

// ConceptRef is a parsed reference to a concept: a bare id (same graph), or
// meaning://{host}/{org}/{repo}/{id} with an optional ?ref= pin.
type ConceptRef struct {
	// Repo is {host}/{org}/{repo}, empty for a bare id.
	Repo string
	ID   string
	// Pin is the ?ref= value, empty when there is none.
	Pin string
}

// ParseConceptRef parses a concept reference, as the format defines it.
func ParseConceptRef(ref string) (ConceptRef, bool) {
	if bareRefPattern.MatchString(ref) {
		return ConceptRef{ID: ref}, true
	}
	m := conceptRefPattern.FindStringSubmatch(ref)
	if m == nil {
		return ConceptRef{}, false
	}
	return ConceptRef{Repo: m[1], ID: m[2], Pin: m[3]}, true
}

// lower lower-cases the whole string by Unicode rules, language-neutrally and
// with context (a final sigma), as JavaScript's toLowerCase does: labels and
// aliases are matched ignoring case.
func lower(s string) string {
	return cases.Lower(language.Und).String(s)
}

func an(kind string) string {
	if strings.ContainsAny(kind[:min(1, len(kind))], "aeiou") {
		return "an " + kind
	}
	return "a " + kind
}
