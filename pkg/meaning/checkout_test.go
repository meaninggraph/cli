package meaning

import (
	"errors"
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

func (u unreadableFile) ReadFile(name string) ([]byte, error) {
	if name == u.path {
		return nil, errors.New("denied")
	}
	return u.FS.ReadFile(name)
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
	if err == nil || !strings.Contains(err.Error(), "/d/.git cannot be read") {
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
		{"a packed lightweight tag", map[string]string{"/d/.git/packed-refs": a + " refs/tags/light\n" + b + " refs/tags/other\n"}, "light", []RefTarget{{Ref: "refs/tags/light", ID: a}}, false},
		{"a packed ref followed by a peel line that is no commit", map[string]string{"/d/.git/packed-refs": a + " refs/tags/odd\n^nonsense\n"}, "odd", []RefTarget{{Ref: "refs/tags/odd", ID: a}}, false},
		{"a loose ref beats a packed one", map[string]string{"/d/.git/refs/heads/main": b, "/d/.git/packed-refs": a + " refs/heads/main\n"}, "main", []RefTarget{{Ref: "refs/heads/main", ID: b}}, false},
		{"a ref of the common directory of a linked worktree", map[string]string{"/d/.git": "gitdir: /m/.git/worktrees/d\n", "/m/.git/worktrees/d/HEAD": a, "/m/.git/worktrees/d/commondir": "../..\n", "/m/.git/refs/heads/main": b, "/m/.git/packed-refs": c + " refs/tags/t\n"}, "main", []RefTarget{{Ref: "refs/heads/main", ID: b}}, false},
		{"a ref file that holds no commit", map[string]string{"/d/.git/refs/heads/main": "ref: refs/heads/other\n", "/d/.git/packed-refs": a + " refs/heads/main\n"}, "main", nil, false},
		{"a name that is not there", map[string]string{"/d/.git/HEAD": a}, "nope", nil, false},
		{"an empty name", map[string]string{"/d/.git/HEAD": a}, "", nil, false},
		{"a name that climbs", map[string]string{"/d/.git/HEAD": a}, "../x", nil, false},
		{"an absolute name", map[string]string{"/d/.git/HEAD": a}, "/etc", nil, false},
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
