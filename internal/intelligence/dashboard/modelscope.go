package dashboard

import (
	"net/http"

	"github.com/marmutapp/superbased-observer/internal/modelfilter"
)

// modelParam reads the dashboard's global Model filter (`model=`), the
// sibling of the `tool=` / `project=` params every filtered endpoint already
// takes. "" means no filter; the value is normalized by the one owner of the
// model-filter predicates (internal/modelfilter), which also decides grain:
//
//   - a handler reading a turn-grain table (a row with its own model column)
//     narrows with modelfilter.RowIs on that column;
//   - a handler reading a session-grain table (sessions, actions, anything
//     reached through a session id) narrows with modelfilter.SessionsUsing,
//     i.e. "sessions that used the model", the way the tool filter narrows
//     by sessions.tool.
func modelParam(r *http.Request) string {
	return modelfilter.Normalize(r.URL.Query().Get("model"))
}
