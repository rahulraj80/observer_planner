package aider

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestSymlinkedTranscriptIsNeverRead pins the SEC-SWEEP finding
// AIDER-SYMLINK-1: a repo can commit a symlink named .aider.chat.history.md
// that points at any file the user can read. Since T-706 the discovery walk
// reruns every two minutes, so a freshly cloned repo was picked up and the
// daemon read the link's target into rows a full-content node ships. The walk
// now lists regular files only, and ParseSessionFile refuses a non-regular
// path whichever discovery named it.
func TestSymlinkedTranscriptIsNeverRead(t *testing.T) {
	home := t.TempDir()
	secret := filepath.Join(home, "notes", "private.md")
	if err := os.MkdirAll(filepath.Dir(secret), 0o700); err != nil {
		t.Fatal(err)
	}
	// The target is a valid aider transcript, so a read WOULD yield rows.
	if err := os.WriteFile(secret, fixture(t, "readonly.md"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(home, "src", "cloned")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(repo, historyFileName)
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}

	found, _ := walkForAiderRoots(home, 1000)
	for _, p := range found {
		if p == link {
			t.Fatalf("the discovery walk listed the symlink %s", link)
		}
	}

	a := NewWithOptions(nil, []string{repo})
	res, err := a.ParseSessionFile(context.Background(), link, 0)
	if err != nil {
		t.Fatalf("ParseSessionFile: %v", err)
	}
	if len(res.ToolEvents) != 0 || len(res.TokenEvents) != 0 || res.NewOffset != 0 {
		t.Fatalf("a symlinked transcript was read: %d tool / %d token events, offset %d",
			len(res.ToolEvents), len(res.TokenEvents), res.NewOffset)
	}
}
