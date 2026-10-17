package llmrun

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/briefing"
	"github.com/marmutapp/superbased-observer/internal/intelligence/learn"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// Prompt construction. BOTH prompts are built from a CONTENT-FREE projection
// of the briefing (numbers, tool names, opaque session hashes, opportunity
// titles/savings) — never raw transcripts, file paths, or prompt text. Remote
// connections are forced onto this evidence-only input by construction
// (spec §3.6 / open decision Q3).

// evidenceProjection renders the briefing's numbers as a compact text block
// the model can reason over. Deterministic: sorted activity/session/opps,
// name-sorted per-session model mixes.
func evidenceProjection(b briefing.Briefing) string {
	var sb strings.Builder
	g := b.AtGlance
	fmt.Fprintf(&sb, "date: %s\n", b.Date)
	fmt.Fprintf(&sb, "sessions: %d; turns: %d; spend_usd: %.2f\n", g.Sessions, g.Turns, g.SpendUSD)
	fmt.Fprintf(&sb, "input_tokens: %d; output_tokens: %d; cache_hit_rate: %.1f%%\n", g.InputTokens, g.OutputTokens, g.CacheHitRate*100)
	fmt.Fprintf(&sb, "error_turns: %d; active_hours: %.1f; fast_share: %.1f%%\n", g.ErrorTurns, g.ActiveHours, g.FastShare*100)

	if tr := b.Trend; tr != nil && tr.WindowDays > 1 {
		// no-bare-usd:allow (observer_planner fork; not yet masked under the token display, see CONTINUATION.md)
		fmt.Fprintf(&sb, "trend_vs_prior_%d_days: spend $%.2f -> $%.2f (%.1f%%), turns %d -> %d (%.1f%%), sessions %d -> %d, cache_hit_rate %.0f pts, error_rate %.1f%% -> %.1f%%, ttft_p95 %dms -> %dms\n",
			tr.WindowDays, tr.SpendPriorUSD, tr.SpendUSD, tr.SpendDeltaPct,
			tr.TurnsPrior, tr.Turns, tr.TurnsDeltaPct, tr.SessionsPrior, tr.Sessions,
			tr.CacheHitRateDelta*100, tr.ErrorRatePrior*100, tr.ErrorRate*100, tr.TTFTP95MS-tr.TTFTP95DeltaMS, tr.TTFTP95MS)
	}

	sb.WriteString("activity:\n")
	for _, a := range b.Activity {
		fmt.Fprintf(&sb, "  %s: sessions=%d turns=%d spend_usd=%.2f\n", a.Tool, a.Sessions, a.Turns, a.SpendUSD)
	}

	if len(b.TopSessions) > 0 {
		sb.WriteString("top_sessions:\n")
		for _, s := range b.TopSessions {
			fmt.Fprintf(&sb, "  %s: tool=%s turns=%d cost_usd=%.2f errors=%d\n", truncateID(s.ID), s.Tool, s.Turns, s.CostUSD, s.Errors)
			if len(s.Models) > 0 {
				var models []string
				for _, m := range s.Models {
					// no-bare-usd:allow (observer_planner fork; not yet masked under the token display, see CONTINUATION.md)
					models = append(models, fmt.Sprintf("%s(%dturns,$%.2f)", m.Name, m.Turns, m.CostUSD))
				}
				sort.Strings(models)
				sb.WriteString("    models: " + strings.Join(models, "; ") + "\n")
			}
		}
	}

	if len(b.Opportunities) > 0 {
		sb.WriteString("opportunities:\n")
		for _, o := range b.Opportunities {
			sav := ""
			if o.SavingsUSD > 0 {
				sav = fmt.Sprintf(" savings_usd=%.2f", o.SavingsUSD)
			}
			fmt.Fprintf(&sb, "  [%s] %s (category=%s scope=%s confidence=%.0f%%)%s\n", o.Detector, o.Title, o.Category, o.Scope, o.Confidence*100, sav)
			if len(o.Justification.Evidence.Items) > 0 {
				var labels []string
				for _, it := range o.Justification.Evidence.Items {
					labels = append(labels, fmt.Sprintf("%s=%.2f", it.Label, it.Value))
				}
				sort.Strings(labels)
				sb.WriteString("    evidence: " + strings.Join(labels, "; ") + "\n")
			}
		}
	}
	return sb.String()
}

