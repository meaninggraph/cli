package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meaninggraph/cli/pkg/meaning"
)

type checkOptions struct {
	format, profile string
	graphs          []string
	// addresses are the --address flags as written; addressFlags are the same, parsed.
	addresses    []string
	addressFlags []addressFlag
}

// addressFlag is one --address: <address>, or <address>=<path> (the path of the
// graph that has that address, one of the paths being checked).
type addressFlag struct{ spec, address, path string }

func newCheckCommand(env Env) *cobra.Command {
	var o checkOptions
	cmd := &cobra.Command{
		Use:   "check [path...]",
		Short: "Check meaning files: schema, references, extends and bindings",
		Long: `Check meaning files against the meaning/draft-1 schema and the rules the
schema cannot say: every reference resolves, extends joins compatible kinds
without a cycle, values-of and units-of name entities, measures and ratios are
consistent, ids and words are unique, and every binding names an existing entity
and property of the ModelSpec model its file lists in "models" and fits its role.

A path is a directory, whose *.meaning.yaml files directly in it are one graph
(files in subdirectories are not part of it), or a file; all the files named
together are one more graph. The default path is the current directory.

References to other graphs are read from directories you supply with --graph:
nothing is fetched. A reference to a graph that was not supplied is an error.

--address gives a graph that is being checked its address, so that its own
references to itself resolve: bare (--address <address>) when one graph is
checked, else once per path as --address <address>=<path>. A directory that is
checked and also given with --graph <address>=<directory> is that graph, with
that address, and is checked once, in full: one run checks a repository and the
dependency it supplied.

Exit codes: 0 no error found, 1 at least one finding of error severity, 2 wrong
usage or a file that cannot be read.`,
		Example: `  meaninggraph check
  meaninggraph check model --graph github.com/meaninggraph/core=../core
  meaninggraph check . --address github.com/meaninggraph/core --profile universal --format json
  meaninggraph check model ../core --address github.com/org/data=model --graph github.com/meaninggraph/core=../core`,
		RunE: func(cmd *cobra.Command, args []string) error { return o.run(cmd, env, args) },
	}
	flags := cmd.Flags()
	flags.StringVar(&o.format, "format", "text", "output format: text or json")
	flags.StringArrayVar(&o.graphs, "graph", nil, "another graph references may name, as <host>/<org>/<repo>=<directory>; repeatable")
	flags.StringArrayVar(&o.addresses, "address", nil, "the address of a graph being checked, <host>/<org>/<repo>, so that references to itself resolve: bare for one graph, else <address>=<path> once per checked path; repeatable")
	flags.StringVar(&o.profile, "profile", "", `extra rules: "universal" for a repository of universal concepts such as meaninggraph/core`)
	return cmd
}

var addressPattern = regexp.MustCompile(`^[A-Za-z0-9.-]+(?:/[A-Za-z0-9._-]+)+$`)

func (o *checkOptions) run(cmd *cobra.Command, env Env, args []string) error {
	profile, err := o.validate()
	if err != nil {
		return err
	}
	supplied, err := loadSupplied(env.FS, o.graphs)
	if err != nil {
		return err
	}
	targets, err := loadTargets(env, args)
	if err != nil {
		return err
	}
	checked, err := o.assignAddresses(env, targets, supplied)
	if err != nil {
		return err
	}
	if profile == meaning.ProfileUniversal && slices.ContainsFunc(targets, func(t target) bool { return t.graph.Dir == "" }) {
		return fmt.Errorf("--profile universal checks a repository: name its directory, not files")
	}
	var reports []graphReport
	asked := map[string]bool{}
	for _, t := range targets {
		reports = append(reports, t.check(env, supplied, profile, asked))
	}
	slices.SortFunc(reports, func(a, b graphReport) int { return strings.Compare(a.label(), b.label()) })
	reports[0].add(unusedGraphs(reports[0].Paths[0], supplied, asked, checked)...)
	if err := o.write(cmd.OutOrStdout(), reports); err != nil {
		return err
	}
	if slices.ContainsFunc(reports, func(r graphReport) bool { return r.Errors > 0 }) {
		return errFindings
	}
	return nil
}

func (o *checkOptions) validate() (meaning.Profile, error) {
	if o.format != "text" && o.format != "json" {
		return "", fmt.Errorf("invalid --format %q: expected text or json", o.format)
	}
	for _, spec := range o.addresses {
		address, path, named := strings.Cut(spec, "=")
		if !addressPattern.MatchString(address) || (named && path == "") {
			return "", fmt.Errorf("invalid --address %q: expected <host>/<org>/<repo>, or <host>/<org>/<repo>=<path> to name the graph it is for", spec)
		}
		o.addressFlags = append(o.addressFlags, addressFlag{spec, address, path})
	}
	switch p := meaning.Profile(o.profile); p {
	case meaning.ProfileDefault, meaning.ProfileUniversal:
		return p, nil
	}
	return "", fmt.Errorf("invalid --profile %q: expected universal", o.profile)
}

