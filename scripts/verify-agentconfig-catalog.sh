#!/usr/bin/env bash
# verify-agentconfig-catalog.sh: assert the committed schema tier of the
# adapter settings catalogue matches a fresh run of
# internal/agentconfig/catgen.
#
# catgen is the SOLE writer of internal/agentconfig/catalog_*.gen.go. It
# reads the pinned vendor schemas under testdata/agentconfig/schemas/
# (manifest.json names them). A pin refresh or a generator change that
# ships without `make agentconfig-catalog-build` + commit fails here.
#
# Like verify-config-schema-build.sh, this NEVER mutates the working tree
# and NEVER shells out to git: it regenerates into a scratch directory and
# byte-diffs against the committed files (both directions, so a stale
# generated file left behind by a removed target also fails).

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

PKG="internal/agentconfig"

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

go run ./internal/agentconfig/catgen -root "$REPO_ROOT" -out "$tmpdir" >/dev/null \
    || { echo "verify-agentconfig-catalog: catgen failed" >&2; exit 1; }

drift=0
for rebuilt in "$tmpdir"/catalog_*.gen.go; do
    name=$(basename "$rebuilt")
    committed="$PKG/$name"
    if [ ! -f "$committed" ]; then
        echo "verify-agentconfig-catalog: $committed is missing; run 'make agentconfig-catalog-build' and commit"
        drift=1
        continue
    fi
    if ! diff -u "$committed" "$rebuilt" > "$tmpdir/.diff" 2>&1; then
        echo "verify-agentconfig-catalog: $committed drifted from a fresh catgen run"
        head -n 80 "$tmpdir/.diff"
        drift=1
    fi
done
for committed in "$PKG"/catalog_*.gen.go; do
    if [ ! -f "$tmpdir/$(basename "$committed")" ]; then
        echo "verify-agentconfig-catalog: $committed is stale (no manifest target emits it); delete it"
        drift=1
    fi
done

if [ "$drift" -ne 0 ]; then
    echo "verify-agentconfig-catalog: FAIL (run 'make agentconfig-catalog-build' and commit)" >&2
    exit 1
fi
echo "verify-agentconfig-catalog: OK"
