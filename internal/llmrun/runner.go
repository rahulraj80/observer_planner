// Package llmrun orchestrates the OPTIONAL LLM analysis connections
// (docs/design/standup-briefing.md §3, Phase B): it turns a composed briefing
// into an LLM narrative paragraph or answers a grounded question over the
// briefing's evidence items only. It is the pure orchestration step between
// the load-bearing data layer (internal/briefing, internal/store) and the
// shared client (internal/llmconn).
//
// NEVER LOAD-BEARING: with no configured connections every method returns the
// template briefing unchanged / ErrNoConnection, and a failed LLM call records
// an analysis_calls error row and falls back to the template — the briefing
// never depends on a model answering. Egress guardrails (scrub + payload cap)
// apply to any REMOTE connection; loopback connections are keyless and send
// the payload untouched.
package llmrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/briefing"
	"github.com/marmutapp/superbased-observer/internal/llmconn"
	"github.com/marmutapp/superbased-observer/internal/scrub"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// ErrNoConnection is returned when no enabled connection serves the requested
// task (the web surface maps it to HTTP 404 with error code no_connection).
var ErrNoConnection = fmt.Errorf("llmrun: no enabled connection for this task")

// PriceFn prices an analysis call for the accounting row (list basis, per the
// spec — the dashboard adapts its cost engine). nil → cost recorded as 0.
type PriceFn func(model string, inputTokens, outputTokens int) (float64, bool)

// Runner executes analysis calls. All fields are optional; zero value is a
// fully-functioning "never egress, never record" runner.
type Runner struct {
	// Store records analysis_calls accounting rows. nil → no accounting.
	Store *store.Store
	// Env resolves connection credentials (env-ref). nil → os.Getenv.
	Env func(string) string
	// Keychain resolves a connection's OS-keychain credential ref
	// (APIKeyKeychain label). nil → a keychain-only connection is
	// reported as an error rather than called keyless.
	Keychain func(string) (string, error)
	// Scrub redacts secrets before a REMOTE egress. nil → scrub.New().
	Scrub *scrub.Scrubber
	// MaxPromptBytes caps the prompt sent to a REMOTE connection (0 → 64KiB).
	MaxPromptBytes int
	// MaxTokens caps the analysis REPLY (0 → the client default).
	MaxTokens int
	// NumCtx, when >0 AND the connection is loopback, is passed through as an
	// Ollama-style context hint (never sent remotely).
	NumCtx int
	// Price prices a completed call for the accounting row. nil → 0.
	Price PriceFn
	// Now stamps accounting rows. nil → time.Now.
	Now func() time.Time
}

// maxRemotePromptBytes is the default cap for a remote connection.
const maxRemotePromptBytes = 64 << 10

// resolve returns the client-ready pieces for the selected connection: the
// credential (loopback may be empty; remote must be non-empty) and an egress
// guardrail applier for remote hosts.
func (r *Runner) resolve(conn llmconn.Connection) (hv string, guard func(*llmconn.Client) *llmconn.Client, err error) {
	env := r.Env
	if env == nil {
		env = os.Getenv
	}
	if conn.APIKeyEnv != "" {
		hv = env(conn.APIKeyEnv)
	}
	if hv == "" && conn.APIKeyKeychain != "" {
		kc := r.Keychain
		if kc == nil {
			return "", nil, fmt.Errorf("llmrun: connection %s uses OS-keychain ref %q but no keychain resolver is wired", conn.ID, conn.APIKeyKeychain)
		}
		v, err := kc(conn.APIKeyKeychain)
		if err != nil {
			return "", nil, fmt.Errorf("llmrun: connection %s: resolve keychain ref %q: %w", conn.ID, conn.APIKeyKeychain, err)
		}
		hv = v
	}
	if conn.Remote() && hv == "" {
		return "", nil, fmt.Errorf("llmrun: connection %s has no credential (env %q / keychain %q)", conn.ID, conn.APIKeyEnv, conn.APIKeyKeychain)
	}
	sc := r.Scrub
	if sc == nil {
		sc = scrub.New()
	}
	if conn.Remote() {
		cap := r.MaxPromptBytes
		if cap <= 0 {
			cap = maxRemotePromptBytes
		}
		guard = func(c *llmconn.Client) *llmconn.Client {
			return c.WithEgressScrub(sc.String, cap)
		}
	}
	return hv, guard, nil
}

