package covergate

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"go.yaml.in/yaml/v3"
)

// These tests pin the shape of the workflows that make the coverage gate
// binding. They parse the YAML and compare the structure, so that a step that
// is turned off (if: false), commented out, filtered, made to tolerate failure
// or preceded by a step that edits the profile fails them, as does a release
// that no longer waits for the gate. The checks run on the real files and,
// below, on mutated copies of them, which must fail.

type workflowFile struct {
	Name        string                    `yaml:"name"`
	On          map[string]map[string]any `yaml:"on"`
	Permissions map[string]string         `yaml:"permissions"`
	Concurrency map[string]any            `yaml:"concurrency"`
	Jobs        map[string]workflowJob    `yaml:"jobs"`
}

type workflowJob struct {
	Name            string            `yaml:"name"`
	RunsOn          string            `yaml:"runs-on"`
	Needs           any               `yaml:"needs"`
	If              any               `yaml:"if"`
	ContinueOnError any               `yaml:"continue-on-error"`
	Permissions     map[string]string `yaml:"permissions"`
	Uses            string            `yaml:"uses"`
	With            map[string]any    `yaml:"with"`
	Steps           []workflowStep    `yaml:"steps"`
}

type workflowStep struct {
	Name            string         `yaml:"name"`
	Uses            string         `yaml:"uses"`
	Run             string         `yaml:"run"`
	With            map[string]any `yaml:"with"`
	If              any            `yaml:"if"`
	ContinueOnError any            `yaml:"continue-on-error"`
}

// parseWorkflow decodes a workflow, refusing any key these tests do not know:
// a new key (env, defaults, strategy, shell, working-directory, secrets ...) is a change
// to review, and to add to the structure here.
func parseWorkflow(data []byte) (workflowFile, error) {
	var w workflowFile
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	return w, dec.Decode(&w)
}

// Every action and the shared workflow is used at one commit, listed here: a
// different commit (even a valid 40-hex id) is a change to review and to make
// in this file. The comments in the workflows name the tag each commit is.
var pinnedActions = map[string]string{
	"actions/checkout":             "d23441a48e516b6c34aea4fa41551a30e30af803", // v6
	"actions/setup-go":             "924ae3a1cded613372ab5595356fb5720e22ba16", // v6
	"goreleaser/goreleaser-action": "f06c13b6b1a9625abc9e6e439d9c05a8f2190e94", // v7
}

const pinnedRelease = "strongo/cicd/.github/workflows/release.yml@5d96b1f3fbb3f12bb1e2762ff5ba54ccb9506504" // v1.21.0

// workflowFiles are the workflow files the repository may hold. A second one,
// calling the shared release workflow on a tag with no gate, would be a way to a
// release that skips the gate, so the directory is closed to anything else.
var workflowFiles = []string{"ci.yml", "release.yml"}

// stepIs compares a step with what it must be: its fields, and nothing that
// could switch it off or let it fail.
func stepIs(got workflowStep, want workflowStep) error {
	if got.If != nil || got.ContinueOnError != nil {
		return fmt.Errorf("step %q must not carry if or continue-on-error", got.Name+got.Run+got.Uses)
	}
	switch {
	case want.Uses != "" && got.Uses != want.Uses+"@"+pinnedActions[want.Uses]:
		return fmt.Errorf("step uses %q, want %s at the commit %s", got.Uses, want.Uses, pinnedActions[want.Uses])
	case want.Uses == "" && got.Uses != "":
		return fmt.Errorf("step uses %q, want the command %q", got.Uses, want.Run)
	case got.Run != want.Run:
		return fmt.Errorf("step runs %q, want %q", got.Run, want.Run)
	case !reflect.DeepEqual(got.With, want.With):
		return fmt.Errorf("step %s has with %v, want %v", got.Uses+got.Run, got.With, want.With)
	}
	return nil
}

func stepsAre(job workflowJob, want []workflowStep) error {
	if len(job.Steps) != len(want) {
		return fmt.Errorf("job %q has %d steps, want %d (in this order: %v)", job.Name, len(job.Steps), len(want), want)
	}
	for i := range want {
		if err := stepIs(job.Steps[i], want[i]); err != nil {
			return fmt.Errorf("job %q step %d: %w", job.Name, i+1, err)
		}
	}
	return nil
}

