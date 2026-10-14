package planlimitslocal

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/planlimits"
)

func fakeReader(home, cfg string, env map[string]string) Reader {
	return Reader{
		Getenv:    func(k string) string { return env[k] },
		Home:      func() (string, error) { return home, nil },
		ConfigDir: func() (string, error) { return cfg, nil },
	}
}

func TestReadFileAndBases(t *testing.T) {
	home, cfg := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", ".credentials.json"), []byte(`{"x":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r := fakeReader(home, cfg, nil)
	src, _ := planlimits.SourceByID(planlimits.SourceClaudeOAuth)
	got, err := r.Read(context.Background(), src.Local)
	if err != nil || string(got["creds"]) != `{"x":1}` {
		t.Fatalf("read %q %v", got, err)
	}
	// CLAUDE_CONFIG_DIR and CODEX_HOME override the home default.
	alt := t.TempDir()
	r = fakeReader(home, cfg, map[string]string{"CLAUDE_CONFIG_DIR": alt, "CODEX_HOME": alt})
	if p, _ := r.Path(src.Local[0]); p != filepath.Join(alt, ".credentials.json") {
		t.Errorf("override %s", p)
	}
	wham, _ := planlimits.SourceByID(planlimits.SourceChatGPTWham)
	if _, err := r.Read(context.Background(), wham.Local); !errors.Is(err, planlimits.ErrUnavailable) {
		t.Errorf("a missing file is unavailable: %v", err)
	}
	if _, err := r.Path(planlimits.LocalSpec{Base: planlimits.BaseHome, Rel: "../../etc/passwd"}); err == nil {
		t.Error("a relative path must not leave its base")
	}
}

func TestReadSQLiteItemReadOnly(t *testing.T) {
	home, cfg := t.TempDir(), t.TempDir()
	dir := filepath.Join(cfg, "Windsurf", "User", "globalStorage")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "state.vscdb")
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE ItemTable (key TEXT UNIQUE ON CONFLICT REPLACE, value BLOB)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO ItemTable VALUES ('windsurf.settings.cachedPlanInfo', '{"planName":"Pro"}')`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	before, _ := os.Stat(p)

	src, _ := planlimits.SourceByID(planlimits.SourceWindsurfCache)
	got, err := fakeReader(home, cfg, nil).Read(context.Background(), src.Local)
	if err != nil || string(got["plan"]) != `{"planName":"Pro"}` {
		t.Fatalf("read %q %v", got, err)
	}
	after, _ := os.Stat(p)
	if !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() {
		t.Error("the vendor store must not be modified")
	}
	cur, _ := planlimits.SourceByID(planlimits.SourceCursorApp)
	if _, err := fakeReader(home, cfg, nil).Read(context.Background(), cur.Local); !errors.Is(err, planlimits.ErrUnavailable) {
		t.Errorf("no Cursor store: %v", err)
	}
	// A store without the key means "not signed in".
	missing := planlimits.LocalSpec{
		Key: "x", Kind: planlimits.LocalSQLiteKV, Base: planlimits.BaseConfig,
		Rel: "Windsurf/User/globalStorage/state.vscdb", Item: "nope",
	}
	if _, err := fakeReader(home, cfg, nil).Read(context.Background(), []planlimits.LocalSpec{missing}); !errors.Is(err, planlimits.ErrAuth) {
		t.Errorf("missing item: %v", err)
	}
}
