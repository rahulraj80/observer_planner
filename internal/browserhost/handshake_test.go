package browserhost

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/browserhost/hostfiles"
)

func TestParseHello(t *testing.T) {
	cases := []struct {
		body    string
		proto   int
		version string
	}{
		{`{"type":"config"}`, 1, ""},
		{`{"type":"config","protocol":2,"extension_version":"1.36.0"}`, 2, "1.36.0"},
		{`{"protocol":"2"}`, 1, ""},
		{`{"protocol":0}`, 1, ""},
		{`{"protocol":-3}`, 1, ""},
		{`{"protocol":9,"extension_version":"1.40.0"}`, 9, "1.40.0"},
		{`{"protocol":2,"extension_version":"1.36.0; rm -rf"}`, 2, ""},
		{`{"protocol":2,"extension_version":"` + strings.Repeat("1", 80) + `"}`, 2, ""},
		{`{"protocol":2,"extension_version":42}`, 2, ""},
		{`not json`, 1, ""},
	}
	for _, tc := range cases {
		h := ParseHello([]byte(tc.body))
		if h.Protocol != tc.proto || h.ExtensionVersion != tc.version {
			t.Errorf("%s: got %+v, want protocol %d version %q", tc.body, h, tc.proto, tc.version)
		}
	}
}

func TestRefusal(t *testing.T) {
	for p := ProtocolMin; p <= ProtocolMax; p++ {
		if r := Refusal(p); r != "" {
			t.Errorf("protocol %d in range refused: %s", p, r)
		}
	}
	if r := Refusal(ProtocolMax + 1); !strings.Contains(r, "update observer") {
		t.Errorf("newer protocol: %q", r)
	}
	if r := Refusal(ProtocolMin - 1); !strings.Contains(r, "update the extension") {
		t.Errorf("older protocol: %q", r)
	}
	if ProtocolCurrent < ProtocolMin || ProtocolCurrent > ProtocolMax {
		t.Errorf("this release's extension protocol %d is outside the host range", ProtocolCurrent)
	}
}

func TestRecordSeenKeepsFirstSeenAndWrites0600(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "obs")
	if s, err := LoadSeen(dir); s != nil || err != nil {
		t.Fatalf("missing record: %+v %v", s, err)
	}
	if err := RecordSeen(dir, Hello{Protocol: 1}, 1000); err != nil {
		t.Fatal(err)
	}
	if err := RecordSeen(dir, Hello{Protocol: 3, ExtensionVersion: "1.40.0"}, 5000); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSeen(dir)
	if err != nil || s == nil {
		t.Fatalf("load: %+v %v", s, err)
	}
	if s.FirstSeenMs != 1000 || s.LastSeenMs != 5000 || s.Protocol != 3 || s.Version != "1.40.0" || !strings.Contains(s.Refused, "newer") {
		t.Fatalf("record: %+v", s)
	}
	fi, err := os.Stat(filepath.Join(dir, SeenFileName))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", fi.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temp files left behind: %v", entries)
	}
	if err := os.WriteFile(filepath.Join(dir, SeenFileName), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSeen(dir); err == nil {
		t.Error("a corrupt record must be an error, not a silent empty")
	}
	if err := RecordSeen(dir, Hello{Protocol: 2}, 9000); err != nil {
		t.Fatalf("a corrupt record must not block the next handshake: %v", err)
	}
}

// TestServiceWorkerProtocolTwin pins the extension's protocol constant to
// ProtocolCurrent: the two ship in the same release, so they must agree.
func TestServiceWorkerProtocolTwin(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "browser-extension", "src", "service-worker.js"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^const SBO_PROTOCOL = (\d+);`).FindSubmatch(raw)
	if m == nil {
		t.Fatal("service-worker.js has no `const SBO_PROTOCOL = <n>;` line")
	}
	if n, _ := strconv.Atoi(string(m[1])); n != ProtocolCurrent {
		t.Fatalf("service-worker.js SBO_PROTOCOL = %d, browserhost.ProtocolCurrent = %d", n, ProtocolCurrent)
	}
}

func TestPresentFindsOnlyObserverFiles(t *testing.T) {
	home := t.TempDir()
	if got := Present(PresenceOptions{Home: home, GOOS: "linux"}); len(got) != 0 {
		t.Fatalf("empty home: %v", got)
	}
	if err := os.MkdirAll(filepath.Join(home, ".config/google-chrome"), 0o755); err != nil {
		t.Fatal(err)
	}
	hostDir, _ := HostInstallDir(home)
	if _, err := hostfiles.WriteHost(hostDir, hostfiles.Env{ObserverBin: "/usr/bin/observer"}); err != nil {
		t.Fatal(err)
	}
	reg, err := NewRegistrar(Options{Home: home, GOOS: "linux", HostPath: hostfiles.LauncherPath(hostDir)})
	if err != nil {
		t.Fatal(err)
	}
	reg.Register()
	got := Present(PresenceOptions{Home: home, GOOS: "linux"})
	if len(got) != len(hostfiles.Files())+1 {
		t.Fatalf("want the manifest and %d host files, got %v", len(hostfiles.Files()), got)
	}
	// After the teardown nothing Observer wrote is present.
	reg.Remove()
	RemoveHostFiles(hostDir, false)
	if got := Present(PresenceOptions{Home: home, GOOS: "linux"}); len(got) != 0 {
		t.Fatalf("after removal: %v", got)
	}
	// A foreign manifest is not Observer's host.
	nm := filepath.Join(home, ".config/google-chrome/NativeMessagingHosts")
	if err := os.MkdirAll(nm, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nm, manifestFileName), []byte(`{"name":"`+HostName+`","description":"mine"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// A Windows home with no files, and one with a foreign bridge.
	win := t.TempDir()
	wdir := filepath.Join(win, ".observer", hostInstallSubdir)
	if err := os.MkdirAll(wdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wdir, WindowsBridgeName), []byte("echo hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Present(PresenceOptions{Home: home, GOOS: "linux", WindowsHomes: []string{t.TempDir(), win}}); len(got) != 0 {
		t.Fatalf("foreign files counted as Observer's host: %v", got)
	}
}
