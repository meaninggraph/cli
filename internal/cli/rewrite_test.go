package cli

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meaninggraph/cli/internal/memfs"
)

// rewriteEnv runs rewrite over files in memory; the files it writes come back in written.
func rewriteEnv(files map[string]string, args ...string) (result, map[string]string) {
	written := map[string]string{}
	var stdout, stderr bytes.Buffer
	code := Run(args, Env{Stdout: &stdout, Stderr: &stderr, FS: memfs.New(files), Abs: fakeAbs, SelfUpdate: offline,
		WriteFile: func(name string, data []byte) error { written[name] = string(data); return nil }})
	return result{code, stdout.String(), stderr.String()}, written
}

const earlier = "format: meaning/draft-1\nid: a\nname: A\ndescription: d\nconcepts:\n" +
	"  - {id: thing, kind: attribute, labels: {en: thing}, description: d}\n"

func TestRewriteIsADryRunUnlessItIsToldToWrite(t *testing.T) {
	t.Parallel()
	files := map[string]string{"/g/a.meaning.yaml": earlier, "/g/b.meaning.yaml": strings.Replace(earlier, "draft-1", "draft-2", 1), "/g/sub/c.meaning.yaml": earlier, "/g/notes.txt": "x"}
	got, written := rewriteEnv(files, "rewrite", "/g")
	want := "/g/a.meaning.yaml: meaning/draft-1 -> meaning/draft-2: 1 format line, 1 kind attribute\n" +
		"/g/b.meaning.yaml: already meaning/draft-2\n" +
		"dry run: 1 file would be written; run with --write to write them\n"
	if got.code != ExitClean || got.stdout != want || got.stderr != "" || len(written) != 0 {
		t.Fatalf("got %+v\nwritten %v", got, written)
	}
	got, written = rewriteEnv(files, "rewrite", "/g", "--write")
	if got.code != ExitClean || got.stdout != want[:strings.Index(want, "dry run")]+"wrote 1 file\n" || got.stderr != "" {
		t.Fatalf("got %+v", got)
	}
	if len(written) != 1 || written["/g/a.meaning.yaml"] != strings.Replace(strings.Replace(earlier, "draft-1", "draft-2", 1), "attribute", "property", 1) {
		t.Fatalf("written %v", written)
	}
}

func TestRewriteListsTheConceptsThatCarryValues(t *testing.T) {
	t.Parallel()
	text := earlier + "  - {id: list, kind: entity, labels: {en: list}, description: d, values: [{id: a, labels: {en: a}}]}\n"
	got, written := rewriteEnv(map[string]string{"/g/a.meaning.yaml": text}, "rewrite", "/g/a.meaning.yaml", "--write")
	if got.code != ExitClean || !strings.Contains(got.stdout, "/g/a.meaning.yaml:7: concept list (kind entity) carries values and is not changed; in meaning/draft-2 values stand on a concept of kind value-set only") || !strings.HasSuffix(got.stdout, "wrote 1 file\n") {
		t.Fatalf("got %+v", got)
	}
	if !strings.Contains(written["/g/a.meaning.yaml"], "kind: entity, labels: {en: list}") {
		t.Fatalf("the list is not changed: %q", written)
	}
	// What is left is what check names.
	if again := execute(map[string]string{"/g/a.meaning.yaml": written["/g/a.meaning.yaml"]}, "check", "/g"); again.code != ExitFindings || !strings.Contains(again.stdout, "concept list: format-word: values belongs on a concept of kind value-set") {
		t.Fatalf("check: %+v", again)
	}
}

func TestRewriteWritesNothingWhenOneFileCannotBeRewritten(t *testing.T) {
	t.Parallel()
	files := map[string]string{"/g/a.meaning.yaml": earlier, "/g/b.meaning.yaml": "id: b\n", "/g/c.meaning.yaml": "a: b\n  c: d\n"}
	got, written := rewriteEnv(files, "rewrite", "/g", "--write")
	if got.code != ExitUsage || got.stdout != "" || len(written) != 0 ||
		!strings.Contains(got.stderr, "meaninggraph: nothing was written; /g/b.meaning.yaml: cannot be rewritten: the file says no format that is read") ||
		!strings.Contains(got.stderr, "/g/c.meaning.yaml: cannot be rewritten: a colon followed by a space") {
		t.Fatalf("got %+v, written %v", got, written)
	}
}

