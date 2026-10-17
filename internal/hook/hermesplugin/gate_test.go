package hermesplugin

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeObserver answers `observer hook hermes pre_tool_call` per FAKE_MODE and
// records its argv and stdin.
const fakeObserver = `#!/bin/sh
printf '%s\n' "$*" > "$FAKE_DIR/argv"
cat > "$FAKE_DIR/stdin"
case "$FAKE_MODE" in
allow) echo '{"decision":"allow","enforced":false}' ;;
deny) echo 'noise'; echo '{"decision":"deny","reason":"no ssh keys","enforced":true}' ;;
ask) echo '{"decision":"ask","reason":"confirm this","enforced":true}' ;;
garbage) echo 'not json' ;;
hang) exec sleep 20 ;;
esac
`

// gateDriver loads the rendered plugin, registers it against a fake ctx and
// calls the pre_tool_call callback once.
const gateDriver = `
import importlib.util, json, os, sys, time
spec = importlib.util.spec_from_file_location("obsplugin", sys.argv[1])
m = importlib.util.module_from_spec(spec); spec.loader.exec_module(m)
m.GATE_TIMEOUT_SECONDS = 1.5
m.GATE_MARGIN_SECONDS = 0.3
if os.environ.get("PRIME_HINT"):
    m._ENFORCED_HINT["tool_gate"] = (os.environ["PRIME_HINT"] == "true", time.monotonic())
hooks = {}
class Ctx:
    def register_hook(self, name, fn): hooks[name] = fn
m.register(Ctx())
out = hooks["pre_tool_call"](tool_name="terminal", args={"command": "cat ~/.ssh/id_rsa"}, session_id="s1", tool_call_id="c1")
print(json.dumps({"ret": out, "hooks": sorted(hooks)}))
`

// freshPosture is an enforced posture for cap written at at.
func freshPosture(at time.Time, cap string) string {
	return `{"schema":1,"enforced":true,"gate_caps":["` + cap + `"],"written_at":"` + at.UTC().Format(time.RFC3339Nano) + `"}`
}

// TestPreToolCallGate runs the embedded plugin's pre_tool_call under a real
// python3 with a fake observer: the reply mapping (deny -> block, ask ->
// approve, allow -> None) and the failure rule (no answer: block with a
// reason and spool only under enforcement).
func TestPreToolCallGate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake observer is a POSIX shell script")
	}
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not on PATH")
	}
	fresh := freshPosture(time.Now(), "tool_gate")
	cases := []struct {
		name, mode, posture, hint string
		want                      string
		spool                     string // the spooled outcome ("" = nothing spooled)
	}{
		{"deny maps to block", "deny", "", "", `"ret": {"action": "block", "message": "no ssh keys"}`, ""},
		{"ask maps to Hermes approval", "ask", "", "", `"ret": {"action": "approve", "message": "confirm this"}`, ""},
		{"allow returns nothing", "allow", "", "", `"ret": null`, ""},
		// T-049 / review M2-09: a fail-open failure is recorded too.
		{"garbage, not enforced", "garbage", "", "", `"ret": null`, "fail_open"},
		{"garbage, enforced", "garbage", fresh, "", `internal error`, "fail_closed"},
		{"timeout, enforced", "hang", fresh, "", `in time`, "fail_closed"},
		{"timeout, not enforced", "hang", "", "", `"ret": null`, "fail_open"},
		{"posture for prompts only", "garbage", freshPosture(time.Now(), "prompt_gate"), "", `"ret": null`, "fail_open"},
		// T-050 / review M2-02: a stale or undated enforced posture is ignored.
		{"stale enforced posture", "garbage", freshPosture(time.Now().Add(-10*time.Minute), "tool_gate"), "", `"ret": null`, "fail_open"},
		{"future-dated posture", "garbage", freshPosture(time.Now().Add(10*time.Minute), "tool_gate"), "", `"ret": null`, "fail_open"},
		{"undated posture", "garbage", `{"schema":1,"enforced":true,"gate_caps":["tool_gate"]}`, "", `"ret": null`, "fail_open"},
		// Review M2-04: a recent enforced:false hint never opens a failure
		// the fresh posture closes; an enforced:true hint closes on its own.
		{"enforced:false hint, fresh enforced posture", "garbage", fresh, "false", `internal error`, "fail_closed"},
		{"enforced:true hint, no posture", "garbage", "", "true", `internal error`, "fail_closed"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			obsDir := filepath.Join(dir, "obs")
			if err := os.MkdirAll(obsDir, 0o700); err != nil {
				t.Fatal(err)
			}
			fake := filepath.Join(dir, "observer")
			if err := os.WriteFile(fake, []byte(fakeObserver), 0o755); err != nil {
				t.Fatal(err)
			}
			if c.posture != "" {
				if err := os.WriteFile(filepath.Join(obsDir, "guard-posture.json"), []byte(c.posture), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			pluginDir := filepath.Join(dir, "plugin")
			if err := WritePlugin(pluginDir, fake); err != nil {
				t.Fatal(err)
			}
			drv := filepath.Join(dir, "drv.py")
			if err := os.WriteFile(drv, []byte(gateDriver), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(py, drv, filepath.Join(pluginDir, "__init__.py"))
			cmd.Env = append(os.Environ(), "FAKE_MODE="+c.mode, "FAKE_DIR="+dir, "PRIME_HINT="+c.hint,
				"OBSERVER_CONFIG="+filepath.Join(obsDir, "config.toml"), "OBSERVER_BIN="+fake)
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("python driver: %v %s", err, out)
			}
			if !strings.Contains(string(out), c.want) {
				t.Fatalf("out = %s; want %s", out, c.want)
			}
			if !strings.Contains(string(out), `"pre_tool_call"`) {
				t.Fatalf("pre_tool_call not registered: %s", out)
			}
			spool, _ := os.ReadFile(filepath.Join(obsDir, "guard-hookfail.jsonl"))
			if c.spool == "" && len(spool) > 0 || c.spool != "" && !strings.Contains(string(spool), `"outcome": "`+c.spool+`"`) {
				t.Fatalf("spool = %q, want outcome %q", spool, c.spool)
			}
			if c.mode != "hang" {
				argv, _ := os.ReadFile(filepath.Join(dir, "argv"))
				if !strings.HasPrefix(string(argv), "hook hermes pre_tool_call --max-runtime 1200ms --config ") {
					t.Fatalf("argv = %q", argv)
				}
				var env map[string]any
				stdin, _ := os.ReadFile(filepath.Join(dir, "stdin"))
				if err := json.Unmarshal(stdin, &env); err != nil || env["tool_name"] != "terminal" || env["session_id"] != "s1" || env["host"] != "hermes" {
					t.Fatalf("envelope = %s (%v)", stdin, err)
				}
			}
		})
	}
}
