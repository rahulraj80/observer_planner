// Package codextrust classifies Observer's Codex hook entries against
// Codex's per-hook trust state (hook-parity T-039).
//
// Codex (0.129.0+, still required on 0.160) dispatches a hooks.json entry
// only after the user trusts it inside Codex (`/hooks`). The approval is
// recorded in ~/.codex/config.toml under
// `[hooks.state."<hooks.json path>:<snake_event>:<group>:<handler>"]` as a
// `trusted_hash` (and an optional `enabled = false` when the user turns
// the entry off). The hash covers the entry's normalized body, so an
// entry whose command changed after it was trusted is untrusted again.
//
// This package reproduces that hash (Hash) so a changed entry can be told
// apart from a trusted one, and classifies every event Observer registers
// (Classify), including an event Observer expects but hooks.json lacks.
// It is DETECTION ONLY: nothing here writes Codex's config, and Observer
// never trusts a hook on the user's behalf (decision D-148).
//
// Pure: callers pass file bytes; no file, SQL, HTTP or fsnotify access.
package codextrust
