// Package telemetrywire plans the opt-in edit that points an AI tool's own
// OpenTelemetry exporter at this node's OTLP receiver, and its removal
// (T-537, operator ruling D-343: "exporter wiring, opt-in per tool").
//
// It is PURE: every operation takes a config file's bytes and returns the
// new bytes, never touching the filesystem (the one writer is
// internal/clientwire, which owns every AI-client integration write and
// takes the shared settings-file lock). No SQL, HTTP, fsnotify or store
// import; pinned by imports_test.go.
//
// Which edit a tool gets is the integration registry's OTLP column
// (integration.OTLPWire.Kind), never a tool name. Three shapes exist, one
// per documented exporter config:
//
//   - env block (Claude Code settings.json "env"): four OTEL keys;
//   - telemetry object (Gemini CLI / Qwen Code settings.json "telemetry");
//   - a marked [otel] table appended to Codex config.toml.
//
// Every edit is minimal and byte-preserving outside Observer's own subtree
// (the D-268 / D-270 credential-file rule: some of these files also hold the
// vendor's credentials, so only the named keys change, unrelated keys keep
// their order and bytes, and no value from the file is ever returned in a
// message). An edit that would overwrite something the user configured is
// refused as a conflict, an edit with nothing to change returns the input
// unchanged (Changed false, so the caller writes nothing), and a removal
// only removes what equals Observer's own wiring.
package telemetrywire
