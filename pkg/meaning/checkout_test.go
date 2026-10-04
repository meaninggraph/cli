package meaning

import (
	"errors"
	"io/fs"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/meaninggraph/cli/internal/memfs"
)

// unreadableFile makes one path unreadable, though it is there.
type unreadableFile struct {
	FS
	path string
}

func (u unreadableFile) ReadFileMax(name string, max int64) ([]byte, error) {
	if name == u.path {
		return nil, errors.New("denied")
	}
	return u.FS.ReadFileMax(name, max)
}

func TestCheckoutCommit(t *testing.T) {
	t.Parallel()
	sha, other := strings.Repeat("a", 40), strings.Repeat("b", 40)
	chain := map[string]string{"/d/.git/HEAD": "ref: refs/heads/a\n"}
	for i, name := range []string{"a", "b", "c", "d", "e"} {
		next := "ref: refs/heads/" + string(rune('b'+i)) + "\n"
		if name == "e" {
			next = sha
		}
		chain["/d/.git/refs/heads/"+name] = next
	}
	tests := []struct {
		name  string
		files map[string]string
		want  string // the commit, or a part of the error
		fail  bool
	}{
		{"a detached HEAD", map[string]string{"/d/.git/HEAD": sha + "\n"}, sha, false},
		{"a branch with a ref file", map[string]string{"/d/.git/HEAD": "ref: refs/heads/main\n", "/d/.git/refs/heads/main": other + "\n"}, other, false},
		{"a branch in packed-refs", map[string]string{"/d/.git/HEAD": "ref: refs/heads/main\n", "/d/.git/packed-refs": "# pack-refs with: peeled\n" + sha + " refs/heads/other\n^" + other + "\n" + other + " refs/heads/main\n"}, other, false},
		{"a linked worktree", map[string]string{"/d/.git": "gitdir: /main/.git/worktrees/d\n", "/main/.git/worktrees/d/HEAD": "ref: refs/heads/feat\n", "/main/.git/worktrees/d/commondir": "../..\n", "/main/.git/refs/heads/feat": sha}, sha, false},
		{"a linked worktree with a relative gitdir", map[string]string{"/d/.git": "gitdir: ../g\n", "/g/HEAD": sha}, sha, false},
		{"a branch that names another branch", map[string]string{"/d/.git/HEAD": "ref: refs/heads/a\n", "/d/.git/refs/heads/a": "ref: refs/heads/b\n", "/d/.git/refs/heads/b": sha}, sha, false},
		{"a ref file of the worktree itself", map[string]string{"/d/.git": "gitdir: /w\n", "/w/HEAD": "ref: refs/bisect/x\n", "/w/refs/bisect/x": sha}, sha, false},
		{"a chain of five refs", chain, sha, false},
		{"no .git", map[string]string{"/d/a.txt": "x"}, "it is not a git checkout", true},
		{"a .git file that names nothing", map[string]string{"/d/.git": "nonsense\n"}, "does not name a git directory", true},
		{"a .git file with an empty gitdir", map[string]string{"/d/.git": "gitdir: \n"}, "does not name a git directory", true},
		{"a git directory that is not there", map[string]string{"/d/.git": "gitdir: /nowhere\n"}, "HEAD cannot be read", true},
		{"no HEAD", map[string]string{"/d/.git/config": "x"}, "HEAD cannot be read", true},
		{"a ref that is not there", map[string]string{"/d/.git/HEAD": "ref: refs/heads/x\n"}, `"refs/heads/x", which is not found`, true},
		{"a ref name that climbs", map[string]string{"/d/.git/HEAD": "ref: refs/../x\n"}, "is not a ref name", true},
		{"an absolute ref name", map[string]string{"/d/.git/HEAD": "ref: /etc/passwd\n"}, "is not a ref name", true},
		{"a HEAD that is no commit", map[string]string{"/d/.git/HEAD": "hello\n"}, "does not name a commit", true},
		{"a chain of six refs", func() map[string]string {
			files := map[string]string{"/d/.git/HEAD": "ref: refs/heads/a\n"}
			for i, name := range []string{"a", "b", "c", "d", "e", "f"} {
				files["/d/.git/refs/heads/"+name] = "ref: refs/heads/" + string(rune('b'+i)) + "\n"
			}
			return files
		}(), "does not name a commit", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := CheckoutCommit(memfs.New(tc.files), "/d")
			switch {
			case tc.fail && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Fatalf("got %q, %v; want an error containing %q", got, err, tc.want)
			case !tc.fail && (err != nil || got != tc.want):
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestCheckoutCommitReportsAGitFileThatCannotBeRead(t *testing.T) {
	t.Parallel()
	files := memfs.New(map[string]string{"/d/.git": "gitdir: /g\n", "/g/HEAD": strings.Repeat("a", 40)})
	_, err := CheckoutCommit(unreadableFile{FS: files, path: "/d/.git"}, "/d")
	if err == nil || !strings.Contains(err.Error(), "/d/.git: denied") {
		t.Fatalf("err = %v", err)
	}
}

func TestRefTargets(t *testing.T) {
	t.Parallel()
	a, b, c := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	tests := []struct {
		name  string
		files map[string]string
		pin   string
		want  []RefTarget
		fail  bool
	}{
		{"a branch", map[string]string{"/d/.git/refs/heads/main": a + "\n"}, "main", []RefTarget{{Ref: "refs/heads/main", ID: a}}, false},
		{"a loose tag may be annotated", map[string]string{"/d/.git/refs/tags/v1": a}, "v1", []RefTarget{{Ref: "refs/tags/v1", ID: a, MaybeTagObject: true}}, false},
		{"the branch of the remote", map[string]string{"/d/.git/refs/remotes/origin/dev": b}, "dev", []RefTarget{{Ref: "refs/remotes/origin/dev", ID: b}}, false},
		{"a branch with a slash", map[string]string{"/d/.git/refs/heads/feat/x": a}, "feat/x", []RefTarget{{Ref: "refs/heads/feat/x", ID: a}}, false},
		{"a branch and a tag of one name", map[string]string{"/d/.git/refs/heads/x": a, "/d/.git/refs/tags/x": b}, "x", []RefTarget{{Ref: "refs/heads/x", ID: a}, {Ref: "refs/tags/x", ID: b, MaybeTagObject: true}}, false},
		{"packed refs, lightweight and annotated", map[string]string{"/d/.git/packed-refs": "# pack-refs with: peeled fully-peeled sorted\n" + a + " refs/tags/light\n" + b + " refs/tags/note\n^" + c + "\n" + a + " refs/heads/main\n"}, "note", []RefTarget{{Ref: "refs/tags/note", ID: c}}, false},
		{"a packed lightweight tag in a fully peeled file", map[string]string{"/d/.git/packed-refs": "# pack-refs with: peeled fully-peeled sorted\n" + a + " refs/tags/light\n"}, "light", []RefTarget{{Ref: "refs/tags/light", ID: a}}, false},
		{"a packed tag with no peeled line in a file that does not say it is fully peeled may be annotated", map[string]string{"/d/.git/packed-refs": "# pack-refs with: peeled\n" + a + " refs/tags/note\n" + b + " refs/heads/main\n"}, "note", []RefTarget{{Ref: "refs/tags/note", ID: a, MaybeTagObject: true}}, false},
		{"a packed tag in a file with no header", map[string]string{"/d/.git/packed-refs": a + " refs/tags/light\n" + b + " refs/tags/other\n"}, "light", []RefTarget{{Ref: "refs/tags/light", ID: a, MaybeTagObject: true}}, false},
		{"a packed branch needs no peeled line", map[string]string{"/d/.git/packed-refs": a + " refs/heads/main\n"}, "main", []RefTarget{{Ref: "refs/heads/main", ID: a}}, false},
		{"a packed ref followed by a peel line that is no commit", map[string]string{"/d/.git/packed-refs": a + " refs/tags/odd\n^nonsense\n"}, "odd", []RefTarget{{Ref: "refs/tags/odd", ID: a, MaybeTagObject: true}}, false},
		{"a loose ref beats a packed one", map[string]string{"/d/.git/refs/heads/main": b, "/d/.git/packed-refs": a + " refs/heads/main\n"}, "main", []RefTarget{{Ref: "refs/heads/main", ID: b}}, false},
		{"a ref of the common directory of a linked worktree", map[string]string{"/d/.git": "gitdir: /m/.git/worktrees/d\n", "/m/.git/worktrees/d/HEAD": a, "/m/.git/worktrees/d/commondir": "../..\n", "/m/.git/refs/heads/main": b, "/m/.git/packed-refs": c + " refs/tags/t\n"}, "main", []RefTarget{{Ref: "refs/heads/main", ID: b}}, false},
		{"a ref file that holds no commit", map[string]string{"/d/.git/refs/heads/main": "ref: refs/heads/other\n", "/d/.git/packed-refs": a + " refs/heads/main\n"}, "main", nil, false},
		{"a name that is not there", map[string]string{"/d/.git/HEAD": a}, "nope", nil, false},
		{"an empty name", map[string]string{"/d/.git/HEAD": a}, "", nil, true},
		{"a name that climbs", map[string]string{"/d/.git/HEAD": a}, "../x", nil, true},
		{"an absolute name", map[string]string{"/d/.git/HEAD": a}, "/etc", nil, true},
		{"a name with a trailing slash", map[string]string{"/d/.git/refs/heads/main": a}, "main/", nil, true},
		{"a name with a leading dot segment", map[string]string{"/d/.git/refs/heads/main": a}, "./main", nil, true},
		{"a name with an empty segment", map[string]string{"/d/.git/refs/heads/main": a}, "feat//main", nil, true},
		{"a name with a dot segment inside", map[string]string{"/d/.git/refs/heads/main": a}, "feat/./main", nil, true},
		{"a name that is a dot", map[string]string{"/d/.git/refs/heads/main": a}, ".", nil, true},
		{"a name with a character outside the grammar", map[string]string{"/d/.git/refs/heads/main": a}, "ma in", nil, true},
		{"another spelling of a loose ref is not the ref", map[string]string{"/d/.git/refs/heads/main": a}, "Main", nil, false},
		{"another spelling of a directory of a ref is not it either", map[string]string{"/d/.git/refs/heads/feat/x": a}, "Feat/x", nil, false},
		{"no .git", map[string]string{"/d/x": "x"}, "main", nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := RefTargets(memfs.New(tc.files), "/d", tc.pin)
			if (err != nil) != tc.fail || !slices.Equal(got, tc.want) {
				t.Fatalf("got %+v, %v; want %+v (fail %v)", got, err, tc.want, tc.fail)
			}
		})
	}
}

// foldFS is a file system that ignores the case of names when it opens a file
// or a directory (a default macOS or Windows one) but lists them as they are
// spelled; files are the names it holds.
type foldFS struct {
	FS
	files []string
}

// real is the spelling of name in the file system.
func (f foldFS) real(name string) string {
	for _, held := range f.files {
		switch lower, wanted := strings.ToLower(held), strings.ToLower(name); {
		case lower == wanted:
			return held
		case strings.HasPrefix(lower, wanted+"/"):
			return held[:len(name)]
		}
	}
	return name
}

func (f foldFS) Stat(name string) (fs.FileInfo, error) { return f.FS.Stat(f.real(name)) }

func (f foldFS) ReadFileMax(name string, max int64) ([]byte, error) {
	return f.FS.ReadFileMax(f.real(name), max)
}

func (f foldFS) ReadDir(name string) ([]fs.DirEntry, error) { return f.FS.ReadDir(f.real(name)) }

func foldedFiles(files map[string]string) foldFS {
	var names []string
	for name := range files {
		names = append(names, name)
	}
	return foldFS{FS: memfs.New(files), files: names}
}

// A ref is read only when it is spelled as it is listed, is a regular file that
// is not a link, and is small: a case-insensitive file system, a link and a huge
// file each give the "cannot be verified" answer, never a match.
func TestGitFilesAreReadBoundedExactlyAndNotThroughLinks(t *testing.T) {
	t.Parallel()
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	loose := map[string]string{"/d/.git/HEAD": "ref: refs/heads/main\n", "/d/.git/refs/heads/main": a, "/d/.git/refs/heads/feat/x": b}
	sys := foldedFiles(loose)
	if got, err := CheckoutCommit(sys, "/d"); err != nil || got != a {
		t.Fatalf("the spelling of HEAD and main is exact: %q, %v", got, err)
	}
	for _, name := range []string{"MAIN", "Main", "feat/X", "FEAT/x"} {
		if got, err := RefTargets(sys, "/d", name); err != nil || len(got) != 0 {
			t.Errorf("another spelling (%s) of a ref is not that ref on a case-insensitive file system: %v, %v", name, got, err)
		}
	}
	if got, err := RefTargets(sys, "/d", "feat/x"); err != nil || len(got) != 1 || got[0].ID != b {
		t.Errorf("the exact spelling is: %v, %v", got, err)
	}
	// HEAD that is spelled in another case is no HEAD.
	if _, err := CheckoutCommit(foldedFiles(map[string]string{"/d/.git/head": a}), "/d"); err == nil || !strings.Contains(err.Error(), "HEAD cannot be read") {
		t.Errorf("head is not HEAD: %v", err)
	}

	// Symbolic links: HEAD, a ref, a directory of refs and packed-refs.
	for name, files := range map[string]struct {
		files    map[string]string
		links    []string
		pin      string
		wantRead string // the part of the error of RefTargets, or of CheckoutCommit when pin is empty
	}{
		"HEAD":                {map[string]string{"/d/.git/HEAD": a}, []string{"/d/.git/HEAD"}, "", "HEAD cannot be read"},
		"a branch of HEAD":    {map[string]string{"/d/.git/HEAD": "ref: refs/heads/main", "/d/.git/refs/heads/main": a}, []string{"/d/.git/refs/heads/main"}, "", "not read"},
		"a ref":               {map[string]string{"/d/.git/refs/heads/main": a}, []string{"/d/.git/refs/heads/main"}, "main", "not read"},
		"a directory of refs": {map[string]string{"/d/.git/refs/heads/feat/x": a}, nil, "feat/x", ""},
		"packed-refs":         {map[string]string{"/d/.git/packed-refs": a + " refs/heads/main\n"}, []string{"/d/.git/packed-refs"}, "main", "packed-refs"},
	} {
		sys := memfs.New(files.files, files.links...)
		var err error
		if files.pin == "" {
			_, err = CheckoutCommit(sys, "/d")
		} else {
			_, err = RefTargets(sys, "/d", files.pin)
		}
		if files.wantRead == "" {
			if err != nil {
				t.Errorf("%s: %v", name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), files.wantRead) {
			t.Errorf("a link as %s: err = %v, want one containing %q", name, err, files.wantRead)
		}
	}

	// Size: a ref over the limit, HEAD over the limit and a packed-refs over its.
	big := strings.Repeat("x", maxRefBytes+1)
	if _, err := RefTargets(memfs.New(map[string]string{"/d/.git/refs/heads/big": big}), "/d", "big"); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("a big ref: %v", err)
	}
	if _, err := CheckoutCommit(memfs.New(map[string]string{"/d/.git/HEAD": big}), "/d"); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("a big HEAD: %v", err)
	}
	if _, err := RefTargets(memfs.New(map[string]string{"/d/.git/packed-refs": strings.Repeat("x", maxPackedRefsBytes+1)}), "/d", "main"); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("a big packed-refs: %v", err)
	}
	if _, err := CheckoutCommit(memfs.New(map[string]string{"/d/.git": "gitdir: " + strings.Repeat("x", maxGitPointerBytes+1)}), "/d"); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("a big .git file: %v", err)
	}
	if _, err := CheckoutCommit(memfs.New(map[string]string{"/d/.git/HEAD": "ref: refs/heads/main", "/d/.git/packed-refs": strings.Repeat("x", maxPackedRefsBytes+1)}), "/d"); err == nil || !strings.Contains(err.Error(), "packed-refs") {
		t.Errorf("a branch of HEAD, and a big packed-refs: %v", err)
	}
	// Exactly at the limit is read.
	exact := a + strings.Repeat("\n", maxRefBytes-40)
	if got, err := CheckoutCommit(memfs.New(map[string]string{"/d/.git/HEAD": exact}), "/d"); err != nil || got != a {
		t.Errorf("a HEAD of %d bytes: %q, %v", len(exact), got, err)
	}

	// A device, a pipe or any file that is not regular is not opened; a directory
	// listing that fails is an error, a directory that is not there is a ref that is not.
	odd := irregularFS{memfs.New(map[string]string{"/d/.git/HEAD": a})}
	if _, err := CheckoutCommit(odd, "/d"); err == nil || !strings.Contains(err.Error(), "not read") {
		t.Errorf("a HEAD that is a pipe: %v", err)
	}
	denied := deniedListing{memfs.New(map[string]string{"/d/.git/HEAD": a})}
	if _, err := CheckoutCommit(denied, "/d"); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Errorf("a directory that cannot be listed: %v", err)
	}
}

