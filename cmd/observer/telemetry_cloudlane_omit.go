//go:build sbo_omit_cloud_intelligence

package main

import (
	"context"
	"errors"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/telemetry"
)

// telemetry_cloudlane_omit.go stands in for the cloud telemetry lane in a build
// without cloud.intelligence: no cloud package is linked. Telemetry still works
// against a self-hosted reporter (`observer telemetry register`).

var errNoCloudTelemetryLane = errors.New("this build has no cloud telemetry lane (cloud.intelligence omitted); register with a self-hosted reporter: observer telemetry register")

func newCloudSender(config.Config, *telemetry.Collector, *slog.Logger) (telemetrySender, error) {
	return nil, errNoCloudTelemetryLane
}

func cloudConsentSync(config.Config, string) error { return errNoCloudTelemetryLane }

func resetCloudTelemetryID(context.Context, config.Config, *telemetry.Collector) error {
	return errNoCloudTelemetryLane
}

func newTelemetryDataCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "data",
		Short: "Show what the telemetry server holds (cloud lane not in this build)",
		RunE:  func(*cobra.Command, []string) error { return errNoCloudTelemetryLane },
	}
}

func newTelemetryDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete",
		Short: "Delete server-side telemetry (cloud lane not in this build)",
		RunE:  func(*cobra.Command, []string) error { return errNoCloudTelemetryLane },
	}
}
