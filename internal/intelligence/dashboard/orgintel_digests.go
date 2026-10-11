package dashboard

import (
	"net/http"
)

// orgintel_digests.go serves the developer's read-only view of the weekly org
// project digests (T-490, D-328 item 3, D-241):
//
//	GET /api/intel/digests   the org's finished weekly digests of the
//	                         projects THIS member works in
//
// The read is a signed pull from the org server the machine is enrolled with
// (orgclient.FetchProjectDigests): the server scopes it by the member's own
// sessions, applies the org's read posture, audits the view and never serves
// a digest a purge or consent withdrawal invalidated. Nothing is cached here.
// The route is Local: each call is an authenticated, audited request to the
// org on the developer's behalf, so it is not remotely reachable.

// handleIntelDigests serves GET /api/intel/digests.
func (s *Server) handleIntelDigests(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	reader, ok := s.opts.OrgIntel.(OrgIntelDigestReader)
	if !ok || reader == nil {
		http.Error(w, "org project digests are not available in this build", http.StatusNotImplemented)
		return
	}
	out, err := reader.ProjectDigests(r)
	if err != nil {
		writeOrgIntelErr(w, err)
		return
	}
	if out.Digests == nil {
		out.Digests = []OrgIntelDigestView{}
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, out)
}
