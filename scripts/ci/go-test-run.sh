#!/usr/bin/env bash
# go-test-run.sh <index> <count> - run one CI test shard (D-609).
#
# The shard's packages come from go-test-shards.sh (narrowed by SHARD_FILTER
# when the push tests only affected packages). They are split by RACE_FILTER:
#   RACE_FILTER unset or a file holding ALL -> every package runs with -race
#     (the nightly, workflow_dispatch and therefore the release gate);
#   otherwise -> the packages listed in RACE_FILTER (scripts/ci/cipkgs race:
#     the ones that use concurrency) run with -race, the rest without.
# Both halves always run, so one failing half does not hide the other.
#
# -count=1: Go never replays cached test results. -gcflags turns checkptr off
# for modernc.org/... only (D-604); the race detector still instruments it.
set -euo pipefail
export LC_ALL=C

index="${1:?index}" count="${2:?count}"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"
cd "${repo_root}"

module="$(go list -m)"
read -r -a pkgs <<<"$("${script_dir}/go-test-shards.sh" list "${index}" "${count}")"
if [[ ${#pkgs[@]} -eq 0 ]]; then
  echo "::notice::shard ${index} of ${count} has no packages"
  exit 0
fi

race=() plain=()
if [[ -z "${RACE_FILTER:-}" ]] || grep -qx 'ALL' "${RACE_FILTER}"; then
  race=("${pkgs[@]}")
else
  declare -A want=()
  while IFS= read -r l; do
    [[ -z "$l" ]] && continue
    if [[ "$l" == "." ]]; then want["${module}"]=1; else want["${module}/${l}"]=1; fi
  done <"${RACE_FILTER}"
  for p in "${pkgs[@]}"; do
    if [[ -n "${want[$p]:-}" ]]; then race+=("$p"); else plain+=("$p"); fi
  done
fi

echo "shard ${index}/${count}: ${#race[@]} package(s) with -race, ${#plain[@]} without"
rc=0
if [[ ${#race[@]} -gt 0 ]]; then
  go test -race -count=1 -gcflags='modernc.org/...=-d=checkptr=0' -timeout 90m "${race[@]}" || rc=1
fi
if [[ ${#plain[@]} -gt 0 ]]; then
  go test -count=1 -timeout 90m "${plain[@]}" || rc=1
fi
exit "${rc}"
