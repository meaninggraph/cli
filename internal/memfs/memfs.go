// Package memfs is an in-memory file system for tests of code that reads
// through meaning.FS. Paths are slash-separated; a leading slash is ignored.
package memfs

import (
	"io/fs"
	"path"
	"path/filepath"
	"strings"
	"testing/fstest"
)

// FS holds files in memory.
type FS struct{ files fstest.MapFS }

// New returns a file system holding the given files (path to content). The
// paths named in symlinks are symbolic links instead of regular files.
func New(files map[string]string, symlinks ...string) FS {
	m := fstest.MapFS{}
	for name, content := range files {
		m[clean(name)] = &fstest.MapFile{Data: []byte(content)}
	}
	for _, name := range symlinks {
		m[clean(name)].Mode = fs.ModeSymlink
	}
	return FS{files: m}
}

func clean(name string) string {
	return path.Clean(strings.TrimPrefix(filepath.ToSlash(name), "/"))
}

// Stat describes a file or directory.
func (m FS) Stat(name string) (fs.FileInfo, error) { return m.files.Stat(clean(name)) }

// ReadFile returns the content of a file.
func (m FS) ReadFile(name string) ([]byte, error) { return m.files.ReadFile(clean(name)) }

// ReadDir lists a directory, sorted by name.
func (m FS) ReadDir(name string) ([]fs.DirEntry, error) { return m.files.ReadDir(clean(name)) }
