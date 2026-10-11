// Package judgeclient is the one LLM-as-judge client: an OpenAI-compatible
// /chat/completions caller (Client), the remote-egress guardrails
// (SecureEgress: secret redaction and a payload cap for any non-loopback
// judge), the URL-derived hosting label and the shared-block-plus-override
// config resolution.
//
// It used to live in cmd/observer next to the Plane A observability wiring
// (obs_wire.go). Modular packaging phase PB split it into its own component
// (judge.client, decision D-408) so the Projects alignment judge tier, which
// stays in the developer distro, does not depend on Plane A observability,
// which D-389 moved out of it. The obs eval and admission judges use the same
// client.
//
// It imports no internal/obs package. Network access happens only inside
// Client.Complete, for a judge the operator configured.
package judgeclient
