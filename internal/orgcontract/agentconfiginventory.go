package orgcontract

import (
	"regexp"
	"strings"
)

// Managed-node AI tool configuration INVENTORY (operator ruling D-112, T-427):
// the organization sees everything on a managed node, not only what its own
// policy set. A managed node reports every catalogued setting it finds in its
// AI tools' settings files (with the value) and every instruction-family file
// (instructions, skills, agents, commands, rules, with the text), carried on
// the push envelope as PushEnvelope.AgentConfigInventory.
//
// The row is the state row's shape (AgentConfigStateRow) with State fixed to
// AgentConfigInventoryState, so the privacy allow-list pinned by
// tests/invariant stays one list. Values ship under the T-428 rule: a
// personal (local-only) file's value ships like any other, and a
// secret-shaped value ships SCRUBBED (the raw secret never leaves the node).
// The whole report ships ONLY when the node ships raw content
// (store.ShareOptions.shipsRawContent) and only from a managed enrolment; an
// individual node never composes it, and the server refuses it from a
// subject without a managed enrolment.
//
// FileID names a catalogue file row, or for a concrete instruction-family
// file the row's tool and kind with the file's path relative to its root
// ("claude-code/skill:~/.claude/skills/review/SKILL.md" for a user file,
// "claude-code/instructions:CLAUDE.md" for a project file). A project file
// carries the project identity hash, never the local root.
//
// COMPATIBILITY: an optional envelope field (omitempty). An older server's
// lenient push decode ignores it; an older node never sends it.

// AgentConfigInventoryState is the State every inventory row carries. It is
// not an agentconfigorg.State: the server keeps the inventory apart from the
// applied state of the org's directives.
const AgentConfigInventoryState = "observed"

// AgentConfigInventoryMaxRows caps one inventory report.
const AgentConfigInventoryMaxRows = 3000

// AgentConfigInventoryMaxBytes caps the total size of the values one report
// carries. Rows past the budget keep their hash and drop the value (Reason
// "value_budget").
const AgentConfigInventoryMaxBytes = 1 << 20

// The closed Reason codes an inventory row may carry.
const (
	// AgentConfigInventorySecret: the value was a secret; it ships scrubbed.
	AgentConfigInventorySecret = "secret_scrubbed"
	// AgentConfigInventoryTruncated: the text was longer than
	// AgentConfigObservedMaxBytes and ships cut.
	AgentConfigInventoryTruncated = "truncated"
	// AgentConfigInventoryBudget: the report's value budget ran out; the
	// row keeps its hash only.
	AgentConfigInventoryBudget = "value_budget"
)

// AgentConfigInventoryReport is one managed node's AI tool configuration.
type AgentConfigInventoryReport struct {
	// Digest is a hash of the rows without their timestamps, so a node
	// sends an unchanged inventory once and the server skips a repeat.
	Digest string `json:"digest"`
	// Rows is the complete inventory (a row missing from a report is no
	// longer on the machine: the server replaces the node's inventory).
	Rows []AgentConfigStateRow `json:"rows"`
	// Truncated: more than AgentConfigInventoryMaxRows rows existed.
	Truncated bool `json:"truncated,omitempty"`
}

// AgentConfigInventoryKindSetting returns the Setting of an inventory row for
// an instruction-family file of one tool and kind ("claude-code/@skill").
func AgentConfigInventoryKindSetting(tool, kind string) string {
	return tool + "/@" + kind
}

var inventoryUnsafe = regexp.MustCompile(`[^A-Za-z0-9._/@:*~\-\[\]]`)

// AgentConfigInventoryFileID builds the FileID of a concrete instruction-
// family file: "<tool>/<kind>:<rel>", with every character outside the
// server's id alphabet replaced by "_" and the whole capped at 256 bytes.
// rel is relative to the user's home ("~/...") or to the project root.
func AgentConfigInventoryFileID(tool, kind, rel string) string {
	rel = strings.ReplaceAll(rel, "\\", "/")
	id := inventoryUnsafe.ReplaceAllString(tool+"/"+kind+":"+rel, "_")
	if len(id) > 256 {
		id = id[:256]
	}
	return id
}
