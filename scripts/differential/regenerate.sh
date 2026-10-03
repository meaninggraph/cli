#!/bin/sh
# Re-runs the Node reference checker over testdata/corpus and rewrites
# testdata/golden/node-verdicts.json (its verdicts) and
# testdata/golden/node-values.json (the values its YAML parser reads from every
# YAML file of the corpus and from a generated matrix of documents: see
# values.mjs). Needs Node and a clean checkout of
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
node scripts/differential/values.mjs "$core" testdata/golden/node-values.json
