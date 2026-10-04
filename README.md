# meaninggraph

`meaninggraph` checks **meaning files**: the `*.meaning.yaml` files that say what the entities and properties of a data model mean. A meaning file declares concepts (country, currency, invoice, ...), lets a concept extend another, and binds concepts to the properties of a [ModelSpec](https://github.com/specscore/modelspec) model. The format, its JSON Schema and the universal concepts live in [github.com/meaninggraph/core](https://github.com/meaninggraph/core) (`FORMAT.md`, `meaning.schema.json`).

`meaninggraph check` is one installable binary that checks the same things as the reference checker in that repository (a Node script that other repositories fetch at a pinned commit): same schema, same cross-concept rules, same binding checks. It is meant never to accept a file the reference checker refuses, and the tests and the mutation run below check that on thousands of files; it refuses some files the reference checker accepts, each on a stated rule (see Differences). It is tested against the reference checker, not proven equal to it. It reads files, writes nothing, sends nothing and has no telemetry.

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
meaninggraph schema [--source]      print the embedded meaning.schema.json, or the core commit it was taken from
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
| `--graph <host>/<org>/<repo>=<dir>` | Another graph that references may name, read from a local directory. Repeatable. A directory that is also one of the paths being checked is that graph, checked in full once, with this address (see "A graph and its dependency in one run"). The directory is checked against the schema like any graph (a supplied graph that is itself invalid is an `unresolved-graph` error). A graph that no file refers to is a warning (`unused-graph`), which says so when the address differs from a referenced one only by case; addresses are compared exactly. |
| `--address <host>/<org>/<repo>` or `--address <host>/<org>/<repo>=<path>` | The address of a graph being checked, so that a `meaning://` reference to itself resolves (it must not carry `?ref=`). The bare form is for one graph. When several are checked, give one `<address>=<path>` for each, repeated: `<path>` is one of the paths being checked, written any way (the files named together are one graph, so any one of them names it). A directory that is checked and is also given with `--graph <address>=<directory>` has that address without `--address`. Contradictions are usage errors (exit 2): two addresses for one graph, one address for two graphs, an address for a path that is not checked, a bare address when more than one graph is checked, a directory given with `--graph` under two addresses. |
| `--profile universal` | The extra rules `core` applies to its own universal concepts: a `LICENSE` file, `license: CC0-1.0` on every file, no `models` and no bindings, no meaning file below the root, and one word names one concept. Needs a directory. |

**A graph and its dependency in one run.** A consumer that wants its own graph and a graph it depends on both checked in full can name both and say which is which, instead of running the tool twice:

```sh
meaninggraph check model /tmp/core \
  --address github.com/datatug/chinookdb=model \
  --graph github.com/meaninggraph/core=/tmp/core
```

`model` is checked as `github.com/datatug/chinookdb`, and `/tmp/core` is both the directory references to `github.com/meaninggraph/core` are read from and a graph that is checked like any other, with that address (no `--address` is needed for it). Each graph has the findings it has in a run of its own (`check model --address github.com/datatug/chinookdb --graph github.com/meaninggraph/core=/tmp/core`, and `check /tmp/core --address github.com/meaninggraph/core`), and each has a summary line; nothing is checked twice. A test compares the one run with the two on a graph and a dependency that both have findings: the findings are the same, graph for graph. `--profile` applies to every graph that is checked, so it is not what you want for a dependency that is not a repository of universal concepts.

Output is deterministic: findings are sorted by file, line, rule and message, and graphs by path. Each finding has a file, a line when known, a stable rule id, a severity (`error`, `warning` or `info`) and a message.

**Exit codes**

| Code | Meaning |
|---|---|
| 0 | No finding of error severity. Warnings and info do not fail a check. |
| 1 | At least one finding of error severity. |
| 2 | Wrong usage, or a file or directory that cannot be read. Nothing was checked. With `--format json` the error is also written to stdout as `{"tool", "version", "ok": false, "error"}`, so a program that reads stdout always gets JSON. |

### schema

`meaninggraph schema` prints `meaning.schema.json`, the schema `check` validates against, exactly as it is embedded in the binary: the same bytes as the file of that name in [github.com/meaninggraph/core](https://github.com/meaninggraph/core) at the commit it was taken from, with nothing added (no line break, no re-indentation). `meaninggraph schema --source` prints that commit, the one recorded in `pkg/meaning/meaning.schema.source`, as 40 hexadecimal digits and a line break. A repository that pins both this tool and a commit of core can test that the two agree, before a file fails:

```sh
meaninggraph schema | cmp - core/meaning.schema.json                              # the schemas are the same bytes
test "$(meaninggraph schema --source)" = "$(git -C core rev-parse HEAD)"          # and from the same commit
```

The first line is the one that matters: a core commit that does not change the schema differs in `--source` and not in the schema. A test compares the printed bytes with the embedded file and with the SHA-256 that `meaning.schema.source` records.

### What it checks

- Every file is inside the YAML subset below (one document, no repeated keys, no anchors or tags ...) and valid against `meaning.schema.json`, embedded in the binary from the commit recorded in `pkg/meaning/meaning.schema.source`.
- Every reference resolves: bare ids in the graph, `meaning://` references in another graph; one `?ref=` pin per other graph across all files.
- `extends` joins compatible kinds and never forms a cycle (also through another graph); `of`, `values-of` and `units-of` name entities, and `values-of` and `units-of` may only narrow what a parent names; a `unit` names exactly one value of its `units-of` entity.
- Measure `inputs` and `dimensions` are of the right kinds; a ratio is never summed, counted or averaged, stated or inherited.
- Concept ids, value ids and source ids are unique; one word names one value of a concept, in any language, ignoring case.
- Every binding names a module listed in `models`, an entity and a property that exist in that module's ModelSpec file, and fits its role: one entity binding per concept, `identifier` and `display-name` on the concept's own entity (identifier in the key, display name a string), `value` not a reference, `foreign-key` pointing at the entity whose rows are the instances of the concept (or of its `values-of` entity).

### What it does not check

- Data. It does not read a dataset, so it does not check that the values stored in a bound column are all known values (`match: labels`/`codes.<code>`).
- That the files you supplied are the pinned version, beyond which commit a checkout's `HEAD` names. For each `?ref=` pin read from a directory given with `--graph`, `check` reads `.git/HEAD` and the ref files and `packed-refs` **as plain files** (no `git` process, no network, no object is opened) and compares. What is read is bounded and strict: only regular files that are not symbolic links, whose names are spelled exactly as asked (a case-insensitive file system does not make `MAIN` the branch `main`), at most 1 KiB for a ref or `HEAD`, 4 KiB for a `.git` pointer file or a `commondir` file and 8 MiB for `packed-refs` (a `.git` that is a link, a named pipe or a device is not opened, and neither is a `commondir`); a pin name that is empty, has a trailing or doubled slash or a `.` or `..` segment is not a name. When a file is there and is not read for one of these reasons, the pin is reported as not verified with the reason:
  - A pin of 40 hexadecimal digits (what `FORMAT.md` advises) must be the commit the checkout is at: the same is silent, another is an error (`pin-checkout-mismatch`).
  - A pin that is a branch or a tag name (`FORMAT.md` allows them: the grammar is `[A-Za-z0-9._/-]+`, and they can move) is looked up as `refs/heads/<name>`, `refs/tags/<name>` and `refs/remotes/origin/<name>`. A checkout of that branch, or at a commit that one of those refs has, is silent; at another commit it is the same error. A name that is not among the refs, or a tag that may be an annotated tag (a loose tag file, or a packed tag with no peeled `^` line in a `packed-refs` that does not say `fully-peeled`; telling it from a commit means reading git objects) cannot be verified: a warning (`pin-not-verified`), as is a directory that is not a git checkout at all (an archive, a copy).

  This catches a forgotten checkout of the wrong version. It proves nothing about the working tree (files edited, added or deleted since the checkout pass), nothing about the git objects (a commit id in `HEAD` need not exist), and it can be fooled by a directory whose `.git/HEAD` was written by hand: it is a check against a mistake, not against a forgery.
- ModelSpec beyond what bindings read: only the entities, keys and properties of the model are used, and models of another repository are not read (a binding to one is an error, as in the reference checker).
- Registry-level rules (who may publish a graph, its licences, its dependencies).

### The YAML subset

The reference checker reads YAML with the `yaml` package of npm. That is a YAML 1.2 parser with many lenient corners, and no Go YAML library reads exactly the same files (see "Why the YAML reader is part of this repository"). So `meaning.schema.json` is checked on a plainly defined subset of YAML 1.2, which `check` enforces before it interprets anything. A file outside the subset gets a finding with a rule id of its own that says what to write instead; it is never half-read. On every file inside the subset the CLI is meant to read the same data as the reference parser, and that is tested, not assumed: `go test` compares the data it reads with values recorded from the reference parser for 10,418 documents (see Testing), and the mutation run compares them for every mutated YAML file both parsers read.

| Rule | What it refuses |
|---|---|
| `yaml-encoding` | Text that is not UTF-8. A leading byte order mark is accepted, but not before an indented first line or a block sequence on the first line (the reference parser counts the mark as a column and refuses those, or reads them differently). |
| `yaml-character` | NUL and other control characters, DEL, U+0085, U+2028, U+2029, and U+FEFF inside the text (a byte order mark is only accepted at the very start). Write them as `\u` escapes in a double-quoted string. |
| `yaml-line-ending` | A carriage return that is not part of a CRLF. Lines end in LF or CRLF. |
| `yaml-tab` | A tab that is not read as text: see "Tabs" below. A tab is accepted inside quotes, inside the text of a comment, and between the first and the last non-blank character of plain and block text; it is refused in indentation (of a comment line too), alone on a line, before a `#`, at the end of a line, after a colon or a dash and between tokens. The finding gives the line, and the message begins with the column of the tab (counted in characters from 1, a tab being one character, since a tab cannot be seen). |
| `yaml-directive` | `%YAML`, `%TAG` and any other `%` directive. |
| `yaml-documents` | A second document (`---` after content) or a document end marker (a closing `...` line); exactly one document per file. |
| `yaml-anchor` | Anchors (`&`), aliases (`*`) and merge keys (`<<`). Write the value out. |
| `yaml-tag` | Tags of any kind, `!!str` included (an earlier version let `!!str` through; it is refused now, because every tag but one needed its own rule). Write the value plainly, in quotes when it must be a string. |
| `yaml-key` | A key that is not a string: `true`, `null`, `12`, `1.5` (YAML reads those as a boolean, null or a number; quote the key), a key that is written over 1024 bytes from its first character to its colon (the quotes, the escapes as written and the blanks before the colon count: the reference parser counts them, in UTF-16 units, and a byte is never fewer than a unit; so a multibyte key may be refused that it reads). |
| `yaml-duplicate-key` | A key repeated in one mapping. |
| `yaml-number` | A hexadecimal or octal number, `.inf`, `.nan`, an integer beyond 2^53 (a double cannot hold it exactly). |
| `yaml-escape` | An escape in a double-quoted string other than the JSON ones, `\/` and `\u` (surrogate pairs included), and a malformed one. |
| `yaml-unsupported` | Keep chomping on a block scalar (`|+`, `>+`: see below), an indentation indicator on a block scalar (`|2`), a block scalar indicator on the line after its key, a blank line inside or at the end of a block scalar that holds more spaces than the text is indented by, explicit keys (`?`), `[key: value]` pairs, a `{ }` entry without `: value`, comments inside `[ ]` or `{ }`, a comment line between a key (or a dash) and a value that is not a collection and starts on a later line (see "Comments" below), a plain scalar inside `[ ]` or `{ }` that continues on the next line, quoted scalars that continue on a second line. |
| `yaml-limit` | A file over 8 MiB or nesting over 64 levels (a model file is limited to 8 MiB and nesting 2). |
| `yaml` | Anything else that YAML 1.2 does not allow (bad indentation, an unclosed quote or collection ...), and a plain value that continues on a new line with a character that starts a token (a quote, a bracket, `*`, `&`, `!`, `%`, `@`, a backtick, `|`, `>` or a comma): put such a value in quotes. |

Inside the subset: block mappings and sequences, flow collections (which may span lines), plain, single- and double-quoted scalars, literal and folded block scalars (`|`, `>`, with `-` for strip chomping or none for clip, not `+`), comments (see "Comments" below), and a leading `---`.

**Block scalars and the end of the file.** A block scalar that runs to the end of a file is where readers disagree most: whether a last line of spaces without a line break is a line, and so how many line breaks a keep-chomped (`|+`, `>+`) scalar ends with. The reference parser reads `"Kept text.\n"` where a reader that counts that last line reads `"Kept text.\n\n"`, and one line of arithmetic did not fix it (it trades these differences for others, because the reference counts that line in some layouts and not in others). So keep chomping is outside the subset (`yaml-unsupported`, saying to use `|`, `>`, `|-` or `>-`), and the other forms were proven against the reference parser, for each of `|`, `>`, `|-` and `>-` (and the refused forms with an indentation indicator), in three positions, with five bodies and 23 endings of the file (no line break, one, several, a last line of spaces fewer than, as many as and more than the indentation, a last line of a tab, a comment after, CRLF, a following key ...): 1,380 documents whose values are recorded in `testdata/golden/node-values.json` and compared by `go test`. A form that did not agree is refused: a blank line of more spaces than the text is indented by (the reference reads it as text, at the end of the file too) and a line of a tab.

### Tabs

The reference parser refuses a tab used as indentation and reads it elsewhere, in ways that depend on what comes before and after it (whether a block scalar is still open, whether a plain value continues on the next line, a comment line between two entries). An earlier version tried to follow those cases one by one and kept accepting a few the reference refuses. So the rule has no context now: it is decided by the line and by what the reader is reading at the tab, never by what came before or comes after, and it is checked in one forward pass over the file (`checkTabs`; a test counts the steps on documents of 5,000 and 10,000 lines of seven kinds and fails if they grow faster than the size, and a file of 8 MiB of tab lines is refused at its second line). Every other tab is `yaml-tab` with the line and the column.

| Position | Result |
|---|---|
| Inside a double-quoted or single-quoted scalar (a quoted key too, also inside `[ ]` and `{ }`) | read: the value has the tab |
| In the text of a comment, between the `#` and the last character of the comment (a comment on a line of its own, after a value, or after a key, a dash or the `---` marker with nothing else on the line) | read: the comment is not data |
| In plain text or in the text of a block scalar, between the first and the last non-blank character of the text (continuation lines included; blanks may stand either side of the tab) | read: it is part of the text |
| In indentation of any line (a comment line, a blank line, a block scalar line) and right after the indentation of a block scalar text line | refused |
| On a line of its own (a line that is only a tab, also the last line without a line break) | refused |
| Before a `#`, at the end of a line outside quotes, at the end of a comment, at the end of the text of a block scalar | refused |
| After a colon or a dash, at the start of a value, between tokens, inside `[ ]` or `{ }` outside quotes | refused |

Each of these was checked against the reference parser's values for the generated documents of `values.mjs` (groups `tab read`, where the CLI must read what the reference reads, and `tab refused`, where it must not read the document at all). The last of the three comment positions (a key, a dash or `---` with nothing else on the line) was checked by a generated family of 48,576 documents (22 comment texts with a tab, 16 heads in different places, 3 spacings, 21 to 24 sets of lines that follow, LF and CRLF) run through the reference parser (`yaml` 2.9.1) and `ParseYAML`: 44,088 are read by both with the same data, 528 are refused by both, and the other 3,960 are refused by `ParseYAML` for a reason that has nothing to do with the tab (the comment-between rule below, or a block scalar that starts on the next line); none is read by `ParseYAML` and refused by the reference parser, and each document is read like the same one with spaces for the tabs. The family is not in the recorded values. Some of the refused positions are positions where the reference parser reads the tab too (a tab at the end of a line, for example); those are `stricter` corpus items: the subset is smaller than the reference's on purpose, because a smaller subset can be proven.

### Comments

A comment line before a block mapping or a block sequence (`key:`, a comment, then the nested entries) is accepted at any column and with or without a space after the `#`: the generated family `comment-lines-collection` (1,431 documents) shows the reference parser reading the same data. A comment line between a key (or a dash) and a value that is not a collection and starts on a later line (a scalar, a quoted scalar, a flow collection, a block scalar) is refused with `yaml-unsupported`: the reference parser reads these in ways that depend on the column of the comment and of the value, and the CLI does not follow them: it refuses the whole shape, and the family `comment-lines` (2,592 documents, 26 filler comments at every line position, also as the last line without a line break) is in the recorded values to hold it to that. Move the comment above the key. The finding is on the line of the comment (the first one, when several stand there) and says on which line the value starts. Comments after a value on the same line, and comment lines between entries of a mapping or sequence, are accepted as they always were.

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

`meaninggraph` is meant never to accept a file the reference checker refuses (the corpus, the recorded values and the mutation run check this; none has found a case since the last fix, and none is a proof). It refuses these kinds of input that the reference checker accepts, on purpose; each is a corpus item marked `stricter` with its reason (see Testing):

- **Anything outside the YAML subset**, rule by rule as in the table above: tabs anywhere but inside quotes, comments and text (above), a comment line between a key or a dash and a value that is not a collection, keep chomping, directives, anchors, aliases and merge keys, any tag (`!!str` included), non-string keys, hexadecimal and octal numbers, integers beyond 2^53, other encodings, control characters, explicit keys, comments in flow collections, a closing `...` line, a plain scalar wrapped inside `[ ]` or `{ }` and so on. The reference parser reads some of these in a way a person would not expect (a tag changes the type, `0x7E9` is a year of 2025), and some it merely accepts.
- **A directory with no meaning file.** The reference checker returns no problems for no files; `check` reports `no-meaning-files`, so that a CI job pointed at the wrong directory does not pass by reading nothing.
- **A model whose entity `key` is a string instead of a list** (`key = "Id"`). JavaScript accepts it because `String.includes` finds a property name inside the string; ModelSpec requires a list. `entity = true` on a property is not an entity named `true` either; the entity must be a string.
- **A supplied graph (`--graph`) that is not itself valid.** The reference checker validates only the graph it was asked to check. It is reported once, on the first reference to it, and not once per reference.
- **A `?ref=` pin that does not match the checkout** it was read from (above): the reference checker fetches the pin and does not look at a directory.
- **A binding to a prototype-named entity or property** (below).

Words are matched ignoring case with the Unicode 16 tables (`golang.org/x/text` v0.42.0), so the case pairs that Node's `toLowerCase` folds (for example U+A7CB and U+0264) are the same word here too (a test lists them). A `models:` path with a trailing slash is refused, as the reference checker refuses it.

**Prototype-named model members.** The reference checker keeps the entities, properties, components, fields and enums of a model in plain JavaScript objects and asks `name in object`. The names of `Object.prototype` (`constructor`, `toString`, `valueOf`, `hasOwnProperty`, `isPrototypeOf`, `propertyIsEnumerable`, `toLocaleString`, `__proto__`, `__defineGetter__`, `__defineSetter__`, `__lookupGetter__`, `__lookupSetter__`) are found in every object, so a model that declares one is refused as having declared it twice, and a binding to an entity that a model lacks is found. That is a bug in the reference checker, not a rule of ModelSpec. `check` reports a model that declares one of these names as an error that says so and says to rename it, because the reference checker will refuse the same model; it does not pretend the model is fine. The names are listed once, in `pkg/meaning/hcl.go` (`PrototypeNames()`). A binding to such a name the model does not declare is refused as any missing entity. The question is open with the reference checker's maintainers, together with the YAML subset: <https://github.com/meaninggraph/core/issues/2>.

One more difference is not about verdicts: YAML syntax errors and unreadable models are findings (exit 1), where the reference checker stops with an exception.

## The library

`github.com/meaninggraph/cli/pkg/meaning` is importable: it parses meaning files, validates them against the schema, resolves `extends` and bindings, and returns a typed graph and a sorted list of findings. It has no command framework, never exits the process, and never touches the network; the file system, other graphs and the model reader come in through `FS`, `Resolver` and `ModelReader`. `OSFS` is opt-in: no function defaults to the host file system, and `Checker` needs no file system at all once a graph is loaded. `ParseYAML(data) (*Node, error)` is the strict YAML reader on its own, for other tools: it returns a nil error for a file it reads, and for a refused one an error that `errors.As` turns into a `*SyntaxError` (`Rule`, `Line`, and `Column` for a tab). The embedded schema is reached through `SchemaJSON()` (a copy), `SchemaSource()` and `SchemaCommit()`; no mutable variable is exported. `Graph.Address` is the `<host>/<org>/<repo>` a graph answers to when it is supplied as a reference target, and must be set (`GraphResolver` refuses a supplied graph without one); it is also what a `meaning://` reference to the graph itself is resolved against. `extends` is resolved in near-linear time, and files and nesting are bounded (`MaxFileBytes`, `MaxYAMLDepth`). The three paths that grow with the square of a hostile input within those limits are linear or bounded: a plain scalar of many lines is built once, the words of a values entity are indexed once however many units name them, and the universal profile lists at most `MaxAmbiguousWords` ambiguous words and then says that the others are not listed. Each has a test that counts the work (allocations or steps), not the time.

```go
g, err := meaning.LoadDir(meaning.OSFS{}, "model")
findings := meaning.Checker{Resolve: meaning.GraphResolver(others)}.Check(g)
```

**The model reader is a stand-in.** `HCLReader` reads the ModelSpec model the way the Node reference checker does (a port of its small HCL parser). It sits behind one interface, `ModelReader`, and is to be replaced by the ModelSpec library (`github.com/modelspec-org/cli`, package `pkg/modelspec`) once that has a release. Nothing else changes when it is.

## Testing

`go test ./...` needs nothing but Go: no network, no Node, no `git`, no subprocess and no write outside `t.TempDir()`. The file system, the output streams, the terminal and the update server are injected, and the suite runs in a few seconds with `-race`.

**100% statement coverage, exactly.** CI runs the tests with the race detector and then `go run ./cmd/covergate cover.out`, which compares covered with total statements in the cover profile (never a rounded percentage). The gate has no threshold, flag or environment variable to lower it, and a test fails if the workflow or the gate is loosened. Every package is covered by its own tests, `main` included, and the gate also fails when a package of the module is missing from the profile or contributes no statement to it (a `TestMain` that exits 0 prints `ok` and leaves no line in the profile): it reads the module's file tree, finds the packages the way `go list ./...` does (directories with a Go file that is not a test, `node_modules` included, as `go list` includes it; not `testdata`, `vendor`, names that start with `.` or `_`, or a nested module; compared in a test with an answer recorded from `go list ./...` of Go 1.27.1 on a tree with each of those), and needs every one of them in the profile. It also parses every `_test.go` file and fails on any `TestMain`, in any package: a `TestMain` is the way a package can run its tests and exit before the profile is written, so none is allowed.

**Differential test against the reference checker.** `testdata/corpus` holds 278 items: the `core` concepts and the Chinook graph (both check clean), edge cases that must stay accepted (valid YAML 1.2 that other Go parsers refuse among them), refusals with one defect each, including every refusal case of the reference checker's own tests, and the `stricter` items that document each difference above. `testdata/golden/node-verdicts.json` records what the Node reference checker said about each item, with the `core` commit it ran at. `go test` runs `meaninggraph check` over every item and compares: it fails if the CLI accepts anything the reference refuses, or refuses something the reference accepts without a `stricter` reason in the item.

**The reference parser's values, in `go test`.** `testdata/golden/node-values.json` records the data the reference checker's YAML parser (`yaml` 2.9.1, called as core calls it) reads from every YAML file of the corpus and from a generated matrix: 10,418 documents in all, 5,062 of which the reference parser reads (and `ParseYAML` must read the same data) and 1,511 of which it refuses (and the CLI must not read), the others refused by the CLI's subset rules for a reason the group allows. The groups are scalars in twelve positions, block scalars with every ending of the file, tabs read and refused in every position, comment lines between a key and its value at every line position, byte order marks, line ends, wrapped plain scalars and block mapping keys written at 1,018 to 1,030 units (plain, quoted, with escapes, with doubled quotes, multibyte, with blanks before the colon; 1,339 documents: the reference parser counts a key from its first character to its colon, and the group is what holds the CLI to refusing every key the reference refuses). The count of documents refused, by rule, is pinned in the test (`wantTally`), so the matrix cannot shrink or turn into refusals without a change that shows in review. `TestParseYAMLReadsWhatTheReferenceParserReads` compares what `ParseYAML` reads from the same text with it, field for field, and fails when it reads other data, reads what the reference refuses, or refuses what the reference reads without one of the rules that group may be refused by. It needs no Node. It is what would have caught a keep-chomped scalar at the end of a file in the default run.

```sh
# refresh the golden verdicts and values (needs Node and a clean checkout of core, `npm ci` done)
scripts/differential/regenerate.sh /path/to/core
# is the embedded schema still the one in core?
scripts/check-schema-drift.sh /path/to/core
```

`regenerate.sh` runs the drift check first, so the verdicts and the values are always recorded at the commit of the embedded schema.

**Mutation run.** `scripts/differential/mutation` (a separate Go module, so it is outside `./...`, outside `go test` and outside the coverage gate) writes random text-level mutants of the core and Chinook files and of corpus items (inserted tabs, carriage returns, quotes, `?`, tags, anchors, escapes, deleted and swapped lines ...). Random text mutation does not reach some shapes, so six families are generated on purpose: block scalars at the end of a file in every style and with every kind of last line, byte order marks before every kind of first line, tabs in every position, comment-like filler lines (with and without tabs, with and without a space after the `#`) at every line position of the core and Chinook files, the same fillers inside the shapes of a concept file, and the names of `Object.prototype` in every name position of the model (block names, attribute names, new attributes, bindings), asks the Node reference checker for each verdict, and runs `meaninggraph check` in process over the same directories. It counts the disagreements by direction and exits 1 on the one that must not happen: the CLI accepting what the reference checker refuses. For each mutated YAML file the reference parser reads, it also compares the values with what `ParseYAML` reads from the same bytes. The same seed gives the same mutants.

```sh
scripts/differential/mutation/run.sh /path/to/core /path/to/chinookdb 24000 1
```

The mutants where the CLI refuses and the reference checker accepts are grouped by rule; each group is one of the stricter rules above, except the ones that are not the CLI's choice: `no-meaning-files` for a mutant that deletes the only file, and `unresolved-graph` for a corpus item that already carries a `stricter` reason. Two runs at fresh seeds: seed 5150 (26,000 mutants) and seed 8086 (24,000). They were meant for the code of the first release, but the commit they ran on was not written down, so both were repeated on commit `eb51592` (release v0.1.0), at the same seeds, which give the same mutants, and gave exactly the numbers below. In both, the CLI accepted no file that the reference checker refuses (0 of 50,000), and read the same data from all 18,612 mutated YAML files that both parsers read (9,673 and 8,939; 0 read differently). The largest groups of mutants the CLI refuses and the reference accepts are tabs (`yaml-tab`, 2,620 and 2,419) and the generated block scalars with keep chomping or an indentation indicator, and the comment-between-key-and-value shape (`yaml-unsupported`, 2,403 and 2,178); the rest are the other stricter rules and the groups named above. The families of this round are what catch the previous round's code: run on the code before this round's fix (round 2), two of 4,000 mutants were files that the CLI accepted and the reference checker refused (a tab on a comment line between entries, and a comment before a scalar value), and none on the new code. Earlier runs of the other families had found 13 such files in the same way (a comment line indented by a tab, or a line of tabs, right after a block scalar); all of them are corpus items and matrix documents now.

`testdata/corpus/core` is a copy of the universal concepts of `github.com/meaninggraph/core` (CC0-1.0); `testdata/corpus/chinook` is the Chinook meaning file (CC0-1.0) and ModelSpec model (MIT, the licence text is `testdata/corpus/chinook/model/LICENSE`) of `github.com/datatug/chinookdb` at commit `8c9e62ed6641c0a00faa3867167d928af4c44b06`.

## Releases

`.github/workflows/release.yml` runs on a push to `main` and on nothing else: no tag push, no manual run, no schedule. It has two jobs. `gate` calls `.github/workflows/ci.yml` (the same workflow that runs on pull requests: format, vet, race tests, the 100% coverage gate, and a GoReleaser snapshot of the packaging), and `release` has `needs: gate`, so GitHub does not start it unless `gate` passed, in the same run, for the same commit. `release` calls the shared `strongo/cicd` release workflow, pinned to a commit: it derives the version from the conventional commits since the last tag (`feat:` is a minor, any other type except `chore:`, `ci:`, `docs:` and `refactor:`, which are skipped, is a patch), tags it, and GoReleaser publishes archives for linux, macOS and windows (amd64, arm64; no windows/arm64) with SHA-256 checksums. macOS binaries are not signed or notarised yet.

The shared workflow tags, and a tag does not start `release.yml` again, so there is no second path to a release: hand-pushing a tag publishes nothing, and nothing but this run creates one. The shared workflow has its own guard that waits for a caller's CI run for the commit; it is not used, because it carries on after three minutes when it finds none, which is the hole `needs: gate` closes. Tests in `internal/covergate` parse both workflows and fail on a loosening (a trigger added, `needs` or the `if` removed, `continue-on-error`, `cancel-in-progress: true` on the release, a moving tag or another commit for the shared workflow or an action: the commits are listed in the test), they fail on any workflow file beside `ci.yml` and `release.yml` (a second workflow that calls the shared release workflow on a tag would skip the gate), and a test fails if any Go file has a build constraint, which would put code outside what the gate measures.

To cut `v1.0.0`: set `allow_major_version_bump: true` in `release.yml` in the commit meant to release it (a `feat!:` commit), let the run after the gate tag it, and set it back to `false`.

## License

Apache-2.0, see `LICENSE`.
