package dashboard

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/config"
)

// adopt_defaults.go is the dashboard twin of `observer config adopt-defaults`
// (backlog item 6, sweep row 19): it appends the compiled-in default adapters
// missing from an explicit [observer.watch] enabled_adapters list. The preview
// is config.AdoptEnabledAdapters (the pure diff the CLI preview, the startup
// WARN and /api/tools/status already share) and the write is
// config.AdoptEnabledAdaptersFile (the CLI's --write path: append only,
// comments and order kept, config.toml.bak first). Nothing else is computed
// here.

// adoptDefaultsResponse is the GET preview and the POST outcome.
type adoptDefaultsResponse struct {
	ConfigPath      string   `json:"config_path"`
	HadExplicitList bool     `json:"had_explicit_list"`
	Existing        []string `json:"existing"`
	Missing         []string `json:"missing"`
	Skipped         bool     `json:"skipped"`
	SkipReason      string   `json:"skip_reason,omitempty"`
	Written         bool     `json:"written"`
	// RestartRequired: enabled_adapters is read once at daemon start, so a
	// newly-added adapter captures nothing until the daemon restarts.
	RestartRequired bool   `json:"restart_required"`
	Error           string `json:"error,omitempty"`
}

// catalogDefaults is the default adapter list, in catalog order: the same
// names `observer config adopt-defaults` takes from adapterdefaults.
func (s *Server) catalogDefaults() []string {
	out := make([]string, len(s.opts.ToolCatalog))
	for i, e := range s.opts.ToolCatalog {
		out[i] = e.Tool
	}
	return out
}

// handleAdoptDefaults serves /api/config/adopt-defaults.
//
//   - GET: the preview (writes nothing).
//   - POST {"adapters": [...]}: the write. The posted list must equal the
//     current preview's missing list: a preview that went stale (the file
//     changed in between) is refused 409 with the fresh preview, so the
//     operator always confirms exactly what is written.
func (s *Server) handleAdoptDefaults(w http.ResponseWriter, r *http.Request) {
	if s.opts.ConfigPath == "" {
		http.Error(w, "no config path: this dashboard was started without a config file", http.StatusConflict)
		return
	}
	preview := func() (adoptDefaultsResponse, error) {
		out := adoptDefaultsResponse{ConfigPath: s.opts.ConfigPath, Existing: []string{}, Missing: []string{}}
		body, err := os.ReadFile(s.opts.ConfigPath)
		if errors.Is(err, os.ErrNotExist) {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		_, res, err := config.AdoptEnabledAdapters(string(body), s.catalogDefaults())
		if err != nil {
			return out, err
		}
		out.HadExplicitList = res.HadExplicitList
		out.Skipped, out.SkipReason = res.Skipped, res.SkipReason
		if res.Existing != nil {
			out.Existing = res.Existing
		}
		if res.Missing != nil {
			out.Missing = res.Missing
		}
		return out, nil
	}

	switch r.Method {
	case http.MethodGet:
		out, err := preview()
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, out)
	case http.MethodPost:
		// Review M3-05: a config write carries the confirm token.
		if !requireJSONConfirm(w, r) {
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<10))
		if err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
		}
		var req struct {
			Adapters []string `json:"adapters"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			http.Error(w, "decode body: "+err.Error(), http.StatusBadRequest)
			return
		}
		cur, err := preview()
		if err != nil {
			writeErr(w, err)
			return
		}
		if !slices.Equal(req.Adapters, cur.Missing) {
			cur.Error = "the config changed since the preview; review the new list and confirm again"
			writeJSONStatus(w, http.StatusConflict, cur)
			return
		}
		if len(cur.Missing) == 0 || cur.Skipped {
			writeJSON(w, cur)
			return
		}
		res, err := config.AdoptEnabledAdaptersFile(s.opts.ConfigPath, s.catalogDefaults())
		if err != nil {
			writeErr(w, err)
			return
		}
		cur.Written = res.Changed && !res.Skipped
		cur.RestartRequired = cur.Written
		if cur.Written {
			s.recordManageAudit(r, "config_adopt_defaults", "added="+strings.Join(res.Missing, ","))
			if s.opts.OnConfigSaved != nil {
				s.opts.OnConfigSaved()
			}
		}
		writeJSON(w, cur)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "GET or POST", http.StatusMethodNotAllowed)
	}
}
