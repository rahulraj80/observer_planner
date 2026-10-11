//go:build sbo_omit_agentaccess_relay && !sbo_omit_capture_proxy

package main

import "github.com/marmutapp/superbased-observer/internal/proxy"

// bindMCPRelayProxy binds nothing: a build without agentaccess.relay has no
// relay tools[] seam (mcprelay_omit.go). Lane MP-L1L, T-715.
func bindMCPRelayProxy(*mcpRelayHandle, *proxy.Options) {}
