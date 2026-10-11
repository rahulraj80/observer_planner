package dashboard

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
)

// update.go is the dashboard's ONE contribution to enterprise update
// management (docs/plans/enterprise-update-management-plan-2026-09-07.md
// §3.7/§3.10): a loopback-only route that lets `observer update apply` reach
// the DAEMON.
//
// Why the CLI cannot just do it itself: the drain and the fork-exec-and-watch
// handshake are only meaningful in the process that OWNS the listeners. A CLI
// that swapped the binary under a running daemon would leave that daemon
// executing its old inode, with no parent watching a successor that was never
// spawned — precisely the failure the handshake exists to prevent.
//
// The dashboard package holds no update logic. It carries a request across a
// process boundary and applies one authorization decision; everything else
// lives behind Options.UpdateApplyFunc in cmd/observer.

// maxUpdateRequestBytes caps the request body. The shape is four small
// fields; anything larger is not a client of this endpoint.
const maxUpdateRequestBytes = 64 << 10

// handleUpdateApply serves POST /api/update/apply.
func (s *Server) handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"POST required"}`, http.StatusMethodNotAllowed)
		return
	}
	if s.opts.UpdateApplyFunc == nil {
		// A nil seam IS the disabled state, and saying so is better than a
		// 404 the caller would read as "old daemon".
		http.Error(w, `{"error":"in-place updates aren't available in this process mode; apply the update manually"}`, http.StatusNotImplemented)
		return
	}
	var req UpdateApplyRequest
	body, err := io.ReadAll(io.LimitReader(r.Body, maxUpdateRequestBytes))
	if err != nil {
		http.Error(w, `{"error":"could not read the request"}`, http.StatusBadRequest)
		return
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, `{"error":"the request body is not valid JSON"}`, http.StatusBadRequest)
			return
		}
	}
	// The apply outlives this request by design: a drain plus a handshake can
	// take minutes, and the seam behind UpdateApplyFunc runs it on its own
	// goroutine and its own context. Two consequences are load-bearing here.
	//
	// First, the context is still detached from r.Context(): a client that
	// disconnects must not cancel a binary swap already in progress, because a
	// cancelled swap is the one state with no owner.
	//
	// Second — and this is what the 202 is for — the handler MUST NOT stay
	// open for the apply. This server is stopped with a 5s Shutdown budget
	// that waits for ACTIVE connections, and the apply itself is what triggers
	// that stop (it releases the listeners mid-handshake). A handler holding
	// the connection therefore made a slow child boot expire the budget, which
	// start.go's errgroup turned into an exit of the SUPERVISING PARENT, with
	// the child alive and `applying` never settled.
	res, err := s.opts.UpdateApplyFunc(context.WithoutCancel(r.Context()), req)
	if err != nil {
		res.Error = err.Error()
		s.opts.Logger.Warn("update apply reported a failure", "state", res.State, "error", err)
		writeJSON(w, res)
		return
	}
	if res.Accepted {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(res)
		return
	}
	writeJSON(w, res)
}

// --- W5: the read side (Settings -> Health card + the update banner) --------
//
// GET /api/update/status is the ONE loopback read the node dashboard needs in
// order to show the org-served answer beside the existing click-gated npm
// probe. It introduces no OUTBOUND request: everything it reports is already
// on this daemon, written by the push cycle and the apply path.
//
// Like the apply route, this package holds no update logic — the composition
// lives behind Options.UpdateStatusFunc in cmd/observer.

// handleUpdateStatus serves GET /api/update/status.
func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if s.opts.UpdateStatusFunc == nil {
		// The honest disabled shape: a 200 saying the feature is off beats a
		// 404 the frontend would have to guess about. The card renders
		// "update management is not enabled on this node".
		writeJSON(w, UpdateStatusResult{})
		return
	}
	res, err := s.opts.UpdateStatusFunc(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, res)
}

// UpdateExtensionVersionRequest is POST /api/update/extension-version.
type UpdateExtensionVersionRequest struct {
	// Version is the VS Code extension's own version. EMPTY IS A VALID
	// REPORT and means "no extension version to report" — ruling R6's honest
	// narrowing: a node with no extension reports nothing, never "0".
	Version string `json:"version"`
}

// handleUpdateExtensionVersion serves POST /api/update/extension-version.
//
// The VS Code extension calls it once per activation. It exists because the
// daemon cannot discover the extension's version on its own: the extension is
// a separate process with its own release channel, and R6's narrowing is that
// the node reports an extension version only when one is RUNNING and has said
// so.
//
// CapabilityLocal, like the LOC editor-change route it mirrors: it is a
// loopback-only report from an editor sharing this machine, not something a
// remote caller has any business writing.
func (s *Server) handleUpdateExtensionVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"POST required"}`, http.StatusMethodNotAllowed)
		return
	}
	if s.opts.UpdateExtensionVersionFunc == nil {
		http.Error(w, `{"error":"update management isn't enabled in this process mode"}`, http.StatusNotImplemented)
		return
	}
	var req UpdateExtensionVersionRequest
	body, err := io.ReadAll(io.LimitReader(r.Body, maxUpdateRequestBytes))
	if err != nil {
		http.Error(w, `{"error":"could not read the request"}`, http.StatusBadRequest)
		return
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, `{"error":"the request body is not valid JSON"}`, http.StatusBadRequest)
			return
		}
	}
	s.opts.UpdateExtensionVersionFunc(req.Version)
	w.WriteHeader(http.StatusNoContent)
}
