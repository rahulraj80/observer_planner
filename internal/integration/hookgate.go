package integration

// Capability-shaped registration gate (hook-parity audit section 5.4, Lane
// C2). It replaces the PromptLaneOnly skip: a mechanism is worth writing
// into a vendor config when at least one of its WIRED bindings serves a
// feature that is switched on. Prompt-family bindings need the prompt hook
// lane, tool-family bindings need the tool guard, and every other family
// (lifecycle, observation, delivery outside a prompt) is always worth it,
// as Claude Code's session and tool capture always was.

// HookFeatures says which switchable Observer hook features are on.
type HookFeatures struct {
	// PromptGate: [guard.prompt].enabled && hook_lane (the prompt-submit
	// engine evaluates the hook lane).
	PromptGate bool
	// ToolGate: the tool guard evaluates pre-tool hooks ([guard] not off).
	ToolGate bool
}

// hookFamily is the feature family a binding serves.
type hookFamily int

const (
	familyAlways hookFamily = iota
	familyPrompt
	familyTool
)

// hookCapFamily maps a capability (and, for delivery, its phase) to its
// family. A user message or context injected at prompt time rides the
// prompt event, so it follows the prompt lane; at session start it is
// always worth it.
func hookCapFamily(b HookBinding) hookFamily {
	switch b.Cap {
	case HookCapPromptGate:
		return familyPrompt
	case HookCapUserMessage, HookCapContextInject:
		if b.Phase == HookPhasePre {
			return familyPrompt
		}
	case HookCapToolGate, HookCapToolRewrite:
		return familyTool
	}
	return familyAlways
}

// wanted reports whether f switches on the family b serves.
func (f HookFeatures) wanted(b HookBinding) bool {
	switch hookCapFamily(b) {
	case familyPrompt:
		return f.PromptGate
	case familyTool:
		return f.ToolGate
	}
	return true
}

// WantsRegistration reports whether the mechanism carries a wired binding
// whose feature f switches on: the condition under which `observer start`
// and `observer init` write its vendor config at all (the event list is
// then every Registered binding).
func (h HookSpec) WantsRegistration(f HookFeatures) bool {
	for _, b := range h.Bindings {
		if b.Wired && f.wanted(b) {
			return true
		}
	}
	return false
}

// WiredPromptOnly reports whether every wired binding of the spec is in the
// prompt family (and at least one is wired): the computed form of the
// PromptLaneOnly flag (audit section 5.2).
func (h HookSpec) WiredPromptOnly() bool {
	n := 0
	for _, b := range h.Bindings {
		if !b.Wired {
			continue
		}
		n++
		if hookCapFamily(b) != familyPrompt {
			return false
		}
	}
	return n > 0
}
