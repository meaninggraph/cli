package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meaninggraph/cli/pkg/meaning"
)

type rewriteOptions struct{ write bool }

func newRewriteCommand(env Env) *cobra.Command {
	var o rewriteOptions
	cmd := &cobra.Command{
		Use:   "rewrite [path...]",
		Short: "Write hand-written meaning/draft-1 files in meaning/draft-2 (a dry run unless --write)",
		Long: `Write meaning files in meaning/draft-2: the format line, the kind attribute
(property), the binding key property: (field:) and the role names entity and
foreign-key (instances and reference). Those words are changed in place and
nothing else is: comments and layout stay. A file already in meaning/draft-2 is
left as it is.

A path is a directory, whose *.meaning.yaml files directly in it are rewritten, or
a file. The default path is the current directory. Without --write nothing is
written: the command says what it would change, file by file.

A concept that carries values is listed and not changed. In meaning/draft-2 a list
of values stands on a concept of kind value-set and nowhere else, and whether the
concept becomes a value set, or its values move to one, is for the owner of the
graph to decide. The files are written with their other words changed, and
"check" names each such concept until it is edited.

The command reads what it wrote: a file whose result is not the data of the file
with those words changed, or that holds a word in a form it does not know how to
edit, is not rewritten; the command says which line, writes nothing at all, and
exits 2. Files that a generator writes are changed in the generator, which writes
them again.`,
		Example: `  meaninggraph rewrite model
  meaninggraph rewrite model --write`,
		RunE: func(cmd *cobra.Command, args []string) error { return o.run(cmd, env, args) },
	}
	cmd.Flags().BoolVar(&o.write, "write", false, "write the files; without it, only say what would change")
	return cmd
}

// rewriteJob is one meaning file and what a rewrite makes of it.
type rewriteJob struct {
	path   string
	result *meaning.Rewritten
}

func (o *rewriteOptions) run(cmd *cobra.Command, env Env, args []string) error {
	paths, err := rewritePaths(env, args)
	if err != nil {
		return err
	}
	var jobs []rewriteJob
	var failures []string
	for _, path := range paths {
		data, err := env.FS.ReadFile(path)
		if err != nil {
			return err
		}
		result, err := meaning.RewriteDraft2(data)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		jobs = append(jobs, rewriteJob{path, result})
	}
	if len(failures) > 0 {
		// The files of a graph change format together: one that cannot be rewritten holds all of them back.
		return fmt.Errorf("nothing was written; %s", strings.Join(failures, "; "))
	}
	out := cmd.OutOrStdout()
	changed := 0
	for _, job := range jobs {
		switch {
		case job.result.Changed():
			changed++
			_, _ = fmt.Fprintf(out, "%s: %s -> %s: %s\n", job.path, meaning.Draft1, meaning.Draft2, job.result.Describe())
		default:
			_, _ = fmt.Fprintf(out, "%s: already %s\n", job.path, meaning.Draft2)
		}
		for _, v := range job.result.Values {
			_, _ = fmt.Fprintf(out, "%s:%d: concept %s (kind %s) carries values and is not changed; in %s values stand on a concept of kind value-set only, so make it a value-set, or move the values to one that it names with values-of\n", job.path, v.Line, v.ID, v.Kind, meaning.Draft2)
		}
	}
	if !o.write {
		_, _ = fmt.Fprintf(out, "dry run: %s would be written; run with --write to write them\n", plural(changed, "file"))
		return nil
	}
	for _, job := range jobs {
		if job.result.Changed() {
			if err := env.WriteFile(job.path, job.result.Text); err != nil {
				return fmt.Errorf("%s: %w", job.path, err)
			}
		}
	}
	_, _ = fmt.Fprintf(out, "wrote %s\n", plural(changed, "file"))
	return nil
}

// rewritePaths lists the meaning files that the paths name: a file as it is, a
// directory by the *.meaning.yaml files directly in it (a symbolic link is not
// one), each once.
func rewritePaths(env Env, args []string) ([]string, error) {
	if len(args) == 0 {
		args = []string{"."}
	}
	var paths []string
	seen := map[string]bool{}
	for _, arg := range args {
		if arg == "" {
			return nil, errors.New("an empty path was given; name a directory or a file (is a variable unset?)")
		}
		path := filepath.Clean(arg)
		fi, err := env.FS.Stat(path)
		if err != nil {
			return nil, err
		}
		found := []string{path}
		if fi.IsDir() {
			entries, err := env.FS.ReadDir(path)
			if err != nil {
				return nil, err
			}
			found = nil
			for _, entry := range entries {
				if strings.HasSuffix(entry.Name(), meaning.FileSuffix) && entry.Type().IsRegular() {
					found = append(found, filepath.Join(path, entry.Name()))
				}
			}
		}
		for _, p := range found {
			abs, err := env.Abs(p)
			if err != nil {
				return nil, err
			}
			if !seen[abs] {
				seen[abs] = true
				paths = append(paths, p)
			}
		}
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no %s file in %s", "*"+meaning.FileSuffix, strings.Join(args, ", "))
	}
	slices.Sort(paths)
	return paths, nil
}
