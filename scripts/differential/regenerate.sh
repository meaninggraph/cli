#!/bin/sh
# Re-runs the Node reference checker over testdata/corpus and rewrites
# testdata/golden/node-verdicts.json. Needs Node and a clean checkout of
# github.com/meaninggraph/core, at the commit recorded in
# pkg/meaning/meaning.schema.source, with its dependencies installed (npm ci).
# The Go tests never run this: they only read the golden file.
#
#   scripts/differential/regenerate.sh <core checkout>
set -eu
core="${1:?usage: scripts/differential/regenerate.sh <github.com/meaninggraph/core checkout>}"
cd "$(dirname "$0")/../.."
scripts/check-schema-drift.sh "$core"
node scripts/differential/run.mjs "$core" testdata/golden/node-verdicts.json