func TestRewriteReportsAFileThatCannotBeWritten(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := Run([]string{"rewrite", "/g/a.meaning.yaml", "--write"}, Env{Stdout: &stdout, Stderr: &stderr, FS: memfs.New(map[string]string{"/g/a.meaning.yaml": earlier}), Abs: fakeAbs, SelfUpdate: offline,
		WriteFile: func(string, []byte) error { return errors.New("read-only file system") }})
	if code != ExitUsage || stderr.String() != "meaninggraph: /g/a.meaning.yaml: read-only file system\n" {
		t.Fatalf("code %d stderr %q", code, stderr.String())
	}
}

func TestRewriteNamesFilesAndDirectoriesOnceEach(t *testing.T) {
	t.Parallel()
	files := map[string]string{"/work/a.meaning.yaml": earlier, "/work/b.meaning.yaml": strings.Replace(earlier, "thing", "other", 2)}
	got, _ := rewriteEnv(map[string]string{"a.meaning.yaml": earlier, "b.meaning.yaml": earlier}, "rewrite")
	if got.code != ExitClean || strings.Count(got.stdout, "meaning/draft-1 -> meaning/draft-2") != 2 || !strings.HasPrefix(got.stdout, "a.meaning.yaml:") {
		t.Fatalf("the default path is the current directory: %+v", got)
	}
	got, _ = rewriteEnv(files, "rewrite", "/work", "/work/a.meaning.yaml", "/work/x/../b.meaning.yaml", "/work")
	if got.code != ExitClean || strings.Count(got.stdout, "meaning/draft-1 -> meaning/draft-2") != 2 || !strings.HasSuffix(got.stdout, "dry run: 2 files would be written; run with --write to write them\n") {
		t.Fatalf("each file once, however it is named: %+v", got)
	}
	links := memfs.New(files, "/work/a.meaning.yaml")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"rewrite", "/work"}, Env{Stdout: &stdout, Stderr: &stderr, FS: links, Abs: fakeAbs, SelfUpdate: offline}); code != ExitClean || strings.Contains(stdout.String(), "a.meaning.yaml") {
		t.Fatalf("a symbolic link is not a meaning file: %s%s", stdout.String(), stderr.String())
	}
}

func TestRewriteRefusesWrongPaths(t *testing.T) {
	t.Parallel()
	files := map[string]string{"/g/a.meaning.yaml": earlier, "/empty/notes.txt": "x"}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"rewrite", ""}, "an empty path was given"},
		{[]string{"rewrite", "/nowhere"}, "file does not exist"},
		{[]string{"rewrite", "/empty"}, "no *.meaning.yaml file in /empty"},
	} {
		if got, _ := rewriteEnv(files, tc.args...); got.code != ExitUsage || !strings.Contains(got.stderr, tc.want) || got.stdout != "" {
			t.Errorf("%v: %+v", tc.args, got)
		}
	}
	// A directory or a file that cannot be read.
	unreadable := failingFS{memfs.New(files)}
	for _, args := range [][]string{{"rewrite", "/g"}, {"rewrite", "/g/a.meaning.yaml"}, {"rewrite", "/dir"}} {
		var stdout, stderr bytes.Buffer
		code := Run(args, Env{Stdout: &stdout, Stderr: &stderr, FS: unreadable, Abs: fakeAbs, SelfUpdate: offline})
		if code != ExitUsage || !strings.Contains(stderr.String(), "cannot be read") {
			t.Errorf("%v: code %d %s", args, code, stderr.String())
		}
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"rewrite", "/g"}, Env{Stdout: &stdout, Stderr: &stderr, FS: memfs.New(files), Abs: func(string) (string, error) { return "", errors.New("no working directory") }, SelfUpdate: offline}); code != ExitUsage || !strings.Contains(stderr.String(), "no working directory") {
		t.Errorf("Abs: code %d %s", code, stderr.String())
	}
}

