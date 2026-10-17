package store

import (
	"context"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/models"
)

// TestLoadOrgIntelTitles: the newest non-empty pull per session wins, and a
// session with no cached title is absent.
func TestLoadOrgIntelTitles(t *testing.T) {
	ctx := context.Background()
	s := intelTestStore(t)
	base := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	rows := []OrgIntelResult{
		{SessionID: "s1", JobID: "j1", Title: "Older title", FetchedAt: base},
		{SessionID: "s1", JobID: "j2", Title: "Newer title", FetchedAt: base.Add(time.Hour)},
		{SessionID: "s2", JobID: "j3", Title: "Only title", FetchedAt: base},
	}
	for _, r := range rows {
		r.SchemaVersion, r.Confidence = "session_enrichment.v2-candidate", "high"
		if err := s.UpsertOrgIntelResult(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.LoadOrgIntelTitles(ctx, []string{"s1", "s2", "s3"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"s1": "Newer title", "s2": "Only title"}
	if len(got) != len(want) || got["s1"] != want["s1"] || got["s2"] != want["s2"] {
		t.Fatalf("titles = %v, want %v", got, want)
	}
	if empty, err := s.LoadOrgIntelTitles(ctx, nil); err != nil || len(empty) != 0 {
		t.Fatalf("empty ids: %v %v", empty, err)
	}
}

// TestLoadOrgAuthoritySessions: only an org-stamped session is reported; a
// personal or unknown (NULL) stamp and a missing id are absent.
func TestLoadOrgAuthoritySessions(t *testing.T) {
	ctx := context.Background()
	s := intelTestStore(t)
	pid, err := s.UpsertProject(ctx, "/p", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"org-1", "personal-1", "unknown-1"} {
		if err := s.UpsertSession(ctx, models.Session{ID: id, ProjectID: pid, Tool: "codex", StartedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	for id, auth := range map[string]any{"org-1": "org", "personal-1": "personal", "unknown-1": nil} {
		if _, err := s.db.ExecContext(ctx, `UPDATE sessions SET authority = ? WHERE id = ?`, auth, id); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.LoadOrgAuthoritySessions(ctx, []string{"org-1", "personal-1", "unknown-1", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got["org-1"] {
		t.Fatalf("org sessions = %v", got)
	}
}
