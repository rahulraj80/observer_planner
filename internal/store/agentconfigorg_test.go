package store

import (
	"context"
	"testing"
	"time"
)

func TestOrgAgentConfigPolicyCacheAndState(t *testing.T) {
	t.Parallel()
	s, _ := newTestStore(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)

	if _, ok, err := s.GetOrgAgentConfigPolicy(ctx); err != nil || ok {
		t.Fatalf("fresh cache: ok=%v err=%v", ok, err)
	}
	if err := s.UpsertOrgAgentConfigPolicy(ctx, OrgAgentConfigPolicyRow{OrgID: "o1", Subject: "dana", Version: 4, Body: "{}", BodyHash: "h", Signature: "s", ServerPubkey: "k", ReceivedAt: at}); err != nil {
		t.Fatal(err)
	}
	row, ok, err := s.GetOrgAgentConfigPolicy(ctx)
	if err != nil || !ok || row.Version != 4 || row.Subject != "dana" || !row.ReceivedAt.Equal(at) {
		t.Fatalf("cache = %+v ok=%v err=%v", row, ok, err)
	}

	rows := []AgentConfigOrgStateRow{
		{
			SettingID: "codex/model", FileID: "codex.config.user", Tool: "codex", Scope: "user", Mode: "enforced", Layer: "o1_org",
			State: "backed_off", ExpectedHash: "aa", ObservedHash: "bb", ObservedValue: `"o3"`, DriftTimes: []time.Time{at, at.Add(time.Minute)},
			LastDriftAt: at.Add(time.Minute), PolicyVersion: 4, ObservedAt: at,
		},
		{
			SettingID: "claude-code/model", FileID: "claude-code.settings.project_local", ProjectRoot: "/w/a", ProjectHash: "ph", Tool: "claude-code",
			Scope: "project_local", Mode: "enforced", State: "drifted", ObservedValue: `"opus"`, ObservedAt: at,
		},
		{
			SettingID: "claude-code/model", FileID: "claude-code.settings.project_local", ProjectRoot: "/w/b", ProjectHash: "ph", Tool: "claude-code",
			Scope: "project_local", Mode: "enforced", State: "applied", ObservedAt: at,
		},
		{
			SettingID: "codex/notify", FileID: "codex.config.user", Tool: "codex", Scope: "user", Mode: "enforced", State: "drifted",
			ObservedValue: `{"OPENAI_API_KEY":"[REDACTED]"}`, ObservedSecret: true, ObservedAt: at,
		},
	}
	if err := s.ReplaceAgentConfigOrgState(ctx, rows); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadAgentConfigOrgState(ctx)
	if err != nil || len(got) != 4 {
		t.Fatalf("load: %v %d", err, len(got))
	}
	for _, r := range got {
		if r.SettingID == "codex/model" && (len(r.DriftTimes) != 2 || r.ObservedValue != `"o3"` || !r.LastDriftAt.Equal(at.Add(time.Minute))) {
			t.Fatalf("round trip lost fields: %+v", r)
		}
		// T-428: the writer hands the store a value already scrubbed; it is
		// kept (scrubbed, not withheld).
		if r.SettingID == "codex/notify" && (r.ObservedValue != `{"OPENAI_API_KEY":"[REDACTED]"}` || !r.ObservedSecret) {
			t.Fatalf("a scrubbed secret-flagged value was not kept: %+v", r)
		}
	}

	// The cache is bound to the enrolment it was signed for: compose reads
	// it only while this node is enrolled as o1/dana.
	if err := s.WriteEnrolment(ctx, Enrolment{OrgID: "o1", OrgServerURL: "https://o1.example", UserID: "dana", Tenancy: "managed"}); err != nil {
		t.Fatal(err)
	}
	// No gate: never composed. A false gate: never composed.
	if rep := s.composeAgentConfigState(ctx, true); rep != nil {
		t.Fatalf("ungated compose = %+v", rep)
	}
	s.SetAgentConfigStateGate(func(context.Context) bool { return false })
	if rep := s.composeAgentConfigState(ctx, true); rep != nil {
		t.Fatalf("individual compose = %+v", rep)
	}
	s.SetAgentConfigStateGate(func(context.Context) bool { return true })
	rep := s.composeAgentConfigState(ctx, true)
	if rep == nil || rep.PolicyVersion != 4 || len(rep.Rows) != 3 {
		t.Fatalf("managed compose = %+v", rep)
	}
	for _, w := range rep.Rows {
		switch w.Setting {
		case "codex/model":
			if w.ObservedValue != `"o3"` || w.DriftCount != 2 {
				t.Fatalf("user-file value not carried: %+v", w)
			}
		case "claude-code/model":
			// Two clones of one project collapse; the worst state wins, and a
			// personal (local-only) file's value ships on a managed node
			// under full content (D-112, T-428).
			if w.State != "drifted" || w.ObservedValue != `"opus"` || w.ProjectHash != "ph" {
				t.Fatalf("project row: %+v", w)
			}
		}
	}
	if rep := s.composeAgentConfigState(ctx, false); rep == nil || rep.Rows[0].ObservedValue != "" && rep.Rows[1].ObservedValue != "" {
		t.Fatalf("values shipped without raw content: %+v", rep)
	}
	for _, w := range s.composeAgentConfigState(ctx, false).Rows {
		if w.ObservedValue != "" {
			t.Fatalf("value without raw content: %+v", w)
		}
	}
	// Review C156-39: a changed digest stays "changed" until a push is
	// accepted (CommitPushedSnapshots), so a failed push re-forces it.
	if !s.noteAgentConfigDigest(rep) {
		t.Fatal("a new digest must force a push")
	}
	if !s.noteAgentConfigDigest(rep) {
		t.Fatal("a digest whose push was never accepted must force the next push too")
	}
	s.CommitPushedSnapshots()
	if s.noteAgentConfigDigest(rep) {
		t.Fatal("a delivered digest must not force another push")
	}

	// Unenrolment clears the policy and its state.
	if err := s.DeleteEnrolment(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.GetOrgAgentConfigPolicy(ctx); ok {
		t.Fatal("unenrol kept the policy")
	}
	if got, _ := s.LoadAgentConfigOrgState(ctx); len(got) != 0 {
		t.Fatalf("unenrol kept state: %+v", got)
	}
}

// TestOrgAgentConfigPolicyIsEnrolmentBound pins review M3-01: a policy row
// signed for another org, or for another member of the same org, reads as
// absent through every consumer-facing reader (the applier's reader, the
// editors' body reader, the state reader and the push report), and nothing
// reads before an enrolment exists.
func TestOrgAgentConfigPolicyIsEnrolmentBound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	at := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		enr    *Enrolment
		wantOK bool
	}{
		{"not enrolled", nil, false},
		{"same org and member", &Enrolment{OrgID: "orgA", UserID: "dana"}, true},
		{"another org", &Enrolment{OrgID: "orgB", UserID: "dana"}, false},
		{"another member of the same org", &Enrolment{OrgID: "orgA", UserID: "sam"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, _ := newTestStore(t)
			if err := s.UpsertOrgAgentConfigPolicy(ctx, OrgAgentConfigPolicyRow{OrgID: "orgA", Subject: "dana", Version: 3, Body: "{}", BodyHash: "h", Signature: "s", ServerPubkey: "k", ReceivedAt: at}); err != nil {
				t.Fatal(err)
			}
			if err := s.ReplaceAgentConfigOrgState(ctx, []AgentConfigOrgStateRow{{SettingID: "codex/model", FileID: "codex.config.user", Tool: "codex", Scope: "user", Mode: "enforced", State: "drifted", ObservedAt: at}}); err != nil {
				t.Fatal(err)
			}
			if tc.enr != nil {
				e := *tc.enr
				e.OrgServerURL, e.Tenancy = "https://org.example", "managed"
				if err := s.WriteEnrolment(ctx, e); err != nil {
					t.Fatal(err)
				}
			}
			s.SetAgentConfigStateGate(func(context.Context) bool { return true })
			if _, ok, err := s.CurrentOrgAgentConfigPolicy(ctx); err != nil || ok != tc.wantOK {
				t.Fatalf("CurrentOrgAgentConfigPolicy ok=%v err=%v, want ok=%v", ok, err, tc.wantOK)
			}
			if _, _, ok := s.OrgAgentConfigBody(ctx); ok != tc.wantOK {
				t.Fatalf("OrgAgentConfigBody ok=%v, want %v", ok, tc.wantOK)
			}
			rows, err := s.CurrentAgentConfigOrgState(ctx)
			if err != nil || (len(rows) > 0) != tc.wantOK {
				t.Fatalf("CurrentAgentConfigOrgState = %d rows err=%v, want rows=%v", len(rows), err, tc.wantOK)
			}
			if rep := s.composeAgentConfigState(ctx, true); (rep != nil) != tc.wantOK {
				t.Fatalf("composeAgentConfigState = %+v, want a report=%v", rep, tc.wantOK)
			}
		})
	}
}

func TestProjectRootsByRemoteHash(t *testing.T) {
	t.Parallel()
	s, d := newTestStore(t)
	ctx := context.Background()
	for _, p := range []struct{ root, hash string }{{"/w/a", "h1"}, {"/w/b", "h1"}, {"/w/c", ""}} {
		if _, err := d.ExecContext(ctx, `INSERT INTO projects (root_path, git_remote_hash, created_at) VALUES (?, ?, '2026-10-04T00:00:00Z')`, p.root, p.hash); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ProjectRootsByRemoteHash(ctx)
	if err != nil || len(got) != 1 || len(got["h1"]) != 2 {
		t.Fatalf("got %v %v", got, err)
	}
}
