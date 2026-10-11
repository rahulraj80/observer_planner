package dashapi

// TerminalRoot is the ONE answer the panel's token→directory seam returns: the
// directory the project panel browses for a launch token, plus the PROVENANCE
// of that directory. Both halves travel together so the surface can never
// disclose a path without disclosing what kind of path it is.
//
// Provenance, not permission (operator ruling 2026-08-28). Authorized=true means
// the run was launched into a root the operator allow-listed under
// [terminal.launch].allowed_project_roots. Authorized=false means the run was
// launched with the default cwd and the panel is serving the directory the
// child actually runs in (the daemon's own working directory). BOTH are
// browsable: the allow-list governs which roots a dashboard client may REQUEST
// at launch time, not which directory an already-running, owner-local terminal
// may browse. Every per-request containment guard (fsview traversal/symlink
// re-verification, gitview argv hardening, the remote allow_terminal_view gate)
// applies identically to either root.
type TerminalRoot struct {
	// Path is the canonical, symlink-resolved directory to browse. Empty means
	// "nothing to browse" (an SSH run, whose cwd is on another machine, or a
	// daemon whose own cwd was unreadable) → 409 no_project_root.
	Path string
	// Authorized reports whether Path is an operator allow-listed project root
	// (true) rather than the run's factual working directory (false).
	Authorized bool
}
