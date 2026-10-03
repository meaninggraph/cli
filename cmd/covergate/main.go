// Command covergate fails unless a Go cover profile covers every statement.
//
//	go test -race -coverprofile=cover.out ./...
//	go run ./cmd/covergate cover.out
package main

import (
	"io"
	"os"

	"github.com/meaninggraph/cli/internal/covergate"
)

var (
	exit = os.Exit
	args = func() []string { return os.Args[1:] }
	open = func(name string) (io.ReadCloser, error) { return os.Open(name) }
)

func main() {
	exit(covergate.Run(args(), os.Stdout, os.Stderr, open))
}
