package controlcoverage

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/integration"
)

// TestCapGroupsCoverTheVocabularyOnce: every capability of the closed
// vocabulary sits in exactly one matrix column, so no binding is ever
// dropped from the matrix.
func TestCapGroupsCoverTheVocabularyOnce(t *testing.T) {
	seen := map[integration.HookCap]string{}
	for _, g := range CapGroups() {
		if g.ID == "" || g.Label == "" || len(g.Caps) == 0 {
			t.Errorf("group %+v is incomplete", g)
		}
		for _, c := range g.Caps {
			if prev, dup := seen[c]; dup {
				t.Errorf("%s is in both %s and %s", c, prev, g.ID)
			}
			seen[c] = g.ID
		}
	}
	for _, c := range integration.HookCaps() {
		if _, ok := seen[c]; !ok {
			t.Errorf("capability %s is in no matrix column", c)
		}
	}
}

// TestHookMatrixIsGeneratedFromBindings checks the matrix against the
// registry itself (never a hand count, so another lane wiring a binding
// moves both sides together): a column is W exactly when some binding of its
// capabilities is Wired, every binding's native event appears in its
// column, and a hook-less row carries its honest-zero note.
func TestHookMatrixIsGeneratedFromBindings(t *testing.T) {
	rows := HookMatrix()
	if len(rows) != len(integration.Capabilities()) {
		t.Fatalf("matrix rows = %d, registry rows = %d", len(rows), len(integration.Capabilities()))
	}
	groupOf := map[integration.HookCap]int{}
	for i, g := range CapGroups() {
		for _, c := range g.Caps {
			groupOf[c] = i
		}
	}
	for _, r := range rows {
		c, _ := integration.For(r.Tool)
		if len(r.Cells) != len(CapGroups()) {
			t.Fatalf("%s: %d cells, want %d", r.Tool, len(r.Cells), len(CapGroups()))
		}
		if len(c.Hook.Bindings) == 0 && c.Hook.Note != "" && r.Note == "" {
			t.Errorf("%s: hook-less row lost its honest-zero note", r.Tool)
		}
		for i, g := range CapGroups() {
			wired := false
			for _, cp := range g.Caps {
				wired = wired || c.Hook.WiredFor(cp)
			}
			if got := r.Cells[i].State == CoverageWired; got != wired {
				t.Errorf("%s/%s: wired cell = %v, registry WiredFor = %v", r.Tool, g.ID, got, wired)
			}
		}
		for _, b := range c.Hook.Bindings {
			cell := r.Cells[groupOf[b.Cap]]
			want := b.NativeEvent + " " + DecisionLetters(b)
			found := false
			for _, ev := range cell.Events {
				found = found || ev == want
			}
			if !found {
				t.Errorf("%s: binding %q missing from column %s (%v)", r.Tool, want, cell.Group, cell.Events)
			}
		}
		if len(r.Bindings) != len(c.Hook.Bindings) {
			t.Errorf("%s: %d binding views, %d bindings", r.Tool, len(r.Bindings), len(c.Hook.Bindings))
		}
	}
}

// TestHookMatrixClaudeCodeToolGate pins one row the audit grounds live: the
// Claude Code tool-gate column is wired and spells PreToolUse's decisions.
func TestHookMatrixClaudeCodeToolGate(t *testing.T) {
	row, ok := HookMatrixFor("claude-code")
	if !ok {
		t.Fatal("claude-code has no row")
	}
	cell := row.Cells[0]
	if cell.Group != "tool_gate" || cell.Mark() != "W" {
		t.Fatalf("tool gate cell = %+v", cell)
	}
	if !strings.Contains(strings.Join(cell.Events, ","), "PreToolUse D A M") {
		t.Errorf("events = %v", cell.Events)
	}
	if _, ok := HookMatrixFor("no-such-tool"); ok {
		t.Error("an unknown tool resolved a row")
	}
}

// TestCapSummariesAgreeWithTheMatrix: the per-capability counts are sums of
// the matrix cells' states.
func TestCapSummariesAgreeWithTheMatrix(t *testing.T) {
	rows := HookMatrix()
	for _, s := range CapSummaries() {
		var wired, provides int
		for _, r := range rows {
			st := r.Caps[s.Cap].State
			if st == CoverageWired {
				wired++
			}
			if st != CoverageNone {
				provides++
			}
		}
		if s.Wired != wired || s.Provides != provides || s.Gap() != provides-wired {
			t.Errorf("%s: summary %+v, matrix wired %d provides %d", s.Cap, s, wired, provides)
		}
	}
}

func TestDecisionLetters(t *testing.T) {
	for _, tc := range []struct {
		dec  []string
		want string
	}{
		{nil, "O"},
		{[]string{integration.HookDecisionAllow}, "O"},
		{[]string{integration.HookDecisionAllow, integration.HookDecisionAsk, integration.HookDecisionDeny, integration.HookDecisionModify}, "D A M"},
		{[]string{integration.HookDecisionContext, integration.HookDecisionMessage}, "C U"},
	} {
		if got := DecisionLetters(integration.HookBinding{Decisions: tc.dec}); got != tc.want {
			t.Errorf("DecisionLetters(%v) = %q, want %q", tc.dec, got, tc.want)
		}
	}
}
