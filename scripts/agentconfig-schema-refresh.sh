#!/usr/bin/env bash
# agentconfig-schema-refresh.sh: re-fetch the pinned vendor settings
# schemas the adapter settings catalogue is generated from
# (docs/plans/adapter-config-and-common-vocabulary-plan-2026-10-03.md
# phase P1), then regenerate the catalogue.
#
# This is the ONLY networked step of the catalogue. It is run by hand, never
# by CI or the daemon. It reads testdata/agentconfig/schemas/manifest.json
# (the one owner of the schema list), downloads each `url` to its `file`,
# refuses a body that is not a JSON object, stamps `fetched_on`, and runs
# catgen. Review the resulting diff (schema files + catalog_*.gen.go) like
# any vendor change: new keys need an overlay row when a schema cannot know
# their owner, danger or scope, and TestBuildHasNoProblems flags an overlay
# whose key the vendor removed.
#
# Usage: scripts/agentconfig-schema-refresh.sh [--no-regen]

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

DIR="testdata/agentconfig/schemas"
MANIFEST="$DIR/manifest.json"
regen=1
[ "${1:-}" = "--no-regen" ] && regen=0

command -v python3 >/dev/null || { echo "agentconfig-schema-refresh: python3 required" >&2; exit 1; }
command -v curl >/dev/null || { echo "agentconfig-schema-refresh: curl required" >&2; exit 1; }

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

python3 - "$MANIFEST" > "$tmpdir/list" <<'PY'
import json, sys
m = json.load(open(sys.argv[1]))
for s in m["schemas"]:
    print(s["file"] + "\t" + s["url"])
PY

while IFS=$'\t' read -r file url; do
    case "$file" in
        */*|..*|"") echo "agentconfig-schema-refresh: refusing file name '$file'" >&2; exit 1 ;;
    esac
    echo "fetch $url"
    curl -fsSL --max-time 60 --proto '=https' "$url" -o "$tmpdir/$file"
    python3 - "$tmpdir/$file" <<'PY'
import json, sys
doc = json.load(open(sys.argv[1]))
if not isinstance(doc, dict):
    sys.exit("not a JSON object: " + sys.argv[1])
PY
done < "$tmpdir/list"

# Every download succeeded: only now replace the pins.
while IFS=$'\t' read -r file _; do
    mv "$tmpdir/$file" "$DIR/$file"
done < "$tmpdir/list"

python3 - "$MANIFEST" <<'PY'
import datetime, json, sys
p = sys.argv[1]
m = json.load(open(p))
m["fetched_on"] = datetime.date.today().isoformat()
with open(p, "w") as f:
    json.dump(m, f, indent=2)
    f.write("\n")
PY

if [ "$regen" -eq 1 ]; then
    go run ./internal/agentconfig/catgen -root "$REPO_ROOT"
    go test ./internal/agentconfig/... -count=1
fi
echo "agentconfig-schema-refresh: done; review 'git diff $DIR internal/agentconfig'"
