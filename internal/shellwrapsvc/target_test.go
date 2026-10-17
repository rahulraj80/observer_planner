package shellwrapsvc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/shellwrap"
)

// winFixture is a throwaway WSL-bridge world: a Linux home and a fake drive
// mount whose c/Users/u stands in for the Windows home. Nothing outside
// t.TempDir() is read or written; no wsl.exe or reg.exe ever runs.
type winFixture struct {
	t         *testing.T
	root      string
	home      string // the Linux home (config.toml lives here)
	winLocal  string // <root>/mnt/c/Users/u
	profile   string // the pwsh profile, local path
	svc       Service
	obs       string
	shellFold func() (string, error)
}

func newWinFixture(t *testing.T) *winFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("exercises the WSL side of the bridge")
	}
	root := t.TempDir()
	f := &winFixture{t: t, root: root, home: filepath.Join(root, "home")}
	f.winLocal = filepath.Join(root, "mnt", "c", "Users", "u")
	f.profile = filepath.Join(f.winLocal, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1")
	f.obs = filepath.Join(f.home, "bin", "observer")
	writeFile(t, f.obs, "#!/bin/sh\n", 0o755)
	f.shellFold = func() (string, error) { return "", errors.New("no registry here") }
	f.svc = Service{
		Home:               f.home,
		GOOS:               "linux",
		Getenv:             func(string) string { return "" },
		ObserverPath:       f.obs,
		WindowsHome:        f.winLocal,
		WindowsMountRoot:   filepath.Join(root, "mnt"),
		WSLDistro:          "Ubuntu-20.04",
		WindowsShellFolder: func() (string, error) { return f.shellFold() },
	}
	return f
}

func (f *winFixture) apply(tools ...string) Outcome {
	f.t.Helper()
	out, err := f.svc.Apply(context.Background(), Request{Target: TargetWindows, Tools: tools})
	if err != nil {
		f.t.Fatalf("Apply(windows): %v", err)
	}
	return out
}

func TestWindowsTargetEnableDisableRoundTrip(t *testing.T) {
	f := newWinFixture(t)
	orig := "Set-Alias g git\r\n# mine" // CRLF, no trailing newline
	writeFile(t, f.profile, orig, 0o644)
	cfgPath := filepath.Join(f.home, ".observer", "config.toml")
	cfgOrig := "# kept\n[shell_wrap]\nenabled = false\n"
	writeFile(t, cfgPath, cfgOrig, 0o600)

	out := f.apply("claude-code", "muse")
	if len(out.Plan.Shims) != 2 {
		t.Fatalf("plan %+v", out.Plan)
	}
	shimDir := filepath.Join(f.winLocal, ".observer", "shims")
	claude := read(t, filepath.Join(shimDir, "claude.cmd"))
	if !strings.Contains(claude, `-d %SBO_DISTRO% -e "%SBO_OBSERVER%" shell-wrap launch-env claude-code`) ||
		!strings.Contains(claude, `set "SBO_OBSERVER=`+f.obs+`"`) {
		t.Fatalf("claude.cmd is not a bridge shim at the running observer:\n%s", claude)
	}
	if muse := read(t, filepath.Join(shimDir, "muse.cmd")); !strings.Contains(muse, `muse -- %*`) {
		t.Fatalf("muse.cmd:\n%s", muse)
	}
	prof := read(t, f.profile)
	if !strings.HasPrefix(prof, orig+"\r\n"+shellwrap.BlockBegin) {
		t.Fatalf("profile: %q", prof)
	}
	if !strings.Contains(prof, `$sboShimDir = 'C:\Users\u\.observer\shims'`+"\r\n") {
		t.Fatalf("profile block must name the WINDOWS shim dir with the file's CRLF: %q", prof)
	}
	cfg, err := config.Load(config.LoadOptions{GlobalPath: cfgPath})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ShellWrap.Windows.Enabled || !cfg.ShellWrap.Windows.Tools["claude-code"] || !cfg.ShellWrap.Windows.Tools["muse"] {
		t.Fatalf("[shell_wrap.windows] not recorded: %+v", cfg.ShellWrap.Windows)
	}
	if cfg.ShellWrap.Enabled || len(cfg.ShellWrap.Tools) != 0 {
		t.Fatalf("the native target's choice changed: %+v", cfg.ShellWrap)
	}
	if !strings.HasPrefix(read(t, cfgPath), "# kept\n") {
		t.Fatal("config comment lost")
	}

	st, err := f.svc.StatusFor(context.Background(), TargetWindows)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Enabled || !st.InSync || st.Bridge == nil || st.Bridge.Distro != "Ubuntu-20.04" || st.ShimDir != `C:\Users\u\.observer\shims` {
		t.Fatalf("status %+v", st)
	}
	// The native target knows nothing about the Windows shims.
	if nst, err := f.svc.Status(context.Background()); err != nil || nst.Active || nst.Enabled {
		t.Fatalf("native status %+v %v", nst, err)
	}

	if _, err := f.svc.DisableFor(context.Background(), TargetWindows, false); err != nil {
		t.Fatal(err)
	}
	if got := read(t, f.profile); got != orig {
		t.Fatalf("profile not restored byte-for-byte: %q", got)
	}
	if _, err := os.Stat(shimDir); !os.IsNotExist(err) {
		t.Fatalf("shim dir left behind: %v", err)
	}
	cfg, _ = config.Load(config.LoadOptions{GlobalPath: cfgPath})
	if cfg.ShellWrap.Windows.Enabled || !cfg.ShellWrap.Windows.Tools["claude-code"] {
		t.Fatalf("disable must record enabled=false and keep the choice: %+v", cfg.ShellWrap.Windows)
	}
}

