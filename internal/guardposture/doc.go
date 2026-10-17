// Package guardposture is the on-disk contract between the daemon and the
// short-lived `observer hook` process for hook FAILURE SEMANTICS (hook-parity
// audit section 5.7, Lane 0 step 10).
//
// Two files sit beside the global config file (config.ResolveGlobalPath, so
// the hook finds them without parsing any TOML):
//
//   - guard-posture.json: whether guard enforcement is in force on this node
//     and for which gate capabilities. ONE writer, the daemon (on start and on
//     every guard reload / org bundle apply). The hook only reads it, FIRST,
//     before it reads stdin, so it knows whether a failure must block.
//   - guard-hookfail.jsonl: an append-only spool of hook failures that were
//     answered with a fail-closed block (timeout, crash, config, guard_init,
//     unparseable). Writers: hook processes, one O_APPEND line each (well
//     under 4 KiB, so concurrent appends never interleave). ONE drainer, the
//     daemon, which renames the file aside, turns each line into a
//     guard_error verdict and persists it through the one guard_events
//     writer, so a fail-closed block reaches the org on the normal push.
//
// stdlib plus internal/fsatomic only, because it is read on the hook path
// (the budgetwarnfile / govern sidecar precedent); pinned by imports_test.go.
package guardposture
