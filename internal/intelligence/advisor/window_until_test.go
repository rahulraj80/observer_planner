package advisor

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestLoadFactsHonoursClosedWindow pins REVIEW-3 P3-19 for the advisor: with
// Since / Until set, only rows inside [Since, Until) are evidence. A session
// whose turns all fall after Until must not appear.
func TestLoadFactsHonoursClosedWindow(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, db.Options{Path: filepath.Join(t.TempDir(), "w.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	st := store.New(database)
	if _, err := st.UpsertProject(ctx, "/tmp/proj", ""); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	seed := func(sid string, at time.Time) {
		if err := st.UpsertSession(ctx, models.Session{ID: sid, ProjectID: 1, Tool: "codex", Model: "gpt-5.4", StartedAt: at}); err != nil {
			t.Fatal(err)
		}
		var evs []models.TokenEvent
		for i := 0; i < minSessionRows; i++ {
			evs = append(evs, models.TokenEvent{
				SourceFile: "/x/" + sid + ".jsonl", SourceEventID: fmt.Sprintf("%s:%d", sid, i), SessionID: sid,
				Timestamp: at.Add(time.Duration(i) * time.Minute), Tool: "codex", Model: "gpt-5.4",
				InputTokens: 100 + int64(i), OutputTokens: 10,
				Source: models.TokenSourceJSONL, Reliability: models.ReliabilityApproximate,
			})
		}
		if _, err := st.InsertTokenEvents(ctx, evs); err != nil {
			t.Fatal(err)
		}
	}
	seed("in-range", now.Add(-8*24*time.Hour))
	seed("after-until", now.Add(-2*time.Hour))

	f, err := LoadFacts(ctx, database, Options{
		WindowDays: 5,
		Since:      now.Add(-10 * 24 * time.Hour),
		Until:      now.Add(-5 * 24 * time.Hour),
	}.withDefaults())
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Sessions) != 1 || f.Sessions[0].ID != "in-range" {
		var ids []string
		for _, s := range f.Sessions {
			ids = append(ids, s.ID)
		}
		t.Fatalf("sessions = %v, want only in-range", ids)
	}
	if len(f.Scores) != 1 || f.Scores[0].SessionID != "in-range" {
		t.Errorf("scores = %+v, want only the in-range session", f.Scores)
	}
}
