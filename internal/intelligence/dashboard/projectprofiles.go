package dashboard

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/textdiff"
)

// Per-project compression-profile assignment (T-421 item 1): the dashboard
// front door for `observer config set --project <root> profiles.<...>`, the
// repo-local <root>/.observer/config.toml project layer. Both front doors
// plan and apply through config.PlanProjectOverlayEdit /
// ApplyProjectOverlayEdit (the one key-preserving owner of that file) and
// resolve the target through config.ParseProfileTarget; this file owns the
// HTTP shape, the known-project gate and the preview-first protocol.
//
// Route: /api/config/project-profiles, whole-route Local and guarded as the
// Settings "profiles" subsection, the same posture as the per-tool card
// beside it (T-369), so the governance guard's hidden / read-only rules
// cover it with no extra rule.
//
//	GET    the known projects with what each one's project file assigns,
//	       the resolvable profile names and the target vocabulary.
//	PUT    {"root","target","profile","preview":true}: the planned change
//	       (path, before, after, base_sha); nothing is written.
//	PUT    {"root","target","profile","base_sha"}: apply that plan.
//	DELETE the same two steps for removing one assignment (the project then
//	       falls back to the global assignment).
//
// Writes carry the confirm token and serialize on configWriteMu. An apply
// must name the base_sha its preview returned; a file that changed in
// between is refused (409) instead of overwritten. Only a root this node
// already knows (the projects table) is accepted, and the config owner
// refuses the home directory, the master config and any symlinked
// .observer path. The daemon picks the change up on the project's next
// session (the overlay cache is keyed on the file's mtime and size), so no
// reload poke is needed.

// projectProfilesLimit caps the projects one GET reads (one stat and one
// small file read each).
const projectProfilesLimit = 500

// projectProfileRow is one known project as the card lists it.
type projectProfileRow struct {
	Root string `json:"root"`
	// HasFile reports whether <root>/.observer/config.toml exists.
	HasFile bool `json:"has_file"`
	// Default / ByProvider / ByTool are what the project file sets
	// (empty = not set there; the global assignment applies).
	Default    string            `json:"default,omitempty"`
	ByProvider map[string]string `json:"by_provider,omitempty"`
	ByTool     map[string]string `json:"by_tool,omitempty"`
	// Error is a project file that exists but does not parse (the daemon
	// ignores it until fixed; the card says so instead of hiding it).
	Error string `json:"error,omitempty"`
	// Refusal says why this project's file cannot be written from here
	// (the directory is gone, it is the home directory, a symlinked
	// .observer); empty when it can.
	Refusal string `json:"refusal,omitempty"`
}

// projectProfilesResponse is the GET body.
type projectProfilesResponse struct {
	Projects      []projectProfileRow `json:"projects"`
	Truncated     bool                `json:"truncated"`
	ProfileNames  []string            `json:"profile_names"`
	Providers     []string            `json:"providers"`
	Tools         []profileAssignTool `json:"tools"`
	GlobalDefault string              `json:"global_default"`
}

// projectProfileRequest is the PUT / DELETE body.
type projectProfileRequest struct {
	Root    string `json:"root"`
	Target  string `json:"target"`
	Profile string `json:"profile"`
	Preview bool   `json:"preview"`
	BaseSHA string `json:"base_sha"`
}

// projectProfileEditResponse is a write's (or preview's) body.
type projectProfileEditResponse struct {
	Root    string `json:"root"`
	Path    string `json:"path"`
	Key     string `json:"key"`
	Existed bool   `json:"existed"`
	Changed bool   `json:"changed"`
	Before  string `json:"before"`
	After   string `json:"after"`
	// Diff is the unified diff of Before -> After (internal/textdiff, the
	// same renderer the config.toml preview uses); empty when unchanged.
	Diff string `json:"diff"`
	// BaseSHA is the sha256 of Before; an apply echoes it.
	BaseSHA string `json:"base_sha"`
	// Applied is true when this response wrote the file.
	Applied bool               `json:"applied"`
	Project *projectProfileRow `json:"project,omitempty"`
}

