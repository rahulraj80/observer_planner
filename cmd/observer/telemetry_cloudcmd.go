//go:build !sbo_omit_cloud_intelligence

package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/cloudgateway"
	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/telemetry"
	"github.com/marmutapp/superbased-observer/internal/telemetry/contract"
)

// The user's own-data controls on the cloud telemetry lane (telemetry plan
// Step 5.2): see what the server holds, delete it, or rotate to a new key.
// They act on this machine's install (the id derived from its cloud device
// key) and need no consent: withdrawing consent must never take away the
// ability to see or delete what was sent.

func loadTelemetryConfig() (config.Config, error) {
	resolved, err := config.ResolveGlobalPath("")
	if err != nil {
		return config.Config{}, fmt.Errorf("resolve config path: %w", err)
	}
	cfg, err := config.Load(config.LoadOptions{GlobalPath: resolved})
	if err != nil {
		return config.Config{}, fmt.Errorf("load config: %w", err)
	}
	return cfg, nil
}

// ownTelemetryLane opens the lane for an own-data action. A node registered
// with a self-hosted reporter is refused: its data lives on that reporter.
func ownTelemetryLane(cfg config.Config) (*cloudgateway.TelemetryLane, error) {
	if telemetryLoadToken(cfg) != "" {
		return nil, fmt.Errorf("this node sends to a self-hosted reporter (%s); use that reporter's /v1/me endpoints", cfg.Telemetry.Endpoint)
	}
	return openTelemetryLane(cfg, func() string { return cfg.Telemetry.EffectiveTier() })
}

func newTelemetryDataCmd() *cobra.Command {
	var full bool
	cmd := &cobra.Command{
		Use:   "data",
		Short: "Show what the telemetry server holds for this machine (--full: every event)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadTelemetryConfig()
			if err != nil {
				return err
			}
			lane, err := ownTelemetryLane(cfg)
			if err != nil {
				return err
			}
			raw, err := lane.OwnData(cmd.Context(), full)
			if err != nil {
				return fmt.Errorf("read telemetry data: %w", err)
			}
			return printIndentedJSON(cmd, raw)
		},
	}
	cmd.Flags().BoolVar(&full, "full", false, "include every stored event (an export)")
	return cmd
}

func newTelemetryDeleteCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete everything the telemetry server holds for this machine",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !yes {
				return fmt.Errorf("telemetry delete removes this machine's server-side telemetry; re-run with --yes to confirm")
			}
			cfg, err := loadTelemetryConfig()
			if err != nil {
				return err
			}
			lane, err := ownTelemetryLane(cfg)
			if err != nil {
				return err
			}
			raw, err := lane.OwnDelete(cmd.Context())
			if err != nil {
				return fmt.Errorf("delete telemetry data: %w", err)
			}
			if err := printIndentedJSON(cmd, raw); err != nil {
				return err
			}
			if cfg.Telemetry.EffectiveTier() != contract.TierOff && cfg.Telemetry.Enabled {
				fmt.Fprintln(cmd.OutOrStdout(), "note: telemetry is still on; the next batch starts a new record. Run `observer telemetry consent off` to stop sending.")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm the deletion")
	return cmd
}

// resetCloudTelemetryID is reset-id on the cloud lane: revoke the install
// server-side first (its history stays until retention ages it out, and the
// old key can never upload again), then clear the device key so the next run
// creates a new key and therefore a new install id. The device key is shared
// with cloud intelligence, so this also signs the machine out of it.
func resetCloudTelemetryID(ctx context.Context, cfg config.Config, c *telemetry.Collector) error {
	gw, err := cloudgateway.Open(cloudgateway.Options{
		CredDir: cloudCredDir(cfg),
		BaseURL: trimRight(cfg.Telemetry.Endpoint, "/"),
	})
	if err != nil {
		return err
	}
	lane, err := gw.Telemetry(func() string { return cfg.Telemetry.EffectiveTier() })
	if err != nil {
		return err
	}
	if err := lane.OwnRevoke(ctx); err != nil {
		return fmt.Errorf("revoke the current install server-side: %w", err)
	}
	if err := gw.ClearCredentials(); err != nil {
		return fmt.Errorf("clear the device key: %w", err)
	}
	if _, err := c.ResetInstallID(ctx); err != nil {
		return err
	}
	return nil
}

func printIndentedJSON(cmd *cobra.Command, raw json.RawMessage) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), string(raw))
		return err
	}
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
