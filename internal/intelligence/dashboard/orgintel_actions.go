package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/store"
)

// orgintel_actions.go serves the node's org-plane Cloud Intelligence ACTIONS
// (parity phase 3, design section 3.2):
//
//	GET  /api/intel/session/{id}   one session's plane, the actions open to the
//	                               developer, its latest submission and the
//	                               cached org result (+ a confirm token)
//	GET  /api/intel/preview?session_id=
//	                               the exact org-plane evidence bytes and both
//	                               digests; builds locally, sends nothing
//	POST /api/intel/enrich         {"session_id","upload_digest"}: queue and
//	                               send one developer-started submission
//	GET  /api/intel/ledger[?id=N]  every submission with its digests (one row
//	                               with its exact bytes while it still holds
//	                               them)
//
// Every route calls the SAME cmd-side functions as `observer intel session |
// preview | enrich | ledger` through Options.OrgIntel, so the two surfaces can
// never disagree.

func (s *Server) orgIntelActionsOr501(w http.ResponseWriter) OrgIntelActionManager {
	m, ok := s.opts.OrgIntel.(OrgIntelActionManager)
	if s.opts.OrgIntel == nil || !ok {
		http.Error(w, "org session enrichment is not available in this build", http.StatusNotImplemented)
		return nil
	}
	return m
}

// orgIntelErrStatus maps the typed refusals onto statuses and codes
// (table-driven; first match wins).
var orgIntelErrStatus = []struct {
	err    error
	status int
	code   string
}{
	{ErrOrgIntelPreviewRequired, http.StatusConflict, "preview_required"},
	{ErrOrgIntelNotFound, http.StatusNotFound, "not_found"},
	{ErrOrgIntelConflict, http.StatusConflict, "not_applicable"},
}

func writeOrgIntelActionErr(w http.ResponseWriter, err error) {
	status, code := http.StatusInternalServerError, "internal"
	for _, row := range orgIntelErrStatus {
		if errors.Is(err, row.err) {
			status, code = row.status, row.code
			break
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "message": err.Error()})
}

// handleIntelSession serves GET /api/intel/session/{id}.
func (s *Server) handleIntelSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/intel/session/")
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}
	m := s.orgIntelActionsOr501(w)
	if m == nil {
		return
	}
	view, err := m.Session(r, id)
	if err != nil {
		writeOrgIntelActionErr(w, err)
		return
	}
	results, err := store.New(s.opts.DB).OrgIntelResultsForSession(r.Context(), id)
	if err != nil {
		http.Error(w, fmt.Sprintf("load org intel: %v", err), http.StatusInternalServerError)
		return
	}
	if len(results) > 0 {
		view.Enriched, view.Result = true, newOrgIntelResultWire(results[0])
	}
	tok := setConfirmCookie(w, r)
	writeJSON(w, map[string]any{"confirm_token": tok, "session": view})
}

// handleIntelPreview serves GET /api/intel/preview?session_id=.
func (s *Server) handleIntelPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("session_id")
	if id == "" {
		http.Error(w, "missing session_id", http.StatusBadRequest)
		return
	}
	m := s.orgIntelActionsOr501(w)
	if m == nil {
		return
	}
	p, err := m.Preview(r, id)
	if err != nil {
		writeOrgIntelActionErr(w, err)
		return
	}
	writeJSON(w, p)
}

// handleIntelEnrich serves POST /api/intel/enrich.
func (s *Server) handleIntelEnrich(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !requireConfirmToken(w, r) {
		return
	}
	m := s.orgIntelActionsOr501(w)
	if m == nil {
		return
	}
	var body struct {
		SessionID    string `json:"session_id"`
		UploadDigest string `json:"upload_digest"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil || body.SessionID == "" {
		http.Error(w, "invalid JSON: session_id is required", http.StatusBadRequest)
		return
	}
	res, err := m.Enrich(r, body.SessionID, body.UploadDigest)
	if err != nil {
		writeOrgIntelActionErr(w, err)
		return
	}
	s.recordManageAudit(r, "org_intel_enrich", fmt.Sprintf("session=%s state=%s", body.SessionID, res.Submission.State))
	writeJSON(w, res)
}

// handleIntelLedger serves GET /api/intel/ledger[?id=N].
func (s *Server) handleIntelLedger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	var id int64
	if raw := r.URL.Query().Get("id"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n <= 0 {
			http.Error(w, "id must be a positive integer", http.StatusBadRequest)
			return
		}
		id = n
	}
	m := s.orgIntelActionsOr501(w)
	if m == nil {
		return
	}
	l, err := m.Ledger(r, id)
	if err != nil {
		writeOrgIntelActionErr(w, err)
		return
	}
	writeJSON(w, l)
}