// truncateID shortens an opaque session hash for the prompt (still unique
// enough to reference; no need to expose the full id to the model).
func truncateID(id string) string {
	if len(id) <= 16 {
		return id
	}
	return id[:16]
}

const narrativeInstruction = `You are writing the one-paragraph "yesterday at a glance" summary for a developer's daily briefing. Use ONLY the numbers below. Summarize the day in 2-3 sentences: the headline activity, the biggest cost driver, and the single most valuable efficiency opportunity. Do not invent facts, file paths, or session content that is not present. Plain text, no markdown, no bullet lists.`

func narrativePrompt(b briefing.Briefing) string {
	return narrativeInstruction + "\n\n" + evidenceProjection(b)
}

const askInstruction = `You are answering ONE question about the developer's daily briefing, grounded ONLY in the data below. Answer concisely (2-4 sentences). If the data cannot answer the question, say so plainly. Reference only numbers present here — never invent sessions, costs, or content. Plain text, no markdown.`

func askPrompt(b briefing.Briefing, question string) string {
	return askInstruction + "\n\n" + evidenceProjection(b) + "\n\nQuestion: " + strings.TrimSpace(question) + "\n"
}

// Ask-your-past prompt. Built from the user's OWN persisted session summaries
// (store.SelectPastSessions) — never raw transcripts — with project roots
// reduced to a bare repo marker and each summary truncated, so a loopback or
// remote model sees only the bounded, node-local grounding.

// questionsInstruction directs the model to propose open questions for the
// developer grounded ONLY in the content-free briefing evidence (numbers, tool
// names, opportunity titles/savings) — never raw transcripts, file paths, or
// prompt text. The model returns a strict-JSON array; the runner rejects an
// unparseable or empty reply so the caller keeps the template questions.
const questionsInstruction = `You are writing the "Open questions" section of a developer's daily AI-cost briefing: 3 short, specific questions about their day that the developer should be able to answer. Ground EVERY question ONLY in the data below. Make the questions actionable — surface decisions the data points at (act on an opportunity, investigate a low cache hit rate, confirm why a tool dominated the day, check whether a session pattern repeats). Never invent sessions, costs, files, or content not present. Return ONLY a JSON array of strings, e.g. ["...", "...", "..."]. No markdown, no numbering, no surrounding text.`

// questionsPrompt renders the grounding for one open-questions call.
func questionsPrompt(b briefing.Briefing) string {
	return questionsInstruction + "\n\n" + evidenceProjection(b)
}

// maxQuestions caps the model's question list so one reply cannot dominate the
// briefing's pinned section.
const maxQuestions = 5

