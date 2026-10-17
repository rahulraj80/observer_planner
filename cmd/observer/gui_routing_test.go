//go:build !sbo_omit_terminals

package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/guilaunch"
	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/toolresolve"
)

// routingFakeInfo is a minimal fs.FileInfo for the injected resolver Env.
type routingFakeInfo struct {
	name string
	mode fs.FileMode
}

func (i routingFakeInfo) Name() string       { return i.name }
func (i routingFakeInfo) Size() int64        { return 0 }
func (i routingFakeInfo) Mode() fs.FileMode  { return i.mode }
func (i routingFakeInfo) ModTime() time.Time { return time.Time{} }
func (i routingFakeInfo) IsDir() bool        { return false }
func (i routingFakeInfo) Sys() any           { return nil }

// routingEnv is a resolver Env whose filesystem is exactly `files`.
func routingEnv(base toolresolve.Env, files map[string]fs.FileMode) toolresolve.Env {
	base.Stat = func(p string) (fs.FileInfo, error) {
		m, ok := files[p]
		if !ok {
			return nil, fs.ErrNotExist
		}
		return routingFakeInfo{name: filepath.Base(p), mode: m}, nil
	}
	base.EvalSymlinks = func(p string) (string, error) {
		if _, ok := files[p]; ok {
			return p, nil
		}
		return "", fs.ErrNotExist
	}
	base.Glob = func(string) ([]string, error) { return nil, nil }
	return base
}

// routingSpec is a synthetic child-env GUI row (never a live registry row), so
// these cases pin the routing seam rather than today's registry contents.
func routingSpec() integration.GUILaunchSpec {
	return integration.GUILaunchSpec{
		ID:      "editor",
		Label:   "Editor",
		Surface: "ide",
		Binary: integration.BinaryResolveSpec{
			Names: integration.BinaryNames{Unix: []string{"editor"}, Windows: []string{"Editor.exe"}},
		},
		Grounded: true,
		Wrap: integration.WrapSpec{
			Kind: integration.WrapChildEnv,
			Env:  []integration.WrapEnvVar{{Name: "ANTHROPIC_BASE_URL"}},
		},
	}
}

// emptyConfig writes a config with no [launch.tools] override, so the
// launcher's ladder runs the resolver (never the operator's real config).
func emptyConfig(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte("[proxy]\nport = 18779\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestGUIRoutingVerdict is the preflight's routing fact, computed by the
// launcher's own composition: a WSL daemon launching a Windows app across
// interop must report that routing does NOT reach the app (with the
// structured reason), while a native install reports that it does.
func TestGUIRoutingVerdict(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the interop and native-Linux fixtures are POSIX-spelled; the WSL case cannot occur on a Windows daemon")
	}
	cfg := emptyConfig(t)
	foreignHome := "/mnt/c/Users/u"
	winExe := filepath.Join(foreignHome, "AppData/Local/Programs", "Editor.exe")

	tests := []struct {
		name        string
		env         toolresolve.Env
		wsl         bool
		wantKnown   bool
		wantReaches bool
		wantReason  guilaunch.WrapReason
	}{
		{
			name: "WSL daemon, Windows-only install: launched via interop, env does not cross",
			env: routingEnv(toolresolve.Env{
				GOOS: "linux", WSL: true, Home: "/home/u", ForeignHomes: []string{foreignHome},
			}, map[string]fs.FileMode{winExe: 0o644}),
			wsl:         true,
			wantKnown:   true,
			wantReaches: false,
			wantReason:  guilaunch.WrapReasonInteropEnvNotPropagated,
		},
		{
			name: "native Linux install on PATH: the wrap reaches the app",
			env: routingEnv(toolresolve.Env{
				GOOS: "linux", Home: "/home/u", ProcessPath: []string{"/usr/bin"},
			}, map[string]fs.FileMode{"/usr/bin/editor": 0o755}),
			wantKnown:   true,
			wantReaches: true,
			wantReason:  guilaunch.WrapReasonNone,
		},
		{
			name: "Windows-only install on a non-WSL Linux daemon: nothing to launch, routing unknown",
			env: routingEnv(toolresolve.Env{
				GOOS: "linux", Home: "/home/u", ForeignHomes: []string{foreignHome},
			}, map[string]fs.FileMode{winExe: 0o644}),
			wantKnown: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := tc.env
			wsl := tc.wsl
			g := &guiLauncher{
				proxyPort:  18779,
				configPath: func() string { return cfg },
				resolveEnv: func() toolresolve.Env { return env },
				isWSL:      func() bool { return wsl },
			}
			reaches, reason := g.routingVerdict("editor", routingSpec())
			if !tc.wantKnown {
				if reaches != nil {
					t.Fatalf("routing = (%v, %q), want unknown (nil)", *reaches, reason)
				}
				return
			}
			if reaches == nil {
				t.Fatal("routing unknown, want a verdict")
			}
			if *reaches != tc.wantReaches || reason != string(tc.wantReason) {
				t.Errorf("routing = (%v, %q), want (%v, %q)", *reaches, reason, tc.wantReaches, tc.wantReason)
			}
		})
	}
}
