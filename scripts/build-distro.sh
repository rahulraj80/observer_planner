#!/usr/bin/env bash
# build-distro.sh: build one prebuilt node distro of observer for one
# platform, with its evidence set (modular packaging plan 5.2, 5.5 and 5.9,
# phase P3, lane MP-REL).
#
#   scripts/build-distro.sh <minimal|developer|managed|full> [options]
#   scripts/build-distro.sh --image <core|full> [options]   (observer-org)
#
#   --goos <os>        target GOOS (default: go env GOOS)
#   --goarch <arch>    target GOARCH (default: go env GOARCH)
#   --version <v>      stamped into main.version (default: dev)
#   --out <dir>        output directory (default: bin/distro)
#   --web              regenerate web/src/lib/components.gen.ts for the
#                      distro, build web/ and sync the embed before the Go
#                      build, then restore both (needs web/node_modules)
#   --proof            run the per-artifact proof gate (sbo-components
#                      proof); adds --run when the target is this host
#   --no-sbom          skip the CycloneDX SBOM
#
# --image builds cmd/observer-org as an org image flavour (plan 3.4, the
# P6b core-image proof): tags from `sbo-components tags --image`, outputs
# named observer-org-<image>-<goos>-<goarch>; --web does not apply (web2
# has no per-image page registry yet). The container image itself is built
# from Dockerfile.observer-org with --build-arg IMAGE=<image>.
#
# Outputs, named observer-<distro>-<goos>-<goarch>[.exe] (plan 5.4):
#   <name>                    the binary (-trimpath, CGO off, -s -w)
#   <name>.buildinfo.txt      `go version -m` of the binary
#   <name>.cdx.json           CycloneDX SBOM (sbo-components sbom)
#   <name>.proof.json         proof record (with --proof)
#
# The tags come from the catalog (`sbo-components tags`), never from this
# script. `full` passes no tag and no stamp, so it is today's release build
# byte for byte. This script builds and proves only: it never signs,
# publishes, tags or pushes anything.
set -euo pipefail

usage() { sed -n '2,36p' "$0" | sed 's/^# \{0,1\}//' >&2; exit 2; }

distro="" image=""
if [[ "${1:-}" == "--image" ]]; then
  image="${2:-}"
  [[ -z "$image" ]] && usage
  shift 2
else
  distro="${1:-}"
  [[ -z "$distro" || "$distro" == -* ]] && usage
  shift
fi
goos="" goarch="" version="dev" out="bin/distro" web=0 proof=0 sbom=1
while [[ $# -gt 0 ]]; do
  case "$1" in
    --goos) goos="$2"; shift 2 ;;
    --goarch) goarch="$2"; shift 2 ;;
    --version) version="$2"; shift 2 ;;
    --out) out="$2"; shift 2 ;;
    --web) web=1; shift ;;
    --proof) proof=1; shift ;;
    --no-sbom) sbom=0; shift ;;
    *) usage ;;
  esac
done

repo="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo"
export CGO_ENABLED=0 GOFLAGS="${GOFLAGS:-}"
goos="${goos:-$(go env GOOS)}"
goarch="${goarch:-$(go env GOARCH)}"
hostos="$(go env GOOS)" hostarch="$(go env GOARCH)"

# The catalog validates the name and owns the tag list.
if [[ -n "$image" ]]; then
  [[ "$web" -eq 1 ]] && { echo "build-distro: --web does not apply to --image" >&2; exit 2; }
  sel=(--image "$image")
  tags="$(go run ./cmd/sbo-components tags --image "$image")"
  cmdpath=./cmd/observer-org
  base="observer-org-${image}"
else
  sel=(--distro "$distro")
  tags="$(go run ./cmd/sbo-components tags --distro "$distro")"
  go run ./cmd/sbo-components validate --distro "$distro" >/dev/null
  cmdpath=./cmd/observer
  base="observer-${distro}"
fi

ext=""
[[ "$goos" == "windows" ]] && ext=".exe"
name="${base}-${goos}-${goarch}${ext}"
mkdir -p "$out"
bin="$out/$name"

embed="internal/intelligence/dashboard/webapp/dist"
restore_web() {
  git checkout -- web/src/lib/components.gen.ts "$embed" 2>/dev/null || true
  git clean -fdq -- "$embed" 2>/dev/null || true
}
if [[ "$web" -eq 1 ]]; then
  if [[ ! -d web/node_modules ]]; then
    echo "build-distro: --web needs web/node_modules (cd web && npm ci)" >&2
    exit 1
  fi
  trap restore_web EXIT
  echo ">> web: page registry for $distro"
  # -proof writes the distro's forbidden-module list; vite.config.ts's
  # sboDistroProof fails the build if any emitted chunk carries one, and
  # records the chunk module graph beside it (<name>.web-proof.modules.json)
  # for the per-distro frontend SBOM (plan 5.1 rev N5).
  mkdir -p "$out"
  webproof="$(cd "$out" && pwd)/$name.web-proof.json"
  go run ./web/compgen -distro "$distro" -proof "$webproof"
  (cd web && SBO_DISTRO_PROOF="$webproof" npm run build)
  scripts/sync-web-embed.sh web/dist "$embed"
fi

echo ">> go build $name (tags: ${tags:-none})"
GOOS="$goos" GOARCH="$goarch" go build -trimpath -buildvcs=false \
  ${tags:+-tags "$tags"} \
  -ldflags="-s -w -X main.version=${version}" \
  -o "$bin" "$cmdpath"

go version -m "$bin" > "$bin.buildinfo.txt"

if [[ "$sbom" -eq 1 ]]; then
  go run ./cmd/sbo-components sbom "${sel[@]}" --binary "$bin" -o "$bin.cdx.json" >/dev/null
fi

if [[ "$proof" -eq 1 ]]; then
  run=()
  [[ -z "$image" && "$goos" == "$hostos" && "$goarch" == "$hostarch" ]] && run=(--run)
  go run ./cmd/sbo-components proof "${sel[@]}" --binary "$bin" "${run[@]}" -o "$bin.proof.json"
fi

ls -l "$bin"
