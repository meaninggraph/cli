package meaning

import (
	"io/fs"
	"os"
)

// FS is the file system the library reads meaning files, and the models they
// name, through. Paths are the host's own (filepath) paths. The library never
// writes, never touches the network and never starts a process.
type FS interface {
	ReadFile(name string) ([]byte, error)
	ReadDir(name string) ([]fs.DirEntry, error)
}

// OSFS reads the host's file system.
type OSFS struct{}

// ReadFile reads a file of the host's file system.
func (OSFS) ReadFile(name string) ([]byte, error) { return os.ReadFile(name) }

// ReadDir lists a directory of the host's file system, sorted by name.
func (OSFS) ReadDir(name string) ([]fs.DirEntry, error) { return os.ReadDir(name) }
