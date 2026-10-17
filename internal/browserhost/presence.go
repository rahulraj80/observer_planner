package browserhost

import (
	"os"
	"path/filepath"

	"github.com/marmutapp/superbased-observer/internal/browserhost/hostfiles"
)

// PresenceOptions configures Present.
type PresenceOptions struct {
	// Home pins the home dir (tests); "" resolves os.UserHomeDir.
	Home string
	// GOOS overrides the OS of the dir-based manifests (tests).
	GOOS string
	// WindowsHomes are cross-mounted Windows profiles (a WSL daemon) whose
	// shared manifest and .bat bridge are checked. The HKCU keys are NOT
	// read: presence is a file check only, so the artifact report never
	// runs reg.exe.
	WindowsHomes []string
}

// Present lists the Observer-written native-host files on disk: the
// dir-based manifests whose content is Observer's, the host files in
// HostInstallDir, and the Windows shared manifest and bridge in each
// Windows home. An empty result means no host Observer installed is left
// (lane MP-ART: artifact.browser-extension's host_removed and no-host
// states). Unlike a teardown dry run it never reports a file it did not
// find.
func Present(opts PresenceOptions) []string {
	var out []string
	if reg, err := NewRegistrar(Options{Home: opts.Home, GOOS: opts.GOOS, DryRun: true}); err == nil {
		for _, m := range reg.Manifests() {
			if body, err := os.ReadFile(m.Path); err == nil && isObserverManifest(body) {
				out = append(out, m.Path)
			}
		}
	}
	if dir, err := HostInstallDir(opts.Home); err == nil {
		for _, name := range hostfiles.Files() {
			p := filepath.Join(dir, name)
			if fi, err := os.Lstat(p); err == nil && fi.Mode().IsRegular() {
				out = append(out, p)
			}
		}
	}
	for _, h := range opts.WindowsHomes {
		dir := filepath.Join(h, ".observer", hostInstallSubdir)
		for _, f := range []struct {
			name string
			ours func([]byte) bool
		}{{WindowsManifestName, isObserverManifest}, {WindowsBridgeName, isObserverBridge}} {
			p := filepath.Join(dir, f.name)
			if body, err := os.ReadFile(p); err == nil && f.ours(body) {
				out = append(out, p)
			}
		}
	}
	return out
}