func plainJob(job workflowJob) error {
	if job.If != nil || job.ContinueOnError != nil || job.Needs != nil || job.RunsOn != "ubuntu-latest" || job.Uses != "" {
		return fmt.Errorf("job %q must run on ubuntu-latest with no if, needs, continue-on-error or uses", job.Name)
	}
	return nil
}

// checkCI verifies ci.yml: the gate and the packaging check.
func checkCI(data []byte) error {
	w, err := parseWorkflow(data)
	if err != nil {
		return err
	}
	if w.Name != "CI" {
		return fmt.Errorf("the workflow is named %q, want CI", w.Name)
	}
	// Pull requests, and the release workflow calls it; no filter of any kind.
	if len(w.On) != 2 {
		return fmt.Errorf("on has %d triggers, want pull_request and workflow_call", len(w.On))
	}
	for _, trigger := range []string{"pull_request", "workflow_call"} {
		if filters, ok := w.On[trigger]; !ok || len(filters) != 0 {
			return fmt.Errorf("trigger %s must be present and without filters (branches, paths, types ...)", trigger)
		}
	}
	if !reflect.DeepEqual(w.Permissions, map[string]string{"contents": "read"}) {
		return fmt.Errorf("permissions are %v, want contents: read", w.Permissions)
	}
	// Only a superseded pull-request run is cancelled: cancelling a run for main
	// would cancel the gate of a release.
	if !reflect.DeepEqual(w.Concurrency, map[string]any{"group": "ci-${{ github.ref }}", "cancel-in-progress": "${{ github.event_name == 'pull_request' }}"}) {
		return fmt.Errorf("concurrency is %v", w.Concurrency)
	}
	if len(w.Jobs) != 2 {
		return fmt.Errorf("the workflow has %d jobs, want test and package", len(w.Jobs))
	}
	test, ok := w.Jobs["test"]
	if !ok {
		return errors.New("job test is missing")
	}
	if err := plainJob(test); err != nil {
		return err
	}
	run := func(command string) workflowStep { return workflowStep{Run: command} }
	if err := stepsAre(test, []workflowStep{
		{Uses: "actions/checkout"},
		{Uses: "actions/setup-go", With: map[string]any{"go-version-file": "go.mod", "cache": true}},
		run(`test -z "$(gofmt -l .)"`),
		run("go vet ./..."),
		run("go mod tidy -diff"),
		run("go test -race -covermode=atomic -coverprofile=cover.out ./..."),
		run("go run ./cmd/covergate cover.out"),
	}); err != nil {
		return err
	}
	pkg, ok := w.Jobs["package"]
	if !ok {
		return errors.New("job package is missing")
	}
	if err := plainJob(pkg); err != nil {
		return err
	}
	return stepsAre(pkg, []workflowStep{
		{Uses: "actions/checkout", With: map[string]any{"fetch-depth": 0}},
		{Uses: "actions/setup-go", With: map[string]any{"go-version-file": "go.mod", "cache": true}},
		{Uses: "goreleaser/goreleaser-action", With: map[string]any{"version": "v2.18.2", "args": "check"}},
		{Uses: "goreleaser/goreleaser-action", With: map[string]any{"version": "v2.18.2", "args": "release --snapshot --clean --skip=publish"}},
		run("set -eu\nbin=\"$(find dist -type f -name meaninggraph -path '*linux_amd64*')\"\n\"$bin\" --version\n\"$bin\" check testdata/corpus/core --address github.com/meaninggraph/core --profile universal\n"),
	})
}

