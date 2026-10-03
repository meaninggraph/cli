#!/bin/sh
# Random text-level mutants of the core and Chinook graphs, through the Node
# reference checker and through `meaninggraph check`, with the disagreements
# counted by direction. Not part of `go test`, and not part of the coverage gate.
#
#   scripts/differential/mutation/run.sh <core checkout> <chinook checkout> [count [seed]]
#
# <core checkout> is a clean checkout of github.com/meaninggraph/core at the
# commit recorded in pkg/meaning/meaning.schema.source, with `npm ci` done;
# <chinook checkout> is github.com/datatug/chinookdb at
# 8c9e62ed6641c0a00faa3867167d928af4c44b06. The mutants are written to a
# temporary directory that is removed at the end, unless MUTATION_KEEP is set.
set -eu
core="${1:?usage: run.sh <core checkout> <chinook checkout> [count [seed]]}"
chinook="${2:?usage: run.sh <core checkout> <chinook checkout> [count [seed]]}"
count="${3:-20000}"
seed="${4:-1}"
here="$(cd "$(dirname "$0")" && pwd)"
out="$(mktemp -d "${TMPDIR:-/tmp}/meaninggraph-mutation.XXXXXX")"
cleanup() { [ -n "${MUTATION_KEEP:-}" ] && echo "mutants kept in $out" || rm -rf "$out"; }
trap cleanup EXIT
node "$here/mutate.mjs" "$core" "$chinook" "$count" "$seed" "$out"
cd "$here" && go run . "$out"
