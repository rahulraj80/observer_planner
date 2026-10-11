package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/codeintel"
	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/mcp/audit"
)

// D-307 (code intelligence off by default): with the DISABLED provider the
// tool surface stays deterministic (the symbol tools remain registered, the
// same registration semantics as an empty index), and every symbol tool
// answers with index_unavailable PLUS codeintel_disabled and the shared
// "turn it on in Settings" sentence. A merely-unavailable index never gets
// the disabled tag.

func newCodeIntelTestServer(t *testing.T, cg codeintel.Provider) (*Server, string) {
	t.Helper()
	tmp := t.TempDir()
	database, err := db.Open(context.Background(), db.Options{Path: filepath.Join(tmp, "obs.db")})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	auditW := audit.NewSQLWriter(database, slog.New(slog.NewTextHandler(io.Discard, nil)), audit.SQLWriterOptions{FlushInterval: 10 * time.Millisecond})
	t.Cleanup(func() { _ = auditW.Close() })
	root := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "x.ts"), []byte("export function anything() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{
		DB:                  database,
		ServerName:          "test",
		ServerVersion:       "0",
		CodeIntel:           cg,
		AuditWriter:         auditW,
		GetSymbolsEnabled:   true,
		GetSymbols:          defaultGetSymbolsOpts(),
		GetRelationsEnabled: true,
		GetRelations:        defaultGetRelationsOpts(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s, root
}

func resHasWarning(res map[string]any, tag string) bool {
	ws, _ := res["warnings"].([]any)
	for _, w := range ws {
		if w == tag {
			return true
		}
	}
	return false
}

func TestCodeIntelDisabled_ToolSurfaceStaysDeterministic(t *testing.T) {
	s, _ := newCodeIntelTestServer(t, codeintel.Disabled())
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, name := range []string{"search_symbols", "get_symbols", "get_relations"} {
		if _, ok := s.tools[name]; !ok {
			t.Errorf("%s not registered with code intelligence off; the tool surface must not depend on the setting", name)
		}
	}
}

func TestCodeIntelDisabled_SymbolToolsSayOffByChoice(t *testing.T) {
	tests := []struct {
		name         string
		cg           codeintel.Provider
		wantDisabled bool
	}{
		{name: "disabled", cg: codeintel.Disabled(), wantDisabled: true},
		{name: "unavailable", cg: codeintel.Unavailable(), wantDisabled: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, root := newCodeIntelTestServer(t, tc.cg)
			calls := []struct {
				tool       string
				args       map[string]any
				reasonPath func(map[string]any) string
			}{
				{
					tool: "search_symbols",
					args: map[string]any{"query": "anything", "project_root": root},
					reasonPath: func(r map[string]any) string {
						n, _ := r["note"].(string)
						return n
					},
				},
				{
					tool: "get_symbols",
					args: map[string]any{"project_root": root, "requests": []map[string]any{{"file": "x.ts", "name": "anything"}}},
					reasonPath: func(r map[string]any) string {
						n, _ := r["note"].(string)
						return n
					},
				},
				{
					tool: "get_relations",
					args: map[string]any{"project_root": root, "file": "x.ts", "name": "anything", "kind": "callees"},
					reasonPath: func(r map[string]any) string {
						n, _ := r["reason"].(string)
						return n
					},
				},
			}
			for _, c := range calls {
				res := callTool(t, s, c.tool, c.args)
				if deg, _ := res["degraded"].(bool); !deg {
					t.Errorf("%s: degraded = false, want true: %+v", c.tool, res)
				}
				if !resHasWarning(res, WarningIndexUnavailable) {
					t.Errorf("%s: index_unavailable missing (older agents rely on it): %v", c.tool, res["warnings"])
				}
				if got := resHasWarning(res, WarningCodeIntelDisabled); got != tc.wantDisabled {
					t.Errorf("%s: codeintel_disabled present = %v, want %v", c.tool, got, tc.wantDisabled)
				}
				explains := strings.Contains(c.reasonPath(res), "Settings > Intelligence > Code intelligence")
				if explains != tc.wantDisabled {
					t.Errorf("%s: explanation names the Settings control = %v, want %v (%q)", c.tool, explains, tc.wantDisabled, c.reasonPath(res))
				}
			}
		})
	}
}

// TestCodeIntelDisabled_SearchSymbolsDirect pins the typed result too, the
// way the existing search_symbols tests do.
func TestCodeIntelDisabled_SearchSymbolsDirect(t *testing.T) {
	tool := newSearchSymbolsTool(codeintel.Disabled())
	raw, _ := json.Marshal(map[string]any{"query": "anything"})
	res, err := tool.Invoke(context.Background(), raw)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	r := res.(searchSymbolsResult)
	if !r.Degraded || r.Note != codeintel.DisabledNote {
		t.Fatalf("result = %+v, want degraded with the disabled note", r)
	}
	if len(r.Warnings) != 2 || r.Warnings[0] != WarningIndexUnavailable || r.Warnings[1] != WarningCodeIntelDisabled {
		t.Errorf("warnings = %v, want [%s %s]", r.Warnings, WarningIndexUnavailable, WarningCodeIntelDisabled)
	}
}
