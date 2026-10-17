//go:build sbo_omit_ui_dashboard && !sbo_omit_notify_email

package main

import (
	"context"
	"errors"

	notifydigest "github.com/marmutapp/superbased-observer/internal/notify/digest"
)

// assemble is digest_briefing.go's twin: the daily briefing is composed by
// the dashboard component, which this build omits.
func (r *briefingDigestRunner) assemble(context.Context, notifydigest.Period) (notifydigest.BriefingData, error) {
	return notifydigest.BriefingData{}, errors.New("the daily briefing digest needs the dashboard component, which this build omits")
}
