package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/govern"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// seedGuardDeny records one enforced guard denial a few minutes ago.
func seedGuardDeny(t *testing.T, s *Server) {
	t.Helper()
	if _, err := store.New(s.opts.DB).InsertGuardEvents(context.Background(), []store.GuardEventRow{{
		TS: time.Now().UTC().Add(-5 * time.Minute), SessionID: "sA", Tool: "claude-code", EventKind: "shell_exec",
		RuleID: "R-101", Category: "destructive", Severity: "critical", Decision: "deny", Enforced: true,
		Source: "builtin", TargetHash: "h",
	}}); err != nil {
		t.Fatal(err)
	}
}

func getNotifications(t *testing.T, s *Server) notificationsResponse {
	t.Helper()
	var out notificationsResponse
	getModelJSON(t, s, "/api/notifications", &out)
	return out
}

func postNotificationState(t *testing.T, s *Server, body string) int {
	t.Helper()
	rr := httptest.NewRecorder()
	req := armLocalWrite(httptest.NewRequest(http.MethodPost, "/api/notifications/state", strings.NewReader(body)))
	s.Handler().ServeHTTP(rr, req)
	return rr.Code
}

func findNotification(r notificationsResponse, prefix string) (int, bool) {
	for i, n := range r.Notifications {
		if strings.HasPrefix(n.ID, prefix) {
			return i, true
		}
	}
	return -1, false
}

// TestNotificationsGuardDenialReadDismissPersist: an enforced guard denial
// is in the feed and unread; marking it read keeps it with the unread count
// down; dismissing removes it, and the dismissal survives a new server over
// the same database (the state file beside it).
func TestNotificationsGuardDenialReadDismissPersist(t *testing.T) {
	s, _ := newTestServer(t)
	seedGuardDeny(t, s)

	r := getNotifications(t, s)
	i, ok := findNotification(r, "guard:R-101:deny:")
	if !ok {
		t.Fatalf("guard denial missing: %+v", r.Notifications)
	}
	id := r.Notifications[i].ID
	if r.Notifications[i].Read || r.Unread < 1 {
		t.Fatalf("new notification should be unread: %+v (unread %d)", r.Notifications[i], r.Unread)
	}
	before := r.Unread

	if code := postNotificationState(t, s, `{"ids":["`+id+`"],"action":"read"}`); code != http.StatusOK {
		t.Fatalf("mark read = %d", code)
	}
	r = getNotifications(t, s)
	if i, ok = findNotification(r, id); !ok || !r.Notifications[i].Read || r.Unread != before-1 {
		t.Fatalf("after read: present=%v unread=%d (was %d)", ok, r.Unread, before)
	}

	if code := postNotificationState(t, s, `{"ids":["`+id+`"],"action":"dismiss"}`); code != http.StatusOK {
		t.Fatalf("dismiss = %d", code)
	}
	if _, ok = findNotification(getNotifications(t, s), id); ok {
		t.Fatal("dismissed notification still listed")
	}

	again, err := New(Options{DB: s.opts.DB, DBPath: s.opts.DBPath})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok = findNotification(getNotifications(t, again), id); ok {
		t.Fatal("dismissal did not persist across a new server")
	}
}

// TestNotificationsHiddenSectionYieldsNothing: on a governed node whose org
// hid the Security page, the guard denial produces no notification.
func TestNotificationsHiddenSectionYieldsNothing(t *testing.T) {
	s, _ := newTestServer(t)
	seedGuardDeny(t, s)
	if _, ok := findNotification(getNotifications(t, s), "guard:R-101:deny:"); !ok {
		t.Fatal("baseline: guard denial missing")
	}
	eff := govern.Effective{
		Active: true, State: govern.StateApplied, Version: 3,
		HiddenSections: []string{"security"}, ReadOnlySections: []string{},
		HiddenSettings: []string{}, ReadOnlySettings: []string{},
	}
	s.opts.Governance = func(context.Context) govern.Effective { return eff }
	if _, ok := findNotification(getNotifications(t, s), "guard:R-101:deny:"); ok {
		t.Fatal("a hidden Security page still notifies about guard denials")
	}
}

func TestNotificationStateRejectsBadRequests(t *testing.T) {
	s, _ := newTestServer(t)
	for _, body := range []string{
		`{"ids":["x"],"action":"archive"}`,
		`{"ids":[],"action":"read"}`,
		`not json`,
	} {
		if code := postNotificationState(t, s, body); code != http.StatusBadRequest {
			t.Errorf("%s -> %d, want 400", body, code)
		}
	}
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/notifications/state", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET state -> %d, want 405", rr.Code)
	}
}

// TestNotificationsMintConfirmTokenAndStateIsAudited pins T-530: the feed
// GET mints the double-submit token the bell echoes, and an accepted state
// change writes one metadata-only audit row (the action and the count).
func TestNotificationsMintConfirmTokenAndStateIsAudited(t *testing.T) {
	s, _ := newTestServer(t)
	seedGuardDeny(t, s)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/notifications", nil))
	var body notificationsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil || body.ConfirmToken == "" {
		t.Fatalf("GET /api/notifications = %d %s, want a confirm_token", rr.Code, rr.Body.String())
	}
	minted := false
	for _, c := range rr.Result().Cookies() {
		if c.Name == remoteConfirmCookie && c.Value == body.ConfirmToken {
			minted = true
		}
	}
	if !minted {
		t.Fatal("the feed did not set the confirm cookie")
	}
	i, ok := findNotification(body, "guard:R-101:deny:")
	if !ok {
		t.Fatalf("guard denial missing: %+v", body.Notifications)
	}
	if code := postNotificationState(t, s, `{"ids":["`+body.Notifications[i].ID+`"],"action":"dismiss"}`); code != http.StatusOK {
		t.Fatalf("dismiss = %d", code)
	}
	var n int
	var detail string
	if err := s.opts.DB.QueryRow(`SELECT COUNT(*), COALESCE(MAX(detail), '') FROM remote_audit WHERE route = '/api/notifications/state'`).Scan(&n, &detail); err != nil {
		t.Fatal(err)
	}
	if n != 1 || detail != "notifications_state action=dismiss count=1" {
		t.Fatalf("audit rows = %d, detail %q; want one notifications_state row", n, detail)
	}
}
