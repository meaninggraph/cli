#!/bin/sh
# Fails when a schema embedded in the binary (meaning.schema.json, the schema of
# meaning/draft-1, and meaning.draft-2.schema.json, the schema of
# meaning/draft-2) differs from the one in a checkout of
# github.com/meaninggraph/core, or when the checkout is at another commit than
# the one recorded beside the embedded copy.
#
#   scripts/check-schema-drift.sh <core checkout>
set -eu
core="${1:?usage: scripts/check-schema-drift.sh <github.com/meaninggraph/core checkout>}"
here="$(cd "$(dirname "$0")/.." && pwd)"
checkout_commit="$(git -C "$core" rev-parse HEAD)"
status=0
for name in meaning.schema meaning.draft-2.schema; do
  embedded="$here/pkg/meaning/$name.json"
  source_file="$here/pkg/meaning/$name.source"
  recorded_commit="$(sed -n 's/^commit: //p' "$source_file")"
  recorded_sha="$(sed -n 's/^sha256: //p' "$source_file")"
  embedded_sha="$(shasum -a 256 "$embedded" | cut -d' ' -f1)"
  checkout_sha="$(shasum -a 256 "$core/$name.json" | cut -d' ' -f1)"
  if [ "$embedded_sha" != "$recorded_sha" ]; then
    echo "drift: the embedded $name.json ($embedded_sha) is not the one recorded in $name.source ($recorded_sha)" >&2
    status=1
  fi
  if [ "$embedded_sha" != "$checkout_sha" ]; then
    echo "drift: the embedded $name.json differs from $core/$name.json ($checkout_sha)" >&2
    status=1
  fi
  if [ "$checkout_commit" != "$recorded_commit" ]; then
    echo "drift: the checkout is at $checkout_commit, the embedded $name.json was taken from $recorded_commit" >&2
    status=1
  fi
  [ "$status" -eq 0 ] && echo "ok: the embedded $name.json is $name.json of core at $recorded_commit"
done
exit "$status"
