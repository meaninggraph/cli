# meaninggraph

`meaninggraph` checks **meaning files**: the `*.meaning.yaml` files that say what the entities and properties of a data model mean. A meaning file declares concepts (country, currency, invoice, ...), lets a concept extend another, and binds concepts to the properties of a [ModelSpec](https://github.com/specscore/modelspec) model. The format, its JSON Schema and the universal concepts live in [github.com/meaninggraph/core](https://github.com/meaninggraph/core) (`FORMAT.md`, `meaning.schema.json`).

`meaninggraph check` is one installable binary that checks the same things as the reference checker in that repository (a Node script that other repositories fetch at a pinned commit): same schema, same cross-concept rules, same binding checks. It never accepts a file the reference checker refuses; it refuses some files the reference checker accepts, each on a stated rule (see Differences). It reads files, writes nothing, sends nothing and has no telemetry.

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

A path is a directory or a file. A **directory** is one graph: the `*.meaning.yaml` files directly in it (files in subdirectories are not part of it; `check` warns about them, and about meaning files that are symbolic links and directories it cannot read). The **files** you name together are one more graph. The default path is `.`; an empty path (`check ""`) is a usage error, and a directory given twice, in any spelling, is checked once.

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
| `--graph <host>/<org>/<repo>=<dir>` | Another graph that references may name, read from a local directory. Repeatable. The directory is checked against the schema like any graph (a supplied graph that is itself invalid is an `unresolved-graph` error). A graph that no file refers to is a warning (`unused-graph`), which says so when the address differs from a referenced one only by case; addresses are compared exactly. |
| `--address <host>/<org>/<repo>` | The address of the graph being checked, so that a `meaning://` reference to itself resolves (it must not carry `?ref=`). Valid when one graph is checked. |
| `--profile universal` | The extra rules `core` applies to its own universal concepts: a `LICENSE` file, `license: CC0-1.0` on every file, no `models` and no bindings, no meaning file below the root, and one word names one concept. Needs a directory. |

Output is deterministic: findings are sorted by file, line, rule and message, and graphs by path. Each finding has a file, a line when known, a stable rule id, a severity (`error`, `warning` or `info`) and a message.

**Exit codes**

| Code | Meaning |
|---|---|
| 0 | No finding of error severity. Warnings and info do not fail a check. |
| 1 | At least one finding of error severity. |
| 2 | Wrong usage, or a file or directory that cannot be read. Nothing was checked. With `--format json` the error is also written to stdout as `{"tool", "version", "ok": false, "error"}`, so a program that reads stdout always gets JSON. |

### What it checks

- Every file is inside the YAML subset below (one document, no repeated keys, no anchors or tags ...) and valid against `meaning.schema.json`, embedded in the binary from the commit recorded in `pkg/meaning/meaning.schema.source`.
- Every reference resolves: bare ids in the graph, `meaning://` references in another graph; one `?ref=` pin per other graph across all files.
- `extends` joins compatible kinds and never forms a cycle (also through another graph); `of`, `values-of` and `units-of` name entities, and `values-of` and `units-of` may only narrow what a parent names; a `unit` names exactly one value of its `units-of` entity.
- Measure `inputs` and `dimensions` are of the right kinds; a ratio is never summed, counted or averaged, stated or inherited.
- Concept ids, value ids and source ids are unique; one word names one value of a concept, in any language, ignoring case.
- Every binding names a module listed in `models`, an entity and a property that exist in that module's ModelSpec file, and fits its role: one entity binding per concept, `identifier` and `display-name` on the concept's own entity (identifier in the key, display name a string), `value` not a reference, `foreign-key` pointing at the entity whose rows are the instances of the concept (or of its `values-of` entity).

### What it does not check

- Data. It does not read a dataset, so it does not check that the values stored in a bound column are all known values (`match: labels`/`codes.<code>`).
- That the files you supplied are the pinned commit, beyond what a checkout's `HEAD` says. For each `?ref=` pin read from a directory given with `--graph`, `check` reads `.git/HEAD` (and the ref file or `packed-refs`; no `git` process, no network): the same commit is silent, a different commit is an error (`pin-checkout-mismatch`), and a directory that is not a git checkout (an archive, a copy) cannot be verified, which is a warning (`pin-not-verified`). It does not look at the working tree or the objects, so a checkout with edited, added or deleted files at the right commit passes.
- ModelSpec beyond what bindings read: only the entities, keys and properties of the model are used, and models of another repository are not read (a binding to one is an error, as in the reference checker).
- Registry-level rules (who may publish a graph, its licences, its dependencies).

### The YAML subset

The reference checker reads YAML with the `yaml` package of npm. That is a YAML 1.2 parser with many lenient corners, and no Go YAML library reads exactly the same files (see "Why the YAML reader is part of this repository"). So `meaning.schema.json` is checked on a plainly defined subset of YAML 1.2, which `check` enforces before it interprets anything. A file outside the subset gets a finding with a rule id of its own that says what to write instead; it is never half-read. On every file inside the subset the CLI reads the same values as the reference parser.

| Rule | What it refuses |
|---|---|
| `yaml-encoding` | Text that is not UTF-8 (a leading byte order mark is accepted). |
| `yaml-character` | NUL and other control characters, DEL, U+0085, U+2028, U+2029. Write them as `\u` escapes in a double-quoted string. |
| `yaml-line-ending` | A carriage return that is not part of a CRLF. Lines end in LF or CRLF. |
| `yaml-tab` | A tab character anywhere in the file. Indent with spaces; write `\t` in a double-quoted string. |
| `yaml-directive` | `%YAML`, `%TAG` and any other `%` directive. |
| `yaml-documents` | A second document (`---` after content) or a document end marker; exactly one document per file. |
| `yaml-anchor` | Anchors (`&`), aliases (`*`) and merge keys (`<<`). Write the value out. |
| `yaml-tag` | Tags of any kind, `!!str` included (an earlier version let `!!str` through; it is refused now, because every tag but one needed its own rule). Write the value plainly, in quotes when it must be a string. |
| `yaml-key` | A key that is not a string: `true`, `null`, `12`, `1.5` (YAML reads those as a boolean, null or a number; quote the key), a key over 1024 characters. |
| `yaml-duplicate-key` | A key repeated in one mapping. |
| `yaml-number` | A hexadecimal or octal number, `.inf`, `.nan`, an integer beyond 2^53 (a double cannot hold it exactly). |
| `yaml-escape` | An escape in a double-quoted string other than the JSON ones, `\/` and `\u` (surrogate pairs included), and a malformed one. |
| `yaml-unsupported` | Explicit keys (`?`), an indentation indicator on a block scalar (`|2`), `[key: value]` pairs, comments inside `[ ]` or `{ }`, quoted scalars that continue on a second line. |
| `yaml-limit` | A file over 8 MiB or nesting over 64 levels (a model file is limited to 8 MiB and nesting 2). |
| `yaml` | Anything else that YAML 1.2 does not allow (bad indentation, an unclosed quote or collection ...). |

Inside the subset: block mappings and sequences, flow collections (which may span lines), plain, single- and double-quoted scalars, literal and folded block scalars (`|`, `>`, with `-` or `+`), full-line comments, and a leading `---`.

### Why the YAML reader is part of this repository

An earlier version used `go.yaml.in/yaml/v3`. It is a YAML 1.1 scanner and refused valid YAML 1.2 that the reference checker accepts (`?` inside a flow collection, a line holding only a tab, JSON escapes such as `\/`). The other Go libraries were compared with the mutation run below, on 6,000 mutants of the real files after the text rules above:

| Parser | Accepts where the reference refuses | Refuses where the reference accepts |
|---|---|---|
| `go.yaml.in/yaml/v3` | 270 | 995 |
| `go.yaml.in/yaml/v4` (rc6) | 327 | 502 |
| `github.com/goccy/go-yaml` | 86 | 498 |

All three are lenient in flow collections (a `}` inside a plain flow scalar, text after a block scalar header), which is the direction that matters: a file they read and the reference refuses. None gets to zero, and a fix would be a patch to someone else's scanner. The files that meaning graphs are written in (the `core` concepts and the Chinook graph) use only block mappings and sequences, `>-` block scalars, single-line flow collections, single-line quoted scalars and comments. So the CLI has a small reader for a declared subset, `pkg/meaning/yaml.go` and `pkg/meaning/yaml_flow.go`, with 100% coverage and the mutation run as its test of agreement. `go.yaml.in/yaml/v3` stays in `go.mod` for one reason: the tests that parse the CI workflows.

### A reference to a graph that was not supplied is an error

The reference checker reports a reference that does not resolve (an unknown repository, a pin that cannot be fetched, a repository referenced without a pin) as a problem, and `FORMAT.md` lists it among what a checker reports. `check` does the same: a reference to a graph you did not supply with `--graph` is an error that names the missing graph, and an unpinned reference to another graph is an error. It does not skip such references as "not checked", because a check that silently skipped them would accept files the reference checker refuses, and a green CI job would then prove less than it says. The cost is one flag; the message says which graph is missing.

### Differences from the reference checker

`meaninggraph` never accepts a file the reference checker refuses (the differential test and the mutation run check this). It refuses these kinds of input that the reference checker accepts, on purpose; each is a corpus item marked `stricter` with its reason (see Testing):

- **Anything outside the YAML subset**, rule by rule as in the table above: tabs, directives, anchors, aliases and merge keys, any tag (`!!str` included), non-string keys, hexadecimal and octal numbers, integers beyond 2^53, other encodings, control characters, explicit keys, comments in flow collections and so on. The reference parser reads some of these in a way a person would not expect (a tag changes the type, `0x7E9` is a year of 2025), and some it merely accepts.
- **A directory with no meaning file.** The reference checker returns no problems for no files; `check` reports `no-meaning-files`, so that a CI job pointed at the wrong directory does not pass by reading nothing.
- **A model whose entity `key` is a string instead of a list** (`key = "Id"`). JavaScript accepts it because `String.includes` finds a property name inside the string; ModelSpec requires a list. `entity = true` on a property is not an entity named `true` either; the entity must be a string.
- **A supplied graph (`--graph`) that is not itself valid.** The reference checker validates only the graph it was asked to check.
- **A binding to a prototype-named entity or property** (below).

Words are matched ignoring case with the Unicode 16 tables (`golang.org/x/text` v0.42.0), so the case pairs that Node's `toLowerCase` folds (for example U+A7CB and U+0264) are the same word here too (a test lists them). A `models:` path with a trailing slash is refused, as the reference checker refuses it.

**Prototype-named model members.** The reference checker keeps the entities, properties, components, fields and enums of a model in plain JavaScript objects and asks `name in object`. The names of `Object.prototype` (`constructor`, `toString`, `valueOf`, `hasOwnProperty`, `isPrototypeOf`, `propertyIsEnumerable`, `toLocaleString`, `__proto__`, `__defineGetter__`, `__defineSetter__`, `__lookupGetter__`, `__lookupSetter__`) are found in every object, so a model that declares one is refused as having declared it twice, and a binding to an entity that a model lacks is found. That is a bug in the reference checker, not a rule of ModelSpec. `check` reports a model that declares one of these names as an error that says so and says to rename it, because the reference checker will refuse the same model; it does not pretend the model is fine. The names are listed once, in `pkg/meaning/hcl.go` (`PrototypeNames()`). A binding to such a name the model does not declare is refused as any missing entity. The question is open with the reference checker's maintainers, together with the YAML subset: <https://github.com/meaninggraph/core/issues/2>.

One more difference is not about verdicts: YAML syntax errors and unreadable models are findings (exit 1), where the reference checker stops with an exception.

## The library

`github.com/meaninggraph/cli/pkg/meaning` is importable: it parses meaning files, validates them against the schema, resolves `extends` and bindings, and returns a typed graph and a sorted list of findings. It has no command framework, never exits the process, and never touches the network; the file system, other graphs and the model reader come in through `FS`, `Resolver` and `ModelReader`. `OSFS` is opt-in: no function defaults to the host file system, and `Checker` needs no file system at all once a graph is loaded. The embedded schema is reached through `SchemaJSON()` (a copy), `SchemaSource()` and `SchemaCommit()`; no mutable variable is exported. `Graph.Address` is the `<host>/<org>/<repo>` a graph answers to when it is supplied as a reference target, and must be set (`GraphResolver` refuses a supplied graph without one); it is also what a `meaning://` reference to the graph itself is resolved against. `extends` is resolved in near-linear time (a chain of 5,000 concepts is a test), and files and nesting are bounded (`MaxFileBytes`, `MaxYAMLDepth`).

```go
g, err := meaning.LoadDir(meaning.OSFS{}, "model")
findings := meaning.Checker{Resolve: meaning.GraphResolver(others)}.Check(g)
```

**The model reader is a stand-in.** `HCLReader` reads the ModelSpec model the way the Node reference checker does (a port of its small HCL parser). It sits behind one interface, `ModelReader`, and is to be replaced by the ModelSpec library (`github.com/modelspec-org/cli`, package `pkg/modelspec`) once that has a release. Nothing else changes when it is.

## Testing

`go test ./...` needs nothing but Go: no network, no Node, no `git`, no subprocess and no write outside `t.TempDir()`. The file system, the output streams, the terminal and the update server are injected, and the suite runs in a few seconds with `-race`.

**100% statement coverage, exactly.** CI runs the tests with the race detector and then `go run ./cmd/covergate cover.out`, which compares covered with total statements in the cover profile (never a rounded percentage). The gate has no threshold, flag or environment variable to lower it, and a test fails if the workflow or the gate is loosened. Every package is covered by its own tests, `main` included.

**Differential test against the reference checker.** `testdata/corpus` holds 221 items: the `core` concepts and the Chinook graph (both check clean), edge cases that must stay accepted (valid YAML 1.2 that other Go parsers refuse among them), refusals with one defect each, including every refusal case of the reference checker's own tests, and the `stricter` items that document each difference above. `testdata/golden/node-verdicts.json` records what the Node reference checker said about each item, with the `core` commit it ran at. `go test` runs `meaninggraph check` over every item and compares: it fails if the CLI accepts anything the reference refuses, or refuses something the reference accepts without a `stricter` reason in the item.

```sh
# refresh the golden verdicts (needs Node and a clean checkout of core, `npm ci` done)
scripts/differential/regenerate.sh /path/to/core
# is the embedded schema still the one in core?
scripts/check-schema-drift.sh /path/to/core
```

`regenerate.sh` runs the drift check first, so the verdicts are always recorded at the commit of the embedded schema.

**Mutation run.** `scripts/differential/mutation` (a separate Go module, so it is outside `./...`, outside `go test` and outside the coverage gate) writes random text-level mutants of the core and Chinook files and of corpus items (inserted tabs, carriage returns, quotes, `?`, tags, anchors, escapes, deleted and swapped lines ...), asks the Node reference checker for each verdict, and runs `meaninggraph check` in process over the same directories. It counts the disagreements by direction and exits 1 on the one that must not happen: the CLI accepting what the reference checker refuses. For each mutated YAML file the reference parser reads, it also compares the values with what `ParseYAML` reads from the same bytes. The same seed gives the same mutants.

```sh
scripts/differential/mutation/run.sh /path/to/core /path/to/chinookdb 20000 1
```

The mutants where the CLI refuses and the reference checker accepts are grouped by rule; each group is one of the stricter rules above (tabs by far the largest, because a single stray tab is easy to insert), except the ones that are not the CLI's choice: `no-meaning-files` for a mutant that deletes the only file, and `unresolved-graph` for a corpus item that already carries a `stricter` reason.

`testdata/corpus/core` is a copy of the universal concepts of `github.com/meaninggraph/core` (CC0-1.0); `testdata/corpus/chinook` is the Chinook meaning file (CC0-1.0) and ModelSpec model (MIT, the licence text is `testdata/corpus/chinook/model/LICENSE`) of `github.com/datatug/chinookdb` at commit `8c9e62ed6641c0a00faa3867167d928af4c44b06`.

## Releases

`.github/workflows/release.yml` runs on a push to `main` and on nothing else: no tag push, no manual run, no schedule. It has two jobs. `gate` calls `.github/workflows/ci.yml` (the same workflow that runs on pull requests: format, vet, race tests, the 100% coverage gate, and a GoReleaser snapshot of the packaging), and `release` has `needs: gate`, so GitHub does not start it unless `gate` passed, in the same run, for the same commit. `release` calls the shared `strongo/cicd` release workflow, pinned to a commit: it derives the version from the conventional commits since the last tag (`feat:` is a minor, any other type except `chore:`, `ci:`, `docs:` and `refactor:`, which are skipped, is a patch), tags it, and GoReleaser publishes archives for linux, macOS and windows (amd64, arm64; no windows/arm64) with SHA-256 checksums. macOS binaries are not signed or notarised yet.

The shared workflow tags, and a tag does not start `release.yml` again, so there is no second path to a release: hand-pushing a tag publishes nothing, and nothing but this run creates one. The shared workflow has its own guard that waits for a caller's CI run for the commit; it is not used, because it carries on after three minutes when it finds none, which is the hole `needs: gate` closes. Tests in `internal/covergate` parse both workflows and fail on a loosening (a trigger added, `needs` or the `if` removed, `continue-on-error`, a moving tag for the shared workflow or an action), and a test fails if any Go file has a build constraint, which would put code outside what the gate measures.

## License

Apache-2.0, see `LICENSE`.
