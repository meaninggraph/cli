package meaning

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
)

var commitID = regexp.MustCompile(`^[0-9a-f]{40}$`)

// What is read of a git checkout is bounded, and only regular files are read:
// a ref or HEAD is a commit id or a ref name (a few dozen bytes), a .git file or
// a commondir file holds a path, and packed-refs is a list of refs that grows
// with the repository.
const (
	maxRefBytes        = 1024
	maxGitPointerBytes = 4096
	maxPackedRefsBytes = 8 << 20
)

// CheckoutCommit returns the commit a git checkout in dir is at: the commit id
// that HEAD holds, or that the branch HEAD names points at. It reads plain
// files only (.git/HEAD, the branch's ref file or .git/packed-refs); it starts
// no process and uses no network, and it follows a linked worktree's .git file
// to its git directory. HEAD, the ref files and packed-refs are read only when
// they are regular files that are not symbolic links, whose names are spelled
// exactly as asked (a case-insensitive file system does not make MAIN the
// branch main), and only up to a documented size (1 KiB for a ref, 4 KiB for a
// .git pointer file or a commondir file, 8 MiB for packed-refs); otherwise the
// commit cannot be read, and the error says so. A .git that is a link, and a
// pointer file or commondir that is a pipe or a device, is not opened.
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
	data, found, err := readGitFile(fsys, gitDir, []string{"HEAD"}, maxRefBytes)
	if err == nil && !found {
		err = fs.ErrNotExist
	}
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
// worktree). The .git entry and a commondir file are read as HEAD and the refs
// are: a regular file that is not a link, of a bounded size. A .git that is a
// link, a pipe or a device, and a commondir that is anything but a regular file,
// are not opened (opening a pipe waits for a writer) and are an error.
func gitLayout(fsys FS, dir string) (gitDir, common string, err error) {
	dotGit := filepath.Join(dir, ".git")
	entry, err := lookup(fsys, dir, ".git")
	switch {
	case err != nil:
		return "", "", err
	case entry == nil:
		return "", "", errors.New("it is not a git checkout (it has no .git)")
	case entry.IsDir():
		return dotGit, dotGit, nil
	}
	if gitDir, err = pointedTo(fsys, dir, ".git", "gitdir: "); err != nil {
		return "", "", err
	}
	switch common, err = pointedTo(fsys, gitDir, "commondir", ""); {
	case errors.Is(err, fs.ErrNotExist):
		common = gitDir
	case err != nil:
		return "", "", err
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
	// MaybeTagObject says that ID may be that of a tag object, not of the commit:
	// a loose refs/tags file holds the id of the tag object when the tag is
	// annotated, and so does a packed one when packed-refs does not say that it is
	// fully peeled and gives no peeled line for it. Telling the two apart means
	// reading the object, which this does not do.
	MaybeTagObject bool
}

// validRefName says whether name can be a branch or tag name as `?ref=` writes
// it (FORMAT.md: letters, digits, dot, underscore, slash and hyphen): no empty
// segment (so no leading, trailing or doubled slash) and no segment that is "."
// or "..".
func validRefName(name string) bool {
	return strings.Trim(name, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._/-") == "" && validSegments(name)
}

// validSegments says that no segment of a ref name is empty, "." or "..".
func validSegments(name string) bool {
	for _, segment := range strings.Split(name, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

// RefTargets finds the refs that the branch or tag name names in the checkout
// in dir: a branch, a tag, or the branch of the remote called origin, each as a
// ref file or in packed-refs. Like CheckoutCommit it reads plain files, bounded,
// not through symbolic links, and with the exact spelling of the name. A name
// that is no valid branch or tag name (empty, a trailing slash, an empty, "." or
// ".." segment, a character outside the grammar) is an error, and so is a ref
// that cannot be read that way.
func RefTargets(fsys FS, dir, name string) ([]RefTarget, error) {
	gitDir, common, err := gitLayout(fsys, dir)
	if err != nil {
		return nil, err
	}
	if !validRefName(name) {
		return nil, fmt.Errorf("%q is not a valid branch or tag name", name)
	}
	packed, fullyPeeled, err := readPackedRefs(fsys, common)
	if err != nil {
		return nil, err
	}
	var targets []RefTarget
	for _, ref := range []string{"refs/heads/" + name, "refs/tags/" + name, "refs/remotes/origin/" + name} {
		tag := strings.HasPrefix(ref, "refs/tags/")
		found := false
		for _, base := range []string{gitDir, common} {
			data, ok, err := readGitFile(fsys, base, strings.Split(ref, "/"), maxRefBytes)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", ref, err)
			}
			if ok {
				if id := strings.TrimSpace(string(data)); commitID.MatchString(id) {
					targets = append(targets, RefTarget{Ref: ref, ID: id, MaybeTagObject: tag})
				}
				found = true
				break
			}
		}
		for i := 0; !found && i < len(packed); i++ {
			if id, packedRef, ok := strings.Cut(packed[i], " "); ok && packedRef == ref && commitID.MatchString(id) {
				target := RefTarget{Ref: ref, ID: id}
				switch peeled := i+1 < len(packed) && strings.HasPrefix(packed[i+1], "^") && commitID.MatchString(packed[i+1][1:]); {
				case peeled:
					// The line that follows is the commit an annotated tag points at.
					target.ID = packed[i+1][1:]
				case tag && !fullyPeeled:
					// Without a peeled line a tag is lightweight only when packed-refs
					// says that every annotated tag has one.
					target.MaybeTagObject = true
				}
				targets = append(targets, target)
			}
		}
	}
	return targets, nil
}

// readPackedRefs reads packed-refs of the common git directory, bounded and not
// through a symbolic link; it returns its lines and whether its header says that
// the annotated tags are all followed by a peeled line (fully-peeled).
func readPackedRefs(fsys FS, common string) (lines []string, fullyPeeled bool, err error) {
	data, found, err := readGitFile(fsys, common, []string{"packed-refs"}, maxPackedRefsBytes)
	if err != nil {
		return nil, false, fmt.Errorf("packed-refs: %w", err)
	}
	if !found {
		return nil, false, nil
	}
	lines = strings.Split(string(data), "\n")
	return lines, strings.HasPrefix(lines[0], "#") && strings.Contains(lines[0], " fully-peeled"), nil
}

// errNotFollowed is the reason a git file is not read although it is there.
var errNotFollowed = errors.New("is a symbolic link or not a regular file, which is not read")

// lookup finds the entry called name in the directory dir, spelled as the
// directory lists it (the open of a file system that ignores case would succeed
// for another spelling); it is nil when there is none, or no such directory.
// The entry says what it is without following a link.
func lookup(fsys FS, dir, name string) (fs.DirEntry, error) {
	entries, err := fsys.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	for _, candidate := range entries {
		if candidate.Name() == name {
			return candidate, nil
		}
	}
	return nil, nil
}

// readGitFile reads the file base/segments..., but only when every name on the
// way is spelled as it is in the directory listing, the directories are
// directories and not links, and the file is a regular file, not a link, a
// device or a pipe, of at most max bytes. found is false when a name is not
// there; an error says why something that is there is not read.
func readGitFile(fsys FS, base string, segments []string, max int64) (data []byte, found bool, err error) {
	current := base
	for i, segment := range segments {
		entry, err := lookup(fsys, current, segment)
		switch {
		case err != nil || entry == nil:
			return nil, false, err
		case i < len(segments)-1 && !entry.IsDir(), i == len(segments)-1 && !entry.Type().IsRegular():
			return nil, false, fmt.Errorf("%s %w", filepath.Join(current, segment), errNotFollowed)
		}
		current = filepath.Join(current, segment)
	}
	data, err = fsys.ReadFileMax(current, max)
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", current, err)
	}
	return data, true, nil
}

// pointedTo reads the file dir/name, which holds a path (after prefix), relative
// to dir: a regular file that is not a link, of at most maxGitPointerBytes. A
// file that is not there is fs.ErrNotExist.
func pointedTo(fsys FS, dir, name, prefix string) (string, error) {
	data, found, err := readGitFile(fsys, dir, []string{name}, maxGitPointerBytes)
	if err == nil && !found {
		err = fs.ErrNotExist
	}
	if err != nil {
		return "", err
	}
	target, ok := strings.CutPrefix(strings.TrimSpace(string(data)), prefix)
	if !ok || target == "" {
		return "", fmt.Errorf("%s does not name a git directory", filepath.Join(dir, name))
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(dir, target)
	}
	return target, nil
}

// readRef finds the commit a ref points at, in a loose ref file or in
// packed-refs.
func readRef(fsys FS, gitDir, common, ref string) (string, error) {
	if !validSegments(ref) {
		return "", fmt.Errorf("its HEAD names the ref %q, which is not a ref name", ref)
	}
	for _, base := range []string{gitDir, common} {
		data, found, err := readGitFile(fsys, base, strings.Split(ref, "/"), maxRefBytes)
		if err != nil {
			return "", fmt.Errorf("the ref %q: %w", ref, err)
		}
		if found {
			return strings.TrimSpace(string(data)), nil
		}
	}
	packed, _, err := readPackedRefs(fsys, common)
	if err != nil {
		return "", err
	}
	for _, line := range packed {
		if id, name, ok := strings.Cut(line, " "); ok && name == ref {
			return id, nil
		}
	}
	return "", fmt.Errorf("its HEAD names the ref %q, which is not found", ref)
}
