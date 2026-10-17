package browserhost

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestDiscoverExtensionIDs pins the unpacked-extension finder (D-571): only
// a load-unpacked entry (location 4) whose manifest names the SuperBased
// extension is offered; a store install, another unpacked extension and an
// invalid id are not; the same id in two files is listed once.
func TestDiscoverExtensionIDs(t *testing.T) {
	home := t.TempDir()
	ours := filepath.Join(home, "src", "browser-extension")
	other := filepath.Join(home, "src", "other-ext")
	write := func(path string, v any) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(v)
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(ours, "manifest.json"), map[string]string{"name": "SuperBased Observer Browser Capture"})
	write(filepath.Join(other, "manifest.json"), map[string]string{"name": "Something Else"})
	const good = "abcdefghijklmnopabcdefghijklmnop"
	const store = "ponmlkjihgfedcbaponmlkjihgfedcba"
	const otherID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	settings := map[string]any{
		"extensions": map[string]any{"settings": map[string]any{
			good:     map[string]any{"location": 4, "path": ours},
			store:    map[string]any{"location": 1, "path": ours},
			otherID:  map[string]any{"location": 4, "path": other},
			"not-id": map[string]any{"location": 4, "path": ours},
		}},
	}
	profile := filepath.Join(home, ".config", "google-chrome", "Default")
	write(filepath.Join(profile, "Secure Preferences"), settings)
	write(filepath.Join(profile, "Preferences"), settings)
	write(filepath.Join(home, ".config", "google-chrome", "System Profile", "Preferences"), settings)

	got := DiscoverExtensionIDs(home, "linux")
	if len(got) != 1 {
		t.Fatalf("got %+v, want exactly the one unpacked SuperBased extension", got)
	}
	if got[0].ID != good || got[0].Browser != "Google Chrome" || got[0].Profile != "Default" {
		t.Fatalf("got %+v", got[0])
	}
	if none := DiscoverExtensionIDs(t.TempDir(), "linux"); len(none) != 0 {
		t.Fatalf("empty home found %+v", none)
	}
}
