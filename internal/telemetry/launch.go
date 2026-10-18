package telemetry

import (
	"context"
	"fmt"

	"github.com/marmutapp/superbased-observer/internal/termcore"
)

// In-app launch tagging (telemetry plan Step 5.9, the giveaway foundation): a
// session launched from inside the app is one the dashboard's terminal host
// started (a terminal_run) and correlated to the observed session with at
// least termcore.MinLinkConfidence, the same bar the app uses before it
// attaches costs or status to a run. Only the closed tag ("in_app" /
// "external") leaves the machine; run ids, paths and commands never do.

const (
	launchInApp    = "in_app"
	launchExternal = "external"
)

// inAppSessions returns the set of session ids with an established in-app
// launch correlation.
func inAppSessions(ctx context.Context, tx sqlTx) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT DISTINCT session_id FROM terminal_run_session WHERE confidence >= ?`,
		termcore.MinLinkConfidence)
	if err != nil {
		return nil, fmt.Errorf("telemetry: in-app sessions: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var sid string
		if err := rows.Scan(&sid); err != nil {
			return nil, fmt.Errorf("telemetry: in-app sessions: %w", err)
		}
		out[sid] = true
	}
	return out, rows.Err()
}

func launchTag(inApp map[string]bool, sessionID string) string {
	if inApp[sessionID] {
		return launchInApp
	}
	return launchExternal
}
