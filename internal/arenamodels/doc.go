// Package arenamodels composes the Agent Arena's per-harness model picker
// options (the judge picker and every candidate picker on the node
// dashboard's Terminals > Arena tab).
//
// It is PURE: no SQL, no HTTP, no filesystem. The dashboard handler loads the
// inputs - the harness's capability row, this node's model-usage history
// (internal/store LoadModelUsage) and the compiled price-snapshot catalogue
// (internal/intelligence/cost SnapshotModels) - and Compose turns them into
// one plain, grouped option list. Every decision dispatches on the
// capability's DATA (Model.Kind, Model.Catalog, Headless.ProxyModelPrefix),
// never on a tool name (CLAUDE.md #3):
//
//   - a harness with no grounded model mechanism is not selectable, with a
//     plain reason (it always runs on its own default);
//   - a VENDOR catalogue (Claude Code -> anthropic, Codex -> openai, grok ->
//     xai) offers the models this harness has run on this machine, then the
//     registry's grounded aliases, then that provider family's catalogue
//     models, newest first;
//   - an OPEN (bring-your-own-model) catalogue offers only models this node
//     has actually used, across every tool, most-used first - a catalogue
//     cannot say which providers this machine holds keys for;
//   - a routed lane that pins a provider prefix (opencode's openrouter/)
//     rewrites router-shaped ids onto that prefix and lists the rest as
//     disabled, with the reason, instead of hiding them.
//
// The provider-family -> model-id prefix vocabulary is the families table in
// arenamodels.go, walked top-down.
package arenamodels
