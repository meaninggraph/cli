package meaning

import (
	"errors"
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
