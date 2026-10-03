package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/strongo/buildinfo"
)

// runMain runs main with the given arguments and returns what it passed to
// exit and wrote to stdout and stderr.
func runMain(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	dir := t.TempDir()
	out, err := os.Create(filepath.Join(dir, "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	errs, err := os.Create(filepath.Join(dir, "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	oldArgs, oldStdout, oldStderr, oldExit := os.Args, os.Stdout, os.Stderr, exit
	defer func() { os.Args, os.Stdout, os.Stderr, exit = oldArgs, oldStdout, oldStderr, oldExit }()
	os.Args = append([]string{"meaninggraph"}, args...)
	os.Stdout, os.Stderr = out, errs
	code = -1
	exit = func(c int) { code = c }
	main()
	os.Stdout, os.Stderr = oldStdout, oldStderr
	for _, f := range []*os.File{out, errs} {
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	return code, read("stdout"), read("stderr")
}

func TestMainPrintsTheVersionAndExitsZero(t *testing.T) {
	code, stdout, stderr := runMain(t, "version")
	if want := buildinfo.Get("meaninggraph").Long() + "\n"; code != 0 || stdout != want || stderr != "" {
		t.Fatalf("code %d stdout %q (want %q) stderr %q", code, stdout, want, stderr)
	}
}

func TestMainChecksFilesAndPassesTheExitCodeOn(t *testing.T) {
	dir := t.TempDir()
	good := "format: meaning/draft-1\nid: demo\nname: Demo\ndescription: d\nconcepts:\n  - {id: a, kind: entity, labels: {en: a}, description: d}\n"
	if err := os.WriteFile(filepath.Join(dir, "a.meaning.yaml"), []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, stdout, stderr := runMain(t, "check", dir); code != 0 || stdout != "ok: "+dir+": 1 concept, 1 file\n" || stderr != "" {
		t.Fatalf("clean: code %d stdout %q stderr %q", code, stdout, stderr)
	}
	bad := good + "  - {id: b, kind: entity, labels: {en: b}, description: d, extends: nowhere}\n"
	if err := os.WriteFile(filepath.Join(dir, "a.meaning.yaml"), []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	// A failing check exits 1 and says which file, which line, which rule and
	// what is wrong, then the summary, on stdout and nothing on stderr.
	wantFailing := filepath.Join(dir, "a.meaning.yaml") + ":7: error: concept b extends: concept nowhere is not declared in this repository [unknown-concept]\n" +
		"failed: " + dir + ": 1 error, 0 warnings, 2 concepts, 1 file\n"
	if code, stdout, stderr := runMain(t, "check", dir); code != 1 || stdout != wantFailing || stderr != "" {
		t.Fatalf("with an error: code %d stdout %q (want %q) stderr %q", code, stdout, wantFailing, stderr)
	}
	// The same with --format json: exit 1, and the finding is in the report.
	if code, stdout, _ := runMain(t, "check", "--format", "json", dir); code != 1 || !strings.Contains(stdout, `"rule": "unknown-concept"`) || !strings.Contains(stdout, `"ok": false`) {
		t.Fatalf("json: code %d stdout %q", code, stdout)
	}
	if code, stdout, stderr := runMain(t, "check", "--format", "xml", dir); code != 2 || stdout != "" || stderr != "meaninggraph: invalid --format \"xml\": expected text or json\n" {
		t.Fatalf("usage: code %d stdout %q stderr %q", code, stdout, stderr)
	}
}
