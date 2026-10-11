package store

import (
	"context"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/turnmerge"
)

// TestUpsertTurnByRequestID_NativeOTelKeepsItsTag: a native_otel turn that is
// merged again (an at-least-once redelivery carrying a later, larger figure)
// keeps its own source tag. Before T-475 a merged native-exact row was always
// rewritten as cc_otel, which would have relabelled every other tool's turn
// as Claude Code's.
func TestUpsertTurnByRequestID_NativeOTelKeepsItsTag(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	first := baseTurn("otel:codex:s1:1", SourceNativeOTel)
	first.InputTokens, first.OutputTokens = 200, 50
	if act, _, err := s.UpsertTurnByRequestID(ctx, first); err != nil || act != turnmerge.ActionInsert {
		t.Fatalf("insert: %v %v", act, err)
	}
	again := baseTurn("otel:codex:s1:1", SourceNativeOTel)
	again.InputTokens, again.OutputTokens = 234, 56
	if act, _, err := s.UpsertTurnByRequestID(ctx, again); err != nil || act != turnmerge.ActionUpdate {
		t.Fatalf("merge: %v %v", act, err)
	}
	count, row, source := readTurnByRequestID(t, s, "otel:codex:s1:1")
	if count != 1 || row.InputTokens != 234 || row.OutputTokens != 56 {
		t.Fatalf("merge result: count=%d row=%+v", count, row)
	}
	if source != SourceNativeOTel {
		t.Fatalf("merged native_otel row relabelled %q", source)
	}
	if got := fidelityForSource(SourceNativeOTel); got != turnmerge.FidelityNativeExact {
		t.Errorf("native_otel fidelity = %v, want native-exact", got)
	}
}

// TestEnsureCaptureSession: the native-export seam only ever INSERTS a
// session; a row the transcript already wrote keeps its project, tool and
// model.
func TestEnsureCaptureSession(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 6, 1, 2, 3, 0, time.UTC)

	created, err := s.EnsureCaptureSession(ctx, "sess-otel", "codex", "[codex]", "gpt-5.5", at)
	if err != nil || !created {
		t.Fatalf("first ensure: created=%v err=%v", created, err)
	}
	if created, err := s.EnsureCaptureSession(ctx, "sess-otel", "codex", "[codex]", "other-model", at); err != nil || created {
		t.Fatalf("second ensure re-created: created=%v err=%v", created, err)
	}

	// A transcript-owned session is never touched.
	pid, err := s.UpsertProject(ctx, "/work/real-repo", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertSession(ctx, models.Session{ID: "sess-real", ProjectID: pid, Tool: "codex", Model: "gpt-5.5", StartedAt: at}); err != nil {
		t.Fatal(err)
	}
	if created, err := s.EnsureCaptureSession(ctx, "sess-real", "codex", "[codex]", "x", at); err != nil || created {
		t.Fatalf("ensure on a transcript session: created=%v err=%v", created, err)
	}
	var root, model string
	if err := s.db.QueryRowContext(ctx, `SELECT p.root_path, s.model FROM sessions s JOIN projects p ON p.id = s.project_id WHERE s.id = 'sess-real'`).Scan(&root, &model); err != nil {
		t.Fatal(err)
	}
	if root != "/work/real-repo" || model != "gpt-5.5" {
		t.Errorf("transcript session changed: root=%q model=%q", root, model)
	}
	if _, err := s.EnsureCaptureSession(ctx, "", "codex", "[codex]", "", at); err == nil {
		t.Error("empty id accepted")
	}
}