// extractJSONArray pulls the first [...] span out of a reply, so a model that
// wraps its JSON in a fenced code block or prose still parses.
func extractJSONArray(s string) string {
	i := strings.Index(s, "[")
	j := strings.LastIndex(s, "]")
	if i < 0 || j < 0 || j < i {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(s[i : j+1])
}

// parseQuestions validates the model's question list: empty and blank entries
// are dropped, the list is capped, and a reply with no usable questions is an
// error (the caller surfaces it as a failed call and keeps the template).
func parseQuestions(text string) ([]string, error) {
	body := extractJSONArray(text)
	if body == "" || !strings.HasPrefix(body, "[") {
		return nil, errors.New("no JSON array in reply")
	}
	var qs []string
	if err := json.Unmarshal([]byte(body), &qs); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	out := make([]string, 0, len(qs))
	for _, q := range qs {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		out = append(out, q)
		if len(out) >= maxQuestions {
			break
		}
	}
	if len(out) == 0 {
		return nil, errors.New("empty question list")
	}
	return out, nil
}

// PastContext is the grounding bundle for an ask-your-past call: the question,
// the retrieval window, and the retrieved (already scrubbed) session summaries.
type PastContext struct {
	// Question is the user's natural-language question.
	Question string
	// WindowDays is the retrieval window length (0 → all time), rendered for
	// the model so it can caveat its answer.
	WindowDays int
	// Project filters retrieval to one project root (rendered as a bare repo
	// marker, never the full path).
	Project string
	// Sessions are the retrieved hits, best-first.
	Sessions []store.PastSession
}

// maxPastSummaryChars bounds one summary in the prompt so a single oversized
// summary cannot blow the context.
const maxPastSummaryChars = 800

const askPastInstruction = `You are answering ONE question about the developer's past AI coding sessions, grounded ONLY in the session summaries below. Answer concisely (3-6 sentences). Cite only facts present in the summaries — what changed, what failed, which tool/model — and if the summaries cannot answer the question, say so plainly and suggest what to look at. Never invent sessions, file paths, or content. Plain text, no markdown.`

// askPastPrompt renders the grounding for one ask-your-past call.
func askPastPrompt(pc PastContext, now time.Time) string {
	var sb strings.Builder
	sb.WriteString(askPastInstruction)
	if pc.WindowDays > 0 {
		fmt.Fprintf(&sb, "\n\nRetrieval window: last %d days\n", pc.WindowDays)
	} else {
		sb.WriteString("\n\nRetrieval window: all time\n")
	}
	if marker := pastRepoMarker(pc.Project); marker != "" {
		fmt.Fprintf(&sb, "project: %s\n", marker)
	}
	sb.WriteString("\nSession summaries:\n")
	if len(pc.Sessions) == 0 {
		sb.WriteString("(none matched)\n")
	}
	for _, s := range pc.Sessions {
		fmt.Fprintf(&sb, "- %s | %s | tool=%s", truncateID(s.SessionID), pastAge(now, s.StartedAt), s.Tool)
		if s.Model != "" {
			fmt.Fprintf(&sb, " | model=%s", s.Model)
		}
		if marker := pastRepoMarker(s.Project); marker != "" {
			fmt.Fprintf(&sb, " | repo=%s", marker)
		}
		fmt.Fprintf(&sb, " | %d actions, %d failed\n", s.ActionCount, s.FailureCount)
		if s.SummaryMD != "" {
			fmt.Fprintf(&sb, "  summary: %s\n", truncatePastSummary(s.SummaryMD))
		}
		if len(s.ErrorMessages) > 0 {
			fmt.Fprintf(&sb, "  recurring errors: %s\n", strings.Join(s.ErrorMessages, "; "))
		}
	}
	sb.WriteString("\nQuestion: ")
	sb.WriteString(strings.TrimSpace(pc.Question))
	sb.WriteString("\n")
	return sb.String()
}

// pastRepoMarker reduces a project root to its last path segment (both
// separators), so the prompt never carries the full machine/user path.
func pastRepoMarker(root string) string {
	r := strings.ReplaceAll(root, "\\", "/")
	r = strings.TrimRight(r, "/")
	if i := strings.LastIndex(r, "/"); i >= 0 {
		r = r[i+1:]
	}
	return r
}

// pastAge renders a relative age for the prompt ("today", "3 days ago").
func pastAge(now, started time.Time) string {
	d := int(now.Sub(started).Hours() / 24)
	if d < 0 {
		d = 0
	}
	if d == 0 {
		return "today"
	}
	return fmt.Sprintf("%d days ago", d)
}

// truncatePastSummary caps one summary in the prompt.
func truncatePastSummary(s string) string {
	if len(s) <= maxPastSummaryChars {
		return s
	}
	return s[:maxPastSummaryChars] + "..."
}

// Coaching-note prompt. Built from the user's OWN recurring failure and
// failure→recovery evidence (briefing.AggregateFailurePatterns over api_turns
// + learn.Derive over failure_context) — never raw transcripts — with project
// roots reduced to a bare repo marker and edited-file paths to basenames, so
// a loopback or remote model sees only the bounded, node-local grounding.

// CoachingContext is the grounding bundle for one coaching-note call: the
// developer's own recurring failure patterns and derived correction rules over
// the window.
type CoachingContext struct {
	// WindowDays is the evidence window length, rendered for the model so it
	// can scope its advice.
	WindowDays int
	// Project filters evidence to one project root (rendered as a bare repo
	// marker, never the full path).
	Project string
	// Patterns are the recurring failure signatures, best-first (caller caps).
	Patterns []briefing.Pattern
	// Rules are the derived failure→recovery correction rules, most-frequent
	// first (caller caps).
	Rules []learn.Rule
}

// maxCoachCommandChars bounds one command summary in the prompt.
const maxCoachCommandChars = 100

const coachInstruction = `You are a coding coach giving the developer ONE piece of micro-feedback about how they used their AI coding assistants over the evidence window below. Ground EVERY claim in the evidence: the recurring errors, how many times a command failed before recovering, and the files a successful fix touched. Write 3-5 sentences. Prefer the single most actionable pattern: name the failure, the retry volume, and the concrete change that fixed it. If the evidence is too thin to support coaching, say so plainly. Never invent commands, files, or facts not present. Plain text, no markdown, no bullet lists.`

// coachPrompt renders the grounding for one coaching-note call.
func coachPrompt(cc CoachingContext, now time.Time) string {
	var sb strings.Builder
	sb.WriteString(coachInstruction)
	fmt.Fprintf(&sb, "\n\nEvidence window: last %d days\n", cc.WindowDays)
	if marker := pastRepoMarker(cc.Project); marker != "" {
		fmt.Fprintf(&sb, "project: %s\n", marker)
	}

	sb.WriteString("\nRecurring failures:\n")
	if len(cc.Patterns) == 0 {
		sb.WriteString("(none)\n")
	}
	for _, p := range cc.Patterns {
		fmt.Fprintf(&sb, "- %s: %d occurrences across %d sessions / %d days (tool=%s, model=%s, %s..%s) | %s\n",
			p.Category, p.Count, len(p.Sessions), len(p.Days), p.Tool, p.Model,
			p.FirstSeen.Format("2006-01-02"), p.LastSeen.Format("2006-01-02"), p.NormMsg)
	}

	sb.WriteString("\nFailure-to-recovery patterns:\n")
	if len(cc.Rules) == 0 {
		sb.WriteString("(none)\n")
	}
	for _, rl := range cc.Rules {
		cat := rl.ErrorCategory
		if cat == "" {
			cat = "uncategorized"
		}
		tools := strings.Join(rl.FailureToolMix, ",")
		if tools == "" {
			tools = "?"
		}
		fmt.Fprintf(&sb, "- %q failed %d times, recovered %d times (category=%s, tools=%s)\n",
			truncateRuleCommand(rl.CommandSummary), rl.FailureCount, rl.RecoveryCount, cat, tools)
		if files := coachBasenames(rl.EditedFiles); len(files) > 0 {
			fmt.Fprintf(&sb, "  fix touched: %s\n", strings.Join(files, ", "))
		}
		if rl.LastObservedAt != "" {
			if last, err := time.Parse(time.RFC3339, rl.LastObservedAt); err == nil {
				fmt.Fprintf(&sb, "  last observed: %s\n", pastAge(now, last))
			}
		}
	}
	return sb.String()
}

// truncateRuleCommand caps one command summary in the prompt.
func truncateRuleCommand(s string) string {
	if len(s) <= maxCoachCommandChars {
		return s
	}
	return s[:maxCoachCommandChars] + "..."
}

// coachBasenames reduces edited-file paths (either separator) to bare
// basenames so the prompt never carries the machine/user directory layout.
// Separator-normalized so the result is identical on every host.
func coachBasenames(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		norm := strings.ReplaceAll(p, "\\", "/")
		norm = strings.TrimRight(norm, "/")
		if i := strings.LastIndex(norm, "/"); i >= 0 {
			norm = norm[i+1:]
		}
		if norm != "" && norm != "." {
			out = append(out, norm)
		}
	}
	return out
}

