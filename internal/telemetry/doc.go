// Package telemetry is the telemetry rail W2 CLIENT collector
// (telemetry-rail plan §D2, canonical plan in observer_reporter
// docs/plans/telemetry-rail-plan-2026-09-13.md). It derives consent-tiered,
// content-free contract events from the node's OWN local DB — the pull
// model: the observer's durable capture tables are the staging source of
// truth, so the collector adds no write path on the capture side and a
// crash loses nothing that capture did not already lose.
//
// Consent (ruling A2): tier `off` is the default in every path — the
// collector emits nothing, stages nothing, and sends nothing until the
// operator escalates the tier AND enables the loop. The OBSERVER_TELEMETRY
// env kill switch hard-stops everything (config.TelemetryKillSwitchActive).
// The server's consent floor (GET /v1/consent/config) can only DOWNGRADE
// what a node sends, never upgrade — enforced here, not by trust.
//
// Privacy shape: every field in a derived event is a closed-vocabulary
// string, a count, or a duration — there is no free-text field in the base
// tier by construction. Deep-tier events are only derived from data the
// local DB already holds, stay length-capped, and are purged first on any
// tier downgrade.
package telemetry
