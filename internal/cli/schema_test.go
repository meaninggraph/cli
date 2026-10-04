package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/meaninggraph/cli/pkg/meaning"
)

// recorded reads a key of pkg/meaning/meaning.schema.source from the file.
func recorded(t *testing.T, key string) string {
	t.Helper()
	data, err := os.ReadFile("../../pkg/meaning/meaning.schema.source")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if value, ok := strings.CutPrefix(line, key+": "); ok {
			return value
		}
	}
	t.Fatalf("meaning.schema.source has no %q", key)
	return ""
}

// `schema` prints the embedded schema as it is embedded: the bytes of the file
// the binary was built from, none added, none changed, so that a consumer can
// compare them with meaning.schema.json of a core checkout.
func TestSchemaPrintsTheEmbeddedFileByteForByte(t *testing.T) {
	t.Parallel()
	file, err := os.ReadFile("../../pkg/meaning/meaning.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	got := execute(nil, "schema")
	if got.code != ExitClean || got.stderr != "" {
		t.Fatalf("got %+v", got)
	}
	if !bytes.Equal([]byte(got.stdout), file) || !bytes.Equal([]byte(got.stdout), meaning.SchemaJSON()) {
		t.Fatalf("the output (%d bytes) is not the embedded file (%d bytes)", len(got.stdout), len(file))
	}
	// The sha256 that meaning.schema.source records for the file is the output's.
	if sum := sha256.Sum256([]byte(got.stdout)); hex.EncodeToString(sum[:]) != recorded(t, "sha256") {
		t.Fatalf("sha256 of the output is %x, the source records %s", sum, recorded(t, "sha256"))
	}
	if again := execute(nil, "schema"); again.stdout != got.stdout {
		t.Fatal("the output changes between runs")
	}
}

// `schema --source` prints the commit of core that the schema was taken from,
// as meaning.schema.source records it, and nothing else.
func TestSchemaSourcePrintsTheCoreCommit(t *testing.T) {
	t.Parallel()
	got := execute(nil, "schema", "--source")
	want := recorded(t, "commit") + "\n"
	if got.code != ExitClean || got.stdout != want || got.stderr != "" {
		t.Fatalf("got %+v, want %q", got, want)
	}
	if !regexp.MustCompile(`^[0-9a-f]{40}\n$`).MatchString(got.stdout) || got.stdout != meaning.SchemaCommit()+"\n" {
		t.Fatalf("not a commit: %q", got.stdout)
	}
}

func TestSchemaTakesNoArgumentAndReportsAnOutputThatCannotBeWritten(t *testing.T) {
	t.Parallel()
	if got := execute(nil, "schema", "extra"); got.code != ExitUsage || got.stdout != "" || !strings.Contains(got.stderr, `unknown command "extra"`) {
		t.Fatalf("got %+v", got)
	}
	for _, args := range [][]string{{"schema"}, {"schema", "--source"}} {
		var stderr bytes.Buffer
		code := Run(args, Env{Stdout: failingWriter{}, Stderr: &stderr, SelfUpdate: offline})
		if code != ExitUsage || stderr.String() != "meaninggraph: closed pipe\n" {
			t.Errorf("%v: code %d stderr %q", args, code, stderr.String())
		}
	}
}
