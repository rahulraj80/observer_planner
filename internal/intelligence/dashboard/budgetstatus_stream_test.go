package dashboard

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/budgetwarnfile"
)

// TestBudgetStatusStreamPushesChanges is T-206 (budget freshness P4): the
// stream sends the /api/budget/status payload on connect, sends it again when
// the daemon rewrites budget-status.json, and sends nothing for an unchanged
// file (only heartbeats).
func TestBudgetStatusStreamPushesChanges(t *testing.T) {
	prevPoll, prevBeat := budgetStreamPoll, budgetStreamHeartbeat
	budgetStreamPoll, budgetStreamHeartbeat = 20*time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { budgetStreamPoll, budgetStreamHeartbeat = prevPoll, prevBeat })

	s, h := newManageServer(t)
	cfg, err := loadConfigForDashboard(s.opts.ConfigPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	statusPath := budgetwarnfile.StatusPath(cfg.Observer.DBPath)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/budget/status/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("status %d, content type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	events := make(chan map[string]any, 8)
	pings := make(chan struct{}, 64)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
		event := ""
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, ": ping"):
				pings <- struct{}{}
			case strings.HasPrefix(line, "event: "):
				event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: ") && event == "status":
				var m map[string]any
				if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &m) == nil {
					events <- m
				}
			}
		}
		close(events)
	}()
	next := func(what string) map[string]any {
		t.Helper()
		select {
		case m, ok := <-events:
			if !ok {
				t.Fatalf("%s: stream closed", what)
			}
			return m
		case <-ctx.Done():
			t.Fatalf("%s: no event", what)
		}
		return nil
	}

	if first := next("connect"); first["available"] != false {
		t.Fatalf("first event = %v, want available=false (no status file yet)", first)
	}
	// An unchanged file sends nothing but heartbeats.
	<-pings
	select {
	case m := <-events:
		t.Fatalf("unchanged status re-sent: %v", m)
	default:
	}
	if err := budgetwarnfile.Write(statusPath, budgetwarnfile.File{
		Windows: []budgetwarnfile.Window{{Window: "daily", Unit: "usd", Cap: 1, Ratio: 0.92, Level: "warn", Crossed: 0.9}},
		Active:  []budgetwarnfile.Notice{{ID: "daily-usd-0.9", Message: "m", Threshold: 0.9, Window: "daily", Unit: "usd"}},
	}); err != nil {
		t.Fatal(err)
	}
	changed := next("after a write")
	if changed["available"] != true {
		t.Fatalf("event after the daemon wrote the file = %v", changed)
	}
	if active, _ := changed["active"].([]any); len(active) != 1 {
		t.Fatalf("active = %v, want the one notice", changed["active"])
	}
}

// TestBudgetStatusStreamRefusesOtherMethods: the stream is GET only.
func TestBudgetStatusStreamRefusesOtherMethods(t *testing.T) {
	_, h := newManageServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/budget/status/stream", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST = %d, want 405", rec.Code)
	}
}
