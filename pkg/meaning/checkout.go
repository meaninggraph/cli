package meaning

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var commitID = regexp.MustCompile(`^[0-9a-f]{40}$`)

// CheckoutCommit returns the commit a git checkout in dir is at: the commit id
// that HEAD holds, or that the branch HEAD names points at. It reads plain
// files only (.git/HEAD, the branch's ref file or .git/packed-refs); it starts
// no process and uses no network, and it follows a linked worktree's .git file
// to its git directory.
//
// It tells which commit the checkout was made at, and nothing more: it does not
// look at the working tree, so files that were edited, added or deleted since
// the checkout are not noticed, and it does not check the objects of the
// repository. An error says why the commit cannot be read, for example that dir
// is not a git checkout.
func CheckoutCommit(fsys FS, dir string) (string, error) {
	dotGit := filepath.Join(dir, ".git")
	info, err := fsys.Stat(dotGit)
	if err != nil {
		return "", errors.New("it is not a git checkout (it has no .git)")
	}
	gitDir, common := dotGit, dotGit
	if !info.IsDir() {
		if gitDir, err = pointedTo(fsys, dir, dotGit, "gitdir: "); err != nil {
			return "", err
		}
		common = gitDir
		if shared, err := pointedTo(fsys, gitDir, filepath.Join(gitDir, "commondir"), ""); err == nil {
			common = shared
		}
	}
	data, err := fsys.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return "", fmt.Errorf("its HEAD cannot be read: %w", err)
	}
	head := strings.TrimSpace(string(data))
	for range 5 {
		ref, symbolic := strings.CutPrefix(head, "ref: ")
		if !symbolic {
			break
		}
		if head, err = readRef(fsys, gitDir, common, ref); err != nil {
			return "", err
		}
	}
	if !commitID.MatchString(head) {
		return "", errors.New("its HEAD does not name a commit")
	}
	return head, nil
}

// pointedTo reads a file that holds a path (after prefix), relative to base.
func pointedTo(fsys FS, base, file, prefix string) (string, error) {
	data, err := fsys.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("%s cannot be read: %w", file, err)
	}
	target, ok := strings.CutPrefix(strings.TrimSpace(string(data)), prefix)
	if !ok || target == "" {
		return "", fmt.Errorf("%s does not name a git directory", file)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(base, target)
	}
	return target, nil
}

// readRef finds the commit a ref points at, in a loose ref file or in
// packed-refs.
func readRef(fsys FS, gitDir, common, ref string) (string, error) {
	if strings.Contains(ref, "..") || filepath.IsAbs(ref) {
		return "", fmt.Errorf("its HEAD names the ref %q, which is not a ref name", ref)
	}
	for _, dir := range []string{gitDir, common} {
		if data, err := fsys.ReadFile(filepath.Join(dir, ref)); err == nil {
			return strings.TrimSpace(string(data)), nil
		}
	}
	if data, err := fsys.ReadFile(filepath.Join(common, "packed-refs")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if id, name, ok := strings.Cut(line, " "); ok && name == ref {
				return id, nil
			}
		}
	}
	return "", fmt.Errorf("its HEAD names the ref %q, which is not found", ref)
}
