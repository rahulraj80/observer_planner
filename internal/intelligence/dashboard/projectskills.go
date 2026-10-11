package dashboard

import (
	"net/http"
	"time"

	"github.com/marmutapp/superbased-observer/internal/store"
)

// projectskills.go is the ONE composition seam for the Projects-page Skills
// tab (S10-SKILLS; docs/projects-page.md "Skills: versions across commits
// and sessions"): GET /api/project/{id}/skills (handleProjectSkills) and
// `observer project --skills` (cmd/observer/project.go) both call
// ComposeProjectSkills, so the tab and the CLI can never disagree. It is a
// SEPARATE composition from ComposeProjectDetail on purpose: the detail
// payload stays byte-identical (pinned by
// TestSkillHistoryLeavesExistingEndpointsByteIdentical).
//
// The read is SQL + the pure internal/skillhistory derivation: no git and
// no filesystem walk on a page load.

// skillSkewMargin resolves [projects].skill_history_skew_seconds (0 ->
// the derivation's default).
func (s *Server) skillSkewMargin() time.Duration {
	if s.opts.Projects.SkillHistorySkewSeconds <= 0 {
		return 0
	}
	return time.Duration(s.opts.Projects.SkillHistorySkewSeconds) * time.Second
}

// handleProjectSkills serves GET /api/project/{id}/skills?days=N - the
// Skills tab (S10-SKILLS). Same project resolution and window as the
// sibling tab endpoints.
func (s *Server) handleProjectSkills(w http.ResponseWriter, r *http.Request) {
	projectID, ok := parseProjectID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	st := store.New(s.db())
	if _, ok := requireProject(ctx, w, r, st, projectID); !ok {
		return
	}
	since, until, days := projectDetailWindow(r)
	out, err := ComposeProjectSkills(ctx, st, ProjectSkillsInput{
		ProjectID: projectID, Days: days, Since: since, Until: until, SkewMargin: s.skillSkewMargin(),
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, out)
}
