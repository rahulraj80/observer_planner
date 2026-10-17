package shellwrapsvc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/platform/crossmount"
	"github.com/marmutapp/superbased-observer/internal/shellwrap"
)

// targetEnv is everything one call needs about the target whose shells it
// manages: the planner's Host (in the target's own path notation), how a
// target path maps to the path this process reaches, and the target's
// recorded choice. The native target maps paths to themselves.
type targetEnv struct {
	target    Target
	host      shellwrap.Host
	local     func(p string) (string, error)
	view      config.ShellWrapTargetConfig
	installed func(id string) bool
	// shimConfig is the config identity every shim of this target carries
	// (P3-09, shimConfigPath).
	shimConfig string
}

func identityPath(p string) (string, error) { return p, nil }

func (te targetEnv) shimDir() string { return shimDirFor(te.host, te.view) }

// checkObserver refuses, before planning, an observer path a bridge shim
// cannot carry (every row would otherwise turn into the same warning).
func (te targetEnv) checkObserver(obs string) error {
	if te.host.Bridge == nil {
		return nil
	}
	if err := shellwrap.CheckBridgeObserverPath(obs); err != nil {
		return fmt.Errorf("shellwrapsvc: the Windows target cannot wrap: %w (run observer from a path without shell metacharacters)", err)
	}
	return nil
}

func (te targetEnv) reopenNote() string {
	if te.host.Bridge != nil {
		return "open a new PowerShell window for the change to take effect (cmd.exe needs the shim folder on PATH by hand)"
	}
	return "open a new shell (or re-source its start-up file) for the change to take effect"
}

// targetEnv resolves t. The Windows target fails with the reason it is not
// available here (TargetInfo.Reason says the same thing).
func (s Service) targetEnv(cfg config.Config, t Target) (targetEnv, error) {
	switch t {
	case TargetNative:
		h, err := s.Host()
		if err != nil {
			return targetEnv{}, err
		}
		return targetEnv{target: t, host: h, local: identityPath, view: targetView(cfg, t), installed: s.Installed, shimConfig: s.shimConfigPath()}, nil
	case TargetWindows:
		wt, err := s.windowsTarget()
		if err != nil {
			return targetEnv{}, err
		}
		h := shellwrap.Host{
			GOOS: "windows",
			Home: wt.winHome,
			Exists: func(p string) bool {
				lp, err := wt.local(p)
				return err == nil && exists(lp)
			},
			Bridge: &shellwrap.WSLBridge{Distro: wt.distro},
		}
		h.DocumentsDir = s.windowsDocuments(wt)
		te := targetEnv{target: t, host: h, local: wt.local, view: targetView(cfg, t), shimConfig: s.shimConfigPath()}
		if s.WindowsInstalled != nil {
			te.installed = func(id string) bool {
				c, ok := shellwrap.CandidateOn(id, h)
				return ok && s.WindowsInstalled(id, c.Side)
			}
		}
		return te, nil
	}
	return targetEnv{}, fmt.Errorf("shellwrapsvc: unknown target %q", t)
}

// TargetInfo describes one target a caller can manage.
type TargetInfo struct {
	ID        Target `json:"id"`
	Label     string `json:"label"`
	GOOS      string `json:"goos"`
	Available bool   `json:"available"`
	// Reason says why an unavailable target cannot be managed here.
	Reason string `json:"reason,omitempty"`
	// Home is the target's home directory in its own notation.
	Home string `json:"home,omitempty"`
	// Distro is the WSL distro a bridge shim names (Windows target only).
	Distro string `json:"distro,omitempty"`
	// Enabled is the target's recorded [shell_wrap] enabled.
	Enabled bool `json:"enabled"`
}

// Targets lists the targets this process can manage: always the native one,
// plus the Windows target when this process runs inside WSL (available or
// not, with the reason). Read-only.
func (s Service) Targets(ctx context.Context) ([]TargetInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	home, err := s.home()
	if err != nil {
		return nil, err
	}
	cfg, _ := s.loadConfig(s.configPath(home))
	native := TargetInfo{
		ID: TargetNative, Label: "this machine (" + s.goos() + ")", GOOS: s.goos(),
		Available: true, Home: home, Enabled: cfg.ShellWrap.Enabled,
	}
	if d := s.distro(); d != "" && s.inWSL() {
		native.Label = "WSL (" + d + ")"
	}
	out := []TargetInfo{native}
	if s.WindowsHome == "" && !s.inWSL() {
		return out, nil
	}
	win := TargetInfo{ID: TargetWindows, Label: "Windows", GOOS: "windows", Enabled: cfg.ShellWrap.Windows.Enabled}
	if wt, err := s.windowsTarget(); err != nil {
		win.Reason = err.Error()
	} else {
		win.Available, win.Home, win.Distro = true, wt.winHome, wt.distro
		win.Label = "Windows (via WSL " + wt.distro + ")"
	}
	return append(out, win), nil
}

