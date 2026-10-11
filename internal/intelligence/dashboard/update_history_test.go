package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestUpdateHistory: GET /api/update/history is `observer update history`
// (sweep row 23): the node-local ledger through store.LoadUpdateEvents,
// newest first, capped by ?limit.
func TestUpdateHistory(t *testing.T) {
	srv := newServerWithFakeExec(t, nil)
	st := store.New(srv.opts.DB)
	base := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	for i, to := range []string{"v1.34.0", "v1.34.1", "v1.34.2"} {
		if err := st.AppendUpdateEvent(context.Background(), store.UpdateEventRow{
			At: base.Add(time.Duration(i) * time.Hour), FromVersion: "v1.33.0", ToVersion: to,
			State: "applied", Detail: "applied " + to,
		}); err != nil {
			t.Fatal(err)
		}
	}
	get := func(q string) []updateHistoryEvent {
		t.Helper()
		rr := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/update/history"+q, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("GET = %d %s", rr.Code, rr.Body.String())
		}
		var out struct {
			Events []updateHistoryEvent `json:"events"`
		}
		if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out.Events
	}
	all := get("")
	if len(all) != 3 || all[0].ToVersion != "v1.34.2" || all[0].Detail != "applied v1.34.2" {
		t.Fatalf("events = %+v, want 3 newest first", all)
	}
	if got := get("?limit=1"); len(got) != 1 {
		t.Errorf("limit=1 returned %d events", len(got))
	}
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/update/history", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST = %d, want 405", rr.Code)
	}
}
