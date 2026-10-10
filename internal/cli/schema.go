package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/meaninggraph/cli/pkg/meaning"
)

// newSchemaCommand prints the schema embedded in the binary, so that a consumer
// that pins both this tool and a commit of github.com/meaninggraph/core can
// compare the schema the tool checks against with the one in its core checkout.
func newSchemaCommand() *cobra.Command {
	var source bool
	var draft int
	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Print the schema of meaning/draft-1 or meaning/draft-2 embedded in this binary",
		Long: `Print meaning.schema.json, the schema of format meaning/draft-1 that "check"
validates a file in that format against, exactly as it is embedded in the binary:
the same bytes as the file of the same name in github.com/meaninggraph/core at the
commit it was taken from, so that the two can be compared byte for byte.

With --draft 2, print meaning.draft-2.schema.json, the schema of meaning/draft-2,
the same way. With --source, print the commit of github.com/meaninggraph/core the
schema was taken from instead (40 hexadecimal digits and a line break): the one
recorded in pkg/meaning/meaning.schema.source, or in meaning.draft-2.schema.source
with --draft 2.`,
		Example: `  meaninggraph schema | cmp - ../core/meaning.schema.json
  meaninggraph schema --draft 2 | cmp - ../core/meaning.draft-2.schema.json
  test "$(meaninggraph schema --source)" = "$(git -C ../core rev-parse HEAD)"`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format := meaning.Draft1
			switch draft {
			case 1:
			case 2:
				format = meaning.Draft2
			default:
				return fmt.Errorf("invalid --draft %d: expected 1 or 2", draft)
			}
			out := meaning.SchemaJSONFor(format)
			if source {
				out = []byte(meaning.SchemaCommitFor(format) + "\n")
			}
			_, err := cmd.OutOrStdout().Write(out)
			return err
		},
	}
	cmd.Flags().BoolVar(&source, "source", false, "print the commit of github.com/meaninggraph/core the schema was taken from, not the schema")
	cmd.Flags().IntVar(&draft, "draft", 1, "the format whose schema is printed: 1 for meaning/draft-1, 2 for meaning/draft-2")
	return cmd
}
