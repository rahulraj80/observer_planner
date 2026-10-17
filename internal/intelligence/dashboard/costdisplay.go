package dashboard

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// costDisplayResponse is the wire shape of GET /api/cost-display: what this
// dashboard renders for a cost figure.
//
//	mode    "usd" (dollar costs, the default) or "tokens" (token usage instead).
//	source  "default" — no verified org choice applies (solo install, an
//	        enrolled node whose server predates the rail, or nothing fetched
//	        yet); "org" — the org's signed display policy decided.
type costDisplayResponse struct {
	Mode    string `json:"mode"`
	Source  string `json:"source"`
	OrgName string `json:"org_name,omitempty"`
	Version int64  `json:"version,omitempty"`
}

// handleCostDisplay serves GET /api/cost-display (operator ruling
// 2026-10-02: an org admin chooses whether DEVELOPERS' dashboards show dollar
// costs or token usage; developers see dollars by default).
//
// Like handleAnnouncements it makes NO network call: it reads the node-local
// cache the push loop wrote after verifying the org's signed document
// (agent migration 148), and it requires CURRENT enrolment — a departed org
// never decides how this dashboard renders, even if a row survived. Every
// answer that is KNOWN degrades to dollars, the default (a solo install, an
// unenrolled node, an org that never chose). An answer this node cannot
// establish (the enrolment status or the cached signed policy cannot be read)
// is NOT the dollars default (Sol A4-03): it is a 503, and the dashboard
// keeps its token-safe view and retries rather than exposing costs an org
// may have chosen to hide (D-013).
//
// The resolution itself is store.ResolveDeveloperCostDisplay, the one seam
// every developer surface shares (this endpoint, the statusline tile, the
// budget-warning messages behind the prompt-hook notice and `observer guard
// status`, the guard's budget verdict text, and since T-204 the reporting CLI
// (cmd/observer/costdisplay_cli.go), MCP tool results, the node digest and
// the Arena provenance footer). The setting is a presentation choice, not an
// access control (the dollars are in the developer's own local database
// either way).
func (s *Server) handleCostDisplay(w http.ResponseWriter, r *http.Request) {
	out, err := s.costDisplay(r.Context())
	if err != nil {
		w.Header().Set("Retry-After", "2")
		http.Error(w, "cost display could not be established yet", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, out)
}

// errCostDisplayUnknown is costDisplay's "this node cannot tell what the org
// chose" (Sol A4-03).
var errCostDisplayUnknown = errors.New("dashboard.costDisplay: the org cost-display choice could not be established")

// statuslineCostDisplay is the statusline tile's mode: the established
// answer, or token usage when none can be established, never dollars by
// default.
func (s *Server) statuslineCostDisplay(ctx context.Context) string {
	out, err := s.costDisplay(ctx)
	if err != nil {
		return orgcontract.DisplayCostTokens
	}
	return out.Mode
}

func (s *Server) costDisplay(ctx context.Context) (costDisplayResponse, error) {
	def := costDisplayResponse{Mode: orgcontract.DisplayCostUSD, Source: "default"}
	if s.opts.OrgClient == nil {
		return def, nil
	}
	st, err := s.opts.OrgClient.Status(ctx)
	if err != nil {
		return costDisplayResponse{}, fmt.Errorf("%w: %w", errCostDisplayUnknown, err)
	}
	// The node's OWN database (s.opts.DB), never s.db(): demo mode swaps the
	// DATA surfaces onto a seeded sample, but how this developer's dashboard
	// renders cost is a fact about the real enrolment, like the enrolment
	// status read above.
	d := store.New(s.opts.DB).ResolveDeveloperCostDisplay(ctx, st.Enrolled, st.OrgID)
	if d.Unverified {
		return costDisplayResponse{}, errCostDisplayUnknown
	}
	if d.Source != "org" {
		return def, nil
	}
	return costDisplayResponse{Mode: d.Mode, Source: d.Source, OrgName: st.OrgName, Version: d.Version}, nil
}
