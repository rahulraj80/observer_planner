package proxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestProxyActivityAndBudgetWarningHeader pins the two budget-latency seams on
// a real proxied round trip: the response carries X-SBO-Budget-Warning while
// the warning source returns a value, and the activity callback sees the
// request and the committed turn.
func TestProxyActivityAndBudgetWarningHeader(t *testing.T) {
	const responseBody = `{"id":"msg_abc","model":"claude-sonnet-4","stop_reason":"end_turn","usage":{"input_tokens":4,"output_tokens":2}}`
	anth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(responseBody))
	}))
	defer anth.Close()

	var mu sync.Mutex
	var kinds []string
	p, err := New(Options{
		AnthropicUpstream: anth.URL,
		OpenAIUpstream:    anth.URL,
		Sink:              &fakeSink{},
		Activity: func(kind string) {
			mu.Lock()
			kinds = append(kinds, kind)
			mu.Unlock()
		},
		BudgetWarning: func() string { return "soft/member:m1 168%" },
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/messages",
		strings.NewReader(`{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)

	if got := rec.Header().Get(HeaderBudgetWarning); got != "soft/member:m1 168%" {
		t.Fatalf("%s = %q", HeaderBudgetWarning, got)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		joined := strings.Join(kinds, ",")
		mu.Unlock()
		if strings.Contains(joined, ActivityRequest) && strings.Contains(joined, ActivityTurn) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("activity kinds = %q, want request and turn", joined)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestProxyBudgetWarningAbsent pins the byte-identical default: no callback,
// no header.
func TestProxyBudgetWarningAbsent(t *testing.T) {
	var p *Proxy
	if p.budgetWarningValue() != "" {
		t.Fatal("nil proxy must stamp nothing")
	}
	p.noteActivity(ActivityRequest) // must not panic
	q := &Proxy{budgetWarning: func() string { return "" }}
	if q.budgetWarningValue() != "" {
		t.Fatal("empty source must stamp nothing")
	}
}
