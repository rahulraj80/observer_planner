// Package toolinstall is the pure planning layer behind the node dashboard's
// guided install / uninstall of AI tools ("Install in terminal" in the New
// terminal dialog, and the matching "Uninstall").
//
// It owns three tables and nothing else (the third, prereq.go, is the
// host-family package-manager table that turns a missing installer
// prerequisite such as unzip into the exact command that installs it):
//
//   - the CHANNEL table (channel.go): for every install channel in the
//     integration registry's closed vocabulary (InstallHint.Channel: npm,
//     script, brew, winget, uv, scoop) how an install argv is read back into a
//     package identity, and whether — and how — that channel has a
//     well-defined, non-destructive uninstall. The uninstall argv is DERIVED as
//     data from the very argv the dashboard executed to install (which the
//     node recorded), never guessed and never supplied by a client. A channel
//     with no defined uninstall (a vendor curl|bash / irm|iex script) carries
//     honest guidance instead of a command.
//   - the LIFECYCLE table (lifecycle.go): the closed status vocabulary of a
//     recorded dashboard install and the allowed transitions between them, so
//     "may this record be uninstalled / forgotten?" is one row lookup.
//   - the RUNTIME table (runtime.go): for each runtime a registry row can
//     require (integration.BinaryResolveSpec.Runtime, today Node.js) its
//     program spelling per OS and grounded install guidance per host family,
//     plus CheckRuntime, which turns one observed `--version` into a closed
//     status (ok / too_old / outside_range / unknown / missing) and the one plain sentence
//     the guided install, every launcher and `observer doctor` show. Which
//     install channels run on the host runtime is a column of the channel
//     table (HostRuntime: npm -> node).
//
// The package is pure: no database/sql, no net/http, no os/exec, no
// fsnotify (pinned by imports_test.go). The record lives in the node-local
// tool_installs table (agent migration 149) behind internal/store; the PTY
// spawn and the exit tracking live in cmd/observer.
package toolinstall