// failingFS reads nothing: a directory it lists has a file it cannot read, and /dir cannot be listed.
type failingFS struct{ memfs.FS }

func (f failingFS) ReadFile(string) ([]byte, error) {
	return nil, errors.New("the file cannot be read")
}

func (f failingFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == "/dir" {
		return nil, errors.New("the directory cannot be read")
	}
	return f.FS.ReadDir(name)
}

func (f failingFS) Stat(name string) (fs.FileInfo, error) {
	if name == "/dir" {
		return f.FS.Stat("/g")
	}
	return f.FS.Stat(name)
}

// The command's own test of itself: every graph of the corpus that is accepted, in meaning/draft-1 and with no other
// graph, is rewritten and checked again. It is accepted in meaning/draft-2 too, unless it holds a list of values, which
// the rewrite leaves for a decision and which check then names.
func TestRewriteOfTheAcceptedGraphsOfTheCorpusIsAcceptedInDraft2(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "testdata", "corpus")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	rewritten, withValues, refused := 0, 0, 0
	for _, entry := range entries {
		var item corpusItem
		readJSON(t, filepath.Join(root, entry.Name(), "item.json"), &item)
		if item.Expect != "accept" || len(item.Graphs) > 0 || item.Profile != "" {
			continue
		}
		files := map[string]string{}
		err := filepath.WalkDir(filepath.Join(root, entry.Name()), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			data, err := os.ReadFile(path)
			rel, _ := filepath.Rel(filepath.Join(root, entry.Name()), path)
			files["/g/"+filepath.ToSlash(rel)] = string(data)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		dir := "/g/" + item.Check
		got, written := rewriteEnv(files, "rewrite", dir, "--write")
		if got.code == ExitUsage && strings.Contains(got.stderr, "does not hold the format line exactly once") {
			// A key written in quotes is a form the command does not edit: it says so, and writes nothing.
			quoted := false
			for _, text := range files {
				quoted = quoted || strings.Contains(text, `"format"`)
			}
			if !quoted || len(written) != 0 {
				t.Errorf("%s: %+v", entry.Name(), got)
			}
			refused++
			continue
		}
		if got.code != ExitClean {
			t.Errorf("%s: %+v", entry.Name(), got)
			continue
		}
		for name, text := range written {
			files[name] = text
		}
		check := []string{"check", "--format", "json", dir}
		if item.Address != "" {
			check = append(check, "--address", item.Address)
		}
		after := execute(files, check...)
		holdsValues := strings.Contains(got.stdout, " carries values and is not changed")
		switch {
		case holdsValues:
			withValues++
			if after.code != ExitFindings || !strings.Contains(after.stdout, "format-word") {
				t.Errorf("%s: a list of values is left for a decision, and check names it:\n%s", entry.Name(), after.stdout)
			}
		case after.code != ExitClean:
			t.Errorf("%s: accepted in meaning/draft-1, not after the rewrite:\n%s", entry.Name(), after.stdout)
		default:
			rewritten++
			if strings.Contains(after.stdout, `"rule": "earlier-format"`) || strings.Contains(after.stdout, `"rule": "earlier-role-name"`) {
				t.Errorf("%s: an earlier word is left:\n%s", entry.Name(), after.stdout)
			}
		}
	}
	if rewritten < 40 || withValues < 3 || refused < 1 {
		t.Errorf("%d graphs rewritten and accepted, %d with a list of values, %d in a form that is not edited", rewritten, withValues, refused)
	}
}

// The one place that writes: it keeps the permissions of the file it replaces, and does not make a file.
func TestReplaceFileKeepsThePermissions(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "a.meaning.yaml")
	if err := os.WriteFile(path, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := replaceFile(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	fi, _ := os.Stat(path)
	if err != nil || string(data) != "new" || fi.Mode().Perm() != 0o640 {
		t.Errorf("%q %v %v", data, fi.Mode(), err)
	}
	if err := replaceFile(path+".missing", []byte("x")); err == nil {
		t.Error("a file that is not there is not made")
	}
	if OSEnv().WriteFile == nil {
		t.Error("the real environment writes")
	}
}