// loadSupplied reads the graphs given with --graph.
func loadSupplied(fsys meaning.FS, specs []string) (map[string]*meaning.Graph, error) {
	supplied := map[string]*meaning.Graph{}
	for _, spec := range specs {
		address, dir, ok := strings.Cut(spec, "=")
		if !ok || !addressPattern.MatchString(address) || dir == "" {
			return nil, fmt.Errorf("invalid --graph %q: expected <host>/<org>/<repo>=<directory>", spec)
		}
		if _, dup := supplied[address]; dup {
			return nil, fmt.Errorf("--graph %s is given twice", address)
		}
		g, err := meaning.LoadDir(fsys, dir)
		if err != nil {
			return nil, fmt.Errorf("--graph %s: %w", address, err)
		}
		g.Address = address
		supplied[address] = g
	}
	return supplied, nil
}

// target is one graph to check and the paths it was read from. owns holds the
// absolute form of each path, and dir the absolute directory of a graph that was
// read from one.
type target struct {
	paths []string
	owns  map[string]bool
	dir   string
	graph *meaning.Graph
}

// loadTargets reads what the paths name: each directory is a graph of its own,
// and the files named together are one more. A path that is the same directory
// or file as an earlier one, written another way, is read once.
func loadTargets(env Env, args []string) ([]target, error) {
	if len(args) == 0 {
		args = []string{"."}
	}
	var targets []target
	var files []string
	owned := map[string]bool{}
	seen := map[string]bool{}
	for _, arg := range args {
		if arg == "" {
			return nil, errors.New("an empty path was given; name a directory or a file (is a variable unset?)")
		}
		path := filepath.Clean(arg)
		abs, err := env.Abs(path)
		if err != nil {
			return nil, err
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true
		fi, err := env.FS.Stat(path)
		if err != nil {
			return nil, err
		}
		if !fi.IsDir() {
			files = append(files, path)
			owned[abs] = true
			continue
		}
		g, err := meaning.LoadDir(env.FS, path)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target{paths: []string{path}, owns: map[string]bool{abs: true}, dir: abs, graph: g})
	}
	if len(files) > 0 {
		slices.Sort(files)
		g, err := meaning.LoadFiles(env.FS, files)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target{paths: files, owns: owned, graph: g})
	}
	return targets, nil
}

// assignAddresses gives each graph that is being checked its address. A graph
// that has none is checked without one, as before. An address comes from
// --address (bare for the only graph, else <address>=<path> for the graph that
// holds the path) or from --graph: a directory that is checked and is also
// supplied with --graph <address>=<directory> is that graph, with that address.
// A contradiction is an error: a graph with two addresses, an address for two
// graphs, an address for a path that is not checked, a bare address when more
// than one graph is checked, a directory supplied under two addresses. It returns
// the addresses of the supplied directories that are checked in this run, which
// are in use.
func (o *checkOptions) assignAddresses(env Env, targets []target, supplied map[string]*meaning.Graph) (map[string]bool, error) {
	abs := func(path string) (string, error) { return env.Abs(filepath.Clean(path)) }
	owner := map[string]int{} // the target that each checked path belongs to
	for i, t := range targets {
		for full := range t.owns {
			owner[full] = i
		}
	}
	addresses := make([]string, len(targets))
	inUse := map[string]bool{} // the addresses of supplied directories that are checked too
	holder := map[string]int{} // the target that each address was given to
	give := func(i int, address, from string) error {
		switch other, taken := holder[address]; {
		case addresses[i] != "" && addresses[i] != address:
			return fmt.Errorf("%s gives the graph at %s the address %s, but another flag gave it the address %s", from, targets[i].label(), address, addresses[i])
		case taken && other != i:
			return fmt.Errorf("%s gives the address %s to the graph at %s, but it is the address of the graph at %s", from, address, targets[i].label(), targets[other].label())
		}
		addresses[i], holder[address] = address, i
		return nil
	}
	for _, a := range o.addressFlags {
		i := 0
		if a.path == "" {
			switch {
			case len(targets) != 1:
				return nil, fmt.Errorf("--address %s names no path, but %d graphs are being checked: write --address %s=<path>, once for each path", a.address, len(targets), a.address)
			case len(o.addressFlags) != 1:
				return nil, fmt.Errorf("--address %s names no path, but there are other --address flags: write --address %s=<path>", a.address, a.address)
			}
		} else {
			full, err := abs(a.path)
			if err != nil {
				return nil, err
			}
			var checked bool
			if i, checked = owner[full]; !checked {
				return nil, fmt.Errorf("--address %s: %s is not one of the paths being checked", a.spec, a.path)
			}
		}
		if addresses[i] != "" {
			return nil, fmt.Errorf("--address %s and an earlier --address both name the graph at %s (%s)", a.spec, targets[i].label(), addresses[i])
		}
		if err := give(i, a.address, "--address "+a.spec); err != nil {
			return nil, err
		}
	}
	for i, t := range targets {
		if t.dir == "" {
			continue // files named together are not a directory that --graph supplies
		}
		var same []string // the addresses --graph gives to this directory
		for _, address := range slices.Sorted(maps.Keys(supplied)) {
			other, err := abs(supplied[address].Dir)
			if err != nil {
				return nil, err
			}
			if other == t.dir {
				same = append(same, address)
			}
		}
		switch {
		case len(same) > 1:
			return nil, fmt.Errorf("%s is given with --graph under %d addresses (%s): a graph has one", t.label(), len(same), strings.Join(same, ", "))
		case len(same) == 1:
			if err := give(i, same[0], "--graph "+same[0]+"="+supplied[same[0]].Dir); err != nil {
				return nil, err
			}
			inUse[same[0]] = true
		}
	}
	for i, t := range targets {
		t.graph.Address = addresses[i]
	}
	return inUse, nil
}

