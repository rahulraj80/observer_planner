package cursor

import (
	"path/filepath"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/adapter"
)

// TestCursorSemanticsStoreDBZeroActionsPossible pins T-585: a chat
// store.db is a byte-offset file that CAN emit action rows (system prompt,
// prompt budget, todos) but whose zero-action state is a designed outcome
// (a store that only names the answering model), so the watcher-health
// misroute fingerprint must not count it. A transcript keeps the default
// fingerprint, and store.db is not CursorNoActions (it still emits rows).
func TestCursorSemanticsStoreDBZeroActionsPossible(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), ".cursor")
	a := NewWithOptions(nil, root)
	cases := []struct {
		name        string
		path        string
		wantKind    adapter.CursorKind
		wantActions bool
	}{
		{"chat store.db", filepath.Join(root, "chats", "ws1", "conv1", "store.db"), adapter.CursorByteOffset, false},
		{"agent transcript", filepath.Join(root, "projects", "slug", "agent-transcripts", "conv1", "conv1.jsonl"), adapter.CursorByteOffset, true},
	}
	for _, tc := range cases {
		if !a.IsSessionFile(tc.path) {
			t.Fatalf("%s: IsSessionFile(%s) = false; fixture path is wrong", tc.name, tc.path)
		}
		sem := a.CursorSemanticsFor(tc.path)
		if sem.Kind != tc.wantKind {
			t.Errorf("%s: Kind = %v, want %v", tc.name, sem.Kind, tc.wantKind)
		}
		if got := sem.ActionsExpected(); got != tc.wantActions {
			t.Errorf("%s: ActionsExpected() = %v, want %v (sem %+v)", tc.name, got, tc.wantActions, sem)
		}
		if !sem.ActionsExpected() && sem.Detail == "" {
			t.Errorf("%s: a suppressed fingerprint needs a Detail the dashboard can show", tc.name)
		}
	}
}
