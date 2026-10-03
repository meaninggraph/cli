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
	gitDir, common, err := gitLayout(fsys, dir)
	if err != nil {
		return "", err
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

// gitLayout finds the git directory of the checkout in dir (the one that holds
// HEAD) and the common one that holds the refs (the same, but for a linked
// worktree).
func gitLayout(fsys FS, dir string) (gitDir, common string, err error) {
	dotGit := filepath.Join(dir, ".git")
	info, err := fsys.Stat(dotGit)
	if err != nil {
		return "", "", errors.New("it is not a git checkout (it has no .git)")
	}
	gitDir, common = dotGit, dotGit
	if !info.IsDir() {
		if gitDir, err = pointedTo(fsys, dir, dotGit, "gitdir: "); err != nil {
			return "", "", err
		}
		common = gitDir
		if shared, err := pointedTo(fsys, gitDir, filepath.Join(gitDir, "commondir"), ""); err == nil {
			common = shared
		}
	}
	return gitDir, common, nil
}

// RefTarget is a ref of a git checkout that a pin names, and what it points at.
type RefTarget struct {
	// Ref is the full name of the ref (refs/heads/main, refs/tags/v1, ...).
	Ref string
	// ID is what the ref file or packed-refs holds, or the commit an annotated
	// tag in packed-refs peels to.
	ID string
	// MaybeTagObject says that ID is that of a tag that may be annotated: a loose
	// refs/tags file holds the id of the tag object then, not that of the commit,
	// and telling them apart means reading the object, which this does not do.
	MaybeTagObject bool
}

// RefTargets finds the refs that the branch or tag name names in the checkout
// in dir: a branch, a tag, or the branch of the remote called origin, each as a
// ref file or in packed-refs. Like CheckoutCommit it reads plain files.
func RefTargets(fsys FS, dir, name string) ([]RefTarget, error) {
	gitDir, common, err := gitLayout(fsys, dir)
	if err != nil {
		return nil, err
	}
	if name == "" || strings.Contains(name, "..") || strings.HasPrefix(name, "/") {
		return nil, nil
	}
	var packed []string
	if data, err := fsys.ReadFile(filepath.Join(common, "packed-refs")); err == nil {
		packed = strings.Split(string(data), "\n")
	}
	var targets []RefTarget
	for _, ref := range []string{"refs/heads/" + name, "refs/tags/" + name, "refs/remotes/origin/" + name} {
		tag := strings.HasPrefix(ref, "refs/tags/")
		found := false
		for _, base := range []string{gitDir, common} {
			if data, err := fsys.ReadFile(filepath.Join(base, ref)); err == nil {
				if id := strings.TrimSpace(string(data)); commitID.MatchString(id) {
					targets = append(targets, RefTarget{Ref: ref, ID: id, MaybeTagObject: tag})
				}
				found = true
				break
			}
		}
		for i := 0; !found && i < len(packed); i++ {
			if id, packedRef, ok := strings.Cut(packed[i], " "); ok && packedRef == ref && commitID.MatchString(id) {
				// A line that follows and starts with ^ is the commit an annotated tag points at.
				if i+1 < len(packed) && strings.HasPrefix(packed[i+1], "^") && commitID.MatchString(packed[i+1][1:]) {
					id = packed[i+1][1:]
				}
				targets = append(targets, RefTarget{Ref: ref, ID: id})
			}
		}
	}
	return targets, nil
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