// graphReport is the result of checking one graph.
type graphReport struct {
	Paths    []string          `json:"paths"`
	Address  string            `json:"address,omitempty"`
	Files    int               `json:"files"`
	Concepts int               `json:"concepts"`
	Errors   int               `json:"errors"`
	Warnings int               `json:"warnings"`
	Findings []meaning.Finding `json:"findings"`
}

func (r graphReport) label() string { return strings.Join(r.Paths, " ") }

func (t target) label() string { return strings.Join(t.paths, " ") }

func (t target) check(env Env, supplied map[string]*meaning.Graph, profile meaning.Profile, asked map[string]bool) graphReport {
	base := meaning.GraphResolver(supplied)
	used := map[string]map[string]bool{}
	resolve := func(repo, pin string) (*meaning.Graph, error) {
		asked[repo] = true
		g, err := base(repo, pin)
		if err == nil {
			if used[repo] == nil {
				used[repo] = map[string]bool{}
			}
			used[repo][pin] = true
		}
		return g, err
	}
	findings := meaning.Checker{Resolve: resolve, Profile: profile}.Check(t.graph)
	for _, repo := range slices.Sorted(maps.Keys(used)) {
		for _, pin := range slices.Sorted(maps.Keys(used[repo])) {
			if f, ok := verifyPin(env, t.paths[0], supplied[repo], pin); ok {
				findings = append(findings, f)
			}
		}
	}
	// A graph with no finding has an empty list, not none: JSON says [] for it.
	report := graphReport{Paths: t.paths, Address: t.graph.Address, Files: len(t.graph.Files), Concepts: len(t.graph.Concepts), Findings: []meaning.Finding{}}
	report.add(findings...)
	return report
}

// add appends findings, keeps them sorted and counts them.
func (r *graphReport) add(findings ...meaning.Finding) {
	r.Findings = append(r.Findings, findings...)
	meaning.SortFindings(r.Findings)
	r.Errors, r.Warnings = 0, 0
	for _, f := range r.Findings {
		switch f.Severity {
		case meaning.Error:
			r.Errors++
		case meaning.Warning:
			r.Warnings++
		}
	}
}

