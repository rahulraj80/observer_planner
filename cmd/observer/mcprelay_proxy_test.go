//go:build !sbo_omit_agentaccess_relay && !sbo_omit_capture_proxy

package main

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/guard"
	"github.com/marmutapp/superbased-observer/internal/orgclient"
	"github.com/marmutapp/superbased-observer/internal/proxy"
)

// mcprelay_proxy_test.go holds the MCP relay tests that drive the proxy's
// tools[] seam (moved from mcprelay_wire_test.go, lane MP-L1L, T-715), so the
// relay's other tests compile in a build without capture.proxy.

// TestMCPRelay_DisabledBindsNothing pins the enabled=false byte-identical
// contract: no handle, no proxy seam, no hook lookup.
func TestMCPRelay_DisabledBindsNothing(t *testing.T) {
	prev := processMCPRelay.Load()
	t.Cleanup(func() { processMCPRelay.Store(prev) })
	processMCPRelay.Store(nil)
	cfg := mcpRelayTestCfg(t, false)
	var opts proxy.Options
	h := wireMCPRelay(cfg, nil, nil, nil, nil)
	if h != nil {
		t.Fatal("disabled relay returned a handle")
	}
	bindMCPRelayProxy(h, &opts)
	if opts.ToolsAllowlist != nil {
		t.Error("disabled relay bound the proxy tools seam")
	}
	if processMCPRelay.Load() != nil {
		t.Error("disabled relay registered a process handle")
	}
	if hookMCPAccessLookup(cfg) != nil {
		t.Error("disabled relay produced a hook lookup")
	}
	mcpRelayPublish(orgclient.PolicyResourceResult{Status: orgclient.PRApplied})
	mcpRelayClear()
}

// TestMCPRelay_DecideRows is the accessRules table test: one case per row,
// on both postures.
func TestMCPRelay_DecideRows(t *testing.T) {
	cases := []struct {
		name    string
		mode    string
		orgAuth bool
		server  string
		tool    string
		want    guard.MCPAccessVerdict
	}{
		{"allow row (enforce)", "enforce", false, "github", "create_issue", guard.MCPAccessVerdict{Known: true, Allowed: true}},
		{"allow row by slug resolves the vserver", "enforce", false, "github", "create_issue", guard.MCPAccessVerdict{Known: true, Allowed: true}},
		{"allow row by vserver id", "enforce", false, "vs-gh", "create_issue", guard.MCPAccessVerdict{Known: true, Allowed: true}},
		{"deny row (enforce) = org grant denied", "enforce", false, "github", "delete_repo", guard.MCPAccessVerdict{Known: true, OrgDenied: true}},
		{"no row matched (enforce) = default-deny, org grant", "enforce", false, "github", "list_repos", guard.MCPAccessVerdict{Known: true, OrgDenied: true}},
		{"deny row in observe = would-deny → allowed", "observe", false, "github", "delete_repo", guard.MCPAccessVerdict{Known: true, Allowed: true}},
		{"unknown server, individual = inert", "enforce", false, "slack", "post", guard.MCPAccessVerdict{}},
		{"unknown server, org authoritative = R-306 shape", "enforce", true, "slack", "post", guard.MCPAccessVerdict{Known: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := appliedHandle(t, mcpRelayTestCfg(t, true), tc.mode, tc.orgAuth)
			got := h.decide(tc.server, tc.tool)
			if got.Known != tc.want.Known || got.Allowed != tc.want.Allowed || got.OrgDenied != tc.want.OrgDenied {
				t.Errorf("decide = %+v, want Known=%v Allowed=%v OrgDenied=%v", got, tc.want.Known, tc.want.Allowed, tc.want.OrgDenied)
			}
		})
	}
	// No table at all: inert on both postures.
	h := newMCPRelayHandle(mcpRelayTestCfg(t, true), nil, nil, slog.New(slog.DiscardHandler))
	h.orgAuthoritative = func() bool { return true }
	if got := h.decide("github", "create_issue"); got.Known {
		t.Errorf("no-table decide = %+v, want inert", got)
	}
}

