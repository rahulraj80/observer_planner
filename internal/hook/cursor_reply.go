package hook

// cursorReply is Cursor's hook reply dialect. It is not adapter.cursor's
// code: the shared tool gate (toolgate.go) and the fail-closed answer
// (failclosed.go) speak it for any host that uses the cursor dialect, so it
// stays compiled when cursor.go (the cursor capture receiver) is left out
// with sbo_omit_adapter_cursor (lane MP-L1D, T-598).
//
// cursorReply is the response shape we emit. Cursor accepts both
// "permission" (for tool gating) and "continue" (for prompt gating); extra
// keys are ignored, so a single reply works across all events.
type cursorReply struct {
	Permission string `json:"permission,omitempty"`
	Continue   bool   `json:"continue,omitempty"`
}