// newClient builds a tuned client for conn, applying the remote guardrail.
func (r *Runner) newClient(conn llmconn.Connection) (*llmconn.Client, error) {
	hv, guard, err := r.resolve(conn)
	if err != nil {
		return nil, err
	}
	c := llmconn.NewClient(conn, hv).WithTuning(r.MaxTokens, r.NumCtx)
	if guard != nil {
		c = guard(c)
	}
	return c, nil
}

// record appends an analysis_calls row (best-effort, never fails the caller).
func (r *Runner) record(conn llmconn.Connection, model, task string, res llmconn.Result, err error) {
	if r.Store == nil {
		return
	}
	now := r.Now
	if now == nil {
		now = time.Now
	}
	call := store.AnalysisCall{
		ConnectionID: conn.ID,
		Model:        model,
		Task:         task,
		InputTokens:  res.InputTokens,
		OutputTokens: res.OutputTokens,
		LatencyMS:    res.LatencyMS,
		Status:       "ok",
	}
	if err != nil {
		call.Status = "error"
		call.ErrorClass = errClass(err)
	}
	if call.Status == "ok" && r.Price != nil {
		if p, ok := r.Price(model, res.InputTokens, res.OutputTokens); ok {
			call.CostUSD = p
		}
	}
	_ = r.Store.RecordAnalysisCall(context.Background(), call, now())
}

