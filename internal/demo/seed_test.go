package demo

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/intelligence/cost"
)

func openSeedDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := db.Open(context.Background(), db.Options{Path: filepath.Join(t.TempDir(), "seed.db")})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

// TestSeedWritesFKOrderedActivity pins the tiny-seed contract: rows land in
// FK order, both ingestion channels (api_turns + token_usage) produce data,
// actions and router advice exist, and every seeded model is priceable by the
// cost engine the briefing uses.
func TestSeedWritesFKOrderedActivity(t *testing.T) {
	ctx := context.Background()
	database := openSeedDB(t)

	res, err := Seed(ctx, database, SeedOptions{Days: 2, Users: 4})
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if res.Sessions == 0 || res.APITurns == 0 || res.TokenEvents == 0 || res.Actions == 0 {
		t.Fatalf("empty result: %+v", res)
	}
	if res.Projects != len(seedProjects) {
		t.Errorf("projects = %d, want %d", res.Projects, len(seedProjects))
	}
	if res.RouterRows == 0 {
		t.Error("no router_decisions seeded — the routing-shadow opportunity card would be dead")
	}

	var sessions, apiturns, tokens, actions int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions`).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_turns`).Scan(&apiturns); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM token_usage`).Scan(&tokens); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM actions`).Scan(&actions); err != nil {
		t.Fatal(err)
	}
	if sessions != res.Sessions || apiturns != res.APITurns || tokens != res.TokenEvents || actions != res.Actions {
		t.Errorf("db vs result mismatch: sessions %d/%d api_turns %d/%d tokens %d/%d actions %d/%d",
			sessions, res.Sessions, apiturns, res.APITurns, tokens, res.TokenEvents, actions, res.Actions)
	}

	// Every seeded model must be priceable by the engine the briefing/cost
	// surfaces use, otherwise recorded-0 rows price to $0 and no opportunity
	// card can compute savings.
	engine := cost.NewEngine(config.IntelligenceConfig{})
	rows, err := database.QueryContext(ctx,
		`SELECT DISTINCT model FROM token_usage UNION SELECT DISTINCT model FROM api_turns WHERE model != ''`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			t.Fatal(err)
		}
		count++
		if _, ok := engine.Lookup(m); !ok {
			t.Errorf("seeded model %q is not priceable by the cost engine", m)
		}
	}
	if count < 10 {
		t.Errorf("distinct seeded models = %d, want >= 10 (the top-30 pool should appear over 4 users)", count)
	}
}

// TestSeedIdempotentReplacesOnlyItself pins re-run safety: a second Seed
// returns the same counts (purge-first), so `observer demoseed` can be
// re-run without double-inserting.
func TestSeedIdempotentReplacesOnlyItself(t *testing.T) {
	ctx := context.Background()
	database := openSeedDB(t)

	first, err := Seed(ctx, database, SeedOptions{Days: 2, Users: 3})
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	second, err := Seed(ctx, database, SeedOptions{Days: 2, Users: 3})
	if err != nil {
		t.Fatalf("Seed (2nd): %v", err)
	}
	if first.Sessions != second.Sessions ||
		first.APITurns != second.APITurns ||
		first.TokenEvents != second.TokenEvents ||
		first.Actions != second.Actions {
		t.Errorf("second run diverged: %+v vs %+v", first, second)
	}

	var sessions int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id LIKE 'seed-%'`).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != second.Sessions {
		t.Errorf("post-purge session count = %d, want %d", sessions, second.Sessions)
	}
}
