package hook

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLockSettingsFileExported: the exported lock is the registration lock
// (same sentinel file), and its releaser removes it.
func TestLockSettingsFileExported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	release, err := LockSettingsFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + settingsLockSuffix); err != nil {
		t.Fatalf("lock sentinel missing: %v", err)
	}
	release()
	if _, err := os.Stat(path + settingsLockSuffix); !os.IsNotExist(err) {
		t.Fatalf("releaser left the sentinel: %v", err)
	}
}
