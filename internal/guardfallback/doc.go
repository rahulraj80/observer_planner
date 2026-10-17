// Package guardfallback is the closed vocabulary for the organization's two
// guard FALLBACK choices (operator rulings 2026-10-04, decision D-221):
//
//   - user_policy_invalid: what a managed node does while the developer's own
//     guard-policy.toml does not load. "loud" (the default, T-323) keeps the
//     built-in rules and the org bundle in force and makes the failure loud;
//     "fail_closed" also closes every ENFORCED gate hook until the file loads
//     again (T-327).
//   - sandbox_unavailable: what a proxied, launchable tool degrades to when its
//     enforcement lever is the filesystem sandbox and no sandbox can be built
//     on the machine. "recorded_acceptance" (the default) launches it
//     unsandboxed, as before; "org_disallow" refuses the launch (D-178).
//
// Both travel as a `[fallbacks]` table inside the ORG-WIDE signed guard
// bundle, so every node reads them through the one verified, key-pinned,
// monotonic rail it already reads its org guard floor from, and an older
// agent that predates the table tolerates it as an unknown key (GUARD-FWD-1)
// and keeps both defaults.
//
// The package is pure: no SQL, no HTTP, no filesystem. It owns the key and
// value table, decoding, strictest-wins folding, the tenancy gate (an
// individual node always runs the defaults) and the text splice the
// dashboard uses to rewrite the table inside an authored bundle without
// disturbing the admin's rules or comments.
package guardfallback
