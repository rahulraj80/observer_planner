package crossmount

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestForeignSuppressedWith pins the ordered rule table behind
// ForeignHomesSuppressed: a staged layout decides alone, then the disable
// knob, then the test-binary guard.
func TestForeignSuppressedWith(t *testing.T) {
	t.Parallel()
	allow := &stagedForeignLayout{root: "/tmp/x", discover: true}
	deny := &stagedForeignLayout{root: "/tmp/x", discover: false}
	cases := []struct {
		name   string
		staged *stagedForeignLayout
		env    string
		guard  bool
		want   bool
	}{
		{"production, knob unset: discover", nil, "", false, false},
		{"production, knob set: suppressed", nil, "1", false, true},
		{"production, knob spelled TRUE: suppressed", nil, " TRUE ", false, true},
		{"production, knob falsy: discover", nil, "0", false, false},
		{"test binary, guard up: suppressed", nil, "", true, true},
		{"test binary, guard opened by the live hatch: discover", nil, "", false, false},
		{"staged deny wins over everything", deny, "", false, true},
		{"staged allow wins over the guard", allow, "", true, false},
		{"staged allow wins over the knob (its root is a temp dir)", allow, "1", true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := foreignSuppressedWith(c.staged, c.env, c.guard); got != c.want {
				t.Errorf("foreignSuppressedWith(%+v, %q, %v) = %v, want %v", c.staged, c.env, c.guard, got, c.want)
			}
		})
	}
}

// TestTestBinaryGuardIsUp proves the guard is actually raised in this test
// binary — the property every other package's tests now rely on without a
// TestMain of their own.
func TestTestBinaryGuardIsUp(t *testing.T) {
	if envTruthy(os.Getenv(AllowLiveForeignHomesInTestEnv)) {
		t.Skipf("%s is set on the command line; the guard is deliberately open", AllowLiveForeignHomesInTestEnv)
	}
	if !testBinaryGuard {
		t.Fatal("testBinaryGuard = false inside a test binary — foreign-home discovery is live for every test (incident 2026-10-02)")
	}
	if !ForeignHomesSuppressed() {
		t.Fatal("ForeignHomesSuppressed() = false inside a test binary with nothing staged")
	}
	resetExtrasCacheForTest()
	t.Cleanup(resetExtrasCacheForTest)
	for _, h := range AllHomes() {
		if h.Origin != "native" {
			t.Errorf("AllHomes() returned foreign home %+v inside a test binary", h)
		}
	}
	if got := WindowsUserName(); got != "" {
		t.Errorf("WindowsUserName() = %q inside a test binary, want \"\" (no interop probe)", got)
	}
}

