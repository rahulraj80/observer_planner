package proxy

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestListenAndServeClosesRequestsThatOutliveTheGrace pins T-553 for the
// proxy listener: a proxied request still in flight when the daemon stops
// (a long streamed turn) is cut once the shutdown grace runs out, so its
// upstream call ends instead of running on (and writing to the DB) after the
// daemon released its port, and the stop itself is not reported as a failure.
// Before the fix Shutdown's deadline error was returned (which failed the
// whole daemon's errgroup) and the in-flight request kept running forever.
func TestListenAndServeClosesRequestsThatOutliveTheGrace(t *testing.T) {
	entered := make(chan struct{}, 1)
	upstreamDone := make(chan time.Time, 1)
	anth := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Drain the body so the server watches the connection and cancels
		// r.Context() when the proxy drops it.
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done():
			upstreamDone <- time.Now()
		case <-time.After(15 * time.Second):
		}
	})
	oai := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	p, _, closeUp := newTestProxy(t, anth, oai)
	defer closeUp()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	served := make(chan error, 1)
	go func() { served <- p.ListenAndServe(ctx, addr) }()

	// Wait for the listener, then start a request that will not finish.
	deadline := time.Now().Add(5 * time.Second)
	for {
		c, err := net.Dial("tcp", addr)
		if err == nil {
			_ = c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("proxy never listened on %s: %v", addr, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	go func() {
		body := `{"model":"claude-sonnet-4","stream":true,"messages":[{"role":"user","content":"hi"}]}`
		req, _ := http.NewRequest(http.MethodPost, "http://"+addr+"/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("request never reached the upstream")
	}

	stopped := time.Now()
	cancel()
	// The grace is 5s; allow 4s on top of it.
	const bound = 9 * time.Second
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("ListenAndServe returned %v on a requested stop; want nil", err)
		}
	case <-time.After(bound):
		t.Fatalf("ListenAndServe did not return within %v of the stop", bound)
	}
	select {
	case at := <-upstreamDone:
		if took := at.Sub(stopped); took > bound {
			t.Fatalf("in-flight upstream call ended %v after the stop; want within %v", took, bound)
		}
	case <-time.After(bound - time.Since(stopped)):
		t.Fatalf("in-flight upstream call still running %v after the stop; the proxy never closed it", bound)
	}
}
