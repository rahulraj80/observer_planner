package config

import (
	"strings"
	"testing"
)

// TestParseProfileTarget pins one row per parse-table entry plus the
// refusals (T-369: the CLI and the dashboard share this parser).
func TestParseProfileTarget(t *testing.T) {
	cases := []struct {
		in      string
		want    ProfileTarget
		wantErr string
	}{
		{in: "default", want: ProfileTarget{Kind: ProfileTargetDefault}},
		{in: "anthropic", want: ProfileTarget{Kind: ProfileTargetProvider, Key: "anthropic"}},
		{in: "openai", want: ProfileTarget{Kind: ProfileTargetProvider, Key: "openai"}},
		{in: "tool:cline", want: ProfileTarget{Kind: ProfileTargetTool, Key: "cline"}},
		{in: "tool:kilo-code-cli", want: ProfileTarget{Kind: ProfileTargetTool, Key: "kilo-code-cli"}},
		{in: "tool:", wantErr: "needs a tool name"},
		{in: "tool:a.b", wantErr: "not a plain tool id"},
		{in: "tool:has space", wantErr: "not a plain tool id"},
		{in: "gemini", wantErr: "unknown traffic class"},
		{in: "", wantErr: "unknown traffic class"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseProfileTarget(tc.in)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("ParseProfileTarget(%q) err = %v, want %q", tc.in, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseProfileTarget(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("ParseProfileTarget(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
			if got.String() != tc.in {
				t.Errorf("String() = %q, want the input spelling %q", got.String(), tc.in)
			}
		})
	}
}

// TestAssignProfileTiers: each tier lands in its own field, the input is
// never mutated, and the result resolves through ResolveProfileName.
func TestAssignProfileTiers(t *testing.T) {
	base := defaultProfiles()
	cases := []struct {
		target, name       string
		provider, tool     string
		wantResolved       string
		wantDefault        string
		wantByToolEntryLen int
	}{
		{"default", "codex-safe", "gemini", "", "codex-safe", "codex-safe", 0},
		{"anthropic", "codex-variant", "anthropic", "", "codex-variant", "default", 0},
		{"tool:cline", "codex-variant", "anthropic", "cline", "codex-variant", "default", 1},
	}
	for _, tc := range cases {
		t.Run(tc.target, func(t *testing.T) {
			out, err := AssignProfile(base, tc.target, tc.name)
			if err != nil {
				t.Fatalf("AssignProfile: %v", err)
			}
			if got := ResolveProfileName(out, tc.provider, tc.tool); got != tc.wantResolved {
				t.Errorf("resolved = %q, want %q", got, tc.wantResolved)
			}
			if out.Default != tc.wantDefault {
				t.Errorf("Default = %q, want %q", out.Default, tc.wantDefault)
			}
			if len(out.ByTool) != tc.wantByToolEntryLen {
				t.Errorf("ByTool = %v, want %d entries", out.ByTool, tc.wantByToolEntryLen)
			}
			if base.ByProvider["anthropic"] != "claude-code" || base.ByTool != nil || base.Default != DefaultProfileName {
				t.Fatalf("input mutated: %+v", base)
			}
		})
	}
	if _, err := AssignProfile(base, "tool:cline", " "); err == nil {
		t.Error("an empty profile name must be refused")
	}
}

// TestUnassignProfile: only a tool assignment can be removed; removing it
// falls the tool back to its provider assignment.
func TestUnassignProfile(t *testing.T) {
	pc, err := AssignProfile(defaultProfiles(), "tool:cline", "codex-variant")
	if err != nil {
		t.Fatal(err)
	}
	out, removed, err := UnassignProfile(pc, "tool:cline")
	if err != nil || !removed {
		t.Fatalf("UnassignProfile = removed %v err %v, want removed", removed, err)
	}
	if got := ResolveProfileName(out, "anthropic", "cline"); got != "claude-code" {
		t.Errorf("after removal cline resolves to %q, want the provider's claude-code", got)
	}
	if out.ByTool != nil {
		t.Errorf("an emptied ByTool must be nil so no empty table is written, got %v", out.ByTool)
	}
	if pc.ByTool["cline"] != "codex-variant" {
		t.Fatal("input mutated")
	}
	if _, removed, err := UnassignProfile(out, "tool:cline"); err != nil || removed {
		t.Errorf("second removal = removed %v err %v, want a no-op", removed, err)
	}
	for _, target := range []string{"default", "anthropic", "openai"} {
		if _, _, err := UnassignProfile(pc, target); err == nil {
			t.Errorf("UnassignProfile(%q) must refuse: that tier always has a profile", target)
		}
	}
}
