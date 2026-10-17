package budgetwarn

import (
	"strings"
	"testing"
	"time"
)

// TestEvaluateSubjectWindowsAreTheirOwnLadder (Sol A5-04, D-016): a per-tool
// or per-model cap shares the window, unit and period of the whole-caller
// window, so its dedup key, notice id and sentence must carry the subject.
// Otherwise the tool's 75% rung would be swallowed by (or would swallow) the
// whole-caller 75% rung, and the developer could not tell which cap spoke.
func TestEvaluateSubjectWindowsAreTheirOwnLadder(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	ladder := []float64{0.75, 0.9, 1}
	whole := Window{Window: "daily", Unit: "usd", Cap: 100, Spent: 76, Scope: "member:m1", Enforcement: "soft", Thresholds: ladder}
	tool := Window{Window: "daily", Unit: "usd", Cap: 10, Spent: 7.5, Scope: "member:m1", Enforcement: "hard", Thresholds: ladder, Subject: "tool:claude-code"}
	model := Window{Window: "daily", Unit: "usd", Cap: 10, Spent: 7.5, Scope: "org", Enforcement: "soft", Thresholds: ladder, Subject: "model:gpt-5"}

	st, next := Evaluate([]Window{whole, tool, model}, State{}, now)
	if len(st.New) != 3 {
		t.Fatalf("New = %d notices, want 3 (one per cap): %+v", len(st.New), st.New)
	}
	if len(next.Notified) != 3 {
		t.Fatalf("Notified keys = %v, want 3 distinct keys", next.Notified)
	}
	ids := map[string]bool{}
	for _, n := range st.New {
		if ids[n.ID] {
			t.Errorf("duplicate notice id %q", n.ID)
		}
		ids[n.ID] = true
	}
	bySubject := func(sub string) Notice {
		for _, n := range st.New {
			if n.Subject == sub {
				return n
			}
		}
		t.Fatalf("no notice for subject %q", sub)
		return Notice{}
	}
	if n := bySubject("tool:claude-code"); n.Threshold != 0.75 || !strings.Contains(n.Message, "budget for tool claude-code") ||
		!strings.Contains(n.ID, "tool:claude-code") {
		t.Errorf("tool notice = %+v", n)
	}
	if n := bySubject("model:gpt-5"); !strings.Contains(n.Message, "budget for model gpt-5") {
		t.Errorf("model notice message = %q", n.Message)
	}
	if n := bySubject(""); strings.Contains(n.Message, " for ") {
		t.Errorf("whole-caller notice names a subject: %q", n.Message)
	}

	// The tool climbs to 90% while the whole caller stays put: only the tool
	// announces, and the whole-caller rung is not re-armed.
	tool.Spent = 9
	st2, _ := Evaluate([]Window{whole, tool, model}, next, now.Add(time.Minute))
	if len(st2.New) != 1 || st2.New[0].Subject != "tool:claude-code" || st2.New[0].Threshold != 0.9 {
		t.Fatalf("second pass New = %+v, want only the tool's 90%% rung", st2.New)
	}
	if len(st2.Active) != 3 {
		t.Errorf("Active = %d, want all three caps still active", len(st2.Active))
	}
}

func TestSubjectLabel(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"":                 "",
		"tool:claude-code": "tool claude-code",
		"model:gpt-5":      "model gpt-5",
	} {
		if got := SubjectLabel(in); got != want {
			t.Errorf("SubjectLabel(%q) = %q, want %q", in, got, want)
		}
	}
}
