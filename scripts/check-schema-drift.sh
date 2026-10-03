#!/bin/sh
# Fails when the meaning.schema.json embedded in the binary differs from the one
# in a checkout of github.com/meaninggraph/core, or when the checkout is at
# another commit than the one recorded beside the embedded copy.
#
#   scripts/check-schema-drift.sh <core checkout>
set -eu
core="${1:?usage: scripts/check-schema-drift.sh <github.com/meaninggraph/core checkout>}"
here="$(cd "$(dirname "$0")/.." && pwd)"
embedded="$here/pkg/meaning/meaning.schema.json"
source_file="$here/pkg/meaning/meaning.schema.source"
recorded_commit="$(sed -n 's/^commit: //p' "$source_file")"
recorded_sha="$(sed -n 's/^sha256: //p' "$source_file")"
checkout_commit="$(git -C "$core" rev-parse HEAD)"
embedded_sha="$(shasum -a 256 "$embedded" | cut -d' ' -f1)"
checkout_sha="$(shasum -a 256 "$core/meaning.schema.json" | cut -d' ' -f1)"
status=0
if [ "$embedded_sha" != "$recorded_sha" ]; then
  echo "drift: the embedded schema ($embedded_sha) is not the one recorded in meaning.schema.source ($recorded_sha)" >&2
  status=1
fi
if [ "$embedded_sha" != "$checkout_sha" ]; then
  echo "drift: the embedded schema differs from $core/meaning.schema.json ($checkout_sha)" >&2
  status=1
fi
if [ "$checkout_commit" != "$recorded_commit" ]; then
  echo "drift: the checkout is at $checkout_commit, the embedded schema was taken from $recorded_commit" >&2
  status=1
fi
[ "$status" -eq 0 ] && echo "ok: the embedded schema is meaning.schema.json of core at $recorded_commit"
exit "$status"
