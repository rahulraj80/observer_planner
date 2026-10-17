package guardtest

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/policy"
)

func base() policy.Event {
	return Base("/repo/sub", "/repo", time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC))
}

func TestEventShapes(t *testing.T) {
	cases := []struct {
		name       string
		ev         policy.Event
		kind       policy.EventKind
		actionType string
		target     string
		session    string
	}{
		{"a command", Command(base(), "rm -rf ./build"), policy.KindShellExec, "run_command", "rm -rf ./build", "guard-test"},
		{"a file write", FileWrite(base(), "~/.ssh/config"), policy.KindFileAccess, "write_file", "~/.ssh/config", "guard-test"},
		{"a raw event keeps its session", Raw(base(), "shell_exec", "git push -f", "", "s-1"), policy.KindShellExec, "", "git push -f", "s-1"},
		{"a raw event without a session", Raw(base(), "file_access", "/etc/passwd", "read_file", ""), policy.KindFileAccess, "read_file", "/etc/passwd", "guard-test"},
	}
	for _, tc := range cases {
		ev := tc.ev
		if ev.Kind != tc.kind || ev.ActionType != tc.actionType || ev.Target != tc.target || ev.SessionID != tc.session {
			t.Errorf("%s: event = %+v", tc.name, ev)
		}
		if ev.Tool != "guard-test" || ev.Cwd != "/repo/sub" || ev.ProjectRoot != "/repo" {
			t.Errorf("%s: base fields lost: %+v", tc.name, ev)
		}
	}
}

// TestRunAnswersBothModes: one dry run reports the verdict under the current
// mode AND under enforce, the question `observer guard test` exists for.
func TestRunAnswersBothModes(t *testing.T) {
	cfg := config.Default().Guard
	cfg.Mode = "observe"
	home := t.TempDir()
	res, err := Run(cfg, home, nil, Command(base(), "git push --force origin main"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict.RuleID != "R-110" || res.EnforceVerdict.Decision != policy.DecisionDeny {
		t.Fatalf("verdicts = %+v / %+v", res.Verdict, res.EnforceVerdict)
	}
	if !res.ModeChangesVerdict() {
		t.Fatalf("observe -> enforce should change the verdict: %s vs %s", res.Verdict.Decision, res.EnforceVerdict.Decision)
	}
	if res.Mode != "observe" {
		t.Fatalf("mode = %q", res.Mode)
	}

	quiet, err := Run(cfg, home, nil, Command(base(), "ls -la"))
	if err != nil {
		t.Fatal(err)
	}
	if quiet.Verdict.RuleID != "" || quiet.ModeChangesVerdict() {
		t.Fatalf("a harmless command matched: %+v", quiet.Verdict)
	}
}

// TestReportJSONKeys pins the --json shape the CLI always printed (the keys,
// alphabetical as the CLI's map encoding emitted them) so the dashboard and
// the CLI answer with the same document.
func TestReportJSONKeys(t *testing.T) {
	cfg := config.Default().Guard
	res, err := Run(cfg, t.TempDir(), nil, Command(base(), "git push --force origin main"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(res.Report())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{`"advice"`, `"decision"`, `"enforce_decision"`, `"kind"`, `"project_root"`, `"reason"`, `"rule_id"`, `"severity"`, `"source"`, `"target"`}
	last := -1
	for _, k := range want {
		i := strings.Index(string(raw), k)
		if i < 0 || i < last {
			t.Fatalf("key %s missing or out of order in %s", k, raw)
		}
		last = i
	}
}
