// Package hookdisable detects the vendor-side switches that silence every
// hook a vendor runs, Observer's included (hook-parity T-046).
//
// A single `"disableAllHooks": true` in ~/.claude/settings.json silences
// Observer on Claude Code AND on every host that replays Claude Code's hook
// settings (cursor-agent, Grok Build, Copilot CLI, VS Code Copilot, Devin
// CLI, DeepSeek Harness). Nothing in Observer's own files changes, so
// neither the hook integrity check nor the registrar notices. This package
// is the one table of such switches; `observer doctor` reports a finding
// and the daemon persists a guard event (which reaches the org on the
// normal guard_events push) when a switch turns on.
//
// Pure: the file read is injected, so the package touches no filesystem,
// database or network itself (pinned by TestImportsArePure).
package hookdisable
