package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/integration"
)

// Proxy `/up/<id>/` lanes for launchers (Sol A5-02 follow-up).
//
// Since A5-02 the proxy refuses a lane it cannot resolve (HTTP 502) instead
// of forwarding the request, and its credential, to a fixed provider. A
// launcher that routes a tool through a lane therefore needs that lane to
// exist. Two halves, one owner:
//
//   - built-in lanes: a registry row's ProxyRoute.Lane declares the grounded
//     host root for the lane its route addresses
//     (integration.DefaultLaneUpstreams). effectiveAgentUpstreams layers
//     [proxy.agent_upstreams] over them, and the proxy is built with the
//     result, so a fresh install routes `observer grok --proxy` and
//     `observer hermes` without a hand-written [proxy.upstreams] entry.
//   - preflight: requireLauncherLane refuses a launch whose lane resolves
//     nowhere, with a message that says how to fix it, instead of letting
//     the tool start and fail on a 502. It asks the running daemon first
//     (GET /-/lanes on the proxy, T-308), so org-distributed
//     gateway.providers lanes count, and falls back to config + built-ins
//     only when no daemon answers.

// effectiveAgentUpstreams is the coding-agent lane table the proxy is built
// with: the registry's built-in lanes, overlaid by [proxy.agent_upstreams]
// (configuration wins; an empty value there disables a lane). The proxy
// prefers a same-id [proxy.upstreams] lane over every entry here.
func effectiveAgentUpstreams(cfg config.Config) map[string]string {
	out := integration.DefaultLaneUpstreams()
	for id, upstream := range cfg.Proxy.AgentUpstreams {
		out[id] = upstream
	}
	return out
}

// launcherLaneUpstream resolves lane the way the proxy does: a
// [proxy.upstreams] lane first, then the effective agent lanes.
func launcherLaneUpstream(cfg config.Config, lane string) (string, bool) {
	if u := strings.TrimSpace(cfg.Proxy.Upstreams[lane]); u != "" {
		return u, true
	}
	if u := strings.TrimSpace(effectiveAgentUpstreams(cfg)[lane]); u != "" {
		return u, true
	}
	return "", false
}

// daemonLanesTimeout bounds the /-/lanes probe; a slower daemon is treated
// as unreachable and the preflight falls back to config.
const daemonLanesTimeout = 750 * time.Millisecond

// daemonLanes asks the RUNNING daemon's proxy at proxyURL which lanes it
// resolves right now (GET <proxyURL>/-/lanes, T-308). ok is false when the
// daemon is not running, is an older build without the endpoint, or answers
// anything but a well-formed body; the caller then falls back to config.
// A package variable so tests can stand in for the daemon.
var daemonLanes = probeDaemonLanes

// probeDaemonLanes is daemonLanes' real implementation.
func probeDaemonLanes(proxyURL string) (lanes map[string]bool, ok bool) {
	ctx, cancel := context.WithTimeout(context.Background(), daemonLanesTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(proxyURL, "/")+"/-/lanes", nil)
	if err != nil {
		return nil, false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	var body struct {
		Lanes *[]string `json:"lanes"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body); err != nil || body.Lanes == nil {
		return nil, false
	}
	out := make(map[string]bool, len(*body.Lanes))
	for _, id := range *body.Lanes {
		out[id] = true
	}
	return out, true
}

// laneVerdict is how the preflight decided a lane (one row per source of
// truth, consulted in order).
type laneVerdict int

const (
	laneResolved      laneVerdict = iota // the lane resolves
	laneDaemonMissing                    // the running daemon answered and lacks it
	laneConfigMissing                    // no daemon answer; config + built-ins lack it
)

// resolveLauncherLane decides lane. The RUNNING daemon is the truth when it
// answers: its live table includes org-distributed gateway.providers lanes
// that no config file names, and an org table can also replace a config
// lane. Only when no daemon answers does the preflight fall back to config
// and the registry built-ins (the launch then starts the daemon or warns
// that the proxy is down, as before).
func resolveLauncherLane(cfg config.Config, lane string) laneVerdict {
	if live, ok := daemonLanes(resolveProxyURL(cfg.Proxy.Port, "")); ok {
		if live[lane] {
			return laneResolved
		}
		return laneDaemonMissing
	}
	if _, ok := launcherLaneUpstream(cfg, lane); ok {
		return laneResolved
	}
	return laneConfigMissing
}

// requireLauncherLane refuses a proxied launch of verb whose lane cannot be
// resolved. alternative names the way out the launcher offers (e.g. "launch
// without --proxy").
func requireLauncherLane(cfg config.Config, verb, lane, alternative string) error {
	if lane == "" {
		return nil
	}
	verdict := resolveLauncherLane(cfg, lane)
	if verdict == laneResolved {
		return nil
	}
	source := "the proxy has no upstream"
	if verdict == laneDaemonMissing {
		source = "the running observer daemon reports no upstream (its live lane table, including any org-distributed lanes)"
	}
	err := fmt.Errorf("observer %s: %s for lane %q, so this launch would be refused (HTTP 502) and its requests never forwarded. "+
		"Add `%s = \"https://<provider host root>\"` under [proxy.upstreams] in the observer config, or %s",
		verb, source, lane, lane, alternative)
	// Launchers silence Cobra, so a plain error would exit 1 with nothing on
	// the terminal. The message is built only from the verb, the lane id and
	// a fixed alternative (no internal error text), so it is safe to show.
	return newVisibleLauncherError(err, err.Error())
}

// requireRouteLane is requireLauncherLane for the lane a tool's own registry
// row declares (ProxyRoute.Lane); a route with no lane needs nothing.
func requireRouteLane(cfg config.Config, tool, verb, alternative string) error {
	c, ok := integration.For(tool)
	if !ok || c.Proxy == nil || c.Proxy.Lane == nil {
		return nil
	}
	return requireLauncherLane(cfg, verb, c.Proxy.Lane.ID, alternative)
}