func TestWindowsTargetDryRunWritesNothing(t *testing.T) {
	f := newWinFixture(t)
	writeFile(t, f.profile, "# p\n", 0o644)
	out, err := f.svc.Apply(context.Background(), Request{Target: TargetWindows, Tools: []string{"codex"}, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !out.DryRun || len(out.Changes) == 0 {
		t.Fatalf("dry run %+v", out)
	}
	for _, c := range out.Changes {
		if c.Kind == "shim" && !strings.HasPrefix(c.Path, `C:\Users\u\`) {
			t.Errorf("preview paths are in Windows notation: %s", c.Path)
		}
	}
	if read(t, f.profile) != "# p\n" {
		t.Fatal("dry run wrote the profile")
	}
	if _, err := os.Stat(filepath.Join(f.winLocal, ".observer")); !os.IsNotExist(err) {
		t.Fatal("dry run created the shim dir")
	}
	if _, err := os.Stat(filepath.Join(f.home, ".observer", "config.toml")); !os.IsNotExist(err) {
		t.Fatal("dry run recorded config")
	}
}

func TestWindowsTargetFollowsRedirectedDocuments(t *testing.T) {
	f := newWinFixture(t)
	redirected := filepath.Join(f.winLocal, "OneDrive", "Documents", "WindowsPowerShell", "Microsoft.PowerShell_profile.ps1")
	writeFile(t, redirected, "# wps\n", 0o644)
	f.shellFold = func() (string, error) { return `%USERPROFILE%\OneDrive\Documents`, nil }
	f.apply("claude-code")
	if !strings.Contains(read(t, redirected), shellwrap.BlockBegin) {
		t.Fatal("the block did not follow the redirected Documents folder")
	}
	if _, err := os.Stat(f.profile); !os.IsNotExist(err) {
		t.Fatal("a profile was created under the unredirected Documents folder")
	}

	// A redirect this process cannot reach (a network share) falls back to
	// <home>\Documents rather than writing somewhere else.
	g := newWinFixture(t)
	writeFile(t, g.profile, "# p\n", 0o644)
	g.shellFold = func() (string, error) { return `\\server\share\Documents`, nil }
	g.apply("claude-code")
	if !strings.Contains(read(t, g.profile), shellwrap.BlockBegin) {
		t.Fatal("unreachable redirect did not fall back to <home>\\Documents")
	}
}

func TestWindowsTargetUnavailable(t *testing.T) {
	home := t.TempDir()
	s := Service{Home: home, GOOS: "linux", Getenv: func(string) string { return "" }, IsWSL: func() bool { return false }, ObserverPath: "/x/observer"}
	if _, err := s.StatusFor(context.Background(), TargetWindows); !errors.Is(err, ErrTargetUnavailable) {
		t.Fatalf("outside WSL: %v", err)
	}
	ts, err := s.Targets(context.Background())
	if err != nil || len(ts) != 1 || ts[0].ID != TargetNative {
		t.Fatalf("outside WSL only the native target is listed: %+v %v", ts, err)
	}

	f := newWinFixture(t)
	f.svc.WSLDistro = ""
	if _, err := f.svc.StatusFor(context.Background(), TargetWindows); err == nil || !strings.Contains(err.Error(), "distro is unknown") {
		t.Fatalf("no distro: %v", err)
	}
	f.svc.WSLDistro = "Ubuntu 20.04"
	if _, err := f.svc.StatusFor(context.Background(), TargetWindows); err == nil || !strings.Contains(err.Error(), "cannot be passed") {
		t.Fatalf("unsafe distro: %v", err)
	}
	f.svc.WSLDistro = "Ubuntu-20.04"
	f.svc.WindowsHome = filepath.Join(f.root, "elsewhere", "u")
	ts, _ = f.svc.Targets(context.Background())
	if len(ts) != 2 || ts[1].Available || !strings.Contains(ts[1].Reason, "not on a WSL drive mount") {
		t.Fatalf("a home off the drive mount: %+v", ts)
	}
}

func TestWindowsTargetsListsAvailableTarget(t *testing.T) {
	f := newWinFixture(t)
	ts, err := f.svc.Targets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 2 || !ts[1].Available || ts[1].Home != `C:\Users\u` || ts[1].Distro != "Ubuntu-20.04" || ts[1].GOOS != "windows" {
		t.Fatalf("targets %+v", ts)
	}
}

func TestWindowsTargetRefusesAnUnsafeObserverPath(t *testing.T) {
	f := newWinFixture(t)
	f.svc.ObserverPath = filepath.Join(f.home, "a&b", "observer")
	writeFile(t, f.svc.ObserverPath, "#!/bin/sh\n", 0o755)
	if _, err := f.svc.Apply(context.Background(), Request{Target: TargetWindows, Tools: []string{"codex"}}); err == nil {
		t.Fatal("an observer path with a cmd metacharacter must be refused before planning")
	}
	if _, err := os.Stat(filepath.Join(f.winLocal, ".observer")); !os.IsNotExist(err) {
		t.Fatal("a refused apply wrote files")
	}
}

func TestWindowsPathMapping(t *testing.T) {
	wt := windowsTarget{mountRoot: "/mnt"}
	good := map[string]string{
		`C:\Users\u\.observer\shims\claude.cmd`: "/mnt/c/Users/u/.observer/shims/claude.cmd",
		`d:\Docs`:                               "/mnt/d/Docs",
		`C:/Users/u`:                            "/mnt/c/Users/u",
	}
	for in, want := range good {
		if got, err := wt.local(in); err != nil || got != want {
			t.Errorf("local(%q) = %q %v, want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{`\\server\share\x`, `relative\x`, `C:`, `C:\Users\..\Admin`, `/mnt/c/x`} {
		if got, err := wt.local(bad); err == nil {
			t.Errorf("local(%q) = %q, want a refusal", bad, got)
		}
	}
	if got, err := wt.windowsPath("/mnt/c/Users/u"); err != nil || got != `C:\Users\u` {
		t.Errorf("windowsPath = %q %v", got, err)
	}
	for _, bad := range []string{"/home/u", "/mnt/cc/Users", "/mnt"} {
		if _, err := wt.windowsPath(bad); err == nil {
			t.Errorf("windowsPath(%q) accepted", bad)
		}
	}
}

func TestRefreshStaleForWindowsRepointsBridgeShims(t *testing.T) {
	f := newWinFixture(t)
	writeFile(t, f.profile, "# p\n", 0o644)
	f.apply("claude-code")
	shim := filepath.Join(f.winLocal, ".observer", "shims", "claude.cmd")
	profBefore := read(t, f.profile)
	moved := filepath.Join(f.home, "new", "observer")
	writeFile(t, moved, "#!/bin/sh\n", 0o755)
	if err := os.Remove(f.obs); err != nil {
		t.Fatal(err)
	}
	f.svc.ObserverPath = ""
	f.svc.Executable = func() (string, error) { return moved, nil }
	out, err := f.svc.RefreshStaleFor(context.Background(), TargetWindows)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Rewritten) != 1 {
		t.Fatalf("rewritten %+v plan %+v", out.Rewritten, out.Plan)
	}
	got := read(t, shim)
	if !strings.Contains(got, `set "SBO_OBSERVER=`+moved+`"`) || !strings.Contains(got, "launch-env claude-code") {
		t.Fatalf("refreshed shim:\n%s", got)
	}
	if read(t, f.profile) != profBefore {
		t.Fatal("the refresh touched the profile")
	}
	// The native refresh is unaffected (nothing enabled there).
	if nout, err := f.svc.RefreshStale(context.Background()); err != nil || len(nout.Rewritten) != 0 {
		t.Fatalf("native refresh %+v %v", nout, err)
	}
}

func TestWindowsInstalledProbeSeesTheSide(t *testing.T) {
	f := newWinFixture(t)
	seen := map[string]shellwrap.Side{}
	f.svc.WindowsInstalled = func(id string, side shellwrap.Side) bool {
		seen[id] = side
		return id == "muse"
	}
	out := f.apply(shellwrap.AllTools)
	if len(out.Plan.Tools) != 1 || out.Plan.Tools[0].ID != "muse" {
		t.Fatalf("`all` must take exactly the installed rows: %+v", out.Plan.Tools)
	}
	if seen["muse"] != shellwrap.SideWSL || seen["claude-code"] != shellwrap.SideHost {
		t.Fatalf("the installed probe was not told the side: %+v", seen)
	}
}
