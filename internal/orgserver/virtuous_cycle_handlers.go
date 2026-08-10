package orgserver

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgserver/auth"
	"github.com/marmutapp/superbased-observer/internal/orgserver/rollup"
)

// virtuousCycleHandler is the Stage 3 virtuous-cycle trend admin export,
// mounted behind the SAML admin session. It reports the cumulative CTA-applies
// trend with projected vs realized savings over the trailing ?days= window
// (default 90) — the org-plane mirror of the node-side virtuous-cycle trend.
// Manual route (no OpenAPI codegen), mirroring obsEndUserSpendHandler.
func virtuousCycleHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		days := 90
		if v := r.URL.Query().Get("days"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				auth.WriteError(w, http.StatusBadRequest, "bad_request", "days must be a positive integer")
				return
			}
			days = n
		}
		res, err := rollup.SelectVirtuousCycle(r.Context(), db, days, time.Now())
		if err != nil {
			auth.WriteError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(res)
	}
}
