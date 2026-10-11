// Package notifycfg holds the neutral [email] and [digest] config blocks: their
// shape, validation, defaults and (for email) credential resolution.
//
// The node config loader (internal/config) and the org server's
// (internal/orgserver/config) embed these types. They used to live in the
// notify.email feature packages internal/notify/email and
// internal/notify/digest, which made the core config package import a
// removable feature; modular packaging phase PB (rev F1) moved the shape here
// so a build tag can remove the sender and the digest renderer. Both feature
// packages re-export these types as aliases, so callers are unchanged.
//
// Pure: no I/O beyond Resolve's read of a configured credential env var or
// file, no network, no observer-internal imports.
package notifycfg
