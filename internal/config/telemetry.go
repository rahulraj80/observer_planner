package config

import (
	"os"
	"strings"
)

// DefaultTelemetryEndpoint is the central telemetry ingest the [telemetry]
// seed points at: the hosted cloud's anonymous telemetry lane (telemetry plan
// Step 5.2, revised 2026-10-17; /v1/telemetry/* on observer-cloud), reached
// with the device key alone. A node registered with a self-hosted
// observer_reporter (an install token from `observer telemetry register`)
// keeps sending there instead. Overridable for a mirror or a test.
const DefaultTelemetryEndpoint = "https://cloud.superbased.app"

// TelemetryConfig is the [telemetry] surface — the central telemetry rail
// CLIENT collector (telemetry-rail plan §D2,
// docs/plans/telemetry-rail-plan-2026-09-13.md, canonical in
// observer_reporter). The collector batches consent-tiered, content-free
// events to the independent central ingest (:48089, observer_reporter).
//
// Consent model (telemetry plan Step 5.7, 2026-10-18): the content-free
// baseline tier is ON by default (config Default()); the zero value of the
// struct is still off. Higher rungs (enrichment, deep) need an explicit
// `observer telemetry consent <tier>`. The whole rail is gated by Enabled
// and by the OBSERVER_TELEMETRY kill switch (hard stop, see
// KillSwitchActive).
//
// LOCAL-ONLY config for an OPT-IN egress rail: it is never distributed via
// the org policy registry, and the org plane has no authority over it (the
// server's own consent floor is fetched at batch time and can only
// DOWNGRADE what this node sends, never upgrade — enforced in the
// collector, not by trust).
type TelemetryConfig struct {
	// Tier is the consent tier: off | baseline | enrichment | deep, the
	// telemetry rungs of the one sharing ladder (contract vocabulary
	// contract.ValidTiers). Default `baseline` (Step 5.7). `off` collects
	// nothing.
	// `baseline` = the content-free counters and prompt-cycle sizes/timings.
	// `enrichment` is reserved for the opt-in enrichment events. `deep`
	// additionally allows the deep-diagnostics events, which carry their own
	// disclosure. Legacy `usage` reads as baseline; `error` (error classes
	// only) is still accepted but no longer offered.
	Tier string `toml:"tier"`
	// Endpoint is the central ingest base URL. Default
	// DefaultTelemetryEndpoint (the hosted cloud lane, /v1/telemetry/*); a
	// self-hosted observer_reporter speaks /v1/register, /v1/ingest and
	// /v1/consent/config instead.
	Endpoint string `toml:"endpoint"`
	// Enabled gates the daemon-side collector loop (staging drain +
	// batcher + transport). Default true with the baseline tier (Step 5.7);
	// `enabled = false` stops the loop while keeping the consent setting.
	Enabled bool `toml:"enabled"`
	// BatchIntervalSeconds is the target batch cadence in seconds (plan
	// §D3/A3: 10–15 min ± jitter). Default 900 (15 min); the collector
	// jitters ±10% around it and ALSO fires early when staged bytes reach
	// MaxBatchBytes. Ignored when Enabled is false.
	BatchIntervalSeconds int `toml:"batch_interval_seconds"`
	// MaxBatchBytes is the compressed-payload cap in bytes (plan §D3/A3:
	// ~256 KB). Default 262144. A batch is cut BEFORE exceeding this so no
	// single upload grows past the cap; events that do not fit stay staged
	// for the next cycle.
	MaxBatchBytes int `toml:"max_batch_bytes"`
	// InstallTokenFile is the path (relative paths resolve against the DB
	// directory) holding the minted install token from POST /v1/register.
	// Managed by `observer telemetry register` / auto-registration; 0600.
	// NEVER distributed, never logged.
	InstallTokenFile string `toml:"install_token_file"`
}

// EffectiveTier returns the configured tier normalized to the contract
// vocabulary with the off default applied ("" and unknown → off; callers
// re-validate against contract.ValidTier before emitting).
func (t TelemetryConfig) EffectiveTier() string {
	switch t.Tier {
	case "baseline", "enrichment", "error", "usage", "deep":
		return t.Tier
	default:
		return "off"
	}
}

// KillSwitchEnv is the environment variable that hard-stops the telemetry
// rail regardless of config (plan §D2: OBSERVER_TELEMETRY=off kills emit
// AND network). Any value other than "" (unset) / "on" is treated as off —
// a kill switch must fail closed, so "0", "false", "no", "off" and any
// typo all disable.
const KillSwitchEnv = "OBSERVER_TELEMETRY"

// TelemetryKillSwitchActive reports whether the OBSERVER_TELEMETRY env
// kill switch is engaged. Unset/"" and explicit "on" (case-insensitive)
// leave the config tier in charge; EVERYTHING else — including "0",
// "false", "off" and any unrecognized value — is a hard stop.
func TelemetryKillSwitchActive() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(KillSwitchEnv)))
	if v == "" || v == "on" {
		return false
	}
	return true
}
