// Package pluginshim embeds the ONE TypeScript shim that bridges every
// in-process plugin host (OpenCode, Kilo CLI, pi, OpenClaw, DeepSeek
// Harness, commandcode Mods) to `observer hook <tool> <event>` (hook-parity
// audit 2026-10-02, Lane D).
//
// The shim is a single go:embed'd file. Render substitutes one JSON profile
// literal into it: the host's plugin contract ("shape"), the native events
// the install subscribes to (each mapped to the abstract capability it
// provides, taken from the internal/integration bindings by the caller), the
// gate and observe deadlines, the observer binary and config, and the guard
// posture / failure spool paths the shim consults when observer cannot
// answer. Nothing else differs between hosts: the per-host glue is data
// (Hosts) plus one adapter function per plugin contract inside the shim.
//
// The package is pure apart from the small file helpers that place the
// rendered shim (the commandcodemod / hermesplugin precedent): no SQL, no
// HTTP, no fsnotify, no internal/integration import (the caller passes the
// subscribed events in).
package pluginshim
