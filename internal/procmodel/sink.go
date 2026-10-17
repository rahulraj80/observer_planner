package procmodel

import "context"

// Sink persists finished runs. The store implements it and translates
// ProcessRun into its own SQL row type at the boundary; process_key is
// UNIQUE, so PersistRuns upserts (insert at exec, update at exit).
type Sink interface {
	PersistRuns(ctx context.Context, runs []ProcessRun) (int, error)
}
