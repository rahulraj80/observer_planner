package dashapi

// CursorSemantics tells /api/health/watcher how to interpret one
// parse_cursors row. It is the dashboard-side mirror of
// adapter.FileCursorSemantics, resolved to plain capability flags at
// the wiring boundary (CLAUDE.md module rules #2 and #3) so this
// package never learns adapter types or tool names.
//
// The zero value is NOT a valid default - use byteOffsetSemantics,
// which reproduces the historical assumption that every tracked file
// is an append-only text log tailed by byte offset.
type CursorSemantics struct {
	// Kind is a stable machine-readable tag ("byte_offset",
	// "watermark", "encrypted", "no_actions") surfaced on the wire.
	Kind string
	// LagMeaningful reports whether `file_size - byte_offset` is a
	// real ingest lag. False when the saved cursor is a watermark (a
	// SQLite row id, a Unix-millis timestamp) rather than a byte
	// count - comparing those to a file size is a category error that
	// pins the row "behind" forever.
	LagMeaningful bool
	// ActionsExpected reports whether a non-trivial file of this shape
	// SHOULD have produced at least one action row. False for
	// watermark-scanned stores, undecodable (encrypted) stores, and
	// token/correlation-only files.
	ActionsExpected bool
	// Reason is a short operator-facing explanation of why a signal is
	// suppressed. Surfaced verbatim; may be empty.
	Reason string
}
