package browserhost

// remove.go is the undo of every write this package makes (modular
// packaging plan 6.3, lane MP-GATE; security ledger MODPKG-5): the
// per-browser dir-based manifests (Linux, macOS), the shared Windows
// manifest and .bat bridge, the per-browser HKCU registry keys, and the
// host launcher files under <home>/.observer/browser-host. Before it
// existed, a removed browser capture left the native-messaging host
// installed, so the extension could still deliver turns.
//
// Checksum rule, the same as the hook and MCP removers: only what Observer
// wrote is removed. A manifest whose name or description is not Observer's,
// a registry key pointing at a manifest Observer did not write, or a bridge
// that is not Observer's template is left in place and reported as needing
// attention, never force-removed. The browser extension itself is outside
// Observer's control and keeps running in the browser.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/browserhost/hostfiles"
)

// observerManifestDescriptionPrefix marks a manifest Observer wrote (both
// the dir-based and the Windows bridge descriptions start with it).
const observerManifestDescriptionPrefix = "SuperBased Observer browser-capture native-messaging host"

// RemoveResult reports one removal target.
type RemoveResult struct {
	// Target is a browser id, a file path or a registry key.
	Target string
	// Path is the file or key the result is about.
	Path string
	// Removed is true when it was (or, in a dry run, would be) removed.
	Removed bool
	// Absent is true when there was nothing to remove.
	Absent bool
	// NeedsAttention explains an entry left in place because it is not
	// Observer's (or no longer only Observer's).
	NeedsAttention string
	Error          error
}

// isObserverManifest reports whether body is a manifest Observer wrote.
func isObserverManifest(body []byte) bool {
	var m manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return false
	}
	return m.Name == HostName && strings.HasPrefix(m.Description, observerManifestDescriptionPrefix)
}

// Remove deletes the dir-based native-messaging manifest of every browser
// this registrar has a grounded directory for on its OS (installed or not:
// a browser uninstalled after registration can still hold the file), plus
// the host launcher files. Honors DryRun (reads, never writes).
func (r *Registrar) Remove() []RemoveResult {
	var out []RemoveResult
	for _, b := range browsers {
		dir, ok := b.nativeMessagingDir(r.home, r.goos)
		if !ok {
			continue
		}
		out = append(out, removeObserverFile(b.ID, filepath.Join(dir, manifestFileName), isObserverManifest, r.opts.DryRun))
	}
	hostDir, err := HostInstallDir(r.home)
	if err != nil {
		return append(out, RemoveResult{Target: "browser-host", Error: err})
	}
	out = append(out, RemoveHostFiles(hostDir, r.opts.DryRun)...)
	return out
}

// RemoveHostFiles deletes the host launcher files Observer installs into
// dir (hostfiles.Files) and then dir itself when it is empty. Any other
// file in dir is left alone.
func RemoveHostFiles(dir string, dryRun bool) []RemoveResult {
	var out []RemoveResult
	for _, name := range hostfiles.Files() {
		out = append(out, removeObserverFile("browser-host", filepath.Join(dir, name), func([]byte) bool { return true }, dryRun))
	}
	if !dryRun {
		if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
			_ = os.Remove(dir)
		}
	}
	return out
}