func (s Service) inWSL() bool {
	if s.goos() != "linux" {
		return false
	}
	if s.IsWSL != nil {
		return s.IsWSL()
	}
	return crossmount.IsWSL()
}

func (s Service) distro() string {
	if s.WSLDistro != "" {
		return s.WSLDistro
	}
	return s.getenv("WSL_DISTRO_NAME")
}

// windowsTarget is the resolved WSL bridge target.
type windowsTarget struct {
	localHome string // /mnt/c/Users/<u>
	winHome   string // C:\Users\<u>
	mountRoot string // /mnt
	distro    string
}

// ErrTargetUnavailable wraps every reason the Windows target cannot be
// managed from this process.
var ErrTargetUnavailable = errors.New("the Windows target is not available here")

func unavailable(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrTargetUnavailable, fmt.Sprintf(format, a...))
}

// windowsTarget resolves the Windows home and distro. It never guesses: a
// home it cannot prove belongs to the current Windows user, or a distro name a
// shim cannot carry, makes the target unavailable with the reason.
func (s Service) windowsTarget() (windowsTarget, error) {
	wt := windowsTarget{mountRoot: strings.TrimRight(s.WindowsMountRoot, "/")}
	if wt.mountRoot == "" {
		wt.mountRoot = "/mnt"
	}
	wt.localHome = s.WindowsHome
	if wt.localHome == "" {
		if !s.inWSL() {
			return windowsTarget{}, unavailable("observer is not running inside WSL")
		}
		var owned []string
		for _, h := range crossmount.ExtraHomes() {
			if h.OS == crossmount.OSWindows && crossmount.HomeOwnedByCurrentWindowsUser(h.Path) {
				owned = append(owned, h.Path)
			}
		}
		switch len(owned) {
		case 0:
			return windowsTarget{}, unavailable("no Windows home that belongs to the current Windows user was found under %s/c/Users", wt.mountRoot)
		case 1:
			wt.localHome = owned[0]
		default:
			return windowsTarget{}, unavailable("several Windows homes match the current Windows user (%s)", strings.Join(owned, ", "))
		}
	}
	win, err := wt.windowsPath(wt.localHome)
	if err != nil {
		return windowsTarget{}, unavailable("%v", err)
	}
	wt.winHome = win
	wt.distro = s.distro()
	if wt.distro == "" {
		return windowsTarget{}, unavailable("the WSL distro is unknown ($WSL_DISTRO_NAME is not set)")
	}
	if !shellwrap.ValidDistro(wt.distro) {
		return windowsTarget{}, unavailable("the WSL distro name %q cannot be passed to wsl.exe safely", wt.distro)
	}
	return wt, nil
}

