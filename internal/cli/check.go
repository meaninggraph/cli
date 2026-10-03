package cli

import (
	"bytes"
	"encoding/json"
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
	format, address, profile string
	graphs                   []string
}

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

Exit codes: 0 no error found, 1 at least one finding of error severity, 2 wrong
usage or a file that cannot be read.`,
		Example: `  meaninggraph check
  meaninggraph check model --graph github.com/meaninggraph/core=../core
  meaninggraph check . --address github.com/meaninggraph/core --profile universal --format json`,
		RunE: func(cmd *cobra.Command, args []string) error { return o.run(cmd, env, args) },
	}
	flags := cmd.Flags()
	flags.StringVar(&o.format, "format", "text", "output format: text or json")
	flags.StringArrayVar(&o.graphs, "graph", nil, "another graph references may name, as <host>/<org>/<repo>=<directory>; repeatable")
	flags.StringVar(&o.address, "address", "", "the address of the graph being checked, <host>/<org>/<repo>, so that references to itself resolve")
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
	targets, err := loadTargets(env.FS, args)
	if err != nil {
		return err
	}
	if o.address != "" && len(targets) != 1 {
		return fmt.Errorf("--address names one graph, but %d graphs are being checked", len(targets))
	}
	if profile == meaning.ProfileUniversal && slices.ContainsFunc(targets, func(t target) bool { return t.graph.Dir == "" }) {
		return fmt.Errorf("--profile universal checks a repository: name its directory, not files")
	}
	var reports []graphReport
	for _, t := range targets {
		t.graph.Address = o.address
		reports = append(reports, t.check(env, supplied, profile))
	}
	slices.SortFunc(reports, func(a, b graphReport) int { return strings.Compare(a.label(), b.label()) })
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
	if o.address != "" && !addressPattern.MatchString(o.address) {
		return "", fmt.Errorf("invalid --address %q: expected <host>/<org>/<repo>", o.address)
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

// target is one graph to check and the paths it was read from.
type target struct {
	paths []string
	graph *meaning.Graph
}

// loadTargets reads what the paths name: each directory is a graph of its own,
// and the files named together are one more.
func loadTargets(fsys meaning.FS, args []string) ([]target, error) {
	if len(args) == 0 {
		args = []string{"."}
	}
	var targets []target
	var files []string
	seen := map[string]bool{}
	for _, arg := range args {
		path := filepath.Clean(arg)
		if seen[path] {
			continue
		}
		seen[path] = true
		fi, err := fsys.Stat(path)
		if err != nil {
			return nil, err
		}
		if !fi.IsDir() {
			files = append(files, path)
			continue
		}
		g, err := meaning.LoadDir(fsys, path)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target{paths: []string{path}, graph: g})
	}
	if len(files) > 0 {
		slices.Sort(files)
		g, err := meaning.LoadFiles(fsys, files)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target{paths: files, graph: g})
	}
	return targets, nil
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

func (t target) check(env Env, supplied map[string]*meaning.Graph, profile meaning.Profile) graphReport {
	base := meaning.GraphResolver(supplied)
	used := map[string]string{}
	resolve := func(repo, pin string) (*meaning.Graph, error) {
		g, err := base(repo, pin)
		if err == nil {
			used[repo] = pin
		}
		return g, err
	}
	// A graph with no finding has an empty list, not none: JSON says [] for it.
	findings := append([]meaning.Finding{}, meaning.Checker{FS: env.FS, Resolve: resolve, Profile: profile}.Check(t.graph)...)
	for _, repo := range slices.Sorted(maps.Keys(used)) {
		findings = append(findings, meaning.Finding{
			File: t.paths[0], Rule: "pin-not-verified", Severity: meaning.Info,
			Message: fmt.Sprintf("meaning://%s?ref=%s was read from the directory given with --graph; the pin is not verified offline", repo, used[repo]),
		})
	}
	meaning.SortFindings(findings)
	report := graphReport{Paths: t.paths, Address: t.graph.Address, Files: len(t.graph.Files), Concepts: len(t.graph.Concepts), Findings: findings}
	for _, f := range findings {
		switch f.Severity {
		case meaning.Error:
			report.Errors++
		case meaning.Warning:
			report.Warnings++
		}
	}
	return report
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
			fmt.Fprintf(&out, "ok: %s: %s, %s\n", r.label(), plural(r.Concepts, "concept"), plural(r.Files, "file"))
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
