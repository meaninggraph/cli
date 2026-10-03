package cli

import (
	"net/http"
	"time"

	"github.com/spf13/cobra"
	"github.com/strongo/cli-helpers/selfupdate"
	selfupdatecmd "github.com/strongo/cli-helpers/selfupdate/cobracmd"
)

// selfUpdateConfig says where meaninggraph's releases are and how they are
// named: the GitHub releases of meaninggraph/cli, with the archive and
// checksum names GoReleaser writes (the library's defaults). Every release
// archive is checked against the release's SHA-256 checksums before it replaces
// the running binary.
func selfUpdateConfig() selfupdate.Config {
	return selfupdate.Config{
		BinaryName:     "meaninggraph",
		Repository:     "meaninggraph/cli",
		CurrentVersion: info.Version,
		SupportedPlatforms: []selfupdate.Platform{
			{GOOS: "linux", GOARCH: "amd64"}, {GOOS: "linux", GOARCH: "arm64"},
			{GOOS: "darwin", GOARCH: "amd64"}, {GOOS: "darwin", GOARCH: "arm64"},
			{GOOS: "windows", GOARCH: "amd64"},
		},
		VersionProbeArgs: []string{"--version"},
		HTTPClient:       &http.Client{Timeout: 30 * time.Second},
	}
}

// newSelfUpdateCommand is the library's self-update command. Its exit codes
// are the command line's own: 0 when it succeeded (including `--check`,
// whether or not an update exists), 2 when it could not do what it was asked.
func newSelfUpdateCommand(env Env) *cobra.Command {
	return selfupdatecmd.New(env.SelfUpdate(), selfupdatecmd.CommandOptions{
		Short:       "Update this binary in place from the latest GitHub release",
		Aliases:     []string{"update"},
		JSONFormat:  true,
		Interactive: env.Interactive,
	})
}
