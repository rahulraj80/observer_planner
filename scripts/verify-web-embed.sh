#!/usr/bin/env bash
# verify-web-embed.sh APP_DIR EMBED_DIR REBUILD_HINT - assert a committed
# go:embed'd dashboard bundle matches a fresh Vite build of its sources.
#
#   make verify-node-embed   web  -> internal/intelligence/dashboard/webapp/dist
#   make verify-org-embed    web2 -> internal/orgserver/dashboard/webapp/dist
#
# The same contract as the ci.yml / ci-private.yml dist-consistency gates
# (fresh build, synced through scripts/sync-web-embed.sh, so source maps are
# excluded exactly as in the committed tree), but runnable locally and from
# any branch. Like verify-webcloud-dist.sh it NEVER mutates the working tree
# and never shells out to git: it builds into a scratch directory and diffs.
# P3-07 (REVIEW-3): a merged web / web2 change without its rebuilt embed
# shipped the routes without their UI in every make / hotfix / image build.
set -euo pipefail

if [ "$#" -ne 3 ]; then
    echo "usage: $0 APP_DIR EMBED_DIR REBUILD_HINT" >&2
    exit 2
fi
app=$1
embed=$2
hint=$3

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

if [ ! -d "$embed" ]; then
    echo "verify-web-embed: missing $embed - run '$hint' and commit" >&2
    exit 1
fi
if [ ! -d node_modules ]; then
    echo "verify-web-embed: missing node_modules - run 'npm ci' from the repo root first (npm workspace)" >&2
    exit 1
fi

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

(
    cd "$app"
    npx vite build --outDir "$tmpdir/build" --emptyOutDir >"$tmpdir/build.log" 2>&1
) || {
    echo "verify-web-embed: vite build of $app failed" >&2
    cat "$tmpdir/build.log" >&2
    exit 1
}
scripts/sync-web-embed.sh "$tmpdir/build" "$tmpdir/embed" >/dev/null

if ! diff -rq "$embed" "$tmpdir/embed" >"$tmpdir/diff.log" 2>&1; then
    echo "verify-web-embed: $embed drifted from a fresh build of $app" >&2
    echo "----- diff -rq: committed vs fresh build -----" >&2
    head -40 "$tmpdir/diff.log" >&2
    echo "-----" >&2
    echo "verify-web-embed: run '$hint' and commit the result." >&2
    exit 1
fi

echo "verify-web-embed: $embed matches a fresh build of $app"