// stageFakeForeignMount builds an on-disk stand-in for the real foreign mount
// under a temp dir: Users/<user>/{NTUSER.DAT,.claude/} on a Linux host,
// <distro>/home/<user>/.claude on a Windows host. It returns the root and
// the user-home path the default detector should report once discovery is
// allowed. Darwin has no foreign side, so callers skip there.
func stageFakeForeignMount(t *testing.T, user string) (root, home string) {
	t.Helper()
	base := t.TempDir()
	switch runtime.GOOS {
	case "linux":
		root = filepath.Join(base, "mnt", "c", "Users")
		home = filepath.Join(root, user)
		mustMkdir(t, filepath.Join(home, ".claude"))
		if err := os.WriteFile(filepath.Join(home, "NTUSER.DAT"), []byte("hive"), 0o600); err != nil {
			t.Fatal(err)
		}
	case "windows":
		root = filepath.Join(base, "wsl")
		home = filepath.Join(root, "FakeDistro", "home", user)
		mustMkdir(t, filepath.Join(home, ".claude"))
	default:
		t.Skipf("no foreign mount on %s", runtime.GOOS)
	}
	return root, home
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestDefaultDetectorGuardHoldsWithForeignHomePresent is the regression for
// incident 2026-10-02 at the source: a detectable foreign home sits at the
// root the default detector scans, and the test-binary guard alone keeps it
// out of AllHomes. The control row proves the staged layout IS found once
// discovery is allowed, so the guard — not a malformed fixture — is what
// blocks it. Not parallel: it stages process-wide state.
func TestDefaultDetectorGuardHoldsWithForeignHomePresent(t *testing.T) {
	for _, c := range []struct {
		name     string
		discover bool
		wantHome bool
	}{
		{"guarded (default in every test binary)", false, false},
		{"control: discovery allowed for the staged root only", true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			root, home := stageFakeForeignMount(t, "operator")
			StageForeignLayoutForTest(t, ForeignLayoutForTest{Root: root, WindowsUser: "operator", Discover: c.discover})
			found := false
			for _, h := range AllHomes() {
				if h.Origin == "native" {
					continue
				}
				if !PathUnder(root, h.Path) {
					t.Errorf("AllHomes() returned %q, outside the staged root %q", h.Path, root)
				}
				if h.Path == home {
					found = true
				}
			}
			if found != c.wantHome {
				t.Errorf("staged foreign home %q found = %v, want %v", home, found, c.wantHome)
			}
			if got := WindowsUserName(); got != "operator" {
				t.Errorf("WindowsUserName() = %q, want the staged %q", got, "operator")
			}
		})
	}
	// Cleanup restored the guard: nothing staged, nothing foreign.
	t.Run("after cleanup", func(t *testing.T) {
		if envTruthy(os.Getenv(AllowLiveForeignHomesInTestEnv)) {
			t.Skip("live hatch open")
		}
		if stagedForeign.Load() != nil {
			t.Fatal("staged layout leaked past its test")
		}
		for _, h := range AllHomes() {
			if h.Origin != "native" {
				t.Errorf("foreign home %+v visible after the staged layout was restored", h)
			}
		}
	})
}

// TestEnvDisableForeignHomesSuppressesDetector pins the production knob at the
// detector the default path builds: with it set, a present foreign layout is
// not enumerated.
func TestEnvDisableForeignHomesSuppressesDetector(t *testing.T) {
	t.Parallel()
	fs := &fakeFS{
		dirs:     map[string]bool{"/mnt/c/Users": true, "/mnt/c/Users/op": true, "/mnt/c/Users/op/AppData": true},
		listings: map[string][]string{"/mnt/c/Users": {"op"}},
	}
	mk := func(env string) *detector {
		return &detector{
			runtimeOS:       OSLinux,
			nativeHome:      func() (string, error) { return "/home/me", nil },
			statDir:         fs.statDir,
			readDir:         fs.readDir,
			exists:          fs.exists,
			getenv:          func(string) string { return "" },
			suppressForeign: foreignSuppressedWith(nil, env, false),
		}
	}
	if got := mk("").extraHomes(); len(got) != 1 {
		t.Fatalf("knob unset: extraHomes = %+v, want the one staged profile", got)
	}
	if got := mk("1").extraHomes(); len(got) != 0 {
		t.Fatalf("knob set: extraHomes = %+v, want none", got)
	}
}

// TestValidateStagedRoot pins that a staged layout can never name a real
// mount: Discover would otherwise be an escape hatch onto /mnt/c/Users.
func TestValidateStagedRoot(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	cases := []struct {
		name    string
		root    string
		wantErr bool
	}{
		{"temp subdir", filepath.Join(tmp, "mnt", "c", "Users"), false},
		{"empty", "", true},
		{"relative", "mnt/c/Users", true},
		{"real WSL mount", wslWindowsUsersRoot, true},
		{"dot-dot escape", filepath.Join(tmp, "..", "..", "mnt", "c", "Users"), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := validateStagedRoot(c.root, tmp)
			if (err != nil) != c.wantErr {
				t.Errorf("validateStagedRoot(%q) err = %v, wantErr %v", c.root, err, c.wantErr)
			}
		})
	}
}
