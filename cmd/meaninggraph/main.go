// Command meaninggraph checks meaning files: the concepts of a data model and
// their bindings to the properties of a ModelSpec model.
package main

import (
	"os"

	"github.com/meaninggraph/cli/internal/cli"
)

var exit = os.Exit

func main() {
	exit(cli.Run(os.Args[1:], cli.OSEnv()))
}