// removeObserverFile removes path when ours(body) holds. A symlink is never
// followed or removed: it is reported for attention.
func removeObserverFile(target, path string, ours func([]byte) bool, dryRun bool) RemoveResult {
	res := RemoveResult{Target: target, Path: path}
	fi, err := os.Lstat(path)
	if os.IsNotExist(err) {
		res.Absent = true
		return res
	}
	if err != nil {
		res.Error = fmt.Errorf("browserhost.Remove: stat %s: %w", path, err)
		return res
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.Mode().IsRegular() {
		res.NeedsAttention = "not a regular file; left in place"
		return res
	}
	body, err := os.ReadFile(path) //nolint:gosec // G304: path is a fixed name under a tool-derived directory.
	if err != nil {
		res.Error = fmt.Errorf("browserhost.Remove: read %s: %w", path, err)
		return res
	}
	if !ours(body) {
		res.NeedsAttention = "not written by Observer (or edited since); left in place"
		return res
	}
	if dryRun {
		res.Removed = true
		return res
	}
	if err := os.Remove(path); err != nil {
		res.Error = fmt.Errorf("browserhost.Remove: remove %s: %w", path, err)
		return res
	}
	res.Removed = true
	return res
}

// RegistryDeleter deletes a registry key. The production reg.exe writer
// implements it; a test fake implements it to observe deletions.
type RegistryDeleter interface {
	DeleteKey(keyPath string) error
}

// WindowsRemoveOptions configures RemoveWindows.
type WindowsRemoveOptions struct {
	// WindowsHome is the Windows user profile reachable from this process
	// (e.g. /mnt/c/Users/<user>). Required.
	WindowsHome string
	// Registry reads and deletes the HKCU keys. Required; it must also
	// implement RegistryDeleter. Tests inject a fake.
	Registry RegistryWriter
	// ToWin overrides the WSL to Windows path translation (tests).
	ToWin func(string) (string, bool)
	// DryRun previews without changing files or the registry. It still
	// READS each key (a query, never a write), so the preview reports what a
	// removal would really do: a key that does not exist is Absent and a key
	// pointing elsewhere needs attention. Before T-700 a dry run did no
	// registry read and reported every grounded key as removed, so the
	// components.leftovers doctor row on a WSL daemon claimed leftovers on a
	// machine that had none.
	DryRun bool
}

// RemoveWindows undoes WindowsRegistrar.Register: for every browser with a
// grounded Windows hive, the HKCU key is deleted when its (Default) points
// at Observer's manifest (a key pointing elsewhere is reported, never
// deleted); then the shared manifest and the .bat bridge are removed when
// they are Observer's.
func RemoveWindows(opts WindowsRemoveOptions) ([]RemoveResult, error) {
	if strings.TrimSpace(opts.WindowsHome) == "" {
		return nil, fmt.Errorf("browserhost.RemoveWindows: WindowsHome is required")
	}
	if opts.Registry == nil {
		return nil, fmt.Errorf("browserhost.RemoveWindows: Registry is required (inject a fake in tests)")
	}
	del, ok := opts.Registry.(RegistryDeleter)
	if !ok {
		return nil, fmt.Errorf("browserhost.RemoveWindows: the registry writer cannot delete keys")
	}
	toWin := opts.ToWin
	if toWin == nil {
		toWin = wslToWindowsPath
	}
	dir := filepath.Join(opts.WindowsHome, ".observer", hostInstallSubdir)
	manifestWSL := filepath.Join(dir, WindowsManifestName)
	bridgeWSL := filepath.Join(dir, WindowsBridgeName)
	manifestWin, _ := toWin(manifestWSL)

	var out []RemoveResult
	for _, b := range browsers {
		if b.winHive == "" {
			continue
		}
		key := browserRegistryKey(b)
		res := RemoveResult{Target: b.ID, Path: key}
		cur, exists, err := opts.Registry.GetDefault(key)
		switch {
		case err != nil:
			res.Error = fmt.Errorf("browserhost.RemoveWindows(%s): query: %w", b.ID, err)
		case !exists:
			res.Absent = true
		case manifestWin == "" || !strings.EqualFold(cur, manifestWin):
			res.NeedsAttention = "the key points at " + cur + ", not Observer's manifest; left in place"
		case opts.DryRun:
			// A preview reads, never deletes: this key is Observer's and a
			// removal would delete it.
			res.Removed = true
		default:
			if err := del.DeleteKey(key); err != nil {
				res.Error = fmt.Errorf("browserhost.RemoveWindows(%s): delete: %w", b.ID, err)
			} else {
				res.Removed = true
			}
		}
		out = append(out, res)
	}
	out = append(out, removeObserverFile("windows-manifest", manifestWSL, isObserverManifest, opts.DryRun))
	out = append(out, removeObserverFile("windows-bridge", bridgeWSL, isObserverBridge, opts.DryRun))
	if !opts.DryRun {
		if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
			_ = os.Remove(dir)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Target < out[j].Target })
	return out, nil
}

// isObserverBridge reports whether body is Observer's .bat bridge template.
func isObserverBridge(body []byte) bool {
	s := string(body)
	return strings.HasPrefix(s, "@echo off\r\n\"%SystemRoot%\\System32\\wsl.exe\" -d ") && strings.Count(s, "\r\n") == 2
}

// DeleteKey runs `reg.exe delete <key> /f`. A missing key is not an error.
func (w *regExeWriter) DeleteKey(keyPath string) error {
	if _, exists, _ := w.GetDefault(keyPath); !exists {
		return nil
	}
	cmd := exec.Command(w.exe, "delete", keyPath, "/f") //nolint:gosec // G204: keyPath is tool-derived (HKCU\<winHive>\...\<HostName>), explicit argv, no shell.
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("reg.exe delete %s: %w: %s", keyPath, err, strings.TrimSpace(string(out)))
	}
	return nil
}
