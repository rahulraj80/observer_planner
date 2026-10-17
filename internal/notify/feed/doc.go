// Package feed is the PURE aggregation layer of the node dashboard's
// notification bell (D-323, T-424): the developer-side sibling of the org
// dashboard's budget alert bell.
//
// It turns facts the node ALREADY records (a budget threshold crossing, an
// access grant issued / expiring / no longer honoured, a queued access
// request that could not be sent, a guard denial or ask, a prompt held for
// confirmation, a cache about to expire, an update to act on, an org
// announcement or policy change, a stalled ingest) into one ordered list of
// notifications. Build walks ONE rule table (Rules) top-down, one rule per
// kind; each rule names the dashboard section that owns it, and a rule whose
// section the organization hid on this machine produces nothing (a managed
// developer is never notified about a page they cannot open). Links into a
// hidden page are dropped too.
//
// The package does no I/O (module discipline #1: no database/sql, net/http or
// fsnotify, pinned by imports_test.go). The dashboard handler loads the facts
// through the store seam and the existing loaders and hands them in as plain
// values; read / dismiss state is applied by ApplyState from whatever the
// state seam (internal/notify/feedstate) persisted.
//
// Money is never rendered here: a figure that is money travels as a number
// (Notification.USD) and the dashboard formats it under the developer's cost
// display, so a tokens-only node never sees a dollar sign from the bell.
package feed