// TestMCPRelay_ToolsSeam pins the proxy seam: strip denied decls, keep
// allowed + (individual) unknown ones, refuse an unapproved hosted connector
// only on an org-authoritative node, and never in observe.
func TestMCPRelay_ToolsSeam(t *testing.T) {
	decls := []proxy.MCPToolDecl{
		{Index: 0, Name: "mcp__github__create_issue", Server: "github", Tool: "create_issue"},
		{Index: 1, Name: "mcp__github__delete_repo", Server: "github", Tool: "delete_repo"},
		{Index: 2, Name: "mcp__slack__post", Server: "slack", Tool: "post"},
	}
	unknownConn := proxy.HostedMCPConnector{Provider: "anthropic", Kind: "anthropic_mcp_servers", Name: "ex", URL: "https://mcp.example.com/sse"}
	approvedConn := proxy.HostedMCPConnector{Provider: "anthropic", Kind: "anthropic_mcp_servers", Name: "gh", URL: "https://mcp.github.com/mcp/"}
	audienceConn := proxy.HostedMCPConnector{Provider: "openai", Kind: "openai_responses_mcp", Name: "gh", URL: "https://gw.acme/mcp/github"}
	cases := []struct {
		name     string
		mode     string
		orgAuth  bool
		conns    []proxy.HostedMCPConnector
		wantKeep []string
		wantDeny string
	}{
		{"individual: strip deny row, keep allow + unknown", "enforce", false, nil, []string{"mcp__github__create_issue", "mcp__slack__post"}, ""},
		{"org authoritative: unknown stripped too", "enforce", true, nil, []string{"mcp__github__create_issue"}, ""},
		{"observe: nothing stripped", "observe", true, nil, []string{"mcp__github__create_issue", "mcp__github__delete_repo", "mcp__slack__post"}, ""},
		{"individual: unknown connector ungoverned", "enforce", false, []proxy.HostedMCPConnector{unknownConn}, []string{"mcp__github__create_issue", "mcp__slack__post"}, ""},
		{"org authoritative: unknown connector refused R-307", "enforce", true, []proxy.HostedMCPConnector{unknownConn}, nil, "R-307"},
		{"org authoritative: server-target connector approved", "enforce", true, []proxy.HostedMCPConnector{approvedConn}, []string{"mcp__github__create_issue"}, ""},
		{"org authoritative: gateway-audience connector approved", "enforce", true, []proxy.HostedMCPConnector{audienceConn}, []string{"mcp__github__create_issue"}, ""},
		{"org authoritative observe: connector would-deny only", "observe", true, []proxy.HostedMCPConnector{unknownConn}, []string{"mcp__github__create_issue", "mcp__github__delete_repo", "mcp__slack__post"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := appliedHandle(t, mcpRelayTestCfg(t, true), tc.mode, tc.orgAuth)
			res := h.ToolsSeam()(proxy.ToolsAllowlistInput{Provider: "anthropic", SessionID: "s", Decls: decls, Connectors: tc.conns})
			if res.DenyRuleID != tc.wantDeny {
				t.Fatalf("deny = %q (%s), want %q", res.DenyRuleID, res.DenyReason, tc.wantDeny)
			}
			if tc.wantDeny != "" {
				if !strings.Contains(res.DenyReason, "not approved") {
					t.Errorf("deny reason %q", res.DenyReason)
				}
				return
			}
			var got []string
			for _, d := range res.Keep {
				got = append(got, d.Name)
			}
			if strings.Join(got, ",") != strings.Join(tc.wantKeep, ",") {
				t.Errorf("keep = %v, want %v", got, tc.wantKeep)
			}
		})
	}
	// No table: everything kept, nothing denied.
	h := newMCPRelayHandle(mcpRelayTestCfg(t, true), nil, nil, slog.New(slog.DiscardHandler))
	res := h.ToolsSeam()(proxy.ToolsAllowlistInput{Decls: decls, Connectors: []proxy.HostedMCPConnector{unknownConn}})
	if len(res.Keep) != 3 || res.DenyRuleID != "" {
		t.Errorf("no-table seam = %+v", res)
	}
}
