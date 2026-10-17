package sessionmsg

import (
	"sort"
	"time"
)

// Prompt-shadow fold (T-563).
//
// Some tools record one prompt twice: their prompt-submit hook writes a
// user_prompt row (HookCaptured) and their own transcript writes another for
// the same turn. Codex is the grounding case: the UserPromptSubmit hook row
// is keyed "user:<turn_id>", the transcript row is keyed "user:<turn_id>"
// when the rollout names the turn and by file line and content hash when it
// does not (legacy rollouts, the first turn of some item-only rollouts). Two
// rows, or one row with two prompts, for one thing the developer typed.
//
// When the session's tool declares the shape (DeriveInput.PromptHookShadow,
// from integration Hook.PromptShadowsTranscript, never a tool name) the
// transcript row is canonical and the hook row folds onto it. Pairing is
// one-to-one, as an ordered rule table:
//
//  1. same MessageID: a hook prompt and a transcript prompt that name the
//     same message (the same turn) pair, closest timestamp first;
//  2. same moment: a hook prompt still unpaired pairs with an unpaired
//     transcript prompt within promptShadowWindow of it, closest first,
//     provided that transcript prompt's MessageID names no hook prompt (a
//     transcript row keyed to a turn pairs only with that turn's hook).
//
// A hook prompt with no transcript twin (the transcript was never captured,
// or is not ingested yet) stays a user message exactly as before; a folded
// hook prompt's keys are aliased onto its twin's row so every key join
// (account observations, AliasIndex) still resolves.

// promptShadowWindow bounds rule 2. The hook fires as the prompt is
// submitted and the transcript records it as the turn starts, seconds
// apart; two different prompts of one session are not submitted within it
// while one of them lacks its own twin.
const promptShadowWindow = 30 * time.Second

// promptShadow is one hook-captured prompt set aside by splitPromptShadows
// and the canonical (transcript) prompt it folds onto.
type promptShadow struct {
	hook      ActionRow
	canonical ActionRow
}

// CanonicalPrompts returns the user_prompt rows of actions that remain once
// every hook prompt with a transcript twin has folded onto that twin: one
// row per prompt the developer typed, in input order (T-570). It is the same
// pairing table Derive applies, so a prompt count taken outside Derive (the
// predictor's message boundaries, a project's per-session prompt counts)
// agrees with the session's Messages tab instead of counting a hook and
// transcript pair twice. shadow is the tool's integration
// Hook.PromptShadowsTranscript; false keeps every user_prompt row.
func CanonicalPrompts(actions []ActionRow, shadow bool) []ActionRow {
	kept, _ := splitPromptShadows(actions, shadow)
	out := make([]ActionRow, 0, len(kept))
	for _, a := range kept {
		if a.ActionType == "user_prompt" {
			out = append(out, a)
		}
	}
	return out
}

// splitPromptShadows returns actions without the hook-captured prompts that
// have a transcript twin, plus those pairs. Order of the kept actions is
// unchanged. enabled=false returns actions untouched.
func splitPromptShadows(actions []ActionRow, enabled bool) ([]ActionRow, []promptShadow) {
	if !enabled {
		return actions, nil
	}
	var hooks, canon []int
	hookKeys := map[string]bool{}
	for i, a := range actions {
		if a.ActionType != "user_prompt" {
			continue
		}
		if a.HookCaptured {
			hooks = append(hooks, i)
			if a.MessageID != "" {
				hookKeys[a.MessageID] = true
			}
		} else {
			canon = append(canon, i)
		}
	}
	if len(hooks) == 0 || len(canon) == 0 {
		return actions, nil
	}

	partner := map[int]int{} // hook index -> canonical index
	used := map[int]bool{}   // canonical index already paired
	for _, rule := range promptShadowRules {
		var cands []promptShadowCandidate
		for _, h := range hooks {
			if _, done := partner[h]; done {
				continue
			}
			for _, c := range canon {
				if used[c] {
					continue
				}
				if d, ok := rule(actions[h], actions[c], hookKeys); ok {
					cands = append(cands, promptShadowCandidate{hook: h, canon: c, gap: d})
				}
			}
		}
		sort.SliceStable(cands, func(i, j int) bool {
			if cands[i].gap != cands[j].gap {
				return cands[i].gap < cands[j].gap
			}
			if cands[i].hook != cands[j].hook {
				return cands[i].hook < cands[j].hook
			}
			return cands[i].canon < cands[j].canon
		})
		for _, cd := range cands {
			if _, done := partner[cd.hook]; done || used[cd.canon] {
				continue
			}
			partner[cd.hook] = cd.canon
			used[cd.canon] = true
		}
	}
	if len(partner) == 0 {
		return actions, nil
	}

	kept := make([]ActionRow, 0, len(actions)-len(partner))
	shadows := make([]promptShadow, 0, len(partner))
	for i, a := range actions {
		if c, ok := partner[i]; ok {
			shadows = append(shadows, promptShadow{hook: a, canonical: actions[c]})
			continue
		}
		kept = append(kept, a)
	}
	return kept, shadows
}

// promptShadowCandidate is one admissible (hook, canonical) pairing under a
// rule, with the timestamp gap that orders it.
type promptShadowCandidate struct {
	hook, canon int
	gap         time.Duration
}

// promptShadowRule reports whether hook may fold onto canonical under one
// rule, and the gap that ranks the pairing (closest first).
type promptShadowRule func(hook, canonical ActionRow, hookKeys map[string]bool) (time.Duration, bool)

// promptShadowRules is the ordered rule table described at the top of this
// file; each rule pairs only what the rules before it left unpaired.
var promptShadowRules = []promptShadowRule{
	// 1. same MessageID (same turn).
	func(h, c ActionRow, _ map[string]bool) (time.Duration, bool) {
		if h.MessageID == "" || h.MessageID != c.MessageID {
			return 0, false
		}
		d, ok := timestampGap(h.Timestamp, c.Timestamp)
		if !ok {
			d = 0
		}
		return d, true
	},
	// 2. same moment, transcript prompt not keyed to any hook's turn.
	func(h, c ActionRow, hookKeys map[string]bool) (time.Duration, bool) {
		if c.MessageID != "" && hookKeys[c.MessageID] {
			return 0, false
		}
		d, ok := timestampGap(h.Timestamp, c.Timestamp)
		if !ok || d > promptShadowWindow {
			return 0, false
		}
		return d, true
	},
}

// timestampGap is |a-b| for two RFC3339 timestamps; ok=false when either
// does not parse (such a pair never matches on time).
func timestampGap(a, b string) (time.Duration, bool) {
	ta, err := time.Parse(time.RFC3339Nano, a)
	if err != nil {
		return 0, false
	}
	tb, err := time.Parse(time.RFC3339Nano, b)
	if err != nil {
		return 0, false
	}
	return absDuration(ta.Sub(tb)), true
}

// aliasPromptShadows points each folded hook prompt's keys at the row its
// canonical twin landed on, so a join on the hook row's message_id or
// source_event_id finds the one user message.
func aliasPromptShadows(state *deriveState, shadows []promptShadow) {
	for _, s := range shadows {
		row, ok := state.byKey[firstNonEmpty(s.canonical.MessageID, s.canonical.SourceEventID)]
		if !ok {
			continue
		}
		row.alias(s.hook.MessageID)
		row.alias(s.hook.SourceEventID)
	}
}
