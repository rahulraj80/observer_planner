//go:build !sbo_omit_export_otel

package main

import (
	"context"
	"log/slog"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/config"
)

func TestBuildSelfObsSink_DisabledReturnsNop(t *testing.T) {
	t.Parallel()
	sink, cleanup, err := buildSelfObsSink(config.Config{}, slog.Default())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	defer cleanup()
	if sink == nil {
		t.Fatal("sink must not be nil")
	}
	if err := sink.ForceFlush(context.Background()); err != nil {
		t.Fatalf("ForceFlush: %v", err)
	}
	if err := sink.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func TestBuildSelfObsSink_EnabledEmptyEndpointNop(t *testing.T) {
	t.Parallel()
	cfg := config.Config{
		SelfObs: config.SelfObsConfig{
			Enabled:  true,
			Endpoint: "",
			Token:    "kid.secret",
		},
	}
	sink, cleanup, err := buildSelfObsSink(cfg, slog.Default())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	defer cleanup()
	if sink == nil {
		t.Fatal("sink must not be nil")
	}
}

func TestBuildSelfObsSink_BadSchemeErrors(t *testing.T) {
	t.Parallel()
	cfg := config.Config{
		SelfObs: config.SelfObsConfig{
			Enabled:  true,
			Endpoint: "ftp://gateway.example/v1/traces",
			Token:    "kid.secret",
		},
	}
	sink, cleanup, err := buildSelfObsSink(cfg, slog.Default())
	if err == nil {
		cleanup()
		t.Fatal("expected scheme error")
	}
	if sink != nil {
		t.Fatal("sink must be nil on error")
	}
}
