#!/usr/bin/env bash
# test-restart-daemon.sh: hermetic test for scripts/restart-daemon.sh (T-553 / T-557).
#
# Runs restart-daemon.sh against a FAKE daemon on a free high port, in a
# private mktemp dir with its own HOME, config, log and pid files. It never
# touches /tmp/obs.log, the real daemon, its port (8820 / 8081) or the
# operator's settings.json: the script under test is always given --port,
# --bin, --log and --no-route-toggle, and HOME points into the temp dir.
#
# Cases:
#   stubborn  the old daemon releases its port on SIGTERM and keeps running
#             (the 2026-10-06 shape). The restart must wait, escalate to
#             SIGKILL with a log line, and relaunch only after the old pid is
#             gone, so there is never a second writer.
#   graceful  the old daemon exits on SIGTERM. No SIGKILL, clean relaunch.
#
# Usage: scripts/test-restart-daemon.sh            (tests scripts/restart-daemon.sh)
#        RESTART_SCRIPT=/path/to/other.sh scripts/test-restart-daemon.sh
# Needs bash, python3, ss (iproute2) and curl. Exit 0 = all cases pass.
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT="${RESTART_SCRIPT:-${REPO_ROOT}/scripts/restart-daemon.sh}"
for need in python3 ss curl; do
  command -v "$need" >/dev/null 2>&1 || { echo "SKIP: $need not found"; exit 0; }
done

T="$(mktemp -d "${TMPDIR:-/tmp}/restart-daemon-test.XXXXXX")"
[[ -n "$T" && -d "$T" ]] || { echo "FAIL: mktemp"; exit 1; }
mkdir -p "$T/home/.observer" "$T/pids"
: >"$T/home/.observer/config.toml"

cleanup() {
  local f
  for f in "$T"/pids/*; do
    [[ -e "$f" ]] || continue
    kill -KILL "$(basename "$f")" 2>/dev/null || true
  done
  [[ -n "$T" && "$T" != "/" && -d "$T" ]] && rm -rf -- "$T"
}
trap cleanup EXIT

# free_port picks an unused loopback port away from the daemon's own.
free_port() {
  python3 - <<'PY'
import socket
s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()
PY
}

cat >"$T/fake.py" <<'PY'
# Fake observer daemon. argv: port mode piddir. mode "stubborn": on SIGTERM
# release the port and keep running. mode "serve": exit on SIGTERM.
import http.server, os, signal, sys, threading, time
port, mode, piddir = int(sys.argv[1]), sys.argv[2], sys.argv[3]
open(os.path.join(piddir, str(os.getpid())), "w").close()
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200); self.end_headers(); self.wfile.write(b"ok")
    def log_message(self, *a): pass
srv = http.server.HTTPServer(("127.0.0.1", port), H)
threading.Thread(target=srv.serve_forever, daemon=True).start()
def term(signum, frame):
    if mode == "stubborn":
        threading.Thread(target=srv.shutdown).start()
        srv.server_close()
    else:
        srv.server_close(); sys.exit(0)
signal.signal(signal.SIGTERM, term)
while True:
    time.sleep(0.2)
PY

# alive <pid>: 0 while the process exists and is not a zombie (an exited
# child awaiting its reap holds no DB handle, so it counts as gone).
alive() {
  local st
  st="$(cat /proc/"$1"/stat 2>/dev/null)" || return 1
  st="${st##*) }"
  [[ "${st:0:1}" != "Z" ]]
}

FAILS=0
fail() { echo "FAIL [$1] $2"; FAILS=$((FAILS + 1)); }

run_case() {
  local name="$1" oldmode="$2" port old rc out newpid i
  port="$(free_port)"
  if [[ "$port" == 8820 || "$port" == 8081 ]]; then fail "$name" "refusing daemon port $port"; return; fi
  cat >"$T/fakebin-$name" <<FB
#!/bin/sh
exec python3 "$T/fake.py" $port serve "$T/pids"
FB
  chmod +x "$T/fakebin-$name"

  python3 "$T/fake.py" "$port" "$oldmode" "$T/pids" &
  old=$!
  disown "$old" 2>/dev/null || true
  for ((i = 0; i < 40; i++)); do curl -fsS -m 1 "http://127.0.0.1:$port/" >/dev/null 2>&1 && break; sleep 0.25; done

  out="$(HOME="$T/home" OBSERVER_CONFIG="$T/home/.observer/config.toml" \
    bash "$SCRIPT" --bin "$T/fakebin-$name" --port "$port" --no-route-toggle \
      --stop-timeout 2 --log "$T/obs-$name.log" 2>&1)"
  rc=$?
  printf '%s\n' "$out" | sed "s/^/  [$name] /"

  [[ $rc -eq 0 ]] || fail "$name" "script exit $rc, want 0"
  if alive "$old"; then fail "$name" "old pid $old still alive after the restart (two daemons)"; fi
  newpid="$(ss -ltnp 2>/dev/null | grep ":$port " | grep -oP 'pid=\K[0-9]+' | head -1)"
  [[ -n "$newpid" ]] || fail "$name" "no daemon listening on :$port after the restart"
  [[ "$newpid" != "$old" ]] || fail "$name" "listener is still the old pid"

  local stopped relaunch sigkill
  stopped="$(printf '%s\n' "$out" | grep -n 'daemon stopped (pid=' | head -1 | cut -d: -f1)"
  relaunch="$(printf '%s\n' "$out" | grep -n 'relaunching:' | head -1 | cut -d: -f1)"
  sigkill="$(printf '%s\n' "$out" | grep -n 'sending SIGKILL' | head -1 | cut -d: -f1)"
  if [[ -z "$stopped" || -z "$relaunch" || "$stopped" -gt "$relaunch" ]]; then
    fail "$name" "the old pid's exit was not confirmed before the relaunch"
  fi
  case "$oldmode" in
    stubborn) [[ -n "$sigkill" && "$sigkill" -lt "${relaunch:-0}" ]] || fail "$name" "no SIGKILL escalation logged before the relaunch" ;;
    serve) [[ -z "$sigkill" ]] || fail "$name" "SIGKILL sent to a daemon that exited on SIGTERM" ;;
  esac

  kill -KILL "$old" "$newpid" 2>/dev/null || true
}

echo "testing $SCRIPT (temp dir $T)"
run_case stubborn stubborn
run_case graceful serve

if [[ $FAILS -eq 0 ]]; then
  echo "PASS: restart-daemon.sh waits for the old pid before relaunching"
  exit 0
fi
echo "FAILED: $FAILS check(s)"
exit 1
