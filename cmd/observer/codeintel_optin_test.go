//go:build !sbo_omit_codeintel

package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/codeintel"
	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestBuildCodeIntelProviderOptIn pins D-307 at the one seam every consumer
// (marker enrichment, body-collapse, the MCP symbol tools) gets its provider
// from: the default config yields the DISABLED provider (unavailable, and
// recognisably off by choice), an explicit on yields the native engine.
func TestBuildCodeIntelProviderOptIn(t *testing.T) {
	ctx := context.Background()
	database, err := dbtemplate.Open(ctx, db.Options{Path: filepath.Join(t.TempDir(), "ci.db")})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	off := buildCodeIntelProvider(config.Default(), database, logger)
	if !codeintel.IsDisabled(off) {
		t.Error("default config: provider is not the disabled provider")
	}
	if off.Available() {
		t.Error("default config: provider reports Available() = true")
	}

	on := config.Default()
	on.CodeIntel.Enabled = true
	if p := buildCodeIntelProvider(on, database, logger); codeintel.IsDisabled(p) {
		t.Error("enabled config: provider reports disabled")
	}
	if p := buildCodeIntelProvider(on, nil, logger); codeintel.IsDisabled(p) || p.Available() {
		t.Error("enabled config with no database: want plain unavailable, not disabled")
	}
}

// TestCodeCollapseSettingsFailClosed: body-collapse compression is a no-op
// unless the module is on, whatever [codeintel.compression] says.
func TestCodeCollapseSettingsFailClosed(t *testing.T) {
	tests := []struct {
		name        string
		enabled     bool
		aggressive  bool
		langs       []string
		preview     bool
		wantEnabled bool
		wantPreview bool
		wantGoExt   bool
	}{
		{name: "default config", wantEnabled: false},
		{name: "module off, aggressive on", enabled: false, aggressive: true, langs: []string{"go"}, preview: true, wantEnabled: false},
		{name: "module on, aggressive off", enabled: true, aggressive: false, langs: []string{"go"}, wantEnabled: false},
		{name: "module on, aggressive on", enabled: true, aggressive: true, langs: []string{"go"}, preview: true, wantEnabled: true, wantPreview: true, wantGoExt: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			if tc.name != "default config" {
				cfg.CodeIntel.Enabled = tc.enabled
				cfg.CodeIntel.Compression.CodeAggressive = tc.aggressive
				cfg.CodeIntel.Compression.AggressiveLanguages = tc.langs
				cfg.CodeIntel.Compression.PreviewOnly = tc.preview
			}
			enabled, exts, preview := codeCollapseSettings(cfg)
			if enabled != tc.wantEnabled || preview != tc.wantPreview {
				t.Fatalf("codeCollapseSettings = (%v, _, %v), want (%v, _, %v)", enabled, preview, tc.wantEnabled, tc.wantPreview)
			}
			if !tc.wantEnabled && len(exts) != 0 {
				t.Errorf("exts = %v, want none when collapse is off", exts)
			}
			if tc.wantGoExt && !exts[".go"] {
				t.Errorf("exts = %v, want .go", exts)
			}
		})
	}
}

// TestCodeIntelOffUpgradeNote walks the note's decision table row by row.
func TestCodeIntelOffUpgradeNote(t *testing.T) {
	tests := []struct {
		name     string
		enabled  bool
		defined  bool
		projects int
		wantNote bool
	}{
		{name: "on", enabled: true, defined: false, projects: 3, wantNote: false},
		{name: "explicitly off", enabled: false, defined: true, projects: 3, wantNote: false},
		{name: "inherited off, never indexed (fresh install)", enabled: false, defined: false, projects: 0, wantNote: false},
		{name: "inherited off, index exists (upgrade)", enabled: false, defined: false, projects: 3, wantNote: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			note := codeIntelOffUpgradeNote(tc.enabled, tc.defined, tc.projects)
			if (note != "") != tc.wantNote {
				t.Fatalf("note = %q, wantNote %v", note, tc.wantNote)
			}
			if tc.wantNote {
				for _, want := range []string{"3 project(s)", "Settings > Intelligence > Code intelligence"} {
					if !strings.Contains(note, want) {
						t.Errorf("note %q missing %q", note, want)
					}
				}
				if strings.ContainsAny(note, "—–") {
					t.Errorf("note %q carries an em or en dash", note)
				}
			}
		})
	}
}

// TestRunCodeIntelOnStartOffPrintsUpgradeNote is the wiring proof: with the
// module off by default and an index left from before, `observer start`'s
// codeintel goroutine indexes nothing and prints the note; with an explicit
// `enabled = false` it stays silent.
func TestRunCodeIntelOnStartOffPrintsUpgradeNote(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		body     string
		wantNote bool
	}{
		{name: "never set", body: "", wantNote: true},
		{name: "explicitly off", body: "[codeintel]\nenabled = false\n", wantNote: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			dbPath := filepath.Join(dir, "observer.db")
			database, err := dbtemplate.Open(ctx, db.Options{Path: dbPath})
			if err != nil {
				t.Fatalf("db.Open: %v", err)
			}
			if _, err := database.ExecContext(ctx,
				`INSERT INTO codeintel_files(project, path, lang, status, indexed_at) VALUES('/repo/a', '/repo/a/x.go', 'go', 'indexed', 1)`); err != nil {
				t.Fatalf("seed: %v", err)
			}
			_ = database.Close()
			cfgPath := filepath.Join(dir, "config.toml")
			body := "[observer]\ndb_path = \"" + filepath.ToSlash(dbPath) + "\"\n" + tc.body
			if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			var out bytes.Buffer
			runCodeIntelOnStart(ctx, cfgPath, &out)
			got := strings.Contains(out.String(), "code intelligence is now off by default")
			if got != tc.wantNote {
				t.Fatalf("note printed = %v, want %v; output %q", got, tc.wantNote, out.String())
			}
			// Off means off: the index is untouched (no new project indexed).
			check, err := dbtemplate.Open(ctx, db.Options{Path: dbPath})
			if err != nil {
				t.Fatalf("reopen: %v", err)
			}
			defer check.Close()
			projects, err := store.New(check).CodeIntelListProjects(ctx)
			if err != nil || len(projects) != 1 {
				t.Fatalf("projects after start = %v, %v; want the one seeded", projects, err)
			}
		})
	}
}

// TestIndexCmdOffNamesTheDashboardControl: the explicit `observer index`
// path refuses honestly and points at the one-click control, not just a key.
func TestIndexCmdOffNamesTheDashboardControl(t *testing.T) {
	if !strings.Contains(codeIntelOffMessage, "Settings > Intelligence > Code intelligence") {
		t.Fatalf("codeIntelOffMessage = %q, want it to name the dashboard control", codeIntelOffMessage)
	}
}
