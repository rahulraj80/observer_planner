//go:build !sbo_omit_adapter_cursor

package hook

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/guard"
	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/scrub"
)

// TestConformance_Cursor runs the cursor fixtures through the guarded
// receiver: deny permission JSON on a destructive command in enforce
// mode, allow + capture in observe mode, and capture proceeding on
// EVERY path (a denied attempt still records).
func TestConformance_Cursor(t *testing.T) {
	t.Parallel()
	sc := scrub.New()

	t.Run("enforce denies destructive shell", func(t *testing.T) {
		t.Parallel()
		g := conformanceGuard(t, "enforce")
		sink := &fakeSink{outcomeRows: 1}
		var out bytes.Buffer
		var persisted []guard.ActionVerdict
		HandleCursorEventGuarded("beforeShellExecution", g,
			func(v guard.ActionVerdict) { persisted = append(persisted, v) },
			sink, sc,
			bytes.NewReader(loadFixture(t, "cursor", "before_shell_rmrf.json")),
			false, &out, &bytes.Buffer{}, 0)
		var reply struct {
			Permission string `json:"permission"`
		}
		if err := json.Unmarshal(out.Bytes(), &reply); err != nil {
			t.Fatalf("reply parse: %v (%s)", err, out.String())
		}
		if reply.Permission != "deny" {
			t.Errorf("permission = %q, want deny", reply.Permission)
		}
		if len(persisted) != 1 || persisted[0].Verdict.RuleID != "R-101" || !persisted[0].Enforced {
			t.Errorf("persisted = %+v, want one enforced R-101", persisted)
		}
		if persisted[0].Input.Tool != models.ToolCursor {
			t.Errorf("verdict tool = %q", persisted[0].Input.Tool)
		}
		// Capture proceeded despite the deny - the attempt is recorded.
		if len(sink.called) == 0 {
			t.Error("denied attempt was not captured")
		}
	})

	t.Run("observe flags but allows", func(t *testing.T) {
		t.Parallel()
		g := conformanceGuard(t, "observe")
		sink := &fakeSink{outcomeRows: 1}
		var out bytes.Buffer
		var persisted []guard.ActionVerdict
		HandleCursorEventGuarded("beforeShellExecution", g,
			func(v guard.ActionVerdict) { persisted = append(persisted, v) },
			sink, sc,
			bytes.NewReader(loadFixture(t, "cursor", "before_shell_rmrf.json")),
			false, &out, &bytes.Buffer{}, 0)
		if !strings.Contains(out.String(), `"permission":"allow"`) {
			t.Errorf("observe reply = %s, want allow", out.String())
		}
		if len(persisted) != 1 || persisted[0].Verdict.RuleID != "R-101" || persisted[0].Enforced {
			t.Errorf("persisted = %+v, want one un-enforced R-101 flag", persisted)
		}
	})

	t.Run("benign command stays silent", func(t *testing.T) {
		t.Parallel()
		g := conformanceGuard(t, "enforce")
		sink := &fakeSink{outcomeRows: 1}
		var out bytes.Buffer
		var persisted []guard.ActionVerdict
		HandleCursorEventGuarded("beforeShellExecution", g,
			func(v guard.ActionVerdict) { persisted = append(persisted, v) },
			sink, sc,
			bytes.NewReader(loadFixture(t, "cursor", "before_shell_benign.json")),
			false, &out, &bytes.Buffer{}, 0)
		if !strings.Contains(out.String(), `"permission":"allow"`) {
			t.Errorf("benign reply = %s", out.String())
		}
		if len(persisted) != 0 {
			t.Errorf("benign command persisted a verdict: %+v", persisted)
		}
		if len(sink.called) == 0 {
			t.Error("benign command not captured")
		}
	})

	t.Run("nil guard degrades to unguarded receiver", func(t *testing.T) {
		t.Parallel()
		sink := &fakeSink{outcomeRows: 1}
		var out bytes.Buffer
		HandleCursorEventGuarded("beforeShellExecution", nil, nil, sink, sc,
			bytes.NewReader(loadFixture(t, "cursor", "before_shell_rmrf.json")),
			false, &out, &bytes.Buffer{}, 0)
		if !strings.Contains(out.String(), `"permission":"allow"`) {
			t.Errorf("nil-guard reply = %s, want unconditional allow", out.String())
		}
	})
}
