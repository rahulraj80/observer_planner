#!/usr/bin/env bash
# restart-daemon_test.sh: regression test for scripts/restart-daemon.sh's
# "never two writers" contract (P3-06, the D-349 second writer).
#
# Everything runs in a private mktemp dir: a throwaway HOME, config, DB, log,
# random high ports and FAKE daemons this test starts itself. It never touches
# the real daemon, :8820 / :8081, ~/.observer or /tmp/obs.log.
#
# Usage: scripts/restart-daemon_test.sh [path/to/restart-daemon.sh]
# Linux / WSL only (it reads /proc). Exits 0 when every case passes.
set -euo pipefail

SCRIPT="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/restart-daemon.sh}"
[[ -f "$SCRIPT" ]] || { echo "no script: $SCRIPT" >&2; exit 2; }
[[ -d /proc/self ]] || { echo "SKIP: needs /proc" >&2; exit 0; }
command -v python3 >/dev/null || { echo "SKIP: needs python3" >&2; exit 0; }

ROOT="$(mktemp -d "${TMPDIR:-/tmp}/restart-daemon-test.XXXXXX")"
PIDS=()
cleanup() {
  local p
  for p in "${PIDS[@]}"; do kill -KILL "$p" 2>/dev/null || true; done
  # The relaunched fake server records its own pid.
  for p in "$ROOT"/*/server.pid; do [[ -f "$p" ]] && kill -KILL "$(cat "$p")" 2>/dev/null || true; done
  rm -rf "$ROOT"
}
trap cleanup EXIT

FAILS=0
fail() { echo "FAIL: $*"; FAILS=$((FAILS + 1)); }
pass() { echo "ok:   $*"; }

free_port() {
  local p
  for _ in $(seq 1 50); do
    p=$((20000 + RANDOM % 20000))
    ss -ltn 2>/dev/null | grep -q ":$p " || { echo "$p"; return; }
  done
  return 1
}

# setup_case <name> -> prints the case dir. HOME/.observer holds config + DB;
# fake/observer is a copy of bash, so /proc/<pid>/exe reads ".../observer"
# exactly as a real daemon's does.
setup_case() {
  local d="$ROOT/$1"
  mkdir -p "$d/home/.observer" "$d/fake" "$d/new"
  : >"$d/home/.observer/config.toml"
  : >"$d/home/.observer/observer.db"
  cp "$(command -v bash)" "$d/fake/observer"
  # The NEW binary: records whether the old daemon was still alive at the
  # moment it was launched, then serves /-/healthz on the test port.
  cat >"$d/new/observer" <<'NEW'
#!/usr/bin/env bash
if kill -0 "$OLD_PID" 2>/dev/null && [[ "$(cut -d' ' -f3 /proc/"$OLD_PID"/stat 2>/dev/null)" != Z ]]; then
  echo alive >"$CASE_DIR/launch-saw-old"
else
  echo gone >"$CASE_DIR/launch-saw-old"
fi
echo $$ >"$CASE_DIR/server.pid"
exec python3 -c '
import http.server, sys
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200); self.end_headers(); self.wfile.write(b"ok")
    def log_message(self, *a): pass
http.server.HTTPServer(("127.0.0.1", int(sys.argv[1])), H).serve_forever()
' "$TEST_PORT"
NEW
  chmod +x "$d/new/observer"
  echo "$d"
}

# start_old <case dir> <term behaviour> : a fake `observer start` that holds
# the DB open and never listens (it already released its port). behaviour
# "grace:N" exits N seconds after SIGTERM; "ignore" ignores SIGTERM.
start_old() {
  local d="$1" how="$2" trapcmd
  case "$how" in
    grace:*) trapcmd="sleep ${how#grace:}; exit 0" ;;
    ignore) trapcmd="" ;;
  esac
  TRAPCMD="$trapcmd" DBF="$d/home/.observer/observer.db" \
    setsid "$d/fake/observer" -c 'exec 3<"$DBF"; trap "$TRAPCMD" TERM; while :; do sleep 0.2; done' \
    fake-observer start --no-open </dev/null >/dev/null 2>&1 &
  OLD_PID=$!
  disown "$OLD_PID" 2>/dev/null || true
  PIDS+=("$OLD_PID")
  sleep 0.5
}

write_lock() {
  printf '{\n  "pid": %s,\n  "db_path": "%s"\n}\n' "$2" "$1/home/.observer/observer.db" \
    >"$1/home/.observer/observer-$2.lock"
}

run_script() {
  local d="$1"; shift
  env -u OBSERVER_DAEMON_CHILD HOME="$d/home" OBSERVER_CONFIG="$d/home/.observer/config.toml" \
    OLD_PID="$OLD_PID" CASE_DIR="$d" TEST_PORT="$TEST_PORT" \
    bash "$SCRIPT" --no-route-toggle --port "$TEST_PORT" --bin "$d/new/observer" --log "$d/obs.log" "$@" \
    >"$d/out.log" 2>&1
}

stop_new() {
  [[ -f "$1/server.pid" ]] && kill -KILL "$(cat "$1/server.pid")" 2>/dev/null || true
}

# Case 1: an old daemon that is already stopping (SIGTERM received, port
# released, DB still open, lock file present). The relaunch must wait for it.
TEST_PORT="$(free_port)"
D="$(setup_case stopping-with-lock)"
start_old "$D" grace:3
write_lock "$D" "$OLD_PID"
kill -TERM "$OLD_PID"
rc=0; run_script "$D" --stop-timeout 15 || rc=$?
if [[ $rc -eq 0 && "$(cat "$D/launch-saw-old" 2>/dev/null)" == gone ]]; then
  pass "stopping daemon found by lock file + DB handle; relaunch waited for it"
else
  fail "stopping daemon (lock): rc=$rc launch-saw-old=$(cat "$D/launch-saw-old" 2>/dev/null || echo none)"; sed 's/^/      /' "$D/out.log"
fi
stop_new "$D"

# Case 2: the same, with no lock file (a daemon that predates lock files or
# lost its lock): found by its open DB handle alone.
TEST_PORT="$(free_port)"
D="$(setup_case stopping-fd-only)"
start_old "$D" grace:3
kill -TERM "$OLD_PID"
rc=0; run_script "$D" --stop-timeout 15 || rc=$?
if [[ $rc -eq 0 && "$(cat "$D/launch-saw-old" 2>/dev/null)" == gone ]]; then
  pass "stopping daemon found by DB handle only; relaunch waited for it"
else
  fail "stopping daemon (fd only): rc=$rc launch-saw-old=$(cat "$D/launch-saw-old" 2>/dev/null || echo none)"; sed 's/^/      /' "$D/out.log"
fi
stop_new "$D"

# Case 3: an old daemon that ignores SIGTERM: SIGKILL after the bound, then
# relaunch, never beside it.
TEST_PORT="$(free_port)"
D="$(setup_case ignores-term)"
start_old "$D" ignore
write_lock "$D" "$OLD_PID"
rc=0; run_script "$D" --stop-timeout 1 || rc=$?
if [[ $rc -eq 0 && "$(cat "$D/launch-saw-old" 2>/dev/null)" == gone ]] && grep -q "sending SIGKILL" "$D/out.log"; then
  pass "SIGTERM-ignoring daemon killed after the bound; relaunch after it was gone"
else
  fail "ignores-term: rc=$rc launch-saw-old=$(cat "$D/launch-saw-old" 2>/dev/null || echo none)"; sed 's/^/      /' "$D/out.log"
fi
stop_new "$D"

# Case 4: a non-integer --stop-timeout is refused before anything is signalled.
TEST_PORT="$(free_port)"
D="$(setup_case bad-timeout)"
start_old "$D" ignore
write_lock "$D" "$OLD_PID"
rc=0; run_script "$D" --stop-timeout 2.5 || rc=$?
if [[ $rc -eq 2 && ! -f "$D/launch-saw-old" ]] && kill -0 "$OLD_PID" 2>/dev/null; then
  pass "--stop-timeout 2.5 refused up front (exit 2, nothing signalled, nothing launched)"
else
  fail "bad-timeout: rc=$rc launched=$([[ -f "$D/launch-saw-old" ]] && echo yes || echo no)"; sed 's/^/      /' "$D/out.log"
fi
stop_new "$D"

if [[ $FAILS -gt 0 ]]; then echo "$FAILS case(s) failed"; exit 1; fi
echo "all restart-daemon cases passed"
