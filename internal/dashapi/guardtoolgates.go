package dashapi

// GuardToolGate is one hook_block tool's state on this node.
type GuardToolGate struct {
	Tool string
	// Channel is the tool's enforcement channel on this node
	// (integration.EnforcementChannel's vocabulary).
	Channel string
	// Current is true when the tool-gate hook is registered and current.
	Current bool
	// Installed is false when the tool has no config home on this machine.
	Installed bool
	// Reason says why the hook does or does not hold.
	Reason string
}
