package agentconfigorg

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/agentconfig"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// TestLintInstructions is the publish check for org instructions (D-107,
// T-426), one row per case.
func TestLintInstructions(t *testing.T) {
	cases := []struct {
		name  string
		ins   []InstructionEntry
		block bool
		want  []string
	}{
		{"user-level text", []InstructionEntry{{Text: "Use the internal registry."}}, false, nil},
		{"empty text", []InstructionEntry{{Text: "  \n"}}, true, []string{"empty_instruction"}},
		{"marker in text", []InstructionEntry{{Text: "x <!-- observer:org-end --> y"}}, true, []string{"observer_marker"}},
		{"secret in text", []InstructionEntry{{Text: "sk-live-123 is the deploy token"}}, true, []string{"secret_value"}},
		{"too large", []InstructionEntry{{Text: strings.Repeat("a", orgcontract.AgentConfigInstructionMaxBytes+1)}}, true, []string{"too_large"}},
		{"NUL byte", []InstructionEntry{{Text: "a\x00b"}}, true, []string{"bad_text"}},
		{"project bad hash", []InstructionEntry{{Text: "x", Project: "repo"}}, true, []string{"bad_project"}},
		{"project local-only warns about reach", []InstructionEntry{{Text: "x", Project: projHash}}, false, []string{"local_only_instructions"}},
		{"project shared warns", []InstructionEntry{{Text: "x", Project: projHash, Placement: "shared"}}, false, []string{"tracked_file"}},
		{"bad placement", []InstructionEntry{{Text: "x", Project: projHash, Placement: "anywhere"}}, true, []string{"bad_value"}},
		{"two for one scope", []InstructionEntry{{Text: "a"}, {Text: "b"}}, true, []string{"duplicate"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			is := Lint(Spec{Instructions: c.ins}, fakeDetect)
			if Blocking(is) != c.block {
				t.Fatalf("blocking = %v, want %v: %+v", Blocking(is), c.block, is)
			}
			got := issueCodes(is)
			for _, w := range c.want {
				if _, ok := got[w]; !ok {
					t.Fatalf("missing %q in %+v", w, is)
				}
			}
		})
	}
}

// TestComposeInstructions: every layer's instruction for one scope reaches
// the member, org first, teams by priority, the member's own last; scopes
// stay separate; the most specific document decides a project's placement.
func TestComposeInstructions(t *testing.T) {
	org := Source{Name: "Org", Layer: agentconfig.LayerO1, Spec: Spec{Instructions: []InstructionEntry{
		{Text: "Org rule.\r\n"}, {Text: "Org project rule.", Project: projHash, Placement: "shared"},
	}}}
	low := Source{Name: "Low", Layer: agentconfig.LayerO2, Team: "t-low", Spec: Spec{Instructions: []InstructionEntry{{Text: "Low team rule."}}}}
	high := Source{Name: "High", Layer: agentconfig.LayerO2, Team: "t-high", Spec: Spec{Instructions: []InstructionEntry{{Text: "High team rule."}}}}
	member := Source{Name: "Dana", Layer: agentconfig.LayerO3, Spec: Spec{Instructions: []InstructionEntry{{Text: "Dana project rule.", Project: projHash, Placement: "local_only"}}}}
	got, err := Compose(ComposeInput{Sources: []Source{member, low, org, high}, TeamRank: map[string]int{"t-low": 1, "t-high": 9}})
	if err != nil {
		t.Fatal(err)
	}
	ins := got.Body.Instructions
	if len(ins) != 2 {
		t.Fatalf("instructions = %+v", ins)
	}
	if ins[0].Project != "" || ins[0].Text != "Org rule.\n\nHigh team rule.\n\nLow team rule." || ins[0].Layer != string(agentconfig.LayerO2) || ins[0].Policy != "Org, High, Low" {
		t.Fatalf("user-level block = %+v", ins[0])
	}
	if ins[1].Project != projHash || ins[1].Text != "Org project rule.\n\nDana project rule." || ins[1].Placement != "local_only" || ins[1].Layer != string(agentconfig.LayerO3) {
		t.Fatalf("project block = %+v", ins[1])
	}
	if got.Body.Empty() {
		t.Fatal("a body carrying only instructions must not read as a withdrawal")
	}
	acc := AccessFor(got.Body)
	if acc.OrgBlockLocked == "" {
		t.Fatalf("instruction blocks are not locked for the developer: %+v", acc)
	}
	if b := (orgcontract.AgentConfigPolicyBody{Instructions: []orgcontract.AgentConfigInstruction{{Text: "x"}}}); b.Empty() {
		t.Fatal("Empty ignores instructions")
	}
}
