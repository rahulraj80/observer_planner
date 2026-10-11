package shellwrap

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/integration"
)

// TestShimsCarryConfigPath pins P3-09 per template: every observer call a
// shim makes carries the non-default config it was enabled with, and a shim
// with no ConfigPath renders no --config at all.
func TestShimsCarryConfigPath(t *testing.T) {
	bridge := &WSLBridge{Distro: "Ubuntu"}
	cases := []struct {
		name   string
		spec   ShimSpec
		render func(ShimSpec) (string, error)
		want   []string
	}{
		{
			"posix terminal",
			ShimSpec{
				Name: "claude", ToolID: "claude-code", Kind: integration.LaunchKindTerminal, Args: []string{"claude"},
				ObserverPath: "/usr/bin/observer", ShimDir: "/h/.observer/shims", ConfigPath: "/h/my cfg/o.toml",
			},
			RenderPOSIXShim,
			[]string{`exec "$sbo_observer" 'claude' --config '/h/my cfg/o.toml' -- "$@"`},
		},
		{
			"posix gui",
			ShimSpec{
				Name: "code", ToolID: "vscode", Kind: integration.LaunchKindGUI, Args: []string{"ide", "vscode"}, ProjectDirArgv: true,
				ObserverPath: "/usr/bin/observer", ShimDir: "/h/.observer/shims", ConfigPath: "/h/o.toml",
			},
			RenderPOSIXShim,
			[]string{`exec "$sbo_observer" 'ide' 'vscode' --config '/h/o.toml'` + "\n", `exec "$sbo_observer" 'ide' 'vscode' --config '/h/o.toml' "$1"`},
		},
		{
			"cmd terminal",
			ShimSpec{
				Name: "claude", ToolID: "claude-code", Kind: integration.LaunchKindTerminal, Args: []string{"claude"},
				ObserverPath: `C:\o\observer.exe`, ShimDir: `C:\o\shims`, ConfigPath: `C:\cfg 100%\o.toml`,
			},
			RenderCmdShim,
			[]string{`"C:\o\observer.exe" claude --config "C:\cfg 100%%\o.toml" -- %*`},
		},
		{
			"bridge wsl side",
			ShimSpec{
				Name: "aider", ToolID: "aider", Kind: integration.LaunchKindTerminal, Args: []string{"aider"},
				ObserverPath: "/usr/local/bin/observer", ShimDir: `C:\o\shims`, ConfigPath: "/home/u/alt/config.toml", Bridge: bridge, Side: SideWSL,
			},
			RenderBridgeCmdShim,
			[]string{`set "SBO_CONFIG=/home/u/alt/config.toml"`, `-e "%SBO_OBSERVER%" aider --config "%SBO_CONFIG%" -- %*`},
		},
		{
			"bridge host side",
			ShimSpec{
				Name: "claude", ToolID: "claude-code", Kind: integration.LaunchKindTerminal, Args: []string{"claude"},
				ObserverPath: "/usr/local/bin/observer", ShimDir: `C:\o\shims`, ConfigPath: "/home/u/alt/config.toml", Bridge: bridge, Side: SideHost,
			},
			RenderBridgeCmdShim,
			[]string{`set "SBO_CONFIG=/home/u/alt/config.toml"`, `shell-wrap launch-env claude-code --config "%SBO_CONFIG%""`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.render(tc.spec)
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("shim lacks %q:\n%s", w, got)
				}
			}
			bare := tc.spec
			bare.ConfigPath = ""
			plain, err := tc.render(bare)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(plain, "--config") || strings.Contains(plain, "SBO_CONFIG") {
				t.Errorf("a default-config shim names a config:\n%s", plain)
			}
		})
	}
}

// TestBridgeShimRefusesUnsafeConfigPath: the config path is spliced into the
// same `for /f` command line as the observer path, so it obeys the same
// closed spelling and is refused, never escaped.
func TestBridgeShimRefusesUnsafeConfigPath(t *testing.T) {
	for _, bad := range []string{"relative/config.toml", "/home/u/a&b/config.toml", "/home/u/100%/c.toml", "/home/u/(x)/c.toml"} {
		_, err := RenderBridgeCmdShim(ShimSpec{
			Name: "aider", ToolID: "aider", Kind: integration.LaunchKindTerminal, Args: []string{"aider"},
			ObserverPath: "/usr/local/bin/observer", ShimDir: `C:\o\shims`, ConfigPath: bad,
			Bridge: &WSLBridge{Distro: "Ubuntu"}, Side: SideWSL,
		})
		if err == nil {
			t.Errorf("config path %q accepted", bad)
		}
	}
}
