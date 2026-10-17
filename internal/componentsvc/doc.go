// Package componentsvc is the ONE writer of the node's [components]
// selection (modular packaging plan section 6.1, phase P1, lane MP-GATE).
//
// Every surface that changes which components are active goes through
// Service: `observer components select|add|remove`, `observer init
// --components/--with/--without`, and the dashboard's Settings > Components
// page (lane MP-DASH). A change is previewed first (Service.Preview: what
// activates, what deactivates, which dependants go with it, the ordered
// teardown of every write a deactivated component made, and whether a
// restart is needed), then applied (Service.Apply: the config write through
// config.PatchFile's comment-preserving structural patches, the teardown
// through the existing owners, and one audit row). An Apply names the
// preview's Fingerprint so a stale preview is refused rather than applied.
//
// Teardown never edits another product's file itself: it calls the existing
// removers (internal/clientwire for hooks, MCP entries, proxy routes and
// telemetry exporters; internal/browserhost for the native-messaging host),
// behind the Teardowner seam so tests never touch a real home.
//
// Data is kept on removal (operator ruling Q7). Purging a removed
// component's rows is a separate, previewed, two-step action (PurgePreview,
// then PurgeExecute naming the plan id) through the typed purge plans in
// internal/store/componentpurge.go, always node-local (no org tombstones).
package componentsvc
