package invariant

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
)

// TestArtifactVersionLockstep pins every separately shipped artifact that
// has a version file of its own (internal/components.Artifacts, VersionFile)
// to the observer release (lane MP-ART, T-601):
//
//   - the release stamp, scripts/sync-npm-version.sh, names the file, so a
//     release can never ship it with a stale version;
//   - the committed file agrees with npm/observer/package.json, the
//     version every channel stamps from: the full string for an editor
//     extension (vsce carries it verbatim), the numeric core for a browser
//     extension (MV3 forbids a pre-release suffix, so 1.36.0-rc.2 stamps
//     1.36.0).
//
// A new artifact row with a version file joins this check by setting
// VersionFile; nothing here names an artifact.
func TestArtifactVersionLockstep(t *testing.T) {
	root := repoRoot(t)
	release := readJSONVersion(t, filepath.Join(root, "npm", "observer", "package.json"))
	stamp, err := os.ReadFile(filepath.Join(root, "scripts", "sync-npm-version.sh"))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, a := range components.Artifacts() {
		if a.VersionFile == "" {
			continue
		}
		checked++
		if !strings.Contains(string(stamp), a.VersionFile) {
			t.Errorf("%s: scripts/sync-npm-version.sh does not stamp %s", a.ID, a.VersionFile)
		}
		got := readJSONVersion(t, filepath.Join(root, filepath.FromSlash(a.VersionFile)))
		want := release
		if a.Kind == components.ArtifactBrowserExtension {
			want = numericCore(release)
		}
		if got != want {
			t.Errorf("%s: %s version %q, want %q (release %s)", a.ID, a.VersionFile, got, want, release)
		}
	}
	if checked == 0 {
		t.Fatal("no artifact declares a version file; the lockstep check would pass vacuously")
	}
}

func readJSONVersion(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if v.Version == "" {
		t.Fatalf("%s has no version", path)
	}
	return v.Version
}

// numericCore strips a SemVer pre-release or build suffix, exactly as the
// stamp script's ${VERSION%%[-+]*} does.
func numericCore(v string) string {
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		return v[:i]
	}
	return v
}