// checkRelease verifies release.yml: it starts on a push to main and on
// nothing else, and the release job needs the gate job, which is ci.yml.
func checkRelease(data []byte) error {
	w, err := parseWorkflow(data)
	if err != nil {
		return err
	}
	if len(w.On) != 1 || !reflect.DeepEqual(w.On["push"], map[string]any{"branches": []any{"main"}}) {
		return fmt.Errorf("on is %v, want only a push to main (no tags, no manual run, no schedule)", w.On)
	}
	if !reflect.DeepEqual(w.Permissions, map[string]string{"contents": "read"}) {
		return fmt.Errorf("permissions are %v, want contents: read", w.Permissions)
	}
	// A second push must wait for a release that is between its tag and its
	// publication, not cancel it.
	if !reflect.DeepEqual(w.Concurrency, map[string]any{"group": "release-${{ github.ref }}", "cancel-in-progress": false}) {
		return fmt.Errorf("concurrency is %v, want a group per ref that never cancels", w.Concurrency)
	}
	if len(w.Jobs) != 2 {
		return fmt.Errorf("the workflow has %d jobs, want gate and release", len(w.Jobs))
	}
	gate, ok := w.Jobs["gate"]
	if !ok {
		return errors.New("job gate is missing")
	}
	if gate.Uses != "./.github/workflows/ci.yml" || gate.If != nil || gate.ContinueOnError != nil || gate.Needs != nil || len(gate.Steps) != 0 || len(gate.With) != 0 ||
		!reflect.DeepEqual(gate.Permissions, map[string]string{"contents": "read"}) {
		return fmt.Errorf("job gate must be a plain call of ./.github/workflows/ci.yml, got %+v", gate)
	}
	release, ok := w.Jobs["release"]
	if !ok {
		return errors.New("job release is missing")
	}
	if release.Uses != pinnedRelease {
		return fmt.Errorf("job release uses %q, want %s", release.Uses, pinnedRelease)
	}
	needsGate := release.Needs == "gate" || reflect.DeepEqual(release.Needs, []any{"gate"})
	if !needsGate || release.If != "github.ref == 'refs/heads/main'" || release.ContinueOnError != nil || len(release.Steps) != 0 {
		return fmt.Errorf("job release must need gate, run only on main, and not tolerate failure: needs %v if %v", release.Needs, release.If)
	}
	if !reflect.DeepEqual(release.Permissions, map[string]string{"contents": "write"}) {
		return fmt.Errorf("job release has permissions %v, want contents: write", release.Permissions)
	}
	// The shared workflow's own look at another workflow (require_workflow_success)
	// is not used: it carries on after three minutes when it finds no run.
	if !reflect.DeepEqual(release.With, map[string]any{"go_version": "1.27.0", "allow_major_version_bump": false}) {
		return fmt.Errorf("job release has with %v", release.With)
	}
	return nil
}

