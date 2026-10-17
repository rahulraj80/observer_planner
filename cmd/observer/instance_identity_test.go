package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/dashapi"
)

// TestRemoteIdentityAnnotate pins T-425's switcher side: a connected row
// carries the remote daemon's own version, schema and hostname, probed once
// per forward; a failed probe names the machine and is retried later; a row
// that is not connected is never probed.
func TestRemoteIdentityAnnotate(t *testing.T) {
	calls := 0
	var reply remoteStatus
	var fail error
	prev := probeRemoteStatus
	probeRemoteStatus = func(string) (remoteStatus, error) {
		calls++
		return reply, fail
	}
	t.Cleanup(func() { probeRemoteStatus = prev })

	now := time.Unix(1000, 0)
	c := &remoteIdentityCache{now: func() time.Time { return now }}

	down := dashapi.InstanceInfo{Name: "dev", Label: "sb-devbox", State: "disconnected"}
	if got := c.annotate(down); got != down || calls != 0 {
		t.Fatalf("a disconnected row was probed or changed: %+v (calls %d)", got, calls)
	}

	up := dashapi.InstanceInfo{Name: "dev", Label: "sb-devbox", State: "connected", LocalPort: 41000, URL: "http://127.0.0.1:41000/"}
	reply = remoteStatus{Version: "1.33.0", SchemaVersion: 121, HostName: "devbox-vm"}
	got := c.annotate(up)
	if got.RemoteVersion != "1.33.0" || got.RemoteSchemaVersion != 121 || got.RemoteHostName != "devbox-vm" || got.RemoteProbeError != "" {
		t.Fatalf("annotate = %+v", got)
	}
	c.annotate(up)
	if calls != 1 {
		t.Fatalf("a live forward was probed %d times, want once", calls)
	}

	// A reconnect on a new port may be a different machine: probe again.
	up.LocalPort, up.URL = 41001, "http://127.0.0.1:41001/"
	reply, fail = remoteStatus{}, errors.New("connection refused")
	got = c.annotate(up)
	if calls != 2 || !strings.Contains(got.RemoteProbeError, "sb-devbox") || got.RemoteVersion != "" {
		t.Fatalf("failed probe: calls %d, row %+v", calls, got)
	}
	c.annotate(up)
	if calls != 2 {
		t.Fatal("a failed probe was retried before remoteProbeRetry")
	}
	now = now.Add(remoteProbeRetry)
	reply, fail = remoteStatus{Version: "1.35.0", SchemaVersion: 158}, nil
	got = c.annotate(up)
	if calls != 3 || got.RemoteVersion != "1.35.0" || got.RemoteProbeError != "" {
		t.Fatalf("retry after failure: calls %d, row %+v", calls, got)
	}
}

// TestProbeRemoteStatusReadsTheRemoteStatus runs the real probe against a
// loopback server shaped like a remote daemon's /api/status.
func TestProbeRemoteStatusReadsTheRemoteStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/status" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"version":"1.33.0+org-v65","schema_version":121,"host_name":"devbox-vm","db_path":"x"}`))
	}))
	defer srv.Close()
	st, err := probeRemoteStatus(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	if st.Version != "1.33.0+org-v65" || st.SchemaVersion != 121 || st.HostName != "devbox-vm" {
		t.Fatalf("probe = %+v", st)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer bad.Close()
	if _, err := probeRemoteStatus(bad.URL); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("probe of a 401 = %v, want the status named", err)
	}
}