// irregularFS lists every file as a named pipe.
type irregularFS struct{ FS }

func (i irregularFS) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := i.FS.ReadDir(name)
	for k, entry := range entries {
		entries[k] = pipeEntry{entry}
	}
	return entries, err
}

type pipeEntry struct{ fs.DirEntry }

func (pipeEntry) Type() fs.FileMode { return fs.ModeNamedPipe }

// deniedListing cannot list the git directory.
type deniedListing struct{ FS }

func (d deniedListing) ReadDir(name string) ([]fs.DirEntry, error) {
	if strings.HasSuffix(name, ".git") {
		return nil, errors.New("denied")
	}
	return d.FS.ReadDir(name)
}

// typedFS lists the named paths as the given kind of file (a pipe, a device)
// whatever the file system under it holds there, as ReadDir of a real directory
// would for a file that mkfifo made.
type typedFS struct {
	FS
	kinds map[string]fs.FileMode
}

func (t typedFS) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := t.FS.ReadDir(name)
	for i, entry := range entries {
		if kind, ok := t.kinds[path.Join(name, entry.Name())]; ok {
			entries[i] = kindEntry{entry, kind}
		}
	}
	return entries, err
}

type kindEntry struct {
	fs.DirEntry
	kind fs.FileMode
}

func (k kindEntry) Type() fs.FileMode { return k.kind }

