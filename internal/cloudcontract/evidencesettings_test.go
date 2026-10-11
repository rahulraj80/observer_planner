package cloudcontract

import "testing"

// TestEvidenceSettingsValidate pins the one validator both planes use: each
// row is one bound.
func TestEvidenceSettingsValidate(t *testing.T) {
	base := DefaultEvidenceSettings()
	cases := []struct {
		name string
		mut  func(*EvidenceSettings)
		ok   bool
	}{
		{"default", func(*EvidenceSettings) {}, true},
		{"unknown version", func(s *EvidenceSettings) { s.Version = 2 }, false},
		{"negative user messages", func(s *EvidenceSettings) { s.UserMessages = -1 }, false},
		{"too many user messages", func(s *EvidenceSettings) { s.UserMessages, s.AssistantMessages, s.FailureClasses = 21, 0, 0 }, false},
		{"excerpt total above 20", func(s *EvidenceSettings) { s.UserMessages, s.AssistantMessages, s.FailureClasses = 10, 10, 1 }, false},
		{"excerpt total exactly 20", func(s *EvidenceSettings) { s.UserMessages, s.AssistantMessages, s.FailureClasses = 10, 7, 3 }, true},
		{"four failure classes", func(s *EvidenceSettings) { s.UserMessages, s.FailureClasses = 1, 4 }, false},
		{"action summaries above 256", func(s *EvidenceSettings) { s.ActionSummaries = 257 }, false},
		{"excerpt bytes below 128", func(s *EvidenceSettings) { s.ExcerptBytes = 127 }, false},
		{"excerpt bytes above 1024", func(s *EvidenceSettings) { s.ExcerptBytes = 1025 }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := base
			tc.mut(&s)
			if err := s.Validate(); (err == nil) != tc.ok {
				t.Fatalf("Validate = %v, want ok=%v", err, tc.ok)
			}
		})
	}
}

// TestParseEvidenceSettings pins the strict decode: partial, null, unknown and
// malformed documents fail closed; empty is the legacy nil.
func TestParseEvidenceSettings(t *testing.T) {
	good, err := DefaultEvidenceSettings().JSON()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		raw   string
		isNil bool
		ok    bool
	}{
		{"empty is legacy", "", true, true},
		{"canonical", good, false, true},
		{"partial", `{"version":1,"user_messages":1}`, true, false},
		{"null field", `{"version":1,"user_messages":null,"assistant_messages":0,"failure_classes":0,"action_summaries":0,"excerpt_bytes":128,"milestones":true,"outcomes":true}`, true, false},
		{"unknown field", `{"version":1,"user_messages":1,"assistant_messages":0,"failure_classes":0,"action_summaries":0,"excerpt_bytes":128,"milestones":true,"surprise":true}`, true, false},
		{"malformed", `{`, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseEvidenceSettings(tc.raw)
			if (err == nil) != tc.ok || (got == nil) != tc.isNil {
				t.Fatalf("Parse = %v, %v; want ok=%v nil=%v", got, err, tc.ok, tc.isNil)
			}
		})
	}
}

// TestEvidenceSettingsForLevel pins the narrowing per org evidence level.
func TestEvidenceSettingsForLevel(t *testing.T) {
	s := DefaultEvidenceSettings()
	cases := []struct {
		level         EvidenceLevel
		user, asst, f int
	}{
		{EvidenceLevelExcerpts, s.UserMessages, s.AssistantMessages, s.FailureClasses},
		{EvidenceLevelTitles, 1, 0, 0},
		{EvidenceLevelStructural, 0, 0, 0},
		{"bogus", 0, 0, 0},
	}
	for _, tc := range cases {
		got := s.ForEvidenceLevel(tc.level)
		if got.UserMessages != tc.user || got.AssistantMessages != tc.asst || got.FailureClasses != tc.f {
			t.Errorf("%s: got %+v", tc.level, got)
		}
		if got.ActionSummaries != s.ActionSummaries || got.Milestones != s.Milestones {
			t.Errorf("%s: structural preferences changed: %+v", tc.level, got)
		}
	}
}

// TestPurposeLabel pins that every known purpose has a friendly label and an
// unknown one reads as itself.
func TestPurposeLabel(t *testing.T) {
	for _, p := range AllPurposes() {
		if p.Label() == string(p) || p.Label() == "" {
			t.Errorf("%s has no friendly label", p)
		}
	}
	if Purpose("made_up").Label() != "made_up" {
		t.Error("an unknown purpose must read as its id")
	}
}