// Card-prioritization prompt (the triage task, Stage 1 "smarter
// opportunities" follow-up). Built from a CONTENT-FREE projection of the
// briefing's opportunity cards (dedup keys, titles, savings, confidence,
// flags) — never raw transcripts or paths. The model returns a strict-JSON
// reordering + grouping; the runner rejects an unparseable or invalid reply so
// the caller falls back to the deterministic sort.

// TriageContext is the grounding bundle for one triage call: the window and
// the opportunity card set to prioritize.
type TriageContext struct {
	// WindowDays is the briefing window length (1 = daily, 7/28 = weekly/monthly).
	WindowDays int
	// Date is the briefing's end day (YYYY-MM-DD).
	Date string
	// Opportunities are the composed cards to prioritize (caller caps).
	Opportunities []briefing.Opportunity
}

const triageInstruction = `You are prioritizing the opportunity cards of a developer's AI-cost briefing. Reorder them best-first (highest priority first) and group related cards under short human labels. Ground your priority ONLY in the data below: savings_usd, confidence, recurrence across days, and severity. Return ONLY a JSON object with exactly these fields:
{"order":["<dedup_key>", ...],"groups":[{"label":"<short label>","keys":["<dedup_key>", ...]}, ...],"summary":"1-2 sentence rationale"}
Rules:
- "order" must list EVERY card key below, exactly once, best-first.
- "groups" clusters related cards under labels like "routing savings", "reliability", or "cache"; a card may appear in at most one group, and cards left out of every group are simply omitted from "groups".
- "summary" cites the numbers above; never invent savings, sessions, or facts.`

