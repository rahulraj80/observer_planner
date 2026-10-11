package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// orgintel_consent.go serves the node's ORG-PLANE Cloud Intelligence consent
// surface (docs/plans/cloud-intelligence-parity-design-2026-10-03.md sections
// 3.2, 3.3 and 3.7):
//
//	GET  /api/intel/status    the org's terms, this machine's consent, pending
//	                          state (and a confirm token for the writes)
//	POST /api/intel/accept    the interactive accept ({"disclosure_digest"}:
//	                          the digest of the text the screen showed)
//	POST /api/intel/withdraw  turn the opt-in off
//
// Every route calls the SAME cmd-side functions as `observer intel status |
// accept | disable` through Options.OrgIntel (cmd-wired), so the dashboard and
// the CLI can never disagree. The writes are Local + confirm-token, like the
// enrolment manager: they write this machine's config.toml and its consent
// record.

func (s *Server) orgIntelOr501(w http.ResponseWriter) OrgIntelManager {
	if s.opts.OrgIntel == nil {
		http.Error(w, "org session enrichment is not available in this build", http.StatusNotImplemented)
		return nil
	}
	return s.opts.OrgIntel
}

func writeOrgIntelErr(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	code := "internal"
	if errors.Is(err, ErrOrgIntelConflict) {
		status, code = http.StatusConflict, "not_applicable"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "message": err.Error()})
}

// handleIntelStatus serves GET /api/intel/status.
func (s *Server) handleIntelStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	m := s.orgIntelOr501(w)
	if m == nil {
		return
	}
	st, err := m.Status(r)
	if err != nil {
		writeOrgIntelErr(w, err)
		return
	}
	tok := setConfirmCookie(w, r)
	writeJSON(w, map[string]any{"confirm_token": tok, "status": st})
}

// handleIntelAccept serves POST /api/intel/accept.
func (s *Server) handleIntelAccept(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !requireConfirmToken(w, r) {
		return
	}
	m := s.orgIntelOr501(w)
	if m == nil {
		return
	}
	var body struct {
		DisclosureDigest string `json:"disclosure_digest"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if body.DisclosureDigest == "" {
		writeOrgIntelErr(w, fmt.Errorf("%w: the accept must name the digest of the terms that were shown", ErrOrgIntelConflict))
		return
	}
	st, err := m.Accept(r, body.DisclosureDigest)
	if err != nil {
		writeOrgIntelErr(w, err)
		return
	}
	s.recordManageAudit(r, "org_intel_accept", fmt.Sprintf("version=%d", activeVersion(st)))
	writeJSON(w, map[string]any{"status": st})
}

// handleIntelWithdraw serves POST /api/intel/withdraw.
func (s *Server) handleIntelWithdraw(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !requireConfirmToken(w, r) {
		return
	}
	m := s.orgIntelOr501(w)
	if m == nil {
		return
	}
	st, err := m.Withdraw(r)
	if err != nil {
		writeOrgIntelErr(w, err)
		return
	}
	s.recordManageAudit(r, "org_intel_withdraw", "")
	writeJSON(w, map[string]any{"status": st})
}

func activeVersion(st OrgIntelStatus) int64 {
	if st.Active == nil {
		return 0
	}
	return st.Active.RequirementVersion
}
