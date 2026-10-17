// Package agentconfigsvc is the I/O boundary of the adapter config editor
// (backlog items 2 and 7, phases P4 and P5;
// docs/plans/adapter-config-and-common-vocabulary-plan-2026-10-03.md
// sections 5 and 7). The pure catalogue, views and hygiene pipeline live in
// internal/agentconfig; this package reads and writes the vendor files and
// records each save in the node-local layer store.
//
// One Service serves the dashboard (internal/intelligence/dashboard) and
// the CLI (`observer agent-config`), so every CLI action has the same
// pipeline as its dashboard twin.
//
// # Reads
//
// A file whose catalogue row is PrivNever (credential stores, editor
// databases, signed caches, Kimi Code's config.toml, ZCode's CLI config)
// is never opened, not even to say whether it exists. Every other file is
// read with a size cap; secrets are masked before anything leaves this
// package (agentconfig.MaskedFile), and values are reported as present /
// absent only.
//
// # Writes (plan section 5.2 steps 8-10)
//
//  1. the cross-process advisory lock every hook-registration writer takes
//     on the same file (internal/hook.LockSettingsFile, injected), so a
//     dashboard save cannot race an `observer start` auto-registration;
//  2. the symlink pin (a link retargeted between read and write is refused);
//  3. a re-read under the lock and a re-run of the hygiene pipeline; the
//     preview etag the caller saw must still match (409 otherwise);
//  4. a backup ring <file>.bak-observer-<stamp> (the routingapply
//     convention, last N kept);
//  5. an atomic write (internal/fsatomic), mode kept;
//  6. a read-back: the bytes on disk must be exactly the bytes planned and
//     must re-parse, else the backup is restored and the save fails;
//  7. the record: layer rows (key, layer, value fingerprint) and a history
//     row naming the changed keys, never their values.
//
// Writes never touch a PrivNever or admin-root file, and on a managed node
// (operator ruling D-102) nothing is written until the organization grants
// editing (phase P6 fills the grants; the Access provider is the seam).
package agentconfigsvc
