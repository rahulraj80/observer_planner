//go:build !sbo_omit_capture_proxy

package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/marmutapp/superbased-observer/internal/guard"
	"github.com/marmutapp/superbased-observer/internal/proxy"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// guardwire_proxy.go is the proxy half of the guard wiring (lane MP-L1L,
// T-715): the adapter that puts the shared Guard behind the proxy's
// request and response scan seams. A build without capture.proxy
// (-tags sbo_omit_capture_proxy) has no proxy to scan, so this file is not
// linked; the guard keeps its hook, watcher and budget paths.

// guardScannerAdapter bridges the daemon's shared *guard.Guard +
// *store.Store behind proxy.GuardScanner, the pipelineAdapter /
// costEngineAdapter pattern: the proxy package holds only its own
// plain types; persistence and alerting live here, behind the seam.
// mcp, when non-nil, receives the §9.2 MCP tool-declaration
// observations the request scan surfaces.
type guardScannerAdapter struct {
	g      *guard.Guard
	st     *store.Store
	logger *slog.Logger
	mcp    *mcpsecRunner
}

// ScanRequest implements proxy.GuardScanner (§8.2 + §8.4). Verdicts
// persist synchronously through the one-owner store seam before the
// decision returns, rare (only verdict-bearing requests pay it), and
// an enforced deny/mask should be on the audit chain before the
// synthetic response leaves. Persistence failure is log-and-continue
// (§17.4): it never blocks the request decision.
//
// MCP declaration observations (§9.2) run DETACHED: the pin diff
// reads + writes the DB, and the request must not wait on it, the
// declarations describe config state, not this request's content.
func (a guardScannerAdapter) ScanRequest(_ context.Context, provider string, body []byte, sessionID string) proxy.GuardRequestResult {
	return a.finishScan(a.g.ScanProxyRequest(provider, body, sessionID, time.Now().UTC()))
}

// ScanPrompt / ScanRequestAfterPrompt implement proxy.PromptPhaseScanner
// (LIVE CORRECTION 2026-09-07): the proxy runs the prompt lane on the
// pre-compression body and the remaining scans on the final body -- see
// the interface's doc comment for why one post-compression scan cannot
// see a pasted secret (the pipeline forward-scrubs it first).
func (a guardScannerAdapter) ScanPrompt(ctx context.Context, provider string, body []byte, sessionID string) proxy.GuardRequestResult {
	return a.finishScan(a.g.ScanProxyPromptFor(proxy.GuardClientTool(ctx), provider, body, sessionID, time.Now().UTC()))
}

func (a guardScannerAdapter) ScanRequestAfterPrompt(_ context.Context, provider string, body []byte, sessionID string) proxy.GuardRequestResult {
	return a.finishScan(a.g.ScanProxyRequestAfterPrompt(provider, body, sessionID, time.Now().UTC()))
}

// finishScan persists/alerts the verdicts, hands MCP declarations to
// the off-path runner, and maps the guard result onto the proxy's
// action vocabulary -- shared by all three scan entry points.
func (a guardScannerAdapter) finishScan(res guard.ProxyRequestResult) proxy.GuardRequestResult {
	a.persistAndAlert(res.Verdicts)
	if res.PromptPreSendDigest != "" {
		mctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := a.st.RecordPromptPreSend(mctx, res.PromptPreSendSessionID, res.PromptPreSendDigest, time.Now().UTC()); err != nil {
			a.logger.Debug("guard: proxy prompt pre-send mark failed", "err", err)
		}
		cancel()
	}
	if len(res.MCPDecls) > 0 && a.mcp != nil {
		decls := res.MCPDecls
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			a.mcp.ObserveDecls(ctx, decls)
		}()
	}
	var out proxy.GuardRequestResult
	switch {
	// Group 3 (phase-3a review, docs/guard-prompt.md "Two follow-ups"
	// item 2): a prompt-lane block sets PromptDeny/PromptStatus/
	// PromptRuleID/PromptReason ALONGSIDE the generic Deny/DenyRuleID/
	// DenyReason fields (scanPrompt never sets Deny alone for a
	// prompt-lane block, see ProxyRequestResult's own doc comment),
	// so this case MUST be checked before the plain res.Deny case
	// below or a prompt-submit interrupt would render through the
	// wrong (egress-worded, always-403) body builder. Switches a
	// proxy-lane deny to the intended 400-for-ask-once/403-for-block
	// developer-facing wording (guardPromptDenyBody).
	case res.PromptDeny:
		out.Action = "prompt_deny"
		out.RuleID = res.PromptRuleID
		out.Reason = res.PromptReason
		out.Status = res.PromptStatus
	case res.Deny:
		out.Action = "deny"
		out.RuleID = res.DenyRuleID
		out.Reason = res.DenyReason
		// Track B: empty unless an org bundle applies.
		out.HumanLine = res.DenyHumanLine
	case res.MaskedBody != nil:
		out.Action = "mask"
		out.Body = res.MaskedBody
	}
	return out
}

// InspectResponse implements proxy.GuardScanner (§8.3): flag/alert
// only, the proxy already delivered the response. apiTurnID anchors
// each persisted verdict to the api_turn this response was captured as
// (0 when the turn was dropped) so the obs trajectory enrichment can
// surface the verdict on the span.
func (a guardScannerAdapter) InspectResponse(_ context.Context, sessionID string, apiTurnID int64, tools []proxy.GuardToolUse) {
	in := make([]guard.ProxyToolUse, 0, len(tools))
	for _, t := range tools {
		in = append(in, guard.ProxyToolUse{Name: t.Name, Input: t.Input})
	}
	verdicts := a.g.InspectProxyResponse(sessionID, in, time.Now().UTC())
	if apiTurnID != 0 {
		for i := range verdicts {
			verdicts[i].Input.APITurnID = apiTurnID
		}
	}
	a.persistAndAlert(verdicts)
}

// persistAndAlert writes record-worthy verdicts through the one-owner
// guard store seam (detached context, the request context may die
// the moment the client has its bytes) and fires desktop alerts
// post-persist (the MaybeAlert call-site contract).
func (a guardScannerAdapter) persistAndAlert(verdicts []guard.ActionVerdict) {
	if len(verdicts) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := a.st.PersistGuardVerdicts(ctx, verdicts); err != nil {
		a.logger.Warn("guard: proxy verdict persist failed", "err", err)
	}
	for i := range verdicts {
		a.g.MaybeAlert(verdicts[i])
	}
}
