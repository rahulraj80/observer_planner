package dashboard

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/integration"
)

// Per-tool compression-profile assignment (T-369): the dashboard front door
// for `observer profile assign tool:NAME <profile>`. Both front doors share
// config.ParseProfileTarget / AssignProfile / UnassignProfile (the one pure
// owner of the target grammar and the edit) and the ProfileStore's name
// validation; this file only owns the HTTP shape and the write envelope.
//
// Route: /api/config/profile-assignments, whole-route Local and guarded as
// the Settings "profiles" subsection, the same posture as the profile CRUD
// routes beside it (so the governance guard's hidden / read-only rules and
// the SPA's shared concealed-route list cover it with no extra rule).
//
//	GET    the current assignment table, the resolvable profile names and
//	       the tool vocabulary the picker offers.
//	PUT    {"target":"tool:cline","profile":"codex-variant"}: assign.
//	DELETE {"target":"tool:cline"}: remove a tool assignment (the tool
//	       falls back to its provider assignment).
//
// Writes carry the confirm token, serialize on configWriteMu, validate
// before the write, verify the file read back, and poke the config-saved
// hook so the proxy's profile router serves NEW sessions with the change
// (no restart; in-flight sessions keep their parameters).

// profileAssignTool is one entry of the picker's tool vocabulary.
type profileAssignTool struct {
	ID string `json:"id"`
	// Proxied is true when the registry declares a proxy route Observer
	// drives today. An assignment for an unproxied tool is legal (it applies
	// once that tool's traffic reaches the proxy) but changes nothing yet,
	// so the picker says so instead of hiding the tool.
	Proxied bool `json:"proxied"`
}

// profileAssignmentsResponse is the GET (and every write's) body.
type profileAssignmentsResponse struct {
	Default      string              `json:"default"`
	ByProvider   map[string]string   `json:"by_provider"`
	ByTool       map[string]string   `json:"by_tool"`
	ProfileNames []string            `json:"profile_names"`
	Tools        []profileAssignTool `json:"tools"`
	// Changed reports whether a write altered the file (false for a GET and
	// for a no-op removal).
	Changed bool `json:"changed"`
	// ConfigEtag is set on a GET only: the base a write sends back as
	// ?base_etag= (T-639). A write's answer carries the pipeline's own
	// config_etag of the saved file.
	ConfigEtag string `json:"config_etag,omitempty"`
}

// profileAssignRequest is the PUT / DELETE body.
type profileAssignRequest struct {
	Target  string `json:"target"`
	Profile string `json:"profile"`
}

// profileAssignTools is the picker vocabulary: every registry tool whose
// lifecycle is still advertised (a dead or deprecated alias is not offered,
// the same predicate every advertising surface uses), each flagged with
// whether its traffic is proxied today. Capability shape, never a tool name.
func profileAssignTools() []profileAssignTool {
	out := []profileAssignTool{}
	for _, c := range integration.Capabilities() {
		if !c.Lifecycle.Advertised() {
			continue
		}
		out = append(out, profileAssignTool{ID: c.Tool, Proxied: c.Proxy != nil})
	}
	slices.SortFunc(out, func(a, b profileAssignTool) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// knownAssignTool reports whether tool is in the closed registry vocabulary.
// The dashboard refuses a free-typed tool id (a sanity check before the
// write); an assignment that already names an unknown tool can still be
// removed.
func knownAssignTool(tool string) bool {
	_, ok := integration.For(tool)
	return ok
}

// profileAssignmentsBody renders the assignment table for cfg.
func (s *Server) profileAssignmentsBody(pc config.ProfilesConfig, changed bool) profileAssignmentsResponse {
	byProvider := pc.ByProvider
	if byProvider == nil {
		byProvider = map[string]string{}
	}
	byTool := pc.ByTool
	if byTool == nil {
		byTool = map[string]string{}
	}
	def := pc.Default
	if def == "" {
		def = config.DefaultProfileName
	}
	return profileAssignmentsResponse{
		Default:      def,
		ByProvider:   byProvider,
		ByTool:       byTool,
		ProfileNames: s.profileStore().Names(),
		Tools:        profileAssignTools(),
		Changed:      changed,
	}
}

// handleProfileAssignments serves /api/config/profile-assignments.
func (s *Server) handleProfileAssignments(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		etag, _ := configEtag(s.opts.ConfigPath)
		cfg, err := loadConfigForDashboard(s.opts.ConfigPath)
		if err != nil {
			writeErr(w, err)
			return
		}
		body := s.profileAssignmentsBody(cfg.Profiles, false)
		body.ConfigEtag = etag
		writeJSON(w, body)
	case http.MethodPut, http.MethodDelete:
		s.writeProfileAssignment(w, r)
	default:
		http.Error(w, "GET, PUT or DELETE", http.StatusMethodNotAllowed)
	}
}

// writeProfileAssignment is the PUT / DELETE half.
func (s *Server) writeProfileAssignment(w http.ResponseWriter, r *http.Request) {
	if !requireJSONConfirm(w, r) {
		return
	}
	if s.opts.ConfigPath == "" {
		http.Error(w, "config path not configured - server has no file to save to", http.StatusConflict)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<10))
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	var req profileAssignRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "decode body: "+err.Error(), http.StatusBadRequest)
		return
	}
	target, err := config.ParseProfileTarget(req.Target)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if r.Method == http.MethodPut {
		if target.Kind == config.ProfileTargetTool && !knownAssignTool(target.Key) {
			http.Error(w, fmt.Sprintf("unknown tool %q: pick a tool from the list", target.Key), http.StatusBadRequest)
			return
		}
		if err := s.profileStore().Validate(req.Profile); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	// The ONE save pipeline (T-572): `?dry_run=1` previews the masked diff
	// and writes nothing, `?base_etag=` refuses a file changed since that
	// preview, and the write is read back and audited like every section save.
	op := "assign"
	if r.Method == http.MethodDelete {
		op = "unassign"
	}
	s.commitConfigEdit(w, r, configEdit{
		section: "profiles",
		edit: func(cfg *config.Config) (any, error) {
			var next config.ProfilesConfig
			var err error
			if r.Method == http.MethodPut {
				next, err = config.AssignProfile(cfg.Profiles, req.Target, req.Profile)
			} else {
				next, _, err = config.UnassignProfile(cfg.Profiles, req.Target)
			}
			if err != nil {
				return nil, err
			}
			// An edit that changes nothing never rewrites the file (no .bak
			// churn, no reload poke).
			if profilesEqual(cfg.Profiles, next) {
				return s.profileAssignmentsBody(cfg.Profiles, false), nil
			}
			cfg.Profiles = next
			return nil, nil
		},
		// Metadata only: the verb and the target kind, never a profile body.
		auditAction: "profile_assignment",
		auditDetail: fmt.Sprintf("op=%s kind=%s", op, target.Kind),
		extra: func(cfg config.Config) map[string]any {
			return responseFields(s.profileAssignmentsBody(cfg.Profiles, true))
		},
	})
}

// profilesEqual compares two assignment tables (a nil map equals an empty
// one: neither writes a table).
func profilesEqual(a, b config.ProfilesConfig) bool {
	return a.Default == b.Default && maps.Equal(a.ByProvider, b.ByProvider) && maps.Equal(a.ByTool, b.ByTool)
}
