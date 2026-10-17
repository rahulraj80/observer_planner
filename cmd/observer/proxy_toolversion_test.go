//go:build !sbo_omit_capture_proxy

package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"
	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestProxyToolVersionSink walks the adapter's decision table (one case
// per row) against a real store.
func TestProxyToolVersionSink(t *testing.T) {
	ctx := context.Background()
	conn, err := dbtemplate.Open(ctx, db.Options{Path: filepath.Join(t.TempDir(), "o.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	s := store.New(conn)
	seed := func(id, tool string) {
		t.Helper()
		ev := models.ToolEvent{
			SourceFile: "/tmp/" + id, SourceEventID: id + "-1", SessionID: id,
			ProjectRoot: "/tmp/tv-proj", Timestamp: time.Now().UTC(), Tool: tool,
			ActionType: models.ActionUserPrompt, Target: "hi", Success: true, RawToolName: "prompt",
		}
		if _, err := s.Ingest(ctx, []models.ToolEvent{ev}, nil, store.IngestOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	seed("cc-1", models.ToolClaudeCode)
	seed("pi-1", models.ToolPi)
	seed("cc-held", models.ToolClaudeCode)
	if _, err := s.SetSessionToolVersion(ctx, models.SessionToolVersion{SessionID: "cc-held", Version: "1.0.0"}); err != nil {
		t.Fatal(err)
	}

	k := proxyToolVersionSink{s: s}
	cases := []struct {
		name        string
		session     string
		wantSettled bool
		wantFirst   string
		wantLast    string
	}{
		{"not ingested yet", "missing", false, "", ""},
		// An upgrade arriving via the proxy: the first build stays, the
		// newer one becomes the latest (agent migration 162).
		{"already holds a version: upgrade recorded as last", "cc-held", true, "1.0.0", "2.1.3"},
		{"owned by another tool (UA spoof)", "pi-1", true, "", ""},
		{"stamps the owning tool's session", "cc-1", true, "2.1.3", "2.1.3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			settled, err := k.StampSessionToolVersion(ctx, tc.session, models.ToolClaudeCode, "2.1.3")
			if err != nil {
				t.Fatal(err)
			}
			if settled != tc.wantSettled {
				t.Errorf("settled = %v, want %v", settled, tc.wantSettled)
			}
			if tc.session == "missing" {
				return
			}
			first, last, err := s.LoadSessionToolVersions(ctx, tc.session)
			if err != nil {
				t.Fatal(err)
			}
			if first != tc.wantFirst || last != tc.wantLast {
				t.Errorf("stored (first, last) = (%q, %q), want (%q, %q)", first, last, tc.wantFirst, tc.wantLast)
			}
		})
	}

	// An older build offered after the upgrade (a second client still on
	// the old CLI) never rewinds the latest.
	if _, err := k.StampSessionToolVersion(ctx, "cc-held", models.ToolClaudeCode, "1.5.0"); err != nil {
		t.Fatal(err)
	}
	if first, last, _ := s.LoadSessionToolVersions(ctx, "cc-held"); first != "1.0.0" || last != "2.1.3" {
		t.Errorf("after an older proxy stamp (first, last) = (%q, %q), want (1.0.0, 2.1.3)", first, last)
	}
}
