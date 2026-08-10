package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/db"
)

func pastDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := db.Open(context.Background(), db.Options{Path: filepath.Join(t.TempDir(), "past.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func pastProject(t *testing.T, db *sql.DB, root string) int {
	t.Helper()
	res, err := db.Exec(`INSERT INTO projects (root_path, created_at) VALUES (?, ?)`, root, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return int(id)
}

// seedPastSession inserts a session carrying summary_md and a fixed clock.
func seedPastSession(t *testing.T, db *sql.DB, projectID int, id, tool, model string, started time.Time, summary string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO sessions (id, project_id, tool, model, started_at, summary_md)
		VALUES (?, ?, ?, ?, ?, ?)`,
		id, projectID, tool, model, started.UTC().Format(time.RFC3339Nano), summary)
	if err != nil {
		t.Fatal(err)
	}
}

func TestPastSearchTerms(t *testing.T) {
	terms := PastSearchTerms("What did I work on last Tuesday? build system")
	if len(terms) != 3 || terms[0] != "tuesday" || terms[1] != "build" || terms[2] != "system" {
		t.Fatalf("terms = %v", terms)
	}
	if got := PastSearchTerms("what is the???"); len(got) != 0 {
		t.Fatalf("stopword-only question → %v, want none", got)
	}
	if got := PastSearchTerms(""); len(got) != 0 {
		t.Fatalf("empty question → %v, want none", got)
	}
	if got := PastSearchTerms("a b"); len(got) != 0 {
		t.Fatalf("short tokens → %v, want none", got)
	}
}

func TestSelectPastSessionsRankingAndFilters(t *testing.T) {
	database := pastDB(t)
	proj := pastProject(t, database, "/repo/alpha")

	base := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	seedPastSession(t, database, proj, "s-freq", "claude", "opus", base.Add(-24*time.Hour),
		"build failed build failed build failed")
	seedPastSession(t, database, proj, "s-once", "claude", "opus", base.Add(-48*time.Hour),
		"the whole build pipeline failed once on tuesday then the build was retried and finally passed after a cache clear and a restart")
	seedPastSession(t, database, proj, "s-topic", "codex", "sonnet", base.Add(-72*time.Hour),
		"Refactored the dashboard; no build involved.")

	q := PastQuery{Terms: PastSearchTerms("build failed"), Limit: 6}
	hits, err := SelectPastSessions(context.Background(), database, q)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, h := range hits {
		ids = append(ids, h.SessionID)
	}
	// Both build-failing sessions match (AND semantics); the more frequent one
	// ranks first; the topic session matches neither term and is absent.
	if len(hits) != 2 {
		t.Fatalf("hits = %v, want the two build-failure sessions", ids)
	}
	if ids[0] != "s-freq" || ids[1] != "s-once" {
		t.Fatalf("BM25 order wrong: %v", ids)
	}
	if hits[0].SummaryMD == "" || hits[0].Tool != "claude" || hits[0].Project != "/repo/alpha" {
		t.Fatalf("hit fields wrong: %+v", hits[0])
	}

	// Window filter excludes everything.
	windowed := PastQuery{Terms: q.Terms, Since: base.Add(-12 * time.Hour)}
	if hits, err := SelectPastSessions(context.Background(), database, windowed); err != nil {
		t.Fatal(err)
	} else if len(hits) != 0 {
		t.Fatalf("windowed hits = %d, want 0", len(hits))
	}

	// Project filter.
	projB := pastProject(t, database, "/repo/beta")
	seedPastSession(t, database, projB, "s-beta", "claude", "opus", base.Add(-24*time.Hour),
		"Build failed on the beta side.")
	projected := PastQuery{Terms: q.Terms, Project: "/repo/beta", Limit: 6}
	if hits, err := SelectPastSessions(context.Background(), database, projected); err != nil {
		t.Fatal(err)
	} else if len(hits) != 1 || hits[0].SessionID != "s-beta" {
		t.Fatalf("projected hits = %+v", hits)
	}

	// Limit cap.
	ltd := PastQuery{Terms: q.Terms, Limit: 1}
	if hits, err := SelectPastSessions(context.Background(), database, ltd); err != nil {
		t.Fatal(err)
	} else if len(hits) != 1 || hits[0].SessionID != "s-freq" {
		t.Fatalf("limited hits = %+v", hits)
	}

	// Empty terms → no hits, no error.
	if hits, err := SelectPastSessions(context.Background(), database, PastQuery{}); err != nil {
		t.Fatal(err)
	} else if len(hits) != 0 {
		t.Fatalf("empty-terms hits = %d, want 0", len(hits))
	}
}

func TestSelectPastSessionsErrorContext(t *testing.T) {
	database := pastDB(t)
	proj := pastProject(t, database, "/repo/alpha")
	seedPastSession(t, database, proj, "s-err", "claude", "opus", time.Now().UTC().Add(-24*time.Hour),
		"Permission denied on the deploy step, resolved by fixing the key.")

	// api_turns rows with recurring error messages (insert raw: id is TEXT).
	_, err := database.Exec(`INSERT INTO api_turns (session_id, timestamp, provider, model, request_id, input_tokens, output_tokens, error_message)
		VALUES (?, ?, 'anthropic', 'opus', 'm1', 0, 0, 'permission denied on /etc/hosts')`, "s-err", time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.Exec(`INSERT INTO api_turns (session_id, timestamp, provider, model, request_id, input_tokens, output_tokens, error_message)
		VALUES (?, ?, 'anthropic', 'opus', 'm2', 0, 0, 'permission denied on /etc/hosts')`, "s-err", time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.Exec(`INSERT INTO api_turns (session_id, timestamp, provider, model, request_id, input_tokens, output_tokens, error_message)
		VALUES (?, ?, 'anthropic', 'opus', 'm3', 0, 0, '401 unauthorized')`, "s-err", time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}

	hits, err := SelectPastSessions(context.Background(), database, PastQuery{Terms: []string{"permission"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %+v", hits)
	}
	if len(hits[0].ErrorMessages) != 2 || hits[0].ErrorMessages[0] != "permission denied on /etc/hosts" {
		t.Fatalf("error context = %v, want deduped recurring-first", hits[0].ErrorMessages)
	}
}

func TestSessionSummariesFTSStaysInSync(t *testing.T) {
	database := pastDB(t)
	proj := pastProject(t, database, "/repo/alpha")
	base := time.Now().UTC().Add(-24 * time.Hour)

	// Insert without a summary → not indexed.
	seedPastSession(t, database, proj, "s-empty", "claude", "opus", base, "")
	if hits, err := SelectPastSessions(context.Background(), database, PastQuery{Terms: []string{"anything"}}); err != nil {
		t.Fatal(err)
	} else if len(hits) != 0 {
		t.Fatalf("empty-summary session indexed: %+v", hits)
	}

	// UPDATE to add a summary → the update triggers index it.
	if _, err := database.Exec(`UPDATE sessions SET summary_md = ? WHERE id = ?`, "build failed and I fixed it", "s-empty"); err != nil {
		t.Fatal(err)
	}
	if hits, err := SelectPastSessions(context.Background(), database, PastQuery{Terms: []string{"build"}}); err != nil {
		t.Fatal(err)
	} else if len(hits) != 1 || hits[0].SessionID != "s-empty" {
		t.Fatalf("post-update hits = %+v", hits)
	}

	// UPDATE to clear the summary → removed from the index.
	if _, err := database.Exec(`UPDATE sessions SET summary_md = NULL WHERE id = ?`, "s-empty"); err != nil {
		t.Fatal(err)
	}
	if hits, err := SelectPastSessions(context.Background(), database, PastQuery{Terms: []string{"build"}}); err != nil {
		t.Fatal(err)
	} else if len(hits) != 0 {
		t.Fatalf("cleared-summary session still indexed: %+v", hits)
	}

	// Re-add, then DELETE the session → the delete trigger removes it.
	if _, err := database.Exec(`UPDATE sessions SET summary_md = ? WHERE id = ?`, "build failed", "s-empty"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM sessions WHERE id = ?`, "s-empty"); err != nil {
		t.Fatal(err)
	}
	if hits, err := SelectPastSessions(context.Background(), database, PastQuery{Terms: []string{"build"}}); err != nil {
		t.Fatal(err)
	} else if len(hits) != 0 {
		t.Fatalf("deleted session still indexed: %+v", hits)
	}
}

func TestSelectPastSessionsFTSInjectionSafe(t *testing.T) {
	database := pastDB(t)
	proj := pastProject(t, database, "/repo/alpha")
	seedPastSession(t, database, proj, "s1", "claude", "opus", time.Now().UTC().Add(-24*time.Hour),
		"build failed today")

	// A term full of FTS operators must be treated as a literal phrase, not
	// executed — matching nothing rather than erroring or dropping the quote.
	terms := []string{`OR * : "" OR : "`}
	if _, err := SelectPastSessions(context.Background(), database, PastQuery{Terms: terms}); err != nil {
		t.Fatalf("injection-like terms must not error: %v", err)
	}
}
