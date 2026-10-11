package main

import (
	"fmt"
	"slices"

	"github.com/marmutapp/superbased-observer/internal/components"
)

// launch_proxy_feasible.go is the one launcher seam for a build without
// capture.proxy (lane MP-L1L, T-715, operator rule D-514). In such a build
// this binary has no proxy to route a tool through, so a launcher that finds
// nothing listening on the proxy port says why (the build) and launches the
// tool unrouted, instead of pointing it at a dead port or telling the
// operator to run `observer start`, which would not help. A proxy that does
// answer (another observer build serving the port) is still used.

// proxyNotInBuild reports whether capture.proxy is compiled out of this
// binary (proxy_omit.go marks it).
func proxyNotInBuild() bool {
	return slices.Contains(components.Omitted(), "capture.proxy")
}

// proxyNotInBuildReason is the clause naming the build and where to get one
// with the proxy; "" when the proxy is compiled in.
func proxyNotInBuildReason() string {
	if !proxyNotInBuild() {
		return ""
	}
	return "the API proxy (capture.proxy) is not in this build; " + components.DistrosHaving("capture.proxy")
}

// proxyNotInBuildNotice is the one stderr line a launcher prints when it
// launches tool unrouted because the proxy is not in this build. ok=false in
// a build with the proxy.
func proxyNotInBuildNotice(tool string) (string, bool) {
	reason := proxyNotInBuildReason()
	if reason == "" {
		return "", false
	}
	return fmt.Sprintf("observer %s: %s. Launching %s without proxy routing: its transcript is still captured, proxy-only token and cost detail is not.", tool, reason, tool), true
}
