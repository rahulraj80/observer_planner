package dashboard

import (
	"net/http"
	"time"

	"github.com/marmutapp/superbased-observer/internal/integration"
)

// toolDoctorReport is the GET /api/health/doctor/tool wire shape: the
// doctorReport of one tool's `observer doctor <tool>` run, plus the tool and
// its CLI command.
type toolDoctorReport struct {
	Tool string `json:"tool"`
	// Command is the terminal equivalent of this run. It is always present:
	// it is what the panel offers to copy when a finding has no dashboard
	// control of its own.
	Command string `json:"command"`
	doctorReport
}

// GET /api/health/doctor/tool?tool=<id> (T-421 item 3): the per-adapter
// doctor (`observer doctor <tool>`, e.g. `observer doctor cursor`) behind a
// "Run checks" action on Settings > Connected tools. It runs the SAME report
// builder the CLI prints (Options.ToolDoctor = cmd/observer's
// buildDoctorReport scoped to the tool): the adapter capture check, control
// coverage, minimum runtime, and the general checks that name the tool. The
// whole-database integrity probe is deferred, exactly as the CLI defers it
// for a per-tool scope.
//
// Owner-local (Local) and on demand: the per-adapter findings quote local
// paths and installed binaries, and the run probes this machine, the same
// class as the mcp-doctor job. The tool must be a registry tool id (a closed
// vocabulary, never free text into the builder). Read-only: it writes
// nothing, so it carries no confirm token and no audit row.
func (s *Server) handleToolDoctor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	tool := r.URL.Query().Get("tool")
	if _, ok := integration.For(tool); !ok {
		http.Error(w, "unknown tool "+quoteForError(tool)+": pick a tool from Connected tools", http.StatusBadRequest)
		return
	}
	command := "observer doctor " + tool
	if s.opts.ToolDoctor == nil {
		http.Error(w, "per-tool checks need the full daemon (observer start); run `"+command+"` in a terminal", http.StatusNotImplemented)
		return
	}
	cfg, err := loadConfigForDashboard(s.opts.ConfigPath)
	if err != nil {
		writeErr(w, err)
		return
	}
	// s.opts.DB, not s.db(): like the Health doctor, this diagnoses the real
	// install even while demo mode is active.
	report, err := s.opts.ToolDoctor(r.Context(), cfg, s.opts.DB, tool)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	checks := make([]doctorCheck, 0, len(report.Checks))
	for _, c := range report.Checks {
		checks = append(checks, doctorCheckRow(c))
	}
	ok, warn, fail := report.Counts()
	writeJSON(w, toolDoctorReport{
		Tool:    tool,
		Command: command,
		doctorReport: doctorReport{
			Checks:      checks,
			OK:          ok,
			Warn:        warn,
			Fail:        fail,
			AllOK:       fail == 0 && warn == 0,
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		},
	})
}

// quoteForError bounds a caller-supplied value echoed in an error.
func quoteForError(v string) string {
	if len(v) > 64 {
		v = v[:64] + "..."
	}
	return `"` + v + `"`
}
