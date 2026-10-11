package browserhost

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/browserhost/hostfiles"
)

// deletingRegistry is fakeRegistry plus RegistryDeleter.
type deletingRegistry struct {
	*fakeRegistry
	deleted []string
}

func (d *deletingRegistry) DeleteKey(keyPath string) error {
	delete(d.values, keyPath)
	d.deleted = append(d.deleted, keyPath)
	return nil
}

func resultFor(rs []RemoveResult, target string) (RemoveResult, bool) {
	for _, r := range rs {
		if r.Target == target {
			return r, true
		}
	}
	return RemoveResult{}, false
}

// TestRemoveUndoesRegisterAndKeepsForeignFiles is MODPKG-5: the host
// Observer installed is removed, a manifest it did not write is reported and
// kept, and a dry run removes nothing.
func TestRemoveUndoesRegisterAndKeepsForeignFiles(t *testing.T) {
	home := t.TempDir()
	for _, rel := range []string{".config/google-chrome", ".config/microsoft-edge"} {
		if err := os.MkdirAll(filepath.Join(home, rel), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	hostDir, _ := HostInstallDir(home)
	if _, err := hostfiles.WriteHost(hostDir, hostfiles.Env{ObserverBin: "/usr/bin/observer"}); err != nil {
		t.Fatal(err)
	}
	reg, err := NewRegistrar(Options{Home: home, GOOS: "linux", HostPath: hostfiles.LauncherPath(hostDir)})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range reg.Register() {
		if r.Error != nil || !r.Wrote {
			t.Fatalf("register %s: %+v", r.Browser, r)
		}
	}
	// The operator replaced Edge's manifest with their own.
	edge := filepath.Join(home, ".config/microsoft-edge/NativeMessagingHosts", manifestFileName)
	if err := os.WriteFile(edge, []byte(`{"name":"`+HostName+`","description":"mine","path":"/x","type":"stdio","allowed_origins":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	dry, _ := NewRegistrar(Options{Home: home, GOOS: "linux", DryRun: true})
	for _, r := range dry.Remove() {
		if r.Removed {
			if _, err := os.Stat(r.Path); err != nil {
				t.Fatalf("dry run removed %s", r.Path)
			}
		}
	}

	rm, _ := NewRegistrar(Options{Home: home, GOOS: "linux"})
	got := rm.Remove()
	chrome, _ := resultFor(got, "chrome")
	if !chrome.Removed {
		t.Fatalf("chrome manifest not removed: %+v", chrome)
	}
	if _, err := os.Stat(chrome.Path); !os.IsNotExist(err) {
		t.Fatal("chrome manifest still on disk")
	}
	edgeRes, _ := resultFor(got, "edge")
	if edgeRes.Removed || edgeRes.NeedsAttention == "" {
		t.Fatalf("a foreign manifest must be kept and reported: %+v", edgeRes)
	}
	if _, err := os.Stat(edge); err != nil {
		t.Fatal("foreign manifest removed")
	}
	if brave, _ := resultFor(got, "brave"); !brave.Absent {
		t.Fatalf("brave had nothing to remove: %+v", brave)
	}
	if _, err := os.Stat(hostDir); !os.IsNotExist(err) {
		t.Fatal("host dir with only Observer's files was not removed")
	}
	// Idempotent.
	for _, r := range rm.Remove() {
		if r.Removed || r.Error != nil {
			t.Fatalf("second removal changed something: %+v", r)
		}
	}
}

// TestRemoveWindowsDeletesOnlyObserversKeys covers the registry half: a key
// pointing at Observer's manifest is deleted, a key pointing elsewhere is
// kept and reported, and a dry run reads but never writes the registry.
func TestRemoveWindowsDeletesOnlyObserversKeys(t *testing.T) {
	winHome := t.TempDir()
	mkWinProfile(t, winHome, "chrome")
	mkWinProfile(t, winHome, "edge")
	fake := &deletingRegistry{fakeRegistry: newFakeRegistry()}
	r := newTestWindowsRegistrar(t, winOpts(winHome, fake))
	if res := r.Register(); res.Error != nil {
		t.Fatal(res.Error)
	}
	var edge Browser
	for _, b := range browsers {
		if b.ID == "edge" {
			edge = b
		}
	}
	fake.values[browserRegistryKey(edge)] = `D:\someone-else\host.json`

	before := len(fake.sets)
	dry, err := RemoveWindows(WindowsRemoveOptions{WindowsHome: winHome, Registry: fake, ToWin: fakeToWin, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.deleted) != 0 || len(fake.sets) != before {
		t.Fatal("a dry run touched the registry")
	}
	if len(dry) == 0 {
		t.Fatal("dry run reported nothing")
	}
	// The preview reads the keys, so it reports what a removal would do.
	if c, _ := resultFor(dry, "chrome"); !c.Removed {
		t.Fatalf("dry run: Observer's chrome key would be removed: %+v", c)
	}
	if e, _ := resultFor(dry, "edge"); e.Removed || e.NeedsAttention == "" {
		t.Fatalf("dry run: a foreign key is reported, not removed: %+v", e)
	}
	if _, err := os.Stat(filepath.Join(winHome, ".observer", hostInstallSubdir, WindowsManifestName)); err != nil {
		t.Fatal("a dry run removed the Windows manifest")
	}

	got, err := RemoveWindows(WindowsRemoveOptions{WindowsHome: winHome, Registry: fake, ToWin: fakeToWin})
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := resultFor(got, "chrome"); !c.Removed {
		t.Fatalf("chrome key not removed: %+v", c)
	}
	if e, _ := resultFor(got, "edge"); e.Removed || e.NeedsAttention == "" {
		t.Fatalf("a foreign key must be kept and reported: %+v", e)
	}
	if _, ok := fake.values[browserRegistryKey(edge)]; !ok {
		t.Fatal("foreign key deleted")
	}
	for _, target := range []string{"windows-manifest", "windows-bridge"} {
		if x, _ := resultFor(got, target); !x.Removed {
			t.Fatalf("%s not removed: %+v", target, x)
		}
	}
	if _, err := RemoveWindows(WindowsRemoveOptions{WindowsHome: winHome, Registry: newFakeRegistry()}); err == nil {
		t.Fatal("a registry that cannot delete must be refused")
	}
}

// TestRemoveWindowsDryRunReportsNoLeftoversOnACleanMachine is T-700: on a
// Windows profile where nothing was ever registered (no keys, no manifest,
// no bridge) a dry run reports nothing removable. Before the fix the dry run
// did no registry read and claimed every grounded HKCU key removable, so the
// components.leftovers doctor row on a WSL daemon always warned.
func TestRemoveWindowsDryRunReportsNoLeftoversOnACleanMachine(t *testing.T) {
	winHome := t.TempDir()
	mkWinProfile(t, winHome, "chrome")
	mkWinProfile(t, winHome, "edge")
	fake := &deletingRegistry{fakeRegistry: newFakeRegistry()}
	dry, err := RemoveWindows(WindowsRemoveOptions{WindowsHome: winHome, Registry: fake, ToWin: fakeToWin, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(dry) == 0 {
		t.Fatal("dry run reported nothing at all")
	}
	for _, r := range dry {
		if r.Removed || r.NeedsAttention != "" || r.Error != nil {
			t.Fatalf("clean machine: %s reported as a leftover: %+v", r.Target, r)
		}
		if !r.Absent {
			t.Fatalf("clean machine: %s should be absent: %+v", r.Target, r)
		}
	}
	if len(fake.deleted) != 0 || len(fake.sets) != 0 {
		t.Fatal("a dry run wrote to the registry")
	}
}
