package dashboard

import (
	"net/http"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgserver/auth"
	"github.com/marmutapp/superbased-observer/internal/orgserver/rollup"
)

// briefing_handlers.go — the org-side Daily Briefing endpoints (Phase C of
// docs/design/standup-briefing.md §4). These are MANUAL routes (no OpenAPI
// codegen; the org spec is gitignored in this checkout), registered in
// server.go buildMux and protected by the SAML session / admin middleware
// exactly like the other manual /api/org/* routes.

// GetOrgBriefing serves GET /api/org/briefing[?date=YYYY-MM-DD]: the composed
// daily briefing, role-scoped (admin → whole org, lead → led teams). The
// default date is the most-recently-completed UTC day. The payload is the pure
// internal/briefing Briefing document — counts, costs, opportunities,
// narrative — never content.
func (a *API) GetOrgBriefing(w http.ResponseWriter, r *http.Request) {
	_, scope, ok := a.caller(w, r)
	if !ok {
		return
	}
	day, err := briefingDay(a.now(), r.URL.Query().Get("date"))
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "date must be YYYY-MM-DD")
		return
	}
	b, err := rollup.Briefing(r.Context(), a.db, day, scope, a.now())
	if err != nil {
		a.fail(w, "org briefing", err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// GetOrgBriefingAggregates serves GET /api/org/briefing/aggregates
// [?date=YYYY-MM-DD]: the pushed hash-only Daily-Briefing aggregate digest
// (node [org_client.share] briefing_aggregates opt-in) for the day. ADMIN-only
// (the raw per-node aggregate feed, closest to the obs/enduser-spend manual
// routes); the role-scoped composed view is /api/org/briefing.
func (a *API) GetOrgBriefingAggregates(w http.ResponseWriter, r *http.Request) {
	day, err := briefingDay(a.now(), r.URL.Query().Get("date"))
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "date must be YYYY-MM-DD")
		return
	}
	rows, err := rollup.BriefingAggregates(r.Context(), a.db, day)
	if err != nil {
		a.fail(w, "org briefing aggregates", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"date":       day.Format("2006-01-02"),
		"aggregates": rows,
	})
}

// briefingDay resolves the briefing UTC day: the most-recently-completed UTC
// day by default, or an explicit ?date=YYYY-MM-DD parsed in UTC (the org
// server keeps its own clock; day boundaries are UTC, unlike the node-local
// briefing).
func briefingDay(now time.Time, dateRaw string) (time.Time, error) {
	if dateRaw != "" {
		t, err := time.Parse("2006-01-02", dateRaw)
		if err != nil {
			return time.Time{}, err
		}
		return t.UTC(), nil
	}
	utc := now.UTC()
	today := time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
	return today.AddDate(0, 0, -1), nil
}
