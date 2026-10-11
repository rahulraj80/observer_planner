package dashboard

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// manage_core.go holds the management-surface helpers every Local + confirm
// route shares (the double-submit confirm token, the config-write lock, the
// metadata-only remote_audit writers, the remote-writer revoke), split out of
// remote_manage.go by lane MP-L1I (T-715) so they stay in a build without
// the remote component (-tags sbo_omit_remote), which leaves the remote
// management handlers out.

// decodeJSONBody decodes a small JSON request body into v (bounded read). A
// decode error is tolerated by callers that treat every field as optional.
func decodeJSONBody(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(v)
}

// readFileTrimmed reads a file and returns its trimmed string content.
func readFileTrimmed(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// Remote-management API (dashboard-management-surface plan §9-§11). Every
// MUTATION (enable/disable/rotate/session-revoke) is a CapabilityLocal route -
// reachable only from the owner-trusted loopback listener, never a remote
// principal. The reads (config/audit/sessions/selfcheck) are View. The pairing
// secret rides ONLY the enable/rotate POST *response* (§11); no GET ever
// returns or reconstructs it.

// remoteManageMu serializes the manage verbs' config/secret read-modify-write
// (enable / disable / rotate / add-device / allow-terminal). Each of these
// handlers does loadConfigForManage → mutate → WriteToml (or a remotecfg secret
// mint) with no other synchronization, so two concurrent verbs could clobber
// each other's full-config write. The verbs are rare, owner-loopback-only
// actions, so whole-section serialization is cheap and correct.
//
// LOCK ORDER (Fix 2 - one config-write lock domain): every manage verb that
// read-modify-writes config.toml ALSO takes Server.configWriteMu (the lock the
// settings.go section/pricing/backup writers hold), so a manage verb and a
// section save can never clobber one another. The order is ALWAYS
// remoteManageMu (OUTER) → configWriteMu (INNER), never the reverse: the
// settings.go writers take configWriteMu but NEVER remoteManageMu, so no reverse
// nesting exists and the two locks can't deadlock. Defers unwind LIFO, so
// configWriteMu is released before remoteManageMu at each site.
var remoteManageMu sync.Mutex

const (
	// remoteConfirmCookie carries the per-panel-load double-submit token (§10).
	// Readable (non-httpOnly) so the SPA can echo it in the header; SameSite=
	// Strict + loopback-scoped so a cross-origin page can neither read nor set
	// it. It is unrelated to the pairing secret.
	remoteConfirmCookie = "sb_remote_confirm"
	// remoteConfirmHeader is the header the SPA echoes the confirm token in.
	remoteConfirmHeader = "X-Observer-Confirm" //nolint:gosec // header name, not a credential
)

// mintConfirmToken returns a fresh 32-byte hex token for the double-submit CSRF
// defense (§10).
func mintConfirmToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

// requireConfirmToken enforces the §10 arm-verb hardening as the FIRST action of
// every enable/disable/rotate handler, before any remotecfg call: POST-only
// (405), application/json required (415), and a non-empty confirm token that
// matches the double-submit cookie in constant time (403). It writes the error
// response itself and returns false when the request must be rejected.
func requireConfirmToken(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return requireJSONConfirm(w, r)
}

// requireJSONConfirm is the method-agnostic half of the §10 double-submit
// hardening: application/json required (415) + a non-empty confirm token that
// matches the double-submit cookie in constant time (403). The caller checks
// the HTTP method first (POST for the arm verbs, PUT for the terminal-policy
// write), so the same confirm-token/one-time-mint discipline P0 established for
// the remote arm verbs is reused for the privilege-expanding terminal-launch
// write without duplicating it. It writes the error response itself and returns
// false when the request must be rejected.
func requireJSONConfirm(w http.ResponseWriter, r *http.Request) bool {
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(strings.ToLower(strings.TrimSpace(ct)), "application/json") {
		http.Error(w, "unsupported media type: application/json required", http.StatusUnsupportedMediaType)
		return false
	}
	header := strings.TrimSpace(r.Header.Get(remoteConfirmHeader))
	var cookieVal string
	if ck, err := r.Cookie(remoteConfirmCookie); err == nil {
		cookieVal = strings.TrimSpace(ck.Value)
	}
	if header == "" || cookieVal == "" || subtle.ConstantTimeCompare([]byte(header), []byte(cookieVal)) != 1 {
		http.Error(w, "forbidden: missing or mismatched confirm token - reload the panel", http.StatusForbidden)
		return false
	}
	return true
}

// setConfirmCookie reuses a well-formed double-submit token already carried by
// this browser session, or mints one when absent. Multiple management panels
// mount in parallel, so rotating the shared cookie on every GET can make an
// otherwise valid sibling response stale before its Save request is sent.
func setConfirmCookie(w http.ResponseWriter, r *http.Request) string {
	token := ""
	if ck, err := r.Cookie(remoteConfirmCookie); err == nil {
		candidate := strings.TrimSpace(ck.Value)
		if len(candidate) == 64 {
			if _, err := hex.DecodeString(candidate); err == nil {
				token = candidate
			}
		}
	}
	if token == "" {
		token = mintConfirmToken()
	}
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: a double-submit confirm token the SPA must read (not a session); Secure over TLS
		Name:     remoteConfirmCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: false, // readable by the SPA to echo in the confirm header
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
	return token
}

// remoteManageStore returns a store over the real DB for the metadata-only
// remote_audit reads/writes (never the demo DB).
func (s *Server) remoteManageStore() *store.Store {
	if s.opts.DB == nil {
		return nil
	}
	return store.New(s.opts.DB)
}

// recordManageAudit appends a metadata-only remote_audit "manage" row for a
// dashboard-initiated management action (plan §4 recommendation), so the audit
// viewer shows who armed/disarmed/revoked. Best-effort; never blocks a request.
func (s *Server) recordManageAudit(r *http.Request, action, detail string) {
	st := s.remoteManageStore()
	if st == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = st.InsertRemoteAudit(ctx, store.RemoteAuditEvent{
		Kind:       "manage",
		Principal:  "local",
		RemoteAddr: hostnameOnly(r.RemoteAddr),
		Route:      r.URL.Path,
		Decision:   "ok",
		Detail:     strings.TrimSpace(action + " " + detail),
	})
}

// recordRemoteAuditRow writes one fully-typed, metadata-only remote_audit row
// through the ONE store seam (store.InsertRemoteAudit). It generalizes
// recordManageAudit for the execute-tier lifecycle events that need an explicit
// kind + device + handle correlation (never a secret). Best-effort; never
// blocks a request. Nil-DB / demo-DB safe (no-op).
func (s *Server) recordRemoteAuditRow(ev store.RemoteAuditEvent) {
	st := s.remoteManageStore()
	if st == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := st.InsertRemoteAudit(ctx, ev); err != nil && s.opts.Logger != nil {
		s.opts.Logger.Debug("remote audit insert failed (best-effort)", "kind", ev.Kind, "error", err)
	}
}

// auditSpawn writes one metadata-only remote_audit row at a terminal SPAWN
// success point (F4, session-attach design §3.5) through the ONE store seam
// (recordRemoteAuditRow → store.InsertRemoteAudit). kind is a SpawnAuditKind
// value (terminal_attach / terminal_resume); an empty kind is a no-op. Metadata
// ONLY - the run identity, the opaque handle (correlation, like every terminal
// event's Route), the tool label, and a coarse decision - NEVER argv, env, or
// terminal content.
//
// F3: the insert is DETACHED (fire-and-forget on its OWN bounded background
// context, the proxy insertTurnDetached precedent) so a contended SQLite writer
// - recordRemoteAuditRow waits up to 3s - can never delay the resume handshake/
// response. The spawn has already succeeded; a slow or failed audit must never
// block it or observe the request's cancellation. The request-derived field
// (RemoteAddr) is captured BEFORE the goroutine so the recycled *http.Request is
// never touched off-thread. Failure stays ignored-but-logged.
func (s *Server) auditSpawn(kind, tool, handle, runID string, r *http.Request) {
	if kind == "" {
		return
	}
	ev := store.RemoteAuditEvent{
		Kind:       kind,
		SessionID:  runID,     // the run identity minted at spawn (never a secret)
		Principal:  "execute", // a spawn is an execute-tier action
		RemoteAddr: hostnameOnly(r.RemoteAddr),
		Route:      handle, // correlate on the terminal handle like every terminal event
		Decision:   "ok",
		Detail:     tool,
	}
	go s.recordRemoteAuditRow(ev)
}

// revokeRemoteWriters terminates every LIVE remote-held terminal writer lease
// through the embedded-terminal manager seam (leaving the owner-local loopback
// writer untouched). It is the admin-transition half of §8.1 item 8: a remote
// disable / rotate / allow_terminal→false must not merely block future writer
// acquires - it must kill the writer a paired device is holding RIGHT NOW. Nil
// LaunchManager (launcher disabled) is a no-op.
func (s *Server) revokeRemoteWriters(reason string) {
	if s.opts.LaunchManager == nil {
		return
	}
	_ = s.opts.LaunchManager.RevokeAllRemoteWriters(reason)
}

// loadConfigForManage loads the config + resolves the path for a management
// write, returning an honest error when no config path is wired (a read-only
// dashboard cannot arm/disarm).
func (s *Server) loadConfigForManage() (config.Config, string, error) {
	if s.opts.ConfigPath == "" {
		return config.Config{}, "", errNoConfigPath
	}
	cfg, err := loadConfigForDashboard(s.opts.ConfigPath)
	if err != nil {
		return config.Config{}, "", err
	}
	return cfg, s.opts.ConfigPath, nil
}

var errNoConfigPath = &manageError{"remote management requires a writable config path (this dashboard was started read-only)"}

type manageError struct{ msg string }

func (e *manageError) Error() string { return e.msg }

func firstNonEmpty(vals []string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// writeSetupSpawnErr maps a CreateSetup failure to an honest status: the setup
// single-flight refusal is 409 (a privileged PTY of this kind is already
// starting - POST-spam is bounded to one live setup op per kind); anything else
// degrades to 500.
func writeSetupSpawnErr(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrLaunchSetupInFlight) {
		writeErrStatus(w, err, http.StatusConflict)
		return
	}
	writeErr(w, err)
}
