#!/usr/bin/env bash
# distro-smoke.sh: run the modular packaging smoke harness (tests/distro)
# against a built observer binary, in its own user, mount and network
# namespace so the daemons it starts can never reach the operator's
# daemon, ports, home or the network (plan section 5.9, ledger MODPKG-1 /
# MODPKG-4, lane MP-SEC).
#
#   scripts/distro-smoke.sh <path-to-observer-binary>
#
# Linux only (it needs unprivileged user namespaces). Inside the namespace
# /tmp is a fresh tmpfs, HOME is a directory under it, /mnt/c (WSL's view of
# the Windows drive) is hidden when present, and only loopback exists.
set -euo pipefail

bin="${1:-}"
if [[ -z "$bin" || ! -x "$bin" ]]; then
  echo "usage: $0 <path-to-observer-binary>" >&2
  exit 2
fi
bin="$(cd "$(dirname "$bin")" && pwd)/$(basename "$bin")"
repo="$(cd "$(dirname "$0")/.." && pwd)"
# The namespace has no network and a new HOME, so keep the module and
# build caches where they are (a moved GOMODCACHE would need a download).
gomodcache="$(go env GOMODCACHE)"
gocache="$(go env GOCACHE)"
case "$bin" in
  /tmp/*) echo "distro-smoke: the binary must live outside /tmp (the harness mounts a fresh /tmp)" >&2; exit 2 ;;
esac

exec unshare -rmn bash -s -- "$bin" "$repo" "$gomodcache" "$gocache" <<'INNER'
set -euo pipefail
bin="$1"; repo="$2"
export GOMODCACHE="$3" GOCACHE="$4" GOPROXY=off
mount -t tmpfs none /tmp
if [[ -d /mnt/c ]]; then mount -t tmpfs none /mnt/c; fi
ip link set lo up
export HOME=/tmp/distro-smoke-home
mkdir -p "$HOME"
cd "$repo"
SBO_DISTRO_SMOKE=1 SBO_DISTRO_BIN="$bin" go test -count=1 -v -timeout 10m ./tests/distro/
INNER
