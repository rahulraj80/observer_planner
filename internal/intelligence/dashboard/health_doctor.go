package dashboard

import (
	"net/http"
	"os"
	"time"

	"github.com/marmutapp/superbased-observer/internal/diag"
	"github.com/marmutapp/superbased-observer/internal/remedy"
)

// doctorCheck is one diag.Check as the wire spells it.
type doctorCheck struct {
	// Name is the check's stable id ("db.integrity", "hooks.checksums"). It is
	// a fixed vocabulary this codebase owns and never carries a path, so it is
	// the one field the remote projection leaves alone.
	Name   string `json:"name"`
	Status string `json:"status"`
	// Message and Details are FREE TEXT assembled by the checks, and both
	// routinely quote absolute paths — written in by the check, or carried in
	// by an err.Error() the check merely concatenated. For a remotely-exposed
	// caller they arrive rewritten through pathRedactor; see
	// doctorReport.LocalDetailWithheld.
	Message string   `json:"message"`
	Details []string `json:"details,omitempty"`
	// Remediations are the check's structured fixes (T-421 item 3), each
	// rendered by internal/remedy: the command the CLI text quotes, and the
	// dashboard control (label + in-app link) the Health page renders as a
	// button. The vocabulary carries no path, so the remote projection
	// leaves it alone.
	Remediations []doctorRemediation `json:"remediations,omitempty"`
}

// doctorRemediation is one remedy.Remediation as the wire spells it.
type doctorRemediation struct {
	Action  string `json:"action"`
	Tool    string `json:"tool,omitempty"`
	Command string `json:"command,omitempty"`
	Label   string `json:"label"`
	Href    string `json:"href"`
}

// doctorRemediations projects a check's remediations; an action this build
// cannot render is dropped rather than shown as a dead button.
func doctorRemediations(in []remedy.Remediation) []doctorRemediation {
	out := make([]doctorRemediation, 0, len(in))
	for _, r := range in {
		c, ok := remedy.ControlFor(r)
		if !ok {
			continue
		}
		out = append(out, doctorRemediation{Action: string(r.Action), Tool: r.Tool, Command: remedy.Command(r), Label: c.Label, Href: c.Href})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// doctorCheckRow is one diag.Check as the wire spells it: the one mapping
// both doctor routes (the whole report and the per-tool one) use.
func doctorCheckRow(c diag.Check) doctorCheck {
	return doctorCheck{
		Name:         c.Name,
		Status:       c.Status.String(),
		Message:      c.Message,
		Details:      c.Details,
		Remediations: doctorRemediations(c.Remediations),
	}
}

// doctorReport is the GET /api/health/doctor wire shape.
type doctorReport struct {
	Checks []doctorCheck `json:"checks"`
	OK     int           `json:"ok"`
	Warn   int           `json:"warn"`
	Fail   int           `json:"fail"`
	AllOK  bool          `json:"all_ok"`
	// GeneratedAt stamps the run; the panel shows it beside the Re-run button.
	GeneratedAt string `json:"generated_at"`

	// LocalDetailWithheld marks this response as the REMOTE-FACING PROJECTION:
	// it was served to a remotely-exposed caller, so every filesystem root this
	// server could identify — the home directory, the config file, the
	// database, the running executable, the temp dir, and every cross-OS home
	// the crossmount bridge enumerates — has been replaced by a placeholder in
	// each check's message and details. The check set, the statuses and the
	// counts are unchanged: which checks failed is the legitimate remote read,
	// and the remediation hints stay actionable with the machine's layout and
	// the operator's identity taken out of them.
	//
	// It is STATED rather than left implicit for the same reason the sibling
	// ETW status route states it: a placeholder-bearing sentence is otherwise
	// indistinguishable from a check that genuinely had nothing local to say,
	// and a surface that quietly shows the operator a different report than the
	// one their own machine produced is the honest-disabled-copy rule broken.
	//
	// It is set for EVERY remote response, including one where no root matched
	// anything, because what it reports is WHICH PROJECTION the caller is
	// looking at — not whether this particular run happened to contain a path.
	// Gating it on "something was actually rewritten" would make the flag flip
	// on and off run to run while the disclosure policy never changed.
	//
	// It does NOT claim complete redaction. Paths under no known root, OS
	// convention paths like /etc/codex/*.toml, and the org enrolment check's
	// user email all survive it — see the residue note on pathRedactor.
	LocalDetailWithheld bool `json:"local_detail_withheld,omitempty"`
}

// GET /api/health/doctor (usability arc P4.8 / review row D1) — the
// `observer doctor` checks as JSON. Same diag.Run, same checks
// (schema, DB integrity, sizes, adapter paths, hook checksums +
// binary, MCP registrations, pidbridge, concurrent daemons, codex
// trust, org enrolment); the Details lines carry the remediation
// hints the CLI prints. Read-only and on-demand — the panel runs it
// on open and on the Re-run button, never on a poll loop (the DB
// integrity check is not free on a large observer.db).
//
// WHAT IT DISCLOSES IS CALLER-DEPENDENT. The route is capability VIEW, so a
// PAIRED REMOTE DEVICE can reach it, and the checks' free text embeds the
// operator's absolute filesystem layout and both their local and Windows user
// names pervasively — most of it arriving through err.Error() in checks whose
// source line contains no path at all. A remote-exposed caller therefore gets
// the same checks with every identifiable root replaced by a placeholder, and
// is TOLD that is what it is reading (LocalDetailWithheld).
//
// This is the same policy the sibling GET /api/process/etw/status applies to
// its command, notes and transport-unavailable reason. Before this existed,
// that route withheld a path while this one printed the very same path in
// details[], which made the pair's redaction partial in practice.
func (s *Server) handleHealthDoctor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	// Listener provenance, resolved at the boundary (never from a request
	// field) — the same signal the ETW status route and the /ws/launch writer
	// bridge use.
	remote := remoteExposedFromContext(r.Context())
	cfg, err := loadConfigForDashboard(s.opts.ConfigPath)
	if err != nil {
		writeErr(w, err)
		return
	}
	exe, _ := os.Executable()
	report := diag.Run(r.Context(), diag.DoctorOptions{
		Config: cfg,
		// s.opts.DB, not s.db(): doctor diagnoses the real install
		// even while demo mode is active (P6.7).
		DB:         s.opts.DB,
		BinaryPath: exe,
		HomeDir:    setupWizardHome, // "" in production; test sandbox override
	})

	// ONE choke point for the disclosure policy: the projection is decided
	// here, once, for every check — never per check and never inside diag,
	// which has a CLI caller that must keep printing the real paths.
	var redactor pathRedactor
	if remote {
		redactor = s.doctorRedactor(cfg, exe)
	}
	checks := make([]doctorCheck, 0, len(report.Checks))
	for _, c := range report.Checks {
		row := doctorCheckRow(c)
		if remote {
			row.Message = redactor.redact(row.Message)
			row.Details = redactor.redactAll(row.Details)
		}
		checks = append(checks, row)
	}
	ok, warn, fail := report.Counts()
	writeJSON(w, doctorReport{
		Checks:              checks,
		OK:                  ok,
		Warn:                warn,
		Fail:                fail,
		AllOK:               fail == 0 && warn == 0,
		GeneratedAt:         time.Now().UTC().Format(time.RFC3339),
		LocalDetailWithheld: remote,
	})
}