// windowsPath maps a drive-mount path (/mnt/c/Users/u) to Windows notation
// (C:\Users\u).
func (wt windowsTarget) windowsPath(local string) (string, error) {
	rest, ok := strings.CutPrefix(path.Clean(local), wt.mountRoot+"/")
	if !ok || rest == "" || !isLetter(rest[0]) || (len(rest) > 1 && rest[1] != '/') {
		return "", fmt.Errorf("%s is not on a WSL drive mount under %s", local, wt.mountRoot)
	}
	win := strings.ToUpper(rest[:1]) + `:\`
	if len(rest) > 2 {
		win += strings.ReplaceAll(rest[2:], "/", `\`)
	}
	return win, nil
}

// local maps a Windows drive path (C:\Users\u\x) to the drive-mount path this
// process reaches (/mnt/c/Users/u/x). A UNC path, a relative path or one with
// a ".." segment is refused: nothing is ever written somewhere the planner did
// not name.
func (wt windowsTarget) local(p string) (string, error) {
	if len(p) < 3 || !isLetter(p[0]) || p[1] != ':' || (p[2] != '\\' && p[2] != '/') {
		return "", fmt.Errorf("%s is not a Windows drive path this process can reach", p)
	}
	rest := strings.ReplaceAll(p[3:], `\`, "/")
	for _, seg := range strings.Split(rest, "/") {
		if seg == ".." {
			return "", fmt.Errorf("%s: a \"..\" segment is refused", p)
		}
	}
	return path.Join(wt.mountRoot, strings.ToLower(p[:1]), rest), nil
}

func isLetter(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }

// windowsDocuments resolves the Windows user's Documents folder (where the
// PowerShell profiles live) from WSL: the User Shell Folders "Personal" value
// (OneDrive or Group Policy redirection), else <home>\Documents. A resolved
// folder this process cannot reach falls back to the default.
func (s Service) windowsDocuments(wt windowsTarget) string {
	def := wt.winHome + `\Documents`
	lookup := s.WindowsShellFolder
	if lookup == nil {
		lookup = interopShellFolder(wt.mountRoot)
	}
	docs, _, ok := shellwrap.ResolveDocumentsDir(shellwrap.DocumentsLookup{
		ShellFolder: lookup,
		Getenv:      wt.getenv,
		Home:        wt.winHome,
	})
	if !ok {
		return def
	}
	lp, err := wt.local(docs)
	if err != nil {
		return def
	}
	if fi, err := os.Stat(lp); err != nil || !fi.IsDir() {
		return def
	}
	return docs
}

// getenv answers the %VAR% references a User Shell Folders value carries,
// from the Windows home alone. Anything else is unknown, which makes the
// registry rung fall through rather than half-expand.
func (wt windowsTarget) getenv(name string) string {
	switch strings.ToUpper(name) {
	case "USERPROFILE":
		return wt.winHome
	case "HOMEDRIVE":
		return wt.winHome[:2]
	case "HOMEPATH":
		return wt.winHome[2:]
	}
	return ""
}

// userShellFoldersRegKey is the per-user folder-redirection key.
const userShellFoldersRegKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\User Shell Folders`

// interopShellFolders memoizes the reg.exe lookup per mount root: the value
// changes only when the operator redirects Documents, and a lookup is an
// interop process start.
var interopShellFolders sync.Map // mountRoot -> func() (string, error)

// interopShellFolder reads the "Personal" value with reg.exe through WSL
// interop (read-only `reg query`, 3 s timeout). It never runs from a test
// binary or while foreign discovery is suppressed.
func interopShellFolder(mountRoot string) func() (string, error) {
	if f, ok := interopShellFolders.Load(mountRoot); ok {
		return f.(func() (string, error))
	}
	f := sync.OnceValues(func() (string, error) {
		if crossmount.ForeignHomesSuppressed() {
			return "", errors.New("shellwrapsvc: foreign discovery suppressed")
		}
		reg := mountRoot + "/c/Windows/System32/reg.exe"
		if _, err := os.Stat(reg); err != nil {
			p, lerr := exec.LookPath("reg.exe")
			if lerr != nil {
				return "", fmt.Errorf("shellwrapsvc: reg.exe not reachable: %w", lerr)
			}
			reg = p
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, reg, "query", userShellFoldersRegKey, "/v", "Personal").Output() /* #nosec G204 -- fixed argv, read-only registry query */
		if err != nil {
			return "", fmt.Errorf("shellwrapsvc: reg query: %w", err)
		}
		v, ok := shellwrap.ParseRegQueryValue(string(out), "Personal")
		if !ok {
			return "", errors.New("shellwrapsvc: reg query: no Personal value")
		}
		return v, nil
	})
	actual, _ := interopShellFolders.LoadOrStore(mountRoot, f)
	return actual.(func() (string, error))
}

// shimConfigPath is the config identity a shim carries as `--config`
// (P3-09): "" when this service runs on the default config (the observer the
// shim calls resolves the same file itself, and the shim stays byte-identical
// to one written before shims carried a config), otherwise the absolute path
// of the config this service reads and records the choice in. Without it a
// shim enabled under `--config /custom.toml` later ran on the DEFAULT config:
// another proxy port, DB, allowed-tools list and budget state.
func (s Service) shimConfigPath() string {
	if s.ConfigPath == "" {
		return ""
	}
	p, err := filepath.Abs(s.ConfigPath)
	if err != nil {
		p = filepath.Clean(s.ConfigPath)
	}
	if home, herr := s.home(); herr == nil && p == filepath.Join(home, ".observer", "config.toml") {
		return ""
	}
	return p
}