func (k kindEntry) IsDir() bool { return k.kind.IsDir() }

// opensNothing fails the test when a file that must not be opened is: opening a
// named pipe waits for a writer, so a CLI that opens one never returns.
type opensNothing struct {
	FS
	t     *testing.T
	paths []string
}

func (o opensNothing) ReadFileMax(name string, max int64) ([]byte, error) {
	if slices.Contains(o.paths, name) {
		o.t.Errorf("%s was opened, though it is not a regular file", name)
	}
	return o.FS.ReadFileMax(name, max)
}

// The .git pointer file and commondir are read like HEAD and the refs are: a
// pipe, a device, a link and a file over the limit are not read, and are never
// opened (a pipe would block), so the pin cannot be verified.
func TestGitPointerAndCommondirAreReadOnlyWhenRegularLinkFreeAndSmall(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("a", 40)
	pointer := map[string]string{"/d/.git": "gitdir: /w\n", "/w/HEAD": "ref: refs/heads/main\n", "/w/refs/heads/main": sha, "/w/commondir": "/c\n", "/c/refs/heads/main": sha}
	for _, tc := range []struct {
		name  string
		files map[string]string
		links []string
		kinds map[string]fs.FileMode
		bad   string // the path that is not read
		want  string // a part of the error
	}{
		{"a .git that is a named pipe", pointer, nil, map[string]fs.FileMode{"/d/.git": fs.ModeNamedPipe}, "/d/.git", "not read"},
		{"a .git that is a device", pointer, nil, map[string]fs.FileMode{"/d/.git": fs.ModeDevice | fs.ModeCharDevice}, "/d/.git", "not read"},
		{"a .git that is a link to a pointer file", pointer, []string{"/d/.git"}, nil, "/d/.git", "not read"},
		{"a .git over the limit", map[string]string{"/d/.git": "gitdir: " + strings.Repeat("x", maxGitPointerBytes)}, nil, nil, "", "larger than"},
		{"a commondir that is a named pipe", pointer, nil, map[string]fs.FileMode{"/w/commondir": fs.ModeNamedPipe}, "/w/commondir", "not read"},
		{"a commondir that is a device", pointer, nil, map[string]fs.FileMode{"/w/commondir": fs.ModeDevice}, "/w/commondir", "not read"},
		{"a commondir that is a link", pointer, []string{"/w/commondir"}, nil, "/w/commondir", "not read"},
		{"a commondir over the limit", map[string]string{"/d/.git": "gitdir: /w\n", "/w/HEAD": sha, "/w/commondir": strings.Repeat("x", maxGitPointerBytes+1)}, nil, nil, "", "larger than"},
		{"a commondir that names nothing", map[string]string{"/d/.git": "gitdir: /w\n", "/w/HEAD": sha, "/w/commondir": "\n"}, nil, nil, "", "does not name a git directory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var sys FS = typedFS{memfs.New(tc.files, tc.links...), tc.kinds}
			sys = opensNothing{sys, t, []string{tc.bad}}
			if _, err := CheckoutCommit(sys, "/d"); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("CheckoutCommit: err = %v, want one containing %q", err, tc.want)
			}
			if _, err := RefTargets(sys, "/d", "main"); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("RefTargets: err = %v, want one containing %q", err, tc.want)
			}
		})
	}
	// The same files, regular, are read: the commit is found through the pointer
	// and through commondir.
	if got, err := CheckoutCommit(memfs.New(pointer), "/d"); err != nil || got != sha {
		t.Errorf("regular files: %q, %v", got, err)
	}
	// A commondir that is not there is the git directory itself.
	if got, err := CheckoutCommit(memfs.New(map[string]string{"/d/.git": "gitdir: /w\n", "/w/HEAD": sha}), "/d"); err != nil || got != sha {
		t.Errorf("no commondir: %q, %v", got, err)
	}
	// A checkout directory that cannot be listed is an error, not "no .git".
	if _, err := CheckoutCommit(deniedListing{memfs.New(map[string]string{"/x.git/.git/HEAD": sha})}, "/x.git"); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Errorf("a directory that cannot be listed: %v", err)
	}
}
