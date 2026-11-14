package cloudgateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// TestTelemetryLaneFailsClosedWithoutConsent proves the lane makes no network
// attempt at all while the live telemetry consent reads off, and that the
// user's own-data actions are not gated.
func TestTelemetryLaneFailsClosedWithoutConsent(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.Error(w, `{"code":"unauthorized"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	gw, err := Open(Options{Cred: &memCred{}, BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	tier := "off"
	lane, err := gw.Telemetry(func() string { return tier })
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := lane.Send(ctx, []byte("x")); !errors.Is(err, ErrTelemetryNotConsented) {
		t.Fatalf("Send err = %v; want ErrTelemetryNotConsented", err)
	}
	if _, err := lane.InstallID(ctx); !errors.Is(err, ErrTelemetryNotConsented) {
		t.Fatalf("InstallID err = %v", err)
	}
	if err := lane.SyncTier(ctx, "baseline", ""); !errors.Is(err, ErrTelemetryNotConsented) {
		t.Fatalf("SyncTier err = %v", err)
	}
	if _, err := lane.Config(ctx); !errors.Is(err, ErrTelemetryNotConsented) {
		t.Fatalf("Config err = %v", err)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("server saw %d requests with consent off; want 0", n)
	}

	// Own-data actions still reach the server (the stub refuses them).
	if _, err := lane.OwnData(ctx, false); err == nil {
		t.Fatal("OwnData against a refusing stub returned nil error")
	}
	if hits.Load() == 0 {
		t.Fatal("OwnData made no request; own-data actions must not be gated")
	}

	// Consent on: the lane attempts the network.
	tier = "baseline"
	before := hits.Load()
	_, _ = lane.Send(ctx, []byte("x"))
	if hits.Load() == before {
		t.Fatal("Send with consent on made no request")
	}
}

func TestTelemetryLaneNeedsBaseURLAndConsent(t *testing.T) {
	gw, _ := Open(Options{Cred: &memCred{}})
	if _, err := gw.Telemetry(func() string { return "baseline" }); !errors.Is(err, ErrNoBaseURL) {
		t.Fatalf("no base URL err = %v; want ErrNoBaseURL", err)
	}
	gw, _ = Open(Options{Cred: &memCred{}, BaseURL: "https://example.invalid"})
	if _, err := gw.Telemetry(nil); err == nil {
		t.Fatal("nil consent source accepted")
	}
}
