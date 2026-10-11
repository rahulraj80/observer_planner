package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/policyfam/nodegov"
)

// command_jobs.go is the dashboard front door for the CLI actions that used
// to be CLI-only (backlog item 6, docs/audits/cli-only-flows-2026-10-03.md):
// `observer score`, `observer patterns`, `observer model-value
// --save-calibration`, `observer summarize`, `observer pricing sync` and the
// `observer mcp` / `mcp-relay` verbs. Each one runs THE CLI COMMAND ITSELF as a
// subprocess through the backfill job runner (realExecBackfill), so the
// dashboard and the terminal share one code path by construction: there is no
// second implementation to drift. The table is closed and the argv is fixed,
// so nothing user-typed ever reaches os/exec.

// commandJob is one row of the closed table.
type commandJob struct {
	// argv is the observer argument vector (subcommand first); the handler
	// appends --config with the dashboard's own config path.
	argv []string
	// writes marks a job that changes state (the DB, a config file, an AI
	// client's config). Those leave a manage row in remote_audit like every
	// other dashboard write; a read-only job (doctor) does not.
	writes bool
	// section is the nav section (and Settings subsection or page tab) of the
	// page that shows the button: the start route is registered with it, and
	// the list / poll serve a job only while it is visible (review M3-04), so
	// a page an organization hides hides its action AND its output.
	section Section
	// cancelable registers POST /api/jobs/<name>/cancel for the row (same
	// class and section as the start route): the operator can stop a long
	// run, which kills the subprocess through its exec context.
	cancelable bool
}

// commandJobs is every CLI action a dashboard button may run, keyed by the
// job name in POST /api/jobs/<name>. registerCommandJobRoutes registers each
// row on its own route with the row's section, so the table is the one owner
// of a job's argv, write flag and governance.
var commandJobs = map[string]commandJob{
	// Sessions: score every unscored session now (the daemon's idle scorer
	// does the same pass on its own schedule; the write is idempotent).
	"score": {argv: []string{"score"}, writes: true, section: SectionSessions},
	// Patterns: derive project_patterns rows.
	"patterns": {argv: []string{"patterns"}, writes: true, section: SectionPatterns},
	// Routing: persist calibration cells into model_calibration.
	"calibration": {argv: []string{"model-value", "--save-calibration"}, writes: true, section: SectionRouting},
	// Session summaries through the configured summary model (an outbound
	// call with the operator's own API key; the button says so).
	"summarize": {argv: []string{"summarize"}, writes: true, section: SectionSettings.WithSettings("intelligence")},
	// Privacy: fetch, verify and apply the public pricing feed once (the CLI
	// refuses on an enrolled node, which gets prices from its organization).
	"pricing-sync": {argv: []string{"pricing", "sync"}, writes: true, section: SectionPrivacy},
	// Security > MCP access (Agent Access node relay): the MCP tab owns
	// these exclusively (review M3-07 class: tagged with the tab).
	"mcp-wrap":          {argv: []string{"mcp", "wrap"}, writes: true, section: SectionSecurity.WithTab("mcp")},
	"mcp-unwrap":        {argv: []string{"mcp", "unwrap"}, writes: true, section: SectionSecurity.WithTab("mcp")},
	"mcp-relay-enable":  {argv: []string{"mcp-relay", "enable"}, writes: true, section: SectionSecurity.WithTab("mcp")},
	"mcp-relay-disable": {argv: []string{"mcp-relay", "disable"}, writes: true, section: SectionSecurity.WithTab("mcp")},
	"mcp-doctor":        {argv: []string{"mcp", "doctor"}, section: SectionSecurity.WithTab("mcp")},
	// Settings > Health (T-421 item 2): the forced whole-database PRAGMA
	// quick_check the doctor skips above [observer.db].integrity_check_max_gb
	// and points at (`observer doctor db --integrity`). It only READS the
	// database (writes: false, so no manage audit row), runs only when the
	// operator clicks, and is cancelable because on a multi-GB database it
	// runs for minutes.
	"doctor-integrity": {argv: []string{"doctor", "db", "--integrity"}, section: SectionSettings.WithSettings("health"), cancelable: true},
	// Settings > Storage (T-493): delete the original database a reclaim
	// renamed aside (<db>.pre-reclaim-<stamp>), after verifying the database
	// in place (quick_check). It runs only when the operator clicks, never on
	// its own; the CLI's own refusal table decides, so the dashboard cannot
	// delete anything the CLI would refuse to. Cancelable: the quick_check
	// reads the whole file and runs for minutes on a large database (a cancel
	// during it deletes nothing).
	"reclaim-cleanup": {argv: []string{"archive", "reclaim-cleanup", "--delete"}, writes: true, section: SectionSettings.WithSettings("storage"), cancelable: true},
}

