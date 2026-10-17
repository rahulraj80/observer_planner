package diag

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/config"
)

// TestCheckDaemonVersion is U3's doctor check: a running daemon whose
// version differs from the CLI's (or predates version reporting) warns and
// names the restart; a matching one is ok.
func TestCheckDaemonVersion(t *testing.T) {
	alive := os.Getppid() // the test runner's parent: alive for the test
	cases := []struct {
		name, daemon, cli string
		status            Status
		mention           string
	}{
		{"older build without a version", "", "1.36.0", StatusWarn, "older build"},
		{"another release", "1.34.0", "1.36.0", StatusWarn, "v1.34.0"},
		{"same release", "1.36.0", "1.36.0", StatusOK, "same as this CLI"},
		{"dev CLI skips", "1.34.0", "dev", StatusOK, "development build"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if _, err := WriteLock(dir, LockInfo{PID: alive, DBPath: filepath.Join(dir, "observer.db"), Version: c.daemon}); err != nil {
				t.Fatal(err)
			}
			var cfg config.Config
			cfg.Observer.DBPath = filepath.Join(dir, "observer.db")
			got := checkDaemonVersion(cfg, c.cli)
			if got.Status != c.status || !strings.Contains(got.Message, c.mention) {
				t.Fatalf("checkDaemonVersion = %s %q, want %s mentioning %q", got.Status, got.Message, c.status, c.mention)
			}
		})
	}
	var cfg config.Config
	cfg.Observer.DBPath = filepath.Join(t.TempDir(), "observer.db")
	if got := checkDaemonVersion(cfg, "1.36.0"); got.Status != StatusOK {
		t.Fatalf("no daemon: %s %q", got.Status, got.Message)
	}
}