// errClass reduces an error to a short stable class for the accounting row.
func errClass(err error) string {
	s := err.Error()
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

// Narrative returns a copy of b whose Narrative is the LLM's one-paragraph
// summary of the SAME numbers (never new facts), plus the id of the
// connection used. When no connection serves the narrative task — or the call
// fails — the template narrative is kept unchanged and an error is recorded.
func (r *Runner) Narrative(ctx context.Context, conns []llmconn.Connection, b briefing.Briefing) (briefing.Briefing, string, error) {
	conn, ok := llmconn.Select(conns, llmconn.TaskNarrative, "")
	if !ok {
		return b, "", ErrNoConnection
	}
	client, err := r.newClient(conn)
	if err != nil {
		r.record(conn, conn.DefaultModel, "narrative", llmconn.Result{}, err)
		return b, "", err
	}
	model := conn.DefaultModel
	res, err := client.Complete(ctx, model, narrativePrompt(b))
	if err != nil {
		r.record(conn, model, "narrative", res, err)
		return b, "", err
	}
	r.record(conn, model, "narrative", res, nil)
	if text := strings.TrimSpace(res.Text); text != "" {
		b.Narrative = text
	}
	return b, conn.ID, nil
}

// Questions proposes open questions for the developer's briefing, grounded in
// the SAME content-free evidence the narrative uses (numbers, tool names,
// opportunity titles/savings) — never raw transcripts or paths. ErrNoConnection
// is returned when no connection serves the questions task. An unparseable or
// empty reply is returned as an error after being recorded, so the caller keeps
// the template questions.
func (r *Runner) Questions(ctx context.Context, conns []llmconn.Connection, b briefing.Briefing) ([]string, error) {
	conn, ok := llmconn.Select(conns, llmconn.TaskQuestions, "")
	if !ok {
		return nil, ErrNoConnection
	}
	client, err := r.newClient(conn)
	if err != nil {
		r.record(conn, conn.DefaultModel, "questions", llmconn.Result{}, err)
		return nil, err
	}
	model := conn.DefaultModel
	res, err := client.Complete(ctx, model, questionsPrompt(b))
	if err != nil {
		r.record(conn, model, "questions", res, err)
		return nil, err
	}
	r.record(conn, model, "questions", res, nil)
	qs, err := parseQuestions(res.Text)
	if err != nil {
		return nil, fmt.Errorf("llmrun: questions reply: %w", err)
	}
	return qs, nil
}

// AskResult is one grounded Q&A answer.
type AskResult struct {
	Text           string `json:"answer"`
	Model          string `json:"model"`
	ConnectionID   string `json:"connection_id"`
	ConnectionName string `json:"connection_name"`
	LatencyMS      int64  `json:"latency_ms"`
}

// Ask answers a grounded question over the briefing's evidence items only
// (content-free — never raw transcripts or paths; remote connections are
// forced onto this evidence-only prompt by construction). ErrNoConnection is
// returned when no connection serves the ask task (→ HTTP 404). A failed call
// is returned as an error after being recorded.
func (r *Runner) Ask(ctx context.Context, conns []llmconn.Connection, question string, b briefing.Briefing) (AskResult, error) {
	if question == "" {
		return AskResult{}, fmt.Errorf("llmrun: empty question")
	}
	conn, ok := llmconn.Select(conns, llmconn.TaskAsk, "")
	if !ok {
		return AskResult{}, ErrNoConnection
	}
	client, err := r.newClient(conn)
	if err != nil {
		r.record(conn, conn.DefaultModel, "ask", llmconn.Result{}, err)
		return AskResult{}, err
	}
	model := conn.DefaultModel
	res, err := client.Complete(ctx, model, askPrompt(b, question))
	if err != nil {
		r.record(conn, model, "ask", res, err)
		return AskResult{}, err
	}
	r.record(conn, model, "ask", res, nil)
	name := conn.Name
	if name == "" {
		name = conn.ID
	}
	return AskResult{
		Text:           res.Text,
		Model:          model,
		ConnectionID:   conn.ID,
		ConnectionName: name,
		LatencyMS:      res.LatencyMS,
	}, nil
}

// AskPast answers a natural-language question grounded in the user's own
// persisted session summaries (store.SelectPastSessions — never raw
// transcripts; the prompt reduces project roots to a bare repo marker and
// applies the remote egress guardrails). ErrNoConnection is returned when no
// connection serves the ask_past task (→ HTTP 404). A failed call is returned
// as an error after being recorded.
func (r *Runner) AskPast(ctx context.Context, conns []llmconn.Connection, pc PastContext) (AskResult, error) {
	if strings.TrimSpace(pc.Question) == "" {
		return AskResult{}, fmt.Errorf("llmrun: empty question")
	}
	conn, ok := llmconn.Select(conns, llmconn.TaskAskPast, "")
	if !ok {
		return AskResult{}, ErrNoConnection
	}
	client, err := r.newClient(conn)
	if err != nil {
		r.record(conn, conn.DefaultModel, "ask_past", llmconn.Result{}, err)
		return AskResult{}, err
	}
	now := r.Now
	if now == nil {
		now = time.Now
	}
	model := conn.DefaultModel
	res, err := client.Complete(ctx, model, askPastPrompt(pc, now()))
	if err != nil {
		r.record(conn, model, "ask_past", res, err)
		return AskResult{}, err
	}
	r.record(conn, model, "ask_past", res, nil)
	name := conn.Name
	if name == "" {
		name = conn.ID
	}
	return AskResult{
		Text:           res.Text,
		Model:          model,
		ConnectionID:   conn.ID,
		ConnectionName: name,
		LatencyMS:      res.LatencyMS,
	}, nil
}

// TriageResult is the model's reordering + grouping of the briefing's
// opportunity cards. Order ALWAYS covers every input card exactly once: the
// runner appends any card the model omitted (in the caller's input order) so
// a data-backed card is never silently dropped by an LLM reply.
type TriageResult struct {
	Order          []string      `json:"order"` // dedup_keys, best-first
	Groups         []TriageGroup `json:"groups,omitempty"`
	Summary        string        `json:"summary,omitempty"`
	Model          string        `json:"model"`
	ConnectionID   string        `json:"connection_id"`
	ConnectionName string        `json:"connection_name"`
	LatencyMS      int64         `json:"latency_ms"`
}

// TriageGroup is one labeled cluster of card dedup_keys.
type TriageGroup struct {
	Label string   `json:"label"`
	Keys  []string `json:"keys"`
}

// Triage reorders + groups the briefing's opportunity cards best-first,
// grounded in a content-free projection (dedup keys, titles, savings,
// confidence, flags) — never raw transcripts or paths. ErrNoConnection when
// no connection serves the triage task (→ HTTP 404). An unparseable or
// invalid reply is returned as an error after accounting, so the caller falls
// back to the deterministic sort.
func (r *Runner) Triage(ctx context.Context, conns []llmconn.Connection, tc TriageContext) (TriageResult, error) {
	conn, ok := llmconn.Select(conns, llmconn.TaskTriage, "")
	if !ok {
		return TriageResult{}, ErrNoConnection
	}
	client, err := r.newClient(conn)
	if err != nil {
		r.record(conn, conn.DefaultModel, "triage", llmconn.Result{}, err)
		return TriageResult{}, err
	}
	model := conn.DefaultModel
	res, err := client.Complete(ctx, model, triagePrompt(tc))
	if err != nil {
		r.record(conn, model, "triage", res, err)
		return TriageResult{}, err
	}
	r.record(conn, model, "triage", res, nil)
	tr, err := parseTriage(res.Text, tc.Opportunities)
	if err != nil {
		return TriageResult{}, fmt.Errorf("llmrun: triage reply: %w", err)
	}
	name := conn.Name
	if name == "" {
		name = conn.ID
	}
	tr.Model = model
	tr.ConnectionID = conn.ID
	tr.ConnectionName = name
	tr.LatencyMS = res.LatencyMS
	return tr, nil
}

// DiagnoseResult is the per-pattern diagnosis set for one window.
type DiagnoseResult struct {
	Diagnoses      []Diagnosis `json:"diagnoses"`
	Model          string      `json:"model"`
	ConnectionID   string      `json:"connection_id"`
	ConnectionName string      `json:"connection_name"`
	LatencyMS      int64       `json:"latency_ms"`
}

// Diagnosis is one failure-pattern diagnosis: the k<index> handle echoed from
// the prompt (mapping back to the caller's patterns slice), the root-cause
// analysis, and one concrete next step.
type Diagnosis struct {
	Index     int    `json:"index"`
	Diagnosis string `json:"diagnosis"`
	NextStep  string `json:"next_step"`
}

// Diagnose produces a per-pattern root-cause diagnosis + next step for the
// user's OWN recurring failure patterns, grounded in the same content-free
// evidence the failure_pattern cards use. ErrNoConnection when no connection
// serves the diagnose task (→ HTTP 404). An unparseable or empty reply is
// returned as an error after accounting.
func (r *Runner) Diagnose(ctx context.Context, conns []llmconn.Connection, dc DiagnoseContext) (DiagnoseResult, error) {
	conn, ok := llmconn.Select(conns, llmconn.TaskDiagnose, "")
	if !ok {
		return DiagnoseResult{}, ErrNoConnection
	}
	client, err := r.newClient(conn)
	if err != nil {
		r.record(conn, conn.DefaultModel, "diagnose", llmconn.Result{}, err)
		return DiagnoseResult{}, err
	}
	model := conn.DefaultModel
	res, err := client.Complete(ctx, model, diagnosePrompt(dc))
	if err != nil {
		r.record(conn, model, "diagnose", res, err)
		return DiagnoseResult{}, err
	}
	r.record(conn, model, "diagnose", res, nil)
	ds, err := parseDiagnoses(res.Text, len(dc.Patterns))
	if err != nil {
		return DiagnoseResult{}, fmt.Errorf("llmrun: diagnose reply: %w", err)
	}
	name := conn.Name
	if name == "" {
		name = conn.ID
	}
	return DiagnoseResult{
		Diagnoses:      ds,
		Model:          model,
		ConnectionID:   conn.ID,
		ConnectionName: name,
		LatencyMS:      res.LatencyMS,
	}, nil
}

// extractJSONObject pulls the first {...} span out of a reply, so a model that
// wraps its JSON in a fenced code block or prose still parses.
func extractJSONObject(s string) string {
	i := strings.Index(s, "{")
	j := strings.LastIndex(s, "}")
	if i < 0 || j < 0 || j < i {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(s[i : j+1])
}

// parseTriage validates the model's reordering against the input card set:
// unknown keys and duplicates are dropped, and every card the model omitted is
// appended in input order so the result always covers the full set exactly
// once. An empty order (unparseable or no valid keys) is an error.
func parseTriage(text string, opps []briefing.Opportunity) (TriageResult, error) {
	valid := make(map[string]struct{}, len(opps))
	for _, o := range opps {
		valid[o.DedupKey] = struct{}{}
	}
	body := extractJSONObject(text)
	if body == "" || !strings.HasPrefix(body, "{") {
		return TriageResult{}, errors.New("no JSON object in reply")
	}
	var raw struct {
		Order  []string `json:"order"`
		Groups []struct {
			Label string   `json:"label"`
			Keys  []string `json:"keys"`
		} `json:"groups"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		return TriageResult{}, fmt.Errorf("decode: %w", err)
	}
	seen := make(map[string]bool, len(opps))
	order := make([]string, 0, len(opps))
	for _, k := range raw.Order {
		if _, ok := valid[k]; !ok || seen[k] {
			continue
		}
		seen[k] = true
		order = append(order, k)
	}
	for _, o := range opps {
		if !seen[o.DedupKey] {
			seen[o.DedupKey] = true
			order = append(order, o.DedupKey)
		}
	}
	if len(order) == 0 {
		return TriageResult{}, errors.New("empty order")
	}
	gseen := make(map[string]bool, len(opps))
	var groups []TriageGroup
	for _, g := range raw.Groups {
		label := strings.TrimSpace(g.Label)
		if label == "" {
			continue
		}
		var keys []string
		for _, k := range g.Keys {
			if _, ok := valid[k]; !ok || gseen[k] {
				continue
			}
			gseen[k] = true
			keys = append(keys, k)
		}
		if len(keys) > 0 {
			groups = append(groups, TriageGroup{Label: label, Keys: keys})
		}
	}
	return TriageResult{Order: order, Groups: groups, Summary: strings.TrimSpace(raw.Summary)}, nil
}

// parseDiagnoses validates the model's diagnoses against the input pattern
// count: out-of-range, duplicate, or empty handles are dropped, and the result
// is sorted by index for determinism. A reply with zero valid diagnoses for a
// non-empty pattern set is an error (the caller surfaces it as a failed call).
func parseDiagnoses(text string, patterns int) ([]Diagnosis, error) {
	if patterns == 0 {
		return nil, nil
	}
	body := extractJSONObject(text)
	if body == "" || !strings.HasPrefix(body, "{") {
		return nil, errors.New("no JSON object in reply")
	}
	var raw struct {
		Diagnoses []struct {
			Index     *int   `json:"index"`
			Diagnosis string `json:"diagnosis"`
			NextStep  string `json:"next_step"`
		} `json:"diagnoses"`
	}
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	seen := make(map[int]bool, patterns)
	var out []Diagnosis
	for _, d := range raw.Diagnoses {
		if d.Index == nil || *d.Index < 0 || *d.Index >= patterns || seen[*d.Index] {
			continue
		}
		diagnosis := strings.TrimSpace(d.Diagnosis)
		next := strings.TrimSpace(d.NextStep)
		if diagnosis == "" && next == "" {
			continue
		}
		seen[*d.Index] = true
		out = append(out, Diagnosis{Index: *d.Index, Diagnosis: diagnosis, NextStep: next})
	}
	if len(out) == 0 {
		return nil, errors.New("no valid diagnoses in reply")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out, nil
}

// Coach generates ONE micro-feedback coaching note grounded in the user's own
// recurring failure and failure→recovery evidence (CoachingContext — never
// raw transcripts; the prompt reduces project roots to a bare repo marker and
// edited-file paths to basenames, and applies the remote egress guardrails).
// ErrNoConnection is returned when no connection serves the coach task (→
// HTTP 404). A failed call is returned as an error after being recorded.
func (r *Runner) Coach(ctx context.Context, conns []llmconn.Connection, cc CoachingContext) (AskResult, error) {
	conn, ok := llmconn.Select(conns, llmconn.TaskCoach, "")
	if !ok {
		return AskResult{}, ErrNoConnection
	}
	client, err := r.newClient(conn)
	if err != nil {
		r.record(conn, conn.DefaultModel, "coach", llmconn.Result{}, err)
		return AskResult{}, err
	}
	now := r.Now
	if now == nil {
		now = time.Now
	}
	model := conn.DefaultModel
	res, err := client.Complete(ctx, model, coachPrompt(cc, now()))
	if err != nil {
		r.record(conn, model, "coach", res, err)
		return AskResult{}, err
	}
	r.record(conn, model, "coach", res, nil)
	name := conn.Name
	if name == "" {
		name = conn.ID
	}
	return AskResult{
		Text:           res.Text,
		Model:          model,
		ConnectionID:   conn.ID,
		ConnectionName: name,
		LatencyMS:      res.LatencyMS,
	}, nil
}
