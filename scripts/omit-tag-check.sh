#!/usr/bin/env bash
# omit-tag-check.sh: vet and test every build-tag variant the tree actually
# has, so an off file cannot rot unnoticed (modular packaging plan 5.9 and
# risk R1, "stub rot"; lane MP-REL).
#
#   scripts/omit-tag-check.sh [--vet-only] [--run <regex>] [--list] [--only <set>]
#
# The tag set is read from the tree, never from a list in this script: every
# sbo_omit_* tag named in a //go:build line, the one-release alias no_obs,
# and the four distro tag sets (sbo-components tags, which add the
# sbo_distro_* stamps). For each one:
#
#   go vet -tags <tags> ./...                 (every package, tests included)
#   go test -tags <tags> <tagged packages>    (the packages holding a file
#                                              whose build line names a tag
#                                              in the set; --run narrows them)
#
# A tag no file names cannot change what compiles, so it is skipped: its
# build is the default build, which the ordinary CI jobs already cover.
# CI runs this in full; on a small host use --vet-only or --run.
set -euo pipefail

vet_only=0 run="" list=0 only=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --vet-only) vet_only=1; shift ;;
    --run) run="$2"; shift 2 ;;
    --list) list=1; shift ;;
    --only) only="$2"; shift 2 ;;
    *) sed -n '2,24p' "$0" | sed 's/^# \{0,1\}//' >&2; exit 2 ;;
  esac
done

repo="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo"
export CGO_ENABLED=0

# Tags named in a build line of a tracked Go file.
mapfile -t named < <(git grep -h -E '^//go:build .*(sbo_omit_|no_obs)' -- '*.go' \
  | grep -oE 'sbo_omit_[A-Za-z0-9_]+|no_obs' | sort -u)

sets=()
for t in "${named[@]}"; do sets+=("$t"); done
for d in minimal developer managed; do
  sets+=("$(go run ./cmd/sbo-components tags --distro "$d")")
done

if [[ "$list" -eq 1 ]]; then
  printf '%s\n' "${sets[@]}"
  exit 0
fi
# --only <set>: one entry of --list (the CI matrix runs one set per job).
# A set that is not in the tree's list is refused, so a stale matrix entry
# fails instead of passing vacuously.
if [[ -n "$only" ]]; then
  found=0
  for s in "${sets[@]}"; do [[ "$s" == "$only" ]] && found=1; done
  if [[ "$found" -eq 0 ]]; then
    echo "omit-tag-check: --only $only is not a tag set of this tree (see --list)" >&2
    exit 2
  fi
  sets=("$only")
fi

# tagged_pkgs <comma tags>: the packages holding a file whose build line
# names one of the tags.
tagged_pkgs() {
  local re
  re="$(echo "$1" | tr ',' '|')"
  git grep -l -E "^//go:build .*(${re})([^A-Za-z0-9_]|$)" -- '*.go' \
    | xargs -r -n1 dirname | sort -u | sed 's#^#./#'
}

fail=0
for set in "${sets[@]}"; do
  echo "== tags: $set"
  if ! go vet -tags "$set" ./...; then
    echo "FAIL vet -tags $set"; fail=1; continue
  fi
  [[ "$vet_only" -eq 1 ]] && continue
  mapfile -t pkgs < <(tagged_pkgs "$set")
  [[ ${#pkgs[@]} -eq 0 ]] && continue
  args=(-count=1 -tags "$set")
  [[ -n "$run" ]] && args+=(-run "$run")
  if ! go test "${args[@]}" "${pkgs[@]}"; then
    echo "FAIL test -tags $set"; fail=1
  fi
done
exit "$fail"
