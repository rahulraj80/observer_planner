package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/dashapi"
	"github.com/marmutapp/superbased-observer/internal/diag"
	"github.com/marmutapp/superbased-observer/internal/platform/crossmount"
	"github.com/marmutapp/superbased-observer/internal/routeprov"
)

// windowsHomeService is the dashboard's Windows-home init seam (T-421 item
// 4): the detection is diag.WindowsHomeTargets (the doctor's own), the write
// is wireAIClients with the --windows-*-home override, so the dashboard
// control and `observer init --windows-<x>-home <path>` cannot write
// different things.
type windowsHomeService struct {
	configPath string
	proxyPort  int
	// Injected so tests never read /mnt/c or shell out to cmd.exe.
	isWSL func() bool
	homes func() []crossmount.HomeRoot
	owned func(string) bool
	wire  func(WireAIClientsOptions) ([]string, string, string, bool, error)
}

// newWindowsHomeService wires the production seam. configPath is the daemon's
// resolved config; like `observer init` without --config, the registered
// commands carry --config only when it is not the default path.
func newWindowsHomeService(configPath string, proxyPort int) dashapi.WindowsHomeService {
	return &windowsHomeService{
		configPath: configPath,
		proxyPort:  proxyPort,
		isWSL:      crossmount.IsWSL,
		homes:      crossmount.AllHomes,
		owned:      crossmount.HomeOwnedByCurrentWindowsUser,
		wire:       wireAIClients,
	}
}

// windowsHomeTargetOptions is the closed table binding each cross-OS target
// to the init options its --windows-*-home flag sets: the tool selector and
// the home override (the same pair the CLI flags produce).
var windowsHomeTargetOptions = map[string]func(o *WireAIClientsOptions, home string){
	"claude-code-windows": func(o *WireAIClientsOptions, home string) {
		o.OnlyClaudeCode, o.WindowsClaudeHome = true, home
	},
	"codex-windows": func(o *WireAIClientsOptions, home string) {
		o.OnlyCodex, o.WindowsCodexHome = true, home
	},
	"cursor-windows": func(o *WireAIClientsOptions, home string) {
		o.OnlyCursor, o.WindowsCursorHome = true, home
	},
}

func (s *windowsHomeService) Targets(context.Context) (bool, []diag.WindowsHomeTarget, error) {
	if !s.isWSL() {
		return false, nil, nil
	}
	var out []diag.WindowsHomeTarget
	for _, t := range diag.WindowsHomeTargets(s.homes(), s.owned) {
		if _, ok := windowsHomeTargetOptions[t.Target]; ok {
			out = append(out, t)
		}
	}
	return true, out, nil
}

func (s *windowsHomeService) Apply(_ context.Context, req dashapi.WindowsHomeApply) ([]string, error) {
	set, ok := windowsHomeTargetOptions[req.Target]
	if !ok {
		return nil, fmt.Errorf("unknown cross-OS target %q", req.Target)
	}
	opts := WireAIClientsOptions{
		RouteWriter:   routeprov.WriterSetup,
		ConfigPath:    s.registrationConfigPath(),
		ProxyPort:     s.proxyPort,
		DryRun:        req.DryRun,
		Force:         req.Force,
		SkipExtension: true,
		WindowsOnly:   true,
	}
	set(&opts, req.Home)
	lines, _, _, _, err := s.wire(opts)
	if err != nil {
		return lines, err
	}
	if len(lines) == 0 || (len(lines) == 1 && strings.TrimSpace(lines[0]) == "") {
		return []string{req.Target + ": nothing to write for this home"}, nil
	}
	return lines, nil
}

// registrationConfigPath mirrors `observer init`'s --config rule: omitted for
// the default config path, passed for a custom one.
func (s *windowsHomeService) registrationConfigPath() string {
	if s.configPath == "" {
		return ""
	}
	if def, err := config.ResolveGlobalPath(""); err == nil && def == s.configPath {
		return ""
	}
	return s.configPath
}
