package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/componentstate"
	"github.com/marmutapp/superbased-observer/internal/componentsvc"
)

// components_proxyroute.go is the start-time half of ledger MODPKG-13
// (T-679): when the proxy component is not active, this daemon binds no
// proxy listener, so a proxy route Observer wrote into another tool's
// config (ANTHROPIC_BASE_URL, codex base_url, ...) makes every session that
// tool starts fail with ConnectionRefused. The normal removal path already
// tears the route down first; this catches a hand edit of [components] or a
// removal applied with skip teardown, and says exactly what to run.

// proxyRouteComponents are the components whose teardown removes a proxy
// route Observer wrote. Adapter rows also carry a route part, but an
// adapter's route only matters here while the proxy itself is gone, which
// the integration.proxy-route probe already covers for every tool.
var proxyRouteComponents = []string{"integration.proxy-route"}

// warnProxyRouteWithoutProxy prints one WARN per leftover route when
// capture.proxy is not active. probe dry-runs the teardown of inactive ids
// (componentsvc.Service.RunTeardown with dryRun) and is only called in that
// case, so an install with the proxy active (every install without a
// [components] block) pays nothing. A probe error is reported, never fatal.
func warnProxyRouteWithoutProxy(ctx context.Context, w io.Writer, snap componentstate.Snapshot, probe leftoverProbe) {
	if snap.Active("capture.proxy") || probe == nil {
		return
	}
	var ids []string
	for _, id := range proxyRouteComponents {
		if !snap.Active(id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return
	}
	outs, err := probe(ctx, ids)
	if err != nil {
		fmt.Fprintf(w, "WARN components: capture.proxy is not active and the check for proxy routes left in other tools' config failed: %v\n", err)
		return
	}
	// REVIEW-4 P4-19: name only places this build has. Without the
	// dashboard the CLI verb is the one fix; without the proxy in the build
	// "turn it back on" is not one.
	fix := "Remove the route " + components.WhereToChange("`observer components teardown "+strings.Join(ids, " ")+"`")
	if reason := proxyNotInBuildReason(); reason != "" {
		fix += " (" + reason + ")."
	} else {
		fix += ", or turn capture.proxy back on."
	}
	for _, o := range outs {
		if !o.Changed {
			continue
		}
		fmt.Fprintf(w, "WARN components: capture.proxy is not active, but %s still routes through the proxy (%s): its sessions will get ConnectionRefused. %s\n",
			o.Target, strings.Join(o.Detail, "; "), fix)
	}
}

// warnProxyRouteWithoutProxyFor wires the warning for a config path with the
// components service's dry-run teardown as the probe.
func warnProxyRouteWithoutProxyFor(ctx context.Context, w io.Writer, snap componentstate.Snapshot, configPath string) {
	if snap.Active("capture.proxy") {
		return
	}
	svc, err := newComponentsService(configPath, nil)
	if err != nil {
		fmt.Fprintf(w, "WARN components: capture.proxy is not active and the check for proxy routes left in other tools' config could not start: %v\n", err)
		return
	}
	warnProxyRouteWithoutProxy(ctx, w, snap, func(ctx context.Context, ids []string) ([]componentsvc.StepOutcome, error) {
		return svc.RunTeardown(ctx, ids, true)
	})
}
