package termcore

import "errors"

// ShellTool is the reserved pseudo-tool name a plain-shell fresh launch is
// recorded and reported under (status feed, run history, remote-sensitivity
// classification). It is never a member of the capability registry's
// launchable-tool set, so it can never collide with a real AI-tool name.
// internal/termsvc re-exports it as termsvc.ShellTool.
const ShellTool = "shell"

var (
	// ErrSandboxUnavailable is returned when a caller requests a sandboxed
	// fresh launch but the terminal service was constructed without a
	// sandbox seam (B9 plan sections 7 and 12, amendment A5). A nil seam means
	// the feature is absent; a sandboxed request FAILS CLOSED rather than
	// silently degrading to an unsandboxed launch. Returned BEFORE a run is
	// minted, so no orphan terminal_runs row is created.
	ErrSandboxUnavailable = errors.New("termsvc: sandboxed launch requested but no sandbox seam is configured")
	// ErrWorkspacePrepFailed wraps a sandbox Prepare error (workspace
	// creation, git clone or bwrap-plan failure). Returned before a run is
	// minted, same fail-closed posture as ErrSandboxUnavailable. The dashboard
	// maps this to a 500.
	ErrWorkspacePrepFailed = errors.New("termsvc: sandbox workspace preparation failed")
)
