package termcore

// Kind classifies how a run was started. The kinds are structurally distinct
// so a handoff-continue is never confused with a fresh agent launch.
// internal/termrun re-exports it as termrun.Kind.
type Kind string

const (
	// KindHandoff is a "continue this session" launch (--continue-from). It
	// carries a source session id (the session continued FROM), which is NOT a
	// correlation target.
	KindHandoff Kind = "handoff"
	// KindFresh is a fresh agent launch with no source session. It has no
	// session id at all until the tool creates one after startup.
	KindFresh Kind = "fresh"
	// KindAttach is a daemon-owned PTY spawned on behalf of the operator's own
	// terminal via the attach socket (session-attach design section 3, Phase
	// 1). Unlike KindFresh, the spawn is requested by the CLI attach client
	// over the owner-only AF_UNIX socket rather than by a dashboard POST; it is
	// recorded and feed-published like every other run so an attach session is
	// a first-class run identity.
	KindAttach Kind = "attach"
	// KindResume is a NATIVE resume launch (session-attach design section 3,
	// Phase 3): the dashboard spawns a daemon-owned PTY running the tool's own
	// resume mechanism (claude `--resume <id>`, codex `resume <id>`) so a
	// CLOSED session reopens its ACTUAL prior conversation, NOT a distilled
	// fork (that is KindHandoff). Like KindHandoff it carries the resumed
	// session as SourceSessionID (the session reopened FROM), which is NOT a
	// correlation target. Unlike the CLI attach bypass it is a
	// dashboard-initiated launch, so termsvc gates it through the SAME
	// fresh-launch Policy allow-lists.
	KindResume Kind = "resume"
	// KindSSH is an outbound SSH remote-system shell
	// (docs/plans/ssh-remote-profiles-plan-2026-08-27.md). The daemon spawns
	// the OpenSSH client inside its own PTY harness, so the bytes on the
	// terminal come from a shell on ANOTHER machine. It has no source session,
	// no correlated target session (the trusted out-of-band control channel is
	// an inherited fd, and file descriptors do not cross an SSH connection),
	// and no local project root: all three record as honest zero values
	// rather than as guesses.
	//
	// Distinct from the INBOUND [remote]/Tailscale feature, which exposes this
	// node's own dashboard to a paired device. Same word, opposite direction.
	KindSSH Kind = "ssh"
	// KindGUI is a DETACHED IDE / desktop-app launch
	// (docs/plans/ide-desktop-launch-plan-2026-09-03.md). The daemon spawns the
	// app outside any PTY (Windows DETACHED_PROCESS, Unix setsid, darwin
	// `open -a`) so it outlives the daemon and owns its own window; the run row
	// exists to record WHAT was launched, with which pid, and whether the
	// routing wrap actually reached it.
	//
	// Structurally unlike every other kind: there is NO PTY handle, so a GUI run
	// never enters the handle maps, never appears in a terminal Snapshot, and is
	// never joinable ("Jump in" is a PTY affordance). It has no source session
	// and no correlation nonce: the child is a GUI, not an `observer <verb>`
	// launcher, so there is no trusted out-of-band channel to echo one on.
	//
	// NOT remote-view sensitive: the remote-sensitivity table gates who may READ
	// a daemon-owned PTY's bytes, and a GUI run has no bytes to read.
	KindGUI Kind = "gui"
)

// Valid reports whether k is a known kind.
func (k Kind) Valid() bool {
	return k == KindHandoff || k == KindFresh || k == KindAttach ||
		k == KindResume || k == KindSSH || k == KindGUI
}

// remoteSensitiveKinds is the set of run kinds classified as remote-VIEW
// sensitive (session-attach design section 3.2). Each binds a daemon-owned
// terminal to a REAL external transcript or shell (an attach session the
// operator started outside the dashboard, a native reopen of a closed
// session's ACTUAL prior conversation, or a live shell on another machine)
// whose TUI can echo API keys or customer data. A fresh/handoff launch has no
// such pre-existing transcript, so it is not remote-sensitive. Whether a
// remote caller may SEE a sensitive-kind PTY is governed by the
// [remote].allow_terminal_view toggle, which DEFAULTS TRUE; set
// allow_terminal_view = false to restore the deny-read posture. This table
// only marks the kinds the toggle applies to; it is a data set (not an
// if-chain) so a new sensitive kind is one row (CLAUDE.md #5).
var remoteSensitiveKinds = map[Kind]bool{
	KindAttach: true,
	KindResume: true,
	KindSSH:    true,
}

// IsRemoteSensitiveKind reports whether a run kind is remote-VIEW sensitive:
// its live PTY is hidden from a remote-exposed snapshot and its websocket
// refused for a remote caller WHEN [remote].allow_terminal_view is off
// (section 3.2). Used at BOTH the dashboard visibleSnapshot / WS gates and
// the cmd adapter's IsRemoteSensitiveSession so the two dispatch on ONE
// shared table, never a per-site if-chain. An unknown kind is not sensitive
// (fresh/handoff are the honest non-sensitive floor).
func IsRemoteSensitiveKind(k Kind) bool { return remoteSensitiveKinds[k] }
