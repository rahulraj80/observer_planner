package qoder

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestToolVersionEmitted pins Issue 2 (migration 125) for qoder: the
// CC-envelope `version` on transcript records (1.0.40 in the fixture)
// becomes exactly one SessionToolVersion for the session, and a segment
// run-log (which carries no version) stamps nothing.
func TestToolVersionEmitted(t *testing.T) {
	src := filepath.Join("..", "..", "..", "testdata", "qoder", "tool-call-session.jsonl")
	body, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	root := t.TempDir()
	projects := filepath.Join(root, ".qoder", "projects")
	sessions := filepath.Join(root, ".qoder", "logs", "sessions")
	dst := filepath.Join(projects, "-home-dev-proj", "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, body, 0o644); err != nil {
		t.Fatal(err)
	}
	a := newTestAdapter(projects, sessions)
	res, err := a.ParseSessionFile(context.Background(), dst, 0)
	if err != nil {
		t.Fatalf("ParseSessionFile: %v", err)
	}
	if len(res.SessionToolVersions) != 1 {
		t.Fatalf("SessionToolVersions = %+v, want exactly one", res.SessionToolVersions)
	}
	got := res.SessionToolVersions[0]
	if got.SessionID != "5d51bc6a-0000-0000-0000-000000000001" || got.Version != "1.0.40" {
		t.Errorf("stamp = %+v, want {5d51bc6a-0000-0000-0000-000000000001 1.0.40}", got)
	}

	segBody, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "qoder", "segments-tokens.jsonl"))
	if err != nil {
		t.Fatalf("read segment fixture: %v", err)
	}
	seg := filepath.Join(sessions, "-home-dev-proj", "sid-seg", "segments", "seg.jsonl")
	if err := os.MkdirAll(filepath.Dir(seg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(seg, segBody, 0o644); err != nil {
		t.Fatal(err)
	}
	segRes, err := a.ParseSessionFile(context.Background(), seg, 0)
	if err != nil {
		t.Fatalf("ParseSessionFile(segment): %v", err)
	}
	if len(segRes.SessionToolVersions) != 0 {
		t.Errorf("segment SessionToolVersions = %+v, want none", segRes.SessionToolVersions)
	}
}
