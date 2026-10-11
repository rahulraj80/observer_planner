package dashboard

// projectdetail.go holds the ONE composition seam for the Projects-page
// detail summary (plan §3.4) — the nine store loads, the
// projectroi.Link/AttributeSpend/Proxies pipeline and the apiProjectDetail
// assembly that GET /api/project/{id} (handleProjectDetail) and
// `observer project` (cmd/observer/project.go, wave W6) both call, so the
// CLI and the dashboard page can never disagree about what a project's
// spend/ROI/commit picture is. Extracted from handleProjectDetail without
// behavior change — see internal/intelligence/dashboard/projects_test.go's
// -run Project tests, which must still pass byte-identically.