func readWorkflow(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("../../.github/workflows/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCIWorkflowIsTheGate(t *testing.T) {
	t.Parallel()
	if err := checkCI(readWorkflow(t, "ci.yml")); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseWaitsForTheGate(t *testing.T) {
	t.Parallel()
	if err := checkRelease(readWorkflow(t, "release.yml")); err != nil {
		t.Fatal(err)
	}
}

// Every loosening below must be caught: each replaces one piece of the real
// file and the check must fail.
func TestWorkflowChecksCatchEveryLoosening(t *testing.T) {
	t.Parallel()
	const gateStep = "        run: go run ./cmd/covergate cover.out\n"
	const testStep = "        run: go test -race -covermode=atomic -coverprofile=cover.out ./...\n"
	ci := []struct{ name, from, to string }{
		{"the gate step switched off", gateStep, "        if: false\n" + gateStep},
		{"the gate step commented out", gateStep, "      #" + strings.TrimPrefix(gateStep, "      ")},
		{"the gate step missing", "      - name: Every statement is covered\n" + gateStep, ""},
		{"the gate step made to tolerate failure", gateStep, "        continue-on-error: true\n" + gateStep},
		{"the gate with an argument", gateStep, "        run: go run ./cmd/covergate -min 90 cover.out\n"},
		{"a filter step before the gate", "      - name: Every statement is covered\n", "      - name: Drop a file from the profile\n        run: sed -i '/cmd/d' cover.out\n      - name: Every statement is covered\n"},
		{"a filter step before the tests", "      - name: Test with the race detector\n", "      - name: Narrow the tests\n        run: export GOFLAGS=-tags=nothing\n      - name: Test with the race detector\n"},
		{"the race detector dropped", testStep, "        run: go test -covermode=atomic -coverprofile=cover.out ./...\n"},
		{"the tests narrowed", testStep, "        run: go test -race -covermode=atomic -coverprofile=cover.out ./... -run TestX\n"},
		{"an environment for the job", "    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@d23441a48e516b6c34aea4fa41551a30e30af803 # v6\n      - uses: actions/setup-go", "    runs-on: ubuntu-latest\n    env:\n      GOFLAGS: -tags=nothing\n    steps:\n      - uses: actions/checkout@d23441a48e516b6c34aea4fa41551a30e30af803 # v6\n      - uses: actions/setup-go"},
		{"a path filter on pull requests", "  pull_request:\n", "  pull_request:\n    paths: ['docs/**']\n"},
		{"a branch filter on pull requests", "  pull_request:\n", "  pull_request:\n    branches: [elsewhere]\n"},
		{"the call trigger removed", "  workflow_call:\n", ""},
		{"a push trigger added", "  pull_request:\n", "  push:\n    branches: [main]\n  pull_request:\n"},
		{"another commit for an action", "actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16", "actions/setup-go@0000000000000000000000000000000000000000"},
		{"another commit for the release tool", "goreleaser/goreleaser-action@f06c13b6b1a9625abc9e6e439d9c05a8f2190e94", "goreleaser/goreleaser-action@0123456789abcdef0123456789abcdef01234567"},
		{"every run cancelled", "cancel-in-progress: ${{ github.event_name == 'pull_request' }}", "cancel-in-progress: true"},
		{"another concurrency group", "group: ci-${{ github.ref }}", "group: ci"},
		{"a moving tag for an action", "actions/checkout@d23441a48e516b6c34aea4fa41551a30e30af803 # v6\n      - uses: actions/setup-go", "actions/checkout@v6\n      - uses: actions/setup-go"},
		{"the job switched off", "  test:\n    name: Format, vet, test, coverage\n", "  test:\n    if: false\n    name: Format, vet, test, coverage\n"},
		{"the job made to tolerate failure", "  test:\n    name: Format, vet, test, coverage\n", "  test:\n    continue-on-error: true\n    name: Format, vet, test, coverage\n"},
		{"the workflow renamed", "name: CI\n", "name: Checks\n"},
		{"write permission", "permissions:\n  contents: read\n\nconcurrency", "permissions:\n  contents: write\n\nconcurrency"},
		{"the packaged binary not run", "          \"$bin\" --version\n", ""},
		{"the snapshot build skipped", "          args: release --snapshot --clean --skip=publish\n", "          args: --version\n"},
		{"an unknown key", "    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@d23441a48e516b6c34aea4fa41551a30e30af803 # v6\n      - uses: actions/setup-go", "    runs-on: ubuntu-latest\n    defaults:\n      run:\n        working-directory: cmd\n    steps:\n      - uses: actions/checkout@d23441a48e516b6c34aea4fa41551a30e30af803 # v6\n      - uses: actions/setup-go"},
	}
	real := string(readWorkflow(t, "ci.yml"))
	for _, tc := range ci {
		if !strings.Contains(real, tc.from) {
			t.Errorf("%s: the text to replace is not in ci.yml", tc.name)
			continue
		}
		if err := checkCI([]byte(strings.Replace(real, tc.from, tc.to, 1))); err == nil {
			t.Errorf("ci.yml with %s passes the check", tc.name)
		}
	}
	release := []struct{ name, from, to string }{
		{"a tag trigger", "    branches:\n      - main\n", "    branches:\n      - main\n    tags:\n      - 'v*'\n"},
		{"a manual trigger", "on:\n  push:\n", "on:\n  workflow_dispatch:\n  push:\n"},
		{"a schedule", "on:\n  push:\n", "on:\n  schedule:\n    - cron: '0 3 * * *'\n  push:\n"},
		{"another branch", "      - main\n", "      - main\n      - dev\n"},
		{"the dependency on the gate removed", "    needs: gate\n", ""},
		{"the dependency on something else", "    needs: gate\n", "    needs: other\n"},
		{"the gate job switched off", "    name: Quality gate\n", "    name: Quality gate\n    if: false\n"},
		{"the gate job made to tolerate failure", "    name: Quality gate\n", "    name: Quality gate\n    continue-on-error: true\n"},
		{"the gate calling something else", "    uses: ./.github/workflows/ci.yml\n", "    uses: ./.github/workflows/other.yml\n"},
		{"the release job without its condition", "    if: github.ref == 'refs/heads/main'\n", ""},
		{"the release job made to tolerate failure", "    needs: gate\n", "    needs: gate\n    continue-on-error: true\n"},
		{"the release cancelled by a second push", "cancel-in-progress: false", "cancel-in-progress: true"},
		{"another group for the release", "group: release-${{ github.ref }}", "group: release"},
		{"another commit for the shared workflow", "@5d96b1f3fbb3f12bb1e2762ff5ba54ccb9506504 # v1.21.0", "@0000000000000000000000000000000000000000 # v1.21.0"},
		{"the shared workflow at a tag", "@5d96b1f3fbb3f12bb1e2762ff5ba54ccb9506504 # v1.21.0", "@v1.21.0"},
		{"the shared guard added", "      allow_major_version_bump: false\n", "      allow_major_version_bump: false\n      require_workflow_success: 'CI'\n"},
		{"write permission for the whole workflow", "permissions:\n  contents: read\n\nconcurrency", "permissions:\n  contents: write\n\nconcurrency"},
		{"a secret passed on", "      allow_major_version_bump: false\n", "      allow_major_version_bump: false\n    secrets: inherit\n"},
		{"a third job", "  release:\n", "  extra:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo\n  release:\n"},
		{"the release job missing", "  release:\n    name: Release\n", "  other:\n    name: Release\n"},
		{"the gate job missing", "  gate:\n    name: Quality gate\n", "  other:\n    name: Quality gate\n"},
	}
	real = string(readWorkflow(t, "release.yml"))
	for _, tc := range release {
		if !strings.Contains(real, tc.from) {
			t.Errorf("%s: the text to replace is not in release.yml", tc.name)
			continue
		}
		if err := checkRelease([]byte(strings.Replace(real, tc.from, tc.to, 1))); err == nil {
			t.Errorf("release.yml with %s passes the check", tc.name)
		}
	}
	if err := checkCI([]byte("name: [")); err == nil {
		t.Error("a file that is not YAML passes")
	}
	if err := checkRelease([]byte("name: [")); err == nil {
		t.Error("a file that is not YAML passes")
	}
}

// checkWorkflowDir fails unless the workflows directory holds exactly the
// known files: nothing else may start a run in this repository.
func checkWorkflowDir(fsys fs.FS) error {
	entries, err := fs.ReadDir(fsys, ".github/workflows")
	if err != nil {
		return err
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	slices.Sort(names)
	if !slices.Equal(names, workflowFiles) {
		return fmt.Errorf(".github/workflows holds %v, want exactly %v", names, workflowFiles)
	}
	return nil
}

func TestOnlyTheKnownWorkflowFilesExist(t *testing.T) {
	t.Parallel()
	if err := checkWorkflowDir(os.DirFS("../..")); err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowDirectoryCheckCatchesAnExtraFile(t *testing.T) {
	t.Parallel()
	file := &fstest.MapFile{Data: []byte("on: push\n")}
	for name, files := range map[string]fstest.MapFS{
		"the known files":     {".github/workflows/ci.yml": file, ".github/workflows/release.yml": file},
		"a second release":    {".github/workflows/ci.yml": file, ".github/workflows/release.yml": file, ".github/workflows/tag-release.yml": file},
		"the other extension": {".github/workflows/ci.yml": file, ".github/workflows/release.yml": file, ".github/workflows/x.yaml": file},
		"a subdirectory":      {".github/workflows/ci.yml": file, ".github/workflows/release.yml": file, ".github/workflows/sub/x.yml": file},
		"a missing release":   {".github/workflows/ci.yml": file},
		"no directory at all": {},
	} {
		err := checkWorkflowDir(files)
		if (name == "the known files") != (err == nil) {
			t.Errorf("%s: error = %v", name, err)
		}
	}
}
