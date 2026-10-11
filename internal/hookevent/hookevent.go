package hookevent

import "github.com/marmutapp/superbased-observer/internal/integration"

// Event is one hook invocation, normalized at the receiver boundary.
type Event struct {
	// Cap and Phase are the binding's capability and phase.
	Cap   integration.HookCap
	Phase string
	// Tool is the registry tool id; NativeEvent the vendor's event name.
	Tool        string
	NativeEvent string
	// SessionID is the vendor's session / conversation id ("" when the
	// payload carries none: ask-once cannot be keyed on it).
	SessionID string
	// Cwd is the host's working directory; ProjectRoot the resolved
	// project root when the payload states one ("" otherwise).
	Cwd         string
	ProjectRoot string
	// ActionType and Operand are the canonical action type and operand
	// selector (internal/tooltax) of the tool a gate event is about;
	// Target is the operand's value.
	ActionType string
	Operand    string
	Target     string
	// Prompt is the developer's prompt text on a prompt event.
	Prompt string
	// PID is the agent process id when an in-process plugin passes it;
	// ProcRel says how the hook relates to the agent process.
	PID     int
	ProcRel integration.ProcRelation
	// Truncated reports a payload cut at the read bound; FieldMissing a
	// payload that parsed but lacked the field the dialect reads (schema
	// drift, never "nothing found").
	Truncated    bool
	FieldMissing bool
}

// FromBinding starts an Event from the binding the receiver resolved: the
// capability, phase, tool, native event and process relation. The receiver
// fills the payload-derived fields.
func FromBinding(tool string, b integration.HookBinding) Event {
	return Event{Cap: b.Cap, Phase: b.Phase, Tool: tool, NativeEvent: b.NativeEvent, ProcRel: b.ProcRel}
}

// Decision is a capability handler's verdict.
type Decision string

const (
	// DecisionAllow lets the host proceed.
	DecisionAllow Decision = "allow"
	// DecisionAsk asks the developer to confirm (native ask, or ask-once).
	DecisionAsk Decision = "ask"
	// DecisionDeny blocks the action or prompt.
	DecisionDeny Decision = "deny"
)

// Outcome is a capability handler's answer, rendered by the binding's
// reply dialect.
type Outcome struct {
	Decision Decision
	// Reason is the text the host shows the agent / developer with a
	// non-allow decision.
	Reason string
	// UserMessage is non-blocking text the developer sees.
	UserMessage string
	// Context is text appended to the model's context.
	Context string
	// ModifiedInput replaces the tool input (tool rewrite).
	ModifiedInput map[string]any
	// ExitCode is the process exit code an exit-code dialect signals the
	// decision with (0 = allow).
	ExitCode int
}

// Allow is the outcome every non-gate capability, and every gate with
// nothing to say, answers with.
func Allow() Outcome { return Outcome{Decision: DecisionAllow} }
