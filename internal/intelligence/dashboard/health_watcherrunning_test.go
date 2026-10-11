package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestWatcherHealthReportsWatcherRunning pins T-585's sidebar half: the
// watcher-health payload says whether THIS process runs the watcher, so the
// sidebar's "watcher" line is keyed to the watcher itself instead of the
// Claude Code proxy-route setup status. A process that does not declare it
// (a standalone dashboard) reports false, never a guess.
func TestWatcherHealthReportsWatcherRunning(t *testing.T) {
	for _, running := range []bool{true, false} {
		s, _ := newTestServer(t)
		s.opts.WatcherRunning = running
		rr := httptest.NewRecorder()
		s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/health/watcher", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("status: %d", rr.Code)
		}
		var got struct {
			WatcherRunning *bool `json:"watcher_running"`
		}
		if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.WatcherRunning == nil || *got.WatcherRunning != running {
			t.Errorf("watcher_running = %v, want %v", got.WatcherRunning, running)
		}
	}
}