// triagePrompt renders the grounding for one triage call.
func triagePrompt(tc TriageContext) string {
	var sb strings.Builder
	sb.WriteString(triageInstruction)
	fmt.Fprintf(&sb, "\n\nWindow: %s (%d days)\n", tc.Date, tc.WindowDays)
	sb.WriteString("cards:\n")
	for _, o := range tc.Opportunities {
		flags := ""
		if o.Recurrence {
			flags += " recurrence"
		}
		if o.Report {
			flags += " report"
		}
		sav := "-"
		if o.SavingsUSD > 0 {
			sav = fmt.Sprintf("%.2f", o.SavingsUSD)
		}
		fmt.Fprintf(&sb, "  %s | %s | %s | savings_usd=%s | confidence=%.0f%% | scope=%s%s\n",
			o.DedupKey, o.Detector, o.Title, sav, o.Confidence*100, o.Scope, flags)
	}
	return sb.String()
}

// Failure-diagnosis prompt (the diagnose task, Stage 1 "failure analysis"
// follow-up). Built from the user's OWN recurring failure patterns
// (briefing.AggregateFailurePatterns over api_turns — the same evidence
// behind the failure_pattern cards and coaching notes), each rendered with a
// k<index> handle the model echoes back. Any unknown handle is rejected by
// the runner.

// DiagnoseContext is the grounding bundle for one diagnosis call: the
// developer's own recurring failure patterns over the window.
type DiagnoseContext struct {
	// WindowDays is the evidence window length.
	WindowDays int
	// Project filters evidence to one project root (rendered as a bare repo
	// marker, never the full path).
	Project string
	// Patterns are the recurring failure signatures, best-first (caller caps).
	Patterns []briefing.Pattern
}

const diagnoseInstruction = `You are diagnosing the developer's recurring AI-assistant failure patterns. Each pattern below is labeled with a k<index> handle and shows the category, tool, model, occurrence count, affected sessions/days, span, and a normalized error message. For each pattern you address, explain in 2-4 sentences what is most likely going wrong — grounded ONLY in this evidence — and give ONE concrete next step the developer can take. Return ONLY a JSON object with exactly this shape:
{"diagnoses":[{"index":0,"diagnosis":"2-4 sentences","next_step":"one concrete action"}, ...]}
Rules:
- "index" must be one of the k<index> handles above; diagnose each handle at most once; skipping a handle is allowed.
- Never invent error messages, files, commands, or facts not present. If a pattern is too thin to diagnose, say so plainly in "diagnosis".`

// diagnosePrompt renders the grounding for one diagnosis call.
func diagnosePrompt(dc DiagnoseContext) string {
	var sb strings.Builder
	sb.WriteString(diagnoseInstruction)
	fmt.Fprintf(&sb, "\n\nEvidence window: last %d days\n", dc.WindowDays)
	if marker := pastRepoMarker(dc.Project); marker != "" {
		fmt.Fprintf(&sb, "project: %s\n", marker)
	}
	sb.WriteString("\nPatterns:\n")
	if len(dc.Patterns) == 0 {
		sb.WriteString("(none)\n")
	}
	for i, p := range dc.Patterns {
		fmt.Fprintf(&sb, "k%d | category=%s (%s) | tool=%s | model=%s | %d occurrences across %d sessions / %d days | %s..%s | %s\n",
			i, p.Category, p.CategoryLabel(), p.Tool, p.Model, p.Count, len(p.Sessions), len(p.Days),
			p.FirstSeen.Format("2006-01-02"), p.LastSeen.Format("2006-01-02"), p.NormMsg)
	}
	return sb.String()
}
