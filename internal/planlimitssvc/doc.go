// Package planlimitssvc is the service layer of the Plans & limits module
// (docs/plans-and-limits.md): the daemon's polling loop, the account
// lifecycle (add, edit, remove, the per-provider experimental switch), the
// status line ingest, the one-shot live probe and the page model.
//
// It owns no SQL (the store seam internal/store/planlimits.go does), no
// network code (internal/planlimitsnet does) and no vendor file reads
// (internal/planlimitslocal does); all three, and the keychain
// (internal/keystore), are injected as interfaces. The decision logic it
// applies (ladder, cadence, backoff, suspicious-reset hold, merge) lives in
// the pure internal/planlimits.
//
// Zero egress by default: with no account added, the loop only reads the
// local database; a network call or a vendor process happens only for an
// account the user added, and an experimental source only after the user
// turned on that provider's experimental switch.
package planlimitssvc
