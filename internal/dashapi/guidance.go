package dashapi

// guidanceToolsMeasurable is THE static answer to "can Observer tell you
// whether this tool actually USED a guidance file?" - a per-tool capability
// flag, not a per-tool behaviour branch (CLAUDE.md #3: the surface renders
// from the flag, it does not switch on the tool name).
//
// true means the tool emits a capture signal Observer already ingests that
// names a guidance file, AND that signal's target is GROUNDED against a real
// transcript - because since wave 2 this flag no longer merely enables a
// sentence, it enables a per-file COUNT, and a count of zero prints as "never
// invoked". Claiming measurability for a signal whose target might be empty or
// might be a path spelled differently from the inventory turns an unverified
// join into a fabricated statement of disuse. That is worse than saying
// nothing.
//
// Only claude-code clears that bar today:
//   - `skill_invoke` from the `Skill` tool call, whose target is the skill
//     NAME (internal/adapter/claudecode/adapter.go::extractTarget, grounded
//     against a live transcript in wave 2), and
//   - `instructions_loaded` from its hook (cmd/observer/hook.go,
//     buildClaudeInstructionsLoadedEvent), whose target is the file path.
//     No other adapter produces this signal at all.
//
// DELIBERATELY NOT LISTED, pending grounding: deepseek, muse, mistral-code and
// freebuff each map a native skill call onto models.ActionSkillInvoke
// (internal/tooltax/table.go), so the ACTION exists - but their targets come
// from adapter-specific extraction that has never been checked against a real
// session, and may be a path or empty. Each becomes `true` here when a
// per-adapter fixture test pins what its skill_invoke target actually
// contains, not before. Until then those tools render "usage is not
// measurable", which is exactly true.
//
// A tool absent from this map is false: absence is the zero value and the zero
// value is the honest one.
var GuidanceToolsMeasurable = map[string]bool{
	"claude-code": true,
}
