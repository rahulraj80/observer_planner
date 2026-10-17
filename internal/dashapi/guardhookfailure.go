package dashapi

// GuardHookFailure is the per-gate-family hook failure mode: true means a
// failing hook of that family blocks the call.
type GuardHookFailure struct {
	ToolGateClosed   bool
	PromptGateClosed bool
}
