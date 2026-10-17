package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/componentstate"
	"github.com/marmutapp/superbased-observer/internal/componentsvc"
	"github.com/marmutapp/superbased-observer/internal/config"
)

// TestStartWarnsWhenAProxyRouteOutlivesTheProxy is ledger MODPKG-13
// (T-679): capture.proxy inactive (here a hand-written minimal base) while a
// tool still routes through the proxy prints one WARN naming the tool, the
// file and the teardown command; nothing prints while the proxy is active,
// and the probe is never called then.
func TestStartWarnsWhenAProxyRouteOutlivesTheProxy(t *testing.T) {
	ctx := context.Background()
	route := []componentsvc.StepOutcome{
		{Component: "integration.proxy-route", Target: "claude-code", Changed: true, Detail: []string{"route: env.ANTHROPIC_BASE_URL in /h/.claude/settings.json"}},
		{Component: "integration.proxy-route", Target: "codex", Changed: false},
	}
	// Removing capture.proxy alone keeps it active while a selected
	// component requires it (proxy-route, compression, routing), so the hand
	// edit that strands a route is a base without the proxy: minimal.
	const proxyOff = "minimal"
	cases := []struct {
		name      string
		distro    string
		probe     []componentsvc.StepOutcome
		probeErr  error
		want      []string
		wantProbe bool
	}{
		{name: "proxy active: silent, no probe"},
		{
			name: "proxy removed, route left", distro: proxyOff, probe: route, wantProbe: true,
			want: []string{"claude-code still routes through the proxy", "ANTHROPIC_BASE_URL", "observer components teardown integration.proxy-route", "ConnectionRefused"},
		},
		{name: "proxy removed, no route left", distro: proxyOff, probe: route[1:], wantProbe: true},
		{
			name: "probe error reported", distro: proxyOff, probeErr: errors.New("boom"), wantProbe: true,
			want: []string{"check for proxy routes", "boom"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.distro == "" {
				requireComponents(t, "capture.proxy")
			}
			snap := componentstate.Resolve(config.ComponentsConfig{Distro: tc.distro}, nil)
			called := false
			probe := func(_ context.Context, ids []string) ([]componentsvc.StepOutcome, error) {
				called = true
				if len(ids) != 1 || ids[0] != "integration.proxy-route" {
					t.Errorf("probe ids: %v", ids)
				}
				return tc.probe, tc.probeErr
			}
			var buf bytes.Buffer
			warnProxyRouteWithoutProxy(ctx, &buf, snap, probe)
			if called != tc.wantProbe {
				t.Fatalf("probe called=%v want %v", called, tc.wantProbe)
			}
			out := buf.String()
			if len(tc.want) == 0 && out != "" {
				t.Fatalf("unexpected output: %q", out)
			}
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("output lacks %q: %q", w, out)
				}
			}
			if n := strings.Count(out, "\n"); len(tc.want) > 0 && n != 1 {
				t.Errorf("want one line, got %d: %q", n, out)
			}
		})
	}
}
