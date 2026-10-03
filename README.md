# meaninggraph

`meaninggraph` checks **meaning files**: the `*.meaning.yaml` files that say what the entities and properties of a data model mean. A meaning file declares concepts (country, currency, invoice, ...), lets a concept extend another, and binds concepts to the properties of a [ModelSpec](https://github.com/specscore/modelspec) model. The format, its JSON Schema and the universal concepts live in [github.com/meaninggraph/core](https://github.com/meaninggraph/core) (`FORMAT.md`, `meaning.schema.json`).

`meaninggraph check` is one installable binary with the same verdict as the reference checker in that repository (a Node script that other repositories fetch at a pinned commit): same schema, same cross-concept rules, same binding checks. It reads files, writes nothing, sends nothing and has no telemetry.

## Install

There is no package-manager install yet. Use a release archive from GitHub, or `go install`.

**Pinned download, for CI.** Pin a version and the SHA-256 of the archive for your platform (the release also publishes `meaninggraph_<version>_checksums.txt`; copy the line for your archive into your repository, so that a changed archive fails your build):

```sh
VERSION=0.1.0                 # the release you pin, without the v
OS=linux ARCH=amd64           # linux, darwin or windows (zip); amd64 or arm64 (not windows/arm64)
SHA256=<the sha256 of that archive, from meaninggraph_${VERSION}_checksums.txt>

curl -fsSLO "https://github.com/meaninggraph/cli/releases/download/v${VERSION}/meaninggraph_${VERSION}_${OS}_${ARCH}.tar.gz"
echo "${SHA256}  meaninggraph_${VERSION}_${OS}_${ARCH}.tar.gz" | sha256sum -c -    # macOS: shasum -a 256 -c -
tar -xzf "meaninggraph_${VERSION}_${OS}_${ARCH}.tar.gz" meaninggraph
./meaninggraph version
```

**Go.**

```sh
go install github.com/meaninggraph/cli/cmd/meaninggraph@v0.1.0   # a released version
```

`meaninggraph self-update` replaces a binary you downloaded and put in a `bin` directory with the latest release, after checking the archive against the release's SHA-256 checksums (`--check` only reports, `--version` pins a release, `--dry-run` shows what would happen). It refuses when it cannot tell how the binary was installed, and a CI job should pin a version instead of updating.

## Commands

```
meaninggraph check [path...]        check meaning files
meaninggraph version                print the version, commit and build date  (also: --version)
meaninggraph self-update            update this binary from the latest GitHub release
```

### check

A path is a directory or a file. A **directory** is one graph: the `*.meaning.yaml` files directly in it (files in subdirectories are not part of it; `check` warns about them). The **files** you name together are one more graph. The default path is `.`.

```sh
meaninggraph check                                  # the current directory
meaninggraph check model                            # a dataset's meaning files kept in model/
meaninggraph check --format json . > report.json
```

A graph that references concepts of another graph (for example a dataset graph that extends the universal concepts, written `meaning://github.com/meaninggraph/core/country?ref=<commit>`) needs that other graph on disk. Nothing is fetched from the network: supply a checkout of it with `--graph`, repeatable:

```sh
git clone https://github.com/meaninggraph/core /tmp/core && git -C /tmp/core checkout <commit>
meaninggraph check model --graph github.com/meaninggraph/core=/tmp/core
```

| Flag | Meaning |
|---|---|
| `--format text\|json` | Output format. Text is `file:line: severity: message [rule]` plus one summary line per graph; JSON has the same findings with a stable shape. |
| `--graph <host>/<org>/<repo>=<dir>` | Another graph that references may name, read from a local directory. Repeatable. |
| `--address <host>/<org>/<repo>` | The address of the graph being checked, so that a `meaning://` reference to itself resolves (it must not carry `?ref=`). Valid when one graph is checked. |
| `--profile universal` | The extra rules `core` applies to its own universal concepts: a `LICENSE` file, `license: CC0-1.0` on every file, no `models` and no bindings, no meaning file below the root, and one word names one concept. Needs a directory. |

Output is deterministic: findings are sorted by file, line, rule and message, and graphs by path. Each finding has a file, a line when known, a stable rule id, a severity (`error`, `warning` or `info`) and a message.

**Exit codes**

| Code | Meaning |
|---|---|
| 0 | No finding of error severity. Warnings and info do not fail a check. |
| 1 | At least one finding of error severity. |
| 2 | Wrong usage, or a file or directory that cannot be read. Nothing was checked. |

### What it checks

- Every file is valid YAML (one document, no repeated keys) and valid against `meaning.schema.json`, embedded in the binary from the commit recorded in `pkg/meaning/meaning.schema.source`.
- Every reference resolves: bare ids in the graph, `meaning://` references in another graph; one `?ref=` pin per other graph across all files.
- `extends` joins compatible kinds and never forms a cycle (also through another graph); `of`, `values-of` and `units-of` name entities, and `values-of` and `units-of` may only narrow what a parent names; a `unit` names exactly one value of its `units-of` entity.
- Measure `inputs` and `dimensions` are of the right kinds; a ratio is never summed, counted or averaged, stated or inherited.
- Concept ids, value ids and source ids are unique; one word names one value of a concept, in any language, ignoring case.
- Every binding names a module listed in `models`, an entity and a property that exist in that module's ModelSpec file, and fits its role: one entity binding per concept, `identifier` and `display-name` on the concept's own entity (identifier in the key, display name a string), `value` not a reference, `foreign-key` pointing at the entity whose rows are the instances of the concept (or of its `values-of` entity).

### What it does not check

- Data. It does not read a dataset, so it does not check that the values stored in a bound column are all known values (`match: labels`/`codes.<code>`).
- That a `?ref=` pin is the commit of the directory you supplied. A pin read from a directory is accepted as given and reported as an `info` finding (`pin-not-verified`).
- ModelSpec beyond what bindings read: only the entities, keys and properties of the model are used, and models of another repository are not read (a binding to one is an error, as in the reference checker).
- Registry-level rules (who may publish a graph, its licences, its dependencies).

### A reference to a graph that was not supplied is an error

The reference checker reports a reference that does not resolve (an unknown repository, a pin that cannot be fetched, a repository referenced without a pin) as a problem, and `FORMAT.md` lists it among what a checker reports. `check` does the same: a reference to a graph you did not supply with `--graph` is an error that names the missing graph, and an unpinned reference to another graph is an error. It does not skip such references as "not checked", because a check that silently skipped them would accept files the reference checker refuses, and a green CI job would then prove less than it says. The cost is one flag; the message says which graph is missing.

### Differences from the reference checker

`meaninggraph` never accepts a file the reference checker refuses. It refuses three kinds of input the reference checker accepts, on purpose:

- **A directory with no meaning file.** The reference checker returns no problems for no files; `check` reports `no-meaning-files`, so that a CI job pointed at the wrong directory does not pass by reading nothing.
- **A model whose entity `key` is a string instead of a list** (`key = "Id"`). JavaScript accepts it because `String.includes` finds a property name inside the string; ModelSpec requires a list.
- **An explicit YAML tag other than `!!str`** (`year: !!int 2025`). The reference parser interprets such a tag; `check` refuses it and asks for the value to be written plainly.

One more difference is not about verdicts: YAML syntax errors and unreadable models are findings (exit 1), where the reference checker stops with an exception.

The three are the corpus items marked `stricter` (see Testing).

## The library

`github.com/meaninggraph/cli/pkg/meaning` is importable: it parses meaning files, validates them against the schema, resolves `extends` and bindings, and returns a typed graph and a sorted list of findings. It has no command framework, never exits the process, and never touches the network; the file system, other graphs and the model reader come in through `FS`, `Resolver` and `ModelReader`.

```go
g, err := meaning.LoadDir(meaning.OSFS{}, "model")
findings := meaning.Checker{Resolve: meaning.GraphResolver(others)}.Check(g)
```

**The model reader is a stand-in.** `HCLReader` reads the ModelSpec model the way the Node reference checker does (a port of its small HCL parser). It sits behind one interface, `ModelReader`, and is to be replaced by the ModelSpec library (`github.com/modelspec-org/cli`, package `pkg/modelspec`) once that has a release. Nothing else changes when it is.

## Testing

`go test ./...` needs nothing but Go: no network, no Node, no `git`, no subprocess and no write outside `t.TempDir()`. The file system, the output streams, the terminal and the update server are injected, and the suite runs in a few seconds with `-race`.

**100% statement coverage, exactly.** CI runs the tests with the race detector and then `go run ./cmd/covergate cover.out`, which compares covered with total statements in the cover profile (never a rounded percentage). The gate has no threshold, flag or environment variable to lower it, and a test fails if the workflow or the gate is loosened. Every package is covered by its own tests, `main` included.

**Differential test against the reference checker.** `testdata/corpus` holds 149 items: the `core` concepts and the Chinook graph (both check clean), edge cases that must stay accepted, and refusals with one defect each, including every refusal case of the reference checker's own tests. `testdata/golden/node-verdicts.json` records what the Node reference checker said about each item, with the `core` commit it ran at. `go test` runs `meaninggraph check` over every item and compares: it fails if the CLI accepts anything the reference refuses, or refuses something the reference accepts without a `stricter` reason in the item.

```sh
# refresh the golden verdicts (needs Node and a clean checkout of core, `npm ci` done)
scripts/differential/regenerate.sh /path/to/core
# is the embedded schema still the one in core?
scripts/check-schema-drift.sh /path/to/core
```

`regenerate.sh` runs the drift check first, so the verdicts are always recorded at the commit of the embedded schema.

`testdata/corpus/core` is a copy of the universal concepts of `github.com/meaninggraph/core` (CC0-1.0); `testdata/corpus/chinook` is the Chinook meaning file (CC0-1.0) and ModelSpec model (MIT) of `github.com/datatug/chinookdb` at commit `8c9e62ed6641c0a00faa3867167d928af4c44b06`.

## Releases

Every merge to `main` whose commits are `feat:` or `fix:` is released by `.github/workflows/release.yml` (the shared `strongo/cicd` release workflow): the version is derived from the conventional commits, tagged, and GoReleaser publishes archives for linux, macOS and windows (amd64, arm64; no windows/arm64) with SHA-256 checksums. The release refuses to run unless CI passed for the same commit. macOS binaries are not signed or notarised yet.

## License

Apache-2.0, see `LICENSE`.
