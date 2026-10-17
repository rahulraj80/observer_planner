package main

import (
	"github.com/marmutapp/superbased-observer/internal/config"
)

// proxy_serving.go is the one answer to "does this node's daemon serve the
// API proxy" (REVIEW-4 P4-18 / P4-19). Three facts can each say no, and the
// first that does names the reason: the build (capture.proxy compiled out),
// the component selection (capture.proxy installed but inactive, so the
// gated "proxy" loop never binds) and [proxy].enabled. A route that points
// at a proxy this node does not serve cannot have produced proxy rows, so
// callers that hand token ownership to the proxy read this, never
// [proxy].enabled alone.

// proxyServingRule is one row of the ordered table proxyServingReason walks.
type proxyServingRule struct {
	off    func(cfg config.Config, captureProxyActive bool) bool
	reason func() string
}

// proxyServingRules is walked top-down; the first row whose off holds wins.
var proxyServingRules = []proxyServingRule{
	{
		off:    func(config.Config, bool) bool { return proxyNotInBuild() },
		reason: proxyNotInBuildReason,
	},
	{
		off:    func(_ config.Config, active bool) bool { return !active },
		reason: func() string { return "the API proxy (capture.proxy) is inactive on this machine" },
	},
	{
		off:    func(cfg config.Config, _ bool) bool { return !cfg.Proxy.Enabled },
		reason: func() string { return "the proxy is off on this node ([proxy].enabled = false)" },
	},
}

// proxyServingReason is "" when this node serves the API proxy, else the one
// reason it does not. captureProxyActive is the component snapshot's answer
// for capture.proxy (componentSnapshotForConfig(cfg).Active).
func proxyServingReason(cfg config.Config, captureProxyActive bool) string {
	for _, r := range proxyServingRules {
		if r.off(cfg, captureProxyActive) {
			return r.reason()
		}
	}
	return ""
}
