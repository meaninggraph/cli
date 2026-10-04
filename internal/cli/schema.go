package cli

import (
	"github.com/spf13/cobra"

	"github.com/meaninggraph/cli/pkg/meaning"
)

// newSchemaCommand prints the schema embedded in the binary, so that a consumer
// that pins both this tool and a commit of github.com/meaninggraph/core can
// compare the schema the tool checks against with the one in its core checkout.
func newSchemaCommand() *cobra.Command {
	var source bool
	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Print the meaning.schema.json embedded in this binary",
		Long: `Print meaning.schema.json, the schema of format meaning/draft-1 that "check"
validates against, exactly as it is embedded in the binary: the same bytes as the
file of the same name in github.com/meaninggraph/core at the commit it was taken
from, so that the two can be compared byte for byte.

With --source, print that commit instead (40 hexadecimal digits and a line break):
the commit of github.com/meaninggraph/core recorded in pkg/meaning/meaning.schema.source.`,
		Example: `  meaninggraph schema | cmp - ../core/meaning.schema.json
  test "$(meaninggraph schema --source)" = "$(git -C ../core rev-parse HEAD)"`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := meaning.SchemaJSON()
			if source {
				out = []byte(meaning.SchemaCommit() + "\n")
			}
			_, err := cmd.OutOrStdout().Write(out)
			return err
		},
	}
	cmd.Flags().BoolVar(&source, "source", false, "print the commit of github.com/meaninggraph/core the schema was taken from, not the schema")
	return cmd
}