// verifyPin compares a pin that was read from a directory given with --graph
// with the checkout that directory is. A commit (40 hexadecimal digits, what
// FORMAT.md advises) must be the commit the checkout is at: equal is fine,
// different is an error, and a directory that is no checkout cannot be verified,
// which is a warning. A branch or a tag (the grammar accepts them, and they can
// move) is looked up in the checkout's refs: the checkout at a commit that the
// name has there is fine; at another it is an error; a name that is not among
// the refs, or may be an annotated tag, cannot be verified, a warning. Only
// files are read: this proves which commit the checkout was made at, not what
// the files in it are.
func verifyPin(env Env, file string, g *meaning.Graph, pin string) (meaning.Finding, bool) {
	commit, err := meaning.CheckoutCommit(env.FS, g.Dir)
	unverified := func(why string) (meaning.Finding, bool) {
		return meaning.Finding{File: file, Rule: "pin-not-verified", Severity: meaning.Warning,
			Message: fmt.Sprintf("meaning://%s?ref=%s was read from %s, which cannot be verified as that version (%s); the pin is trusted, not checked", g.Address, pin, g.Dir, why)}, true
	}
	mismatch := func(format string, args ...any) (meaning.Finding, bool) {
		return meaning.Finding{File: file, Rule: "pin-checkout-mismatch", Severity: meaning.Error,
			Message: fmt.Sprintf("meaning://%s is pinned at %s, but %s, given with --graph, %s; check out the pinned version there", g.Address, pin, g.Dir, fmt.Sprintf(format, args...))}, true
	}
	switch {
	case err != nil:
		return unverified(err.Error())
	case len(pin) == 40 && isHex(pin):
		if commit != pin {
			return mismatch("is a checkout of %s", commit)
		}
		return meaning.Finding{}, false
	}
	targets, err := meaning.RefTargets(env.FS, g.Dir, pin)
	if err != nil {
		return unverified(err.Error())
	}
	if len(targets) == 0 {
		return unverified(fmt.Sprintf("%q is a branch or tag name, which can move, and the checkout has no such ref; pin a commit", pin))
	}
	var seen []string
	maybe := false
	for _, target := range targets {
		if target.ID == commit {
			return meaning.Finding{}, false
		}
		seen = append(seen, fmt.Sprintf("%s is at %s", target.Ref, target.ID))
		maybe = maybe || target.MaybeTagObject
	}
	if maybe {
		return unverified(fmt.Sprintf("%q is a tag that may be annotated, and an annotated tag cannot be compared with the checkout's commit without reading git objects; pin a commit", pin))
	}
	return mismatch("is a checkout of %s, and %s", commit, strings.Join(seen, ", "))
}

func isHex(s string) bool {
	return strings.Trim(s, "0123456789abcdef") == ""
}

// unusedGraphs warns about a graph given with --graph that no reference named,
// and says so when a reference differs from it only by case. A supplied
// directory that is also checked in this run (checked) is in use.
func unusedGraphs(file string, supplied map[string]*meaning.Graph, asked, checked map[string]bool) []meaning.Finding {
	var findings []meaning.Finding
	for _, address := range slices.Sorted(maps.Keys(supplied)) {
		if asked[address] || checked[address] {
			continue
		}
		message := fmt.Sprintf("the graph %s was given with --graph, but no file refers to it", address)
		for _, repo := range slices.Sorted(maps.Keys(asked)) {
			if strings.EqualFold(repo, address) {
				message += fmt.Sprintf("; the files refer to %s, which differs only by case (addresses are compared exactly)", repo)
			}
		}
		findings = append(findings, meaning.Finding{File: file, Rule: "unused-graph", Severity: meaning.Warning, Message: message})
	}
	return findings
}

func (o *checkOptions) write(w io.Writer, reports []graphReport) error {
	if o.format == "json" {
		return writeJSON(w, reports)
	}
	return writeText(w, reports)
}

// writeText prints each finding as file:line: severity: message [rule], then
// one summary line per graph.
func writeText(w io.Writer, reports []graphReport) error {
	var out bytes.Buffer
	for _, r := range reports {
		for _, f := range r.Findings {
			location := f.File
			if f.Line > 0 {
				location = fmt.Sprintf("%s:%d", f.File, f.Line)
			}
			fmt.Fprintf(&out, "%s: %s: %s [%s]\n", location, f.Severity, f.Message, f.Rule)
		}
		if r.Errors == 0 {
			warnings := ""
			if r.Warnings > 0 {
				warnings = ", " + plural(r.Warnings, "warning")
			}
			fmt.Fprintf(&out, "ok: %s: %s, %s%s\n", r.label(), plural(r.Concepts, "concept"), plural(r.Files, "file"), warnings)
			continue
		}
		fmt.Fprintf(&out, "failed: %s: %s, %s, %s, %s\n", r.label(), plural(r.Errors, "error"), plural(r.Warnings, "warning"), plural(r.Concepts, "concept"), plural(r.Files, "file"))
	}
	_, err := w.Write(out.Bytes())
	return err
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

type jsonSchemaInfo struct {
	Format     string `json:"format"`
	CoreCommit string `json:"core_commit"`
}

type jsonReport struct {
	Tool    string         `json:"tool"`
	Version string         `json:"version"`
	Schema  jsonSchemaInfo `json:"schema"`
	OK      bool           `json:"ok"`
	Graphs  []graphReport  `json:"graphs"`
}

func writeJSON(w io.Writer, reports []graphReport) error {
	ok := !slices.ContainsFunc(reports, func(r graphReport) bool { return r.Errors > 0 })
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(jsonReport{
		Tool: "meaninggraph", Version: info.Version, OK: ok, Graphs: reports,
		Schema: jsonSchemaInfo{Format: "meaning/draft-1", CoreCommit: meaning.SchemaCommit()},
	})
}
