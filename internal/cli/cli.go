// Package cli is the meaninggraph command line: the commands, their output and
// their exit codes. Everything that touches the outside world (the file
// system, the output streams, the terminal, the update server) comes in
// through Env, so the whole command line runs in a test without a process, a
// network or a real terminal.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/strongo/buildinfo"
	buildinfocmd "github.com/strongo/buildinfo/cobracmd"
	"github.com/strongo/cli-helpers/selfupdate"

	"github.com/meaninggraph/cli/pkg/meaning"
)

// Exit codes of the command line.
const (
	// ExitClean means the command succeeded: a check found no error.
	ExitClean = 0
	// ExitFindings means a check found at least one finding of error severity.
	ExitFindings = 1
	// ExitUsage means the command was used wrongly or a file could not be
	// read: nothing was checked.
	ExitUsage = 2
)

// info is this binary's build identity, stamped at link time by the release
// build and read from the Go build information otherwise.
var info = buildinfo.Get("meaninggraph")

// Env is everything the command line needs from the outside.
type Env struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	// FS reads the files that are checked.
	FS meaning.FS
	// Interactive says whether the process has a terminal to ask a question
	// on; nil means the update library's own terminal check.
	Interactive func() bool
	// SelfUpdate describes where updates come from; tests point it at a fake
	// release server.
	SelfUpdate func() selfupdate.Config
}

// OSEnv is the environment of the real process.
func OSEnv() Env {
	return Env{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, FS: meaning.OSFS{}, SelfUpdate: selfUpdateConfig}
}

// errFindings is returned by a check that found errors; Run turns it into
// ExitFindings and prints nothing more, since the findings were the output.
var errFindings = errors.New("the check found errors")

// Run runs the command line with the arguments after the program name and
// returns the exit code. It never exits the process itself.
func Run(args []string, env Env) int {
	err := newRoot(env).run(args)
	switch {
	case err == nil:
		return ExitClean
	case errors.Is(err, errFindings):
		return ExitFindings
	}
	fmt.Fprintf(env.Stderr, "meaninggraph: %v\n", err)
	return ExitUsage
}

type root struct{ cmd *cobra.Command }

func (r root) run(args []string) error {
	r.cmd.SetArgs(args)
	return r.cmd.Execute()
}

func newRoot(env Env) root {
	cmd := &cobra.Command{
		Use:           "meaninggraph",
		Short:         "Check meaning files: the concepts of a data model and their bindings to its properties",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.CompletionOptions.DisableDefaultCmd = true
	cmd.SetIn(env.Stdin)
	cmd.SetOut(env.Stdout)
	cmd.SetErr(env.Stderr)
	buildinfocmd.WireCobra(cmd, info)
	cmd.AddCommand(newCheckCommand(env), newSelfUpdateCommand(env))
	return root{cmd: cmd}
}
