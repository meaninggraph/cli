package meaning

import (
	"errors"
	"io"
	"io/fs"
	"os"
)

// ErrTooLarge is the error of FS.ReadFileMax for a file of more than the bytes
// asked for.
var ErrTooLarge = errors.New("the file is larger than what is read of it")

// FS is the file system the library reads meaning files, and the models they
// name, through. Paths are the host's own (filepath) paths. The library never
// writes, never touches the network and never starts a process.
type FS interface {
	Stat(name string) (fs.FileInfo, error)
	ReadFile(name string) ([]byte, error)
	// ReadFileMax reads a file of at most max bytes without reading more of a
	// larger one, which is the error ErrTooLarge.
	ReadFileMax(name string, max int64) ([]byte, error)
	ReadDir(name string) ([]fs.DirEntry, error)
}

// OSFS reads the host's file system.
type OSFS struct{}

// Stat describes a file or directory of the host's file system.
func (OSFS) Stat(name string) (fs.FileInfo, error) { return os.Stat(name) }

// ReadFile reads a file of the host's file system.
func (OSFS) ReadFile(name string) ([]byte, error) { return os.ReadFile(name) }

// ReadDir lists a directory of the host's file system, sorted by name.
func (OSFS) ReadDir(name string) ([]fs.DirEntry, error) { return os.ReadDir(name) }

// ReadFileMax reads a file of the host's file system, at most max bytes of it.
func (OSFS) ReadFileMax(name string, max int64) ([]byte, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, ErrTooLarge
	}
	return data, nil
}
