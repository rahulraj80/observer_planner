// Package modelfilter owns the SQL predicates behind the dashboard's global
// Model filter (the "Model" chip beside Window / Tool / Project in the node
// dashboard's filter bar, query param `model=`).
//
// One model filter has two honest meanings, chosen by the GRAIN of the row a
// query reads, never by the page or the tool:
//
//   - Turn grain (api_turns, token_usage, summary_calls, cache_events, any
//     row that carries its own model column): the row matches when its own
//     model equals the filter. Spend, tokens and cost narrow to that model.
//   - Session grain (sessions, actions, compression events, anything that
//     reaches a model only through its session): the row matches when its
//     session USED the model, i.e. the session has at least one api_turns or
//     token_usage row with that model, or its sessions.model is that model.
//     Tool filtering is session grain too, so a session-grain page under a
//     model filter lists whole sessions, exactly as it does under a tool
//     filter.
//
// The package is pure string building (no database/sql, no net/http): every
// reader (the dashboard handlers, the cost engine, discover, advisor, the
// pattern store) composes the fragment it needs into its own WHERE slice, and
// every value is a bound `?` argument.
package modelfilter
