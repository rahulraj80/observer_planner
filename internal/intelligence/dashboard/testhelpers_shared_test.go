package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// testhelpers_shared_test.go holds small helpers that were defined in test
// files carrying a component omit tag but are used by untagged tests (lane
// MP-L1K, T-715), so those tests compile in every build.

// allowAllLaunchPolicy admits any handle: the launch-policy leg of §4.δ (the
// run was policy-checked at spawn; here we isolate the revoke behaviour).
type allowAllLaunchPolicy struct{}

func (allowAllLaunchPolicy) Allowed(string) bool { return true }

// readControlFrame reads through geometry/binary frames until the requested
// control message arrives. It returns the decoded wire frame for field-level
// assertions (notably the writer-lease generation contract).
func readControlFrame(t *testing.T, c *websocket.Conn, want string) wsControl {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		typ, data, err := c.Read(ctx)
		cancel()
		if err != nil {
			t.Fatalf("read %s control frame: %v", want, err)
		}
		if typ != websocket.MessageText {
			continue
		}
		var ctrl wsControl
		if json.Unmarshal(data, &ctrl) == nil && ctrl.T == want {
			return ctrl
		}
	}
	t.Fatalf("timed out waiting for %s control frame", want)
	return wsControl{}
}

// putTerminalSection builds a fresh dashboard over a temp config with the given
// [terminal.attach] body and PUTs the given JSON to the terminal section.
func putTerminalSection(t *testing.T, cfgBody, jsonBody string) *httptest.ResponseRecorder {
	t.Helper()
	tdir := t.TempDir()
	cfgPath := filepath.Join(tdir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(cfgBody), 0o644); err != nil {
		t.Fatal(err)
	}
	database, err := openTestDB(context.Background(), db.Options{Path: filepath.Join(tdir, "d.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	server, err := New(Options{DB: database, ConfigPath: cfgPath})
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr,
		httptest.NewRequest(http.MethodPut, "/api/config/section/terminal", strings.NewReader(jsonBody)))
	return rr
}

// newTagTestServer seeds three sessions across one project so the
// classification filters have something to discriminate. Each session carries a
// distinct token/cost profile so the per-tag rollup is checkable.
func newTagTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tags.db")
	database, err := openTestDB(context.Background(), db.Options{Path: path})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	st := store.New(database)
	root := t.TempDir()
	base := time.Now().UTC().Add(-time.Hour)
	var events []models.ToolEvent
	var tokens []models.TokenEvent
	for i, sid := range []string{"sess-a", "sess-b", "sess-c"} {
		ts := base.Add(time.Duration(i) * time.Minute)
		events = append(events, models.ToolEvent{
			SourceFile: "f.jsonl", SourceEventID: sid + "-e1", SessionID: sid,
			ProjectRoot: root, Timestamp: ts, Tool: models.ToolClaudeCode,
			Model: "claude-opus-4-7", ActionType: models.ActionReadFile,
			Target: "a.go", Success: true,
		})
		tokens = append(tokens, models.TokenEvent{
			SourceFile: "f.jsonl", SourceEventID: sid + "-t1", SessionID: sid,
			ProjectRoot: root, Timestamp: ts, Tool: models.ToolClaudeCode,
			Model: "claude-opus-4-7", InputTokens: int64(1000 * (i + 1)),
			OutputTokens: int64(100 * (i + 1)), Source: "jsonl", Reliability: "unreliable",
		})
	}
	if _, err := st.Ingest(context.Background(), events, tokens, store.IngestOptions{}); err != nil {
		t.Fatalf("seed Ingest: %v", err)
	}

	s, err := New(Options{DB: database, DBPath: path})
	if err != nil {
		t.Fatalf("dashboard.New: %v", err)
	}
	return s, st
}

func doJSON(t *testing.T, s *Server, method, path, body string) (int, map[string]any) {
	t.Helper()
	var rdr *strings.Reader
	if body == "" {
		rdr = strings.NewReader("")
	} else {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	out := map[string]any{}
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s %s: decode body %q: %v", method, path, rec.Body.String(), err)
		}
	}
	return rec.Code, out
}
