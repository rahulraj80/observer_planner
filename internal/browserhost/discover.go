package browserhost

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ExtensionCandidate is one unpacked SuperBased browser-capture extension a
// Chromium profile has loaded: the id `observer init` would otherwise ask the
// operator to copy out of chrome://extensions (D-571, lane PICKERS-2).
type ExtensionCandidate struct {
	ID      string
	Browser string
	Profile string
	Path    string
}

// extensionNameMarker is what the extension's manifest name carries
// (browser-extension/manifest.json "SuperBased Observer ... Browser Capture").
const extensionNameMarker = "SuperBased Observer"

// unpackedLocation is Chromium's extension location code for a
// load-unpacked extension (extensions::mojom::ManifestLocation kUnpacked).
const unpackedLocation = 4

// maxPrefsBytes bounds the profile preferences file read.
const maxPrefsBytes = 16 << 20

// DiscoverExtensionIDs lists the unpacked SuperBased extensions loaded in
// every grounded browser's profiles under home (goos picks the per-OS
// user-data dir; "windows" reads the Windows profile path). It reads only
// each profile's Preferences / Secure Preferences extensions.settings map and
// the matched extension's own manifest.json name; it never reads cookies,
// history, logins or any other profile file. Best-effort: an unreadable or
// absent profile is skipped. The result is sorted and deduplicated by id.
func DiscoverExtensionIDs(home, goos string) []ExtensionCandidate {
	seen := map[string]bool{}
	var out []ExtensionCandidate
	for _, b := range browsers {
		root, ok := userDataDir(b, home, goos)
		if !ok {
			continue
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || (e.Name() != "Default" && !strings.HasPrefix(e.Name(), "Profile ")) {
				continue
			}
			for _, file := range []string{"Secure Preferences", "Preferences"} {
				for _, c := range unpackedSuperBased(filepath.Join(root, e.Name(), file)) {
					if seen[c.ID] {
						continue
					}
					seen[c.ID] = true
					c.Browser, c.Profile = b.Name, e.Name()
					out = append(out, c)
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// userDataDir is a browser's user-data dir (the parent of its profiles).
func userDataDir(b Browser, home, goos string) (string, bool) {
	if goos == "windows" {
		if b.winProfile == "" {
			return "", false
		}
		return filepath.Join(home, filepath.FromSlash(b.winProfile), "User Data"), true
	}
	return b.profileDir(home, goos)
}

// unpackedSuperBased reads one preferences file's extensions.settings and
// returns the unpacked entries whose manifest names the SuperBased extension.
func unpackedSuperBased(prefsPath string) []ExtensionCandidate {
	f, err := os.Open(prefsPath)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var prefs struct {
		Extensions struct {
			Settings map[string]struct {
				Location int    `json:"location"`
				Path     string `json:"path"`
			} `json:"settings"`
		} `json:"extensions"`
	}
	if err := json.NewDecoder(io.LimitReader(f, maxPrefsBytes)).Decode(&prefs); err != nil {
		return nil
	}
	var out []ExtensionCandidate
	for id, s := range prefs.Extensions.Settings {
		if s.Location != unpackedLocation || s.Path == "" || !ValidExtensionID(id) {
			continue
		}
		if !isSuperBasedExtensionDir(s.Path) {
			continue
		}
		out = append(out, ExtensionCandidate{ID: id, Path: s.Path})
	}
	return out
}

// isSuperBasedExtensionDir reports whether dir's manifest.json names the
// SuperBased browser-capture extension.
func isSuperBasedExtensionDir(dir string) bool {
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil || len(raw) > 1<<20 {
		return false
	}
	var m struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return false
	}
	return strings.Contains(m.Name, extensionNameMarker)
}
