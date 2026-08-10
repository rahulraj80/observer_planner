package main

import (
	"database/sql"
	"path/filepath"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/intelligence/cost"
	"github.com/marmutapp/superbased-observer/internal/llmconn"
	"github.com/marmutapp/superbased-observer/internal/llmkeyring"
	"github.com/marmutapp/superbased-observer/internal/llmrun"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// llmAnalysis builds the OPTIONAL daily-briefing LLM analysis surfaces
// (docs/design/standup-briefing.md §3, Phase B): the runner and the converted
// connection set from [llm.connections]. nil runner / empty conns when the
// user has configured nothing — the briefing page and Q&A then behave
// exactly as Phase A (template narrative, POST /api/briefing/ask → 404).
// Remote connections are inert without allow_remote=true + a key env ref
// (validated at config load; Select + resolve double-check at call time).
func llmAnalysis(cfg config.Config, database *sql.DB, engine *cost.Engine) (*llmrun.Runner, []llmconn.Connection) {
	conns := cfg.LLM.ToConnections()
	if len(conns) == 0 {
		return nil, nil
	}
	var price llmrun.PriceFn
	if engine != nil {
		price = func(model string, in, out int) (float64, bool) {
			p, ok := engine.Lookup(model)
			if !ok {
				return 0, false
			}
			return cost.Compute(p, cost.TokenBundle{Input: int64(in), Output: int64(out)}), true
		}
	}
	// The OS-keychain store for api_key_keychain refs. Fallback dir
	// sits next to the DB (same posture as the org bearer store).
	keys := llmkeyring.Open(filepath.Dir(cfg.Observer.DBPath))
	return &llmrun.Runner{
		Store: store.New(database),
		Price: price,
		Keychain: func(label string) (string, error) {
			return keys.Load(label)
		},
	}, conns
}
