// Command covergate fails unless a Go cover profile covers every statement.
//
//	go test -race -coverprofile=cover.out ./...
//	go run ./cmd/covergate cover.out
package main

import (
	"io"
	"io/fs"
	"os"

	"github.com/meaninggraph/cli/internal/covergate"
)

var (
	exit = os.Exit
	args = func() []string { return os.Args[1:] }
	open = func(name string) (io.ReadCloser, error) { return os.Open(name) }
	// root is the module's file tree, read for the packages that the profile must
	// hold: run the gate from the module root.
	root = func() fs.FS { return os.DirFS(".") }
)

func main() {
	exit(covergate.Run(args(), os.Stdout, os.Stderr, open, root()))
}
