package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
)

// Profile experiments (usability arc P6.4 / review §8.2): productized
// A/B over Track-R profiles. Definitions live as [[experiments]] in
// config.toml (written through the ONE save pipeline, commitConfigEdit,
// whose live seams fire the OnConfigSaved hot-reload hook, so a started
// experiment splits NEW sessions immediately); arm membership is a pure hash of the session
// ID, recomputed at report time — no arm state is persisted and
// nothing new enters the org-push wire.
//
// Reports are EVIDENCE, not verdicts: per-arm n / $ / CV / turns /
// cache causes / compression savings, presented next to the §2.4
// methodology's decision-rule guidance. The operator applies the rule.

// handleExperiments serves GET /api/experiments (list, fresh from
// config) and POST /api/experiments (start one).
func (s *Server) handleExperiments(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg, err := loadConfigForDashboard(s.opts.ConfigPath)
		if err != nil {
			writeErr(w, err)
			return
		}
		exps := cfg.Experiments
		if exps == nil {
			exps = []config.ExperimentConfig{}
		}
		// config_etag is the base a start / stop sends back as ?base_etag=
		// (T-639), so a file changed since this list was read is refused
		// with 409 config_changed instead of overwritten.
		etag, _ := configEtag(s.opts.ConfigPath)
		writeJSON(w, map[string]any{"experiments": exps, "config_etag": etag})
	case http.MethodPost:
		s.handleExperimentStart(w, r)
	default:
		http.Error(w, "GET or POST", http.StatusMethodNotAllowed)
	}
}

// handleExperimentStart validates and appends a RUNNING experiment.
// Refusals are loud: bad shape, unknown profiles, a duplicate name,
// or a second running experiment on the same class (the arm hash is
// salted per experiment — two simultaneous splits of one class would
// fight over sessions).
func (s *Server) handleExperimentStart(w http.ResponseWriter, r *http.Request) {
	if s.opts.ConfigPath == "" {
		http.Error(w, "no config path configured", http.StatusConflict)
		return
	}
	var body struct {
		Name      string `json:"name"`
		Class     string `json:"class"`
		Control   string `json:"control"`
		Candidate string `json:"candidate"`
		Note      string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad body: "+err.Error(), http.StatusBadRequest)
		return
	}
	exp := config.ExperimentConfig{
		Name:      strings.TrimSpace(body.Name),
		Class:     strings.TrimSpace(body.Class),
		Control:   strings.TrimSpace(body.Control),
		Candidate: strings.TrimSpace(body.Candidate),
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		Note:      body.Note,
	}
	if err := config.ValidateExperiment(exp); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	store := config.ProfileStore{Dir: config.DefaultProfilesDir(s.opts.ConfigPath)}
	for _, p := range []string{exp.Control, exp.Candidate} {
		if err := store.Validate(p); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	// The ONE save pipeline (T-639, like T-572): `?dry_run=1` previews the
	// masked diff and writes nothing, `?base_etag=` refuses a file changed
	// since that preview, and the write is validated, read back, hot-reloaded
	// and audited. The duplicate / class checks run on the config loaded
	// under the save's lock, so they keep their 409.
	s.commitConfigEdit(w, r, configEdit{
		section: "experiments",
		edit: func(cfg *config.Config) (any, error) {
			for _, e := range cfg.Experiments {
				if e.Name == exp.Name {
					return nil, configEditRefusal{status: http.StatusConflict, msg: fmt.Sprintf("experiment %q already exists (stopped experiments keep their record - pick a new name)", exp.Name)}
				}
				if e.Running() && e.Class == exp.Class {
					return nil, configEditRefusal{status: http.StatusConflict, msg: fmt.Sprintf("experiment %q is already running on class %q - stop it first", e.Name, e.Class)}
				}
			}
			cfg.Experiments = append(cfg.Experiments, exp)
			return nil, nil
		},
		// Metadata only: the verb and the traffic class, never the
		// operator's experiment name or note.
		auditAction: "experiment",
		auditDetail: "op=start class=" + exp.Class,
		afterWrite: func(config.Config) map[string]any {
			return map[string]any{"started": exp, "hot": s.opts.OnConfigSaved != nil}
		},
	})
}

// handleExperimentStop serves POST /api/experiments/stop {"name"} —
// stamps StoppedAt, freezing the reporting window. The record stays
// for its report.
func (s *Server) handleExperimentStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if s.opts.ConfigPath == "" {
		http.Error(w, "no config path configured", http.StatusConflict)
		return
	}
	// The ONE save pipeline (T-639): dry run, base etag, read-back, audit.
	s.commitConfigEdit(w, r, configEdit{
		section: "experiments",
		edit: func(cfg *config.Config) (any, error) {
			for i := range cfg.Experiments {
				if cfg.Experiments[i].Name != body.Name {
					continue
				}
				if !cfg.Experiments[i].Running() {
					return nil, configEditRefusal{status: http.StatusConflict, msg: fmt.Sprintf("experiment %q is not running", body.Name)}
				}
				cfg.Experiments[i].StoppedAt = time.Now().UTC().Format(time.RFC3339)
				return nil, nil
			}
			return nil, configEditRefusal{status: http.StatusNotFound, msg: fmt.Sprintf("unknown experiment %q", body.Name)}
		},
		auditAction: "experiment",
		auditDetail: "op=stop",
		afterWrite: func(config.Config) map[string]any {
			return map[string]any{"stopped": body.Name}
		},
	})
}

// handleExperimentReport serves GET /api/experiments/report?name=N.
func (s *Server) handleExperimentReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	name := r.URL.Query().Get("name")
	cfg, err := loadConfigForDashboard(s.opts.ConfigPath)
	if err != nil {
		writeErr(w, err)
		return
	}
	var exp *config.ExperimentConfig
	for i := range cfg.Experiments {
		if cfg.Experiments[i].Name == name {
			exp = &cfg.Experiments[i]
			break
		}
	}
	if exp == nil {
		http.Error(w, fmt.Sprintf("unknown experiment %q", name), http.StatusNotFound)
		return
	}
	report, err := ComputeExperimentReport(r.Context(), s.db(), s.opts.CostEngine, *exp)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, report)
}