// commandJobNames returns the table's names in a stable order.
func commandJobNames() []string {
	names := make([]string, 0, len(commandJobs))
	for n := range commandJobs {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// commandJobSection is the section a job registry mode belongs to (benchmark
// runs belong to Benchmarks).
func commandJobSection(mode string) Section {
	if row, ok := commandJobs[mode]; ok {
		return row.section
	}
	return SectionBenchmarks
}

// sectionConcealed reports whether the organization hides a registered
// Section value on this node right now: its nav section, its page tab or its
// Settings subsection (the governance guard's own rules, applied to a value
// that is not a route, here a job's owning page).
func (s *Server) sectionConcealed(ctx context.Context, sec Section) bool {
	if s.opts.Governance == nil {
		return false
	}
	eff := s.opts.Governance(ctx)
	if !eff.VisibilityActive() {
		return false
	}
	nav, sub := sec.Split()
	if nav != SectionNone && !nodegov.IsUnhideableNavSection(string(nav)) {
		if eff.IsNavSectionHidden(string(nav)) {
			return true
		}
		if tab := sec.Tab(); tab != "" && eff.IsNavTabHidden(nodegov.NavTabID(string(nav), tab)) {
			return true
		}
	}
	return sub != "" && !nodegov.IsUnhideableSettingsSection(sub) && eff.IsSettingsSectionHidden(sub)
}

// isCommandJobMode reports whether a job-registry mode belongs to the command
// jobs (including the benchmark runs, which use the same poll endpoint).
func isCommandJobMode(mode string) bool {
	if _, ok := commandJobs[mode]; ok {
		return true
	}
	return strings.HasPrefix(mode, benchmarkJobModePrefix)
}

// startJob registers a running job under mode and spawns args through the
// shared runner. --config is appended here so every caller passes the same
// config the daemon reads.
func (s *Server) startJob(mode string, args []string) (*backfillJob, error) {
	jobID, err := newJobID()
	if err != nil {
		return nil, fmt.Errorf("generate job id: %w", err)
	}
	job := &backfillJob{
		ID:        jobID,
		Mode:      mode,
		Status:    "running",
		StartedAt: time.Now().UTC(),
	}
	s.backfillMu.Lock()
	s.backfillSeq++
	job.seq = s.backfillSeq
	s.backfillJobs[jobID] = job
	s.backfillMu.Unlock()

	full := append([]string{}, args...)
	if s.opts.ConfigPath != "" {
		full = append(full, "--config", s.opts.ConfigPath)
	}
	go s.runBackfillJob(jobID, full)
	return job, nil
}

// commandJobHandler serves POST /api/jobs/<name> for one table row.
func (s *Server) commandJobHandler(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		row, ok := commandJobs[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		// Review M3-05: every job POST carries the owner-local double-submit
		// confirm token (minted by GET /api/jobs), like every other dashboard
		// write; the button's two-click arm is UI state only.
		if !requireJSONConfirm(w, r) {
			return
		}
		job, err := s.startJob(name, row.argv)
		if err != nil {
			writeErr(w, err)
			return
		}
		if row.writes {
			s.recordManageAudit(r, "job_"+name, "job="+job.ID)
		}
		writeJSON(w, map[string]any{
			"job_id":     job.ID,
			"mode":       name,
			"status":     "running",
			"started_at": job.StartedAt.Format(time.RFC3339),
		})
	}
}

// commandJobCancelHandler serves POST /api/jobs/<name>/cancel for one
// cancelable table row (T-421 item 2): {"job_id": "..."} stops that run of
// that job. It is registered with the row's own class and section, so the
// governance guard that gates the start route gates the cancel too. A job of
// another mode, or an unknown id, is a 404; a job that already finished is a
// 409. Cancelling kills the subprocess through its exec context; the job then
// ends with status "canceled".
func (s *Server) commandJobCancelHandler(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		row, ok := commandJobs[name]
		if !ok || !row.cancelable {
			http.NotFound(w, r)
			return
		}
		if !requireJSONConfirm(w, r) {
			return
		}
		var req struct {
			JobID string `json:"job_id"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
			http.Error(w, "decode body: "+err.Error(), http.StatusBadRequest)
			return
		}
		s.backfillMu.Lock()
		job, found := s.backfillJobs[req.JobID]
		if !found || job.Mode != name {
			s.backfillMu.Unlock()
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}
		if job.Status != "running" {
			status := job.Status
			s.backfillMu.Unlock()
			http.Error(w, "job is not running (status "+status+")", http.StatusConflict)
			return
		}
		job.canceled = true
		cancel := job.cancel
		s.backfillMu.Unlock()
		if cancel != nil {
			cancel()
		}
		if row.writes {
			s.recordManageAudit(r, "job_"+name+"_cancel", "job="+req.JobID)
		}
		writeJSON(w, map[string]any{"job_id": req.JobID, "mode": name, "status": "canceling"})
	}
}

// handleCommandJobs serves GET /api/jobs (the command jobs, newest first, so a
// card can restore its last run after navigation) and GET /api/jobs/<id> (one
// command job's snapshot, the poll). An unknown name under POST is a 404: the
// table's own routes are more specific patterns, so a POST that lands here
// names no row. Backfill / scan / prune jobs are deliberately NOT served here:
// they stay behind the Settings governance of /api/backfill/jobs/.
//
// Review M3-04: both are owner-Local (a job's output is local diagnostics of
// a Local-only action), and a job whose owning page, tab or Settings
// subsection the organization hides is not served (the list skips it, the
// poll answers 404), so hiding a page hides its earlier output too. The list
// also mints the confirm token the job POSTs echo (M3-05).
func (s *Server) handleCommandJobs(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/jobs"), "/")
	if r.Method == http.MethodPost {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	if id == "" {
		mode := r.URL.Query().Get("mode")
		snaps := make([]backfillJob, 0)
		s.backfillMu.Lock()
		for _, j := range s.backfillJobs {
			if !isCommandJobMode(j.Mode) || (mode != "" && j.Mode != mode) {
				continue
			}
			snaps = append(snaps, *j)
		}
		s.backfillMu.Unlock()
		visible := snaps[:0]
		for _, j := range snaps {
			if !s.sectionConcealed(r.Context(), commandJobSection(j.Mode)) {
				visible = append(visible, j)
			}
		}
		sort.Slice(visible, func(i, j int) bool { return visible[i].seq > visible[j].seq })
		writeJSON(w, map[string]any{"jobs": visible, "confirm_token": setConfirmCookie(w, r)})
		return
	}
	s.backfillMu.Lock()
	job, ok := s.backfillJobs[id]
	var snap backfillJob
	if ok {
		snap = *job
	}
	s.backfillMu.Unlock()
	if !ok || !isCommandJobMode(snap.Mode) || s.sectionConcealed(r.Context(), commandJobSection(snap.Mode)) {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}
	writeJSON(w, snap)
}

// benchmarkJobModePrefix prefixes the job modes of dashboard benchmark runs
// ("benchmark-dry-run", "benchmark-run"); they poll through /api/jobs/<id>.
const benchmarkJobModePrefix = "benchmark-"
