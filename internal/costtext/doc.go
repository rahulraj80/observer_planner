// Package costtext is the ONE Go-side seam that renders a developer's cost
// figures and cost-bearing text under the organization's developer cost
// display (operator ruling D-013, 2026-10-02: admins always see dollars; an
// admin chooses whether developers see dollar costs or only token usage).
//
// It is the Go twin of the node dashboard's formatter seam
// (shared/lib/format.ts): the mode is resolved elsewhere, once, through
// store.ResolveDeveloperCostDisplay (the same answer GET /api/cost-display,
// the statusline and the guard's budget text read), and every surface that
// renders money outside the browser asks this package how:
//
//   - Tokens(mode)            whether figures render as token usage.
//   - MaskDollars(mode, s)    hides the dollar amounts inside prose, never
//     converting them (a dollar figure cannot be re-denominated
//     into tokens without fabricating a number).
//   - IsDollarKey(key)        the one rule for which structured fields
//     (JSON keys, CSV columns) carry dollars.
//   - StripDollarFields(v)    drops those fields from decoded JSON and masks
//     the strings that remain.
//   - NewWriter(dst, mode)    an io.Writer for a CLI report: in token mode it
//     shapes a JSON document (or JSON lines) through StripDollarFields and
//     masks every other line, keeping table columns aligned.
//
// Pure: no SQL, no HTTP, no file system, no fsnotify (pinned by
// imports_test.go). The no-bare-usd guard (cmd/observer/no_bare_usd_test.go)
// fails when a Go file renders a dollar figure without being classified.
package costtext