// knownProjectRoots returns the project roots this node knows, most recently
// active first, capped at limit (truncated reports the cap bit). It reads the
// real install's database, never the demo one: the write it gates lands on
// this machine's files.
func (s *Server) knownProjectRoots(ctx context.Context, limit int) (roots []string, truncated bool, err error) {
	if s.opts.DB == nil {
		return nil, false, nil
	}
	rows, err := s.opts.DB.QueryContext(ctx,
		`SELECT root_path FROM projects
		 WHERE root_path <> ''
		 ORDER BY COALESCE(last_session_at, created_at) DESC, id DESC
		 LIMIT ?`, limit+1)
	if err != nil {
		return nil, false, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var root string
		if err := rows.Scan(&root); err != nil {
			return nil, false, err
		}
		roots = append(roots, root)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(roots) > limit {
		return roots[:limit], true, nil
	}
	return roots, false, nil
}

// isKnownProjectRoot reports whether root is exactly a projects.root_path.
func (s *Server) isKnownProjectRoot(ctx context.Context, root string) (bool, error) {
	if s.opts.DB == nil || root == "" {
		return false, nil
	}
	var one int
	err := s.opts.DB.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE root_path = ? LIMIT 1`, root).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("look up project: %w", err)
	}
	return true, nil
}

// masterConfigPath is the resolved global config path the project writer
// must never touch.
func (s *Server) masterConfigPath() string {
	p, err := config.ResolveGlobalPath(s.opts.ConfigPath)
	if err != nil {
		return s.opts.ConfigPath
	}
	return p
}

// projectProfileRowFor reads one project's file.
func (s *Server) projectProfileRowFor(root string) projectProfileRow {
	row := projectProfileRow{Root: root}
	if err := config.CheckProjectOverlayTarget(root, s.masterConfigPath()); err != nil {
		row.Refusal = strings.TrimPrefix(err.Error(), "config: ")
	}
	if _, err := os.Stat(root); err != nil {
		return row
	}
	pc, exists, err := config.ReadProjectOverlayProfiles(root)
	row.HasFile = exists
	if err != nil {
		row.Error = strings.TrimPrefix(err.Error(), "config: ")
		return row
	}
	row.Default = pc.Default
	if len(pc.ByProvider) > 0 {
		row.ByProvider = pc.ByProvider
	}
	if len(pc.ByTool) > 0 {
		row.ByTool = pc.ByTool
	}
	return row
}

// handleProjectProfiles serves /api/config/project-profiles.
func (s *Server) handleProjectProfiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listProjectProfiles(w, r)
	case http.MethodPut, http.MethodDelete:
		s.writeProjectProfile(w, r)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		http.Error(w, "GET, PUT or DELETE", http.StatusMethodNotAllowed)
	}
}

func (s *Server) listProjectProfiles(w http.ResponseWriter, r *http.Request) {
	cfg, err := loadConfigForDashboard(s.opts.ConfigPath)
	if err != nil {
		writeErr(w, err)
		return
	}
	roots, truncated, err := s.knownProjectRoots(r.Context(), projectProfilesLimit)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := projectProfilesResponse{
		Projects:      make([]projectProfileRow, 0, len(roots)),
		Truncated:     truncated,
		ProfileNames:  s.profileStore().Names(),
		Providers:     config.ProfileProviderClasses(),
		Tools:         profileAssignTools(),
		GlobalDefault: s.profileAssignmentsBody(cfg.Profiles, false).Default,
	}
	for _, root := range roots {
		out.Projects = append(out.Projects, s.projectProfileRowFor(root))
	}
	writeJSON(w, out)
}

// writeProjectProfile is the PUT / DELETE half: preview, or apply a preview.
func (s *Server) writeProjectProfile(w http.ResponseWriter, r *http.Request) {
	if !requireJSONConfirm(w, r) {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<10))
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	var req projectProfileRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "decode body: "+err.Error(), http.StatusBadRequest)
		return
	}
	known, err := s.isKnownProjectRoot(r.Context(), req.Root)
	if err != nil {
		writeErr(w, err)
		return
	}
	if !known {
		http.Error(w, "not a project this node knows: pick one from the list", http.StatusBadRequest)
		return
	}
	target, err := config.ParseProfileTarget(req.Target)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	remove := r.Method == http.MethodDelete
	if !remove {
		if target.Kind == config.ProfileTargetTool && !knownAssignTool(target.Key) {
			http.Error(w, fmt.Sprintf("unknown tool %q: pick a tool from the list", target.Key), http.StatusBadRequest)
			return
		}
		if err := s.profileStore().Validate(req.Profile); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	if !req.Preview && strings.TrimSpace(req.BaseSHA) == "" {
		http.Error(w, "preview the change first: an apply must carry the base_sha its preview returned", http.StatusBadRequest)
		return
	}
	key := config.ProfileTargetKey(target)

	// Same lock domain as every other in-process config-file writer.
	s.configWriteMu.Lock()
	defer s.configWriteMu.Unlock()

	edit, err := config.PlanProjectOverlayEdit(config.ProjectOverlayEditRequest{
		Root: req.Root, Key: key, Value: req.Profile, Remove: remove, ExcludeFile: s.masterConfigPath(),
	})
	if err != nil {
		http.Error(w, strings.TrimPrefix(err.Error(), "config: "), http.StatusBadRequest)
		return
	}
	sum := sha256.Sum256(edit.Before)
	resp := projectProfileEditResponse{
		Root: req.Root, Path: edit.Path, Key: key, Existed: edit.Existed, Changed: edit.Changed,
		Before: string(edit.Before), After: string(edit.After), BaseSHA: hex.EncodeToString(sum[:]),
	}
	if edit.Changed {
		resp.Diff, _ = textdiff.Unified(edit.Path, edit.Path+" (after)", resp.Before, resp.After, 3)
	}
	if req.Preview {
		writeJSON(w, resp)
		return
	}
	if req.BaseSHA != resp.BaseSHA {
		http.Error(w, "the project file changed since the preview; preview again", http.StatusConflict)
		return
	}
	if err := config.ApplyProjectOverlayEdit(edit); err != nil {
		if errors.Is(err, config.ErrProjectOverlayChanged) {
			http.Error(w, "the project file changed since the preview; preview again", http.StatusConflict)
			return
		}
		writeErr(w, err)
		return
	}
	if edit.Changed {
		verb := "assign"
		if remove {
			verb = "remove"
		}
		s.recordManageAudit(r, "project_profile_"+verb, fmt.Sprintf("key=%s profile=%s root=%s", key, req.Profile, req.Root))
	}
	resp.Applied = edit.Changed
	row := s.projectProfileRowFor(req.Root)
	resp.Project = &row
	writeJSON(w, resp)
}
