package planlimits

import (
	"strconv"
	"strings"
	"time"
)

// drv_manual.go is manual plan entry: the user types the plan, the allowance
// per window and, optionally, how much is used and the renewal day. Every
// number is estimated (Validate enforces it). When the user leaves "used"
// empty and the unit is money, the service fills ParamObservedUsed with the
// spend Observer itself captured for the provider's tools since the window
// started, so the card can say "about N used" from real activity.

// ParamObservedUsed is the service-injected used amount (in the window's
// unit) measured from Observer's own captured activity.
const ParamObservedUsed = "_observed_used"

// manualUnits maps the typed unit to the model unit.
var manualUnits = map[string]Unit{
	"usd":      UnitUSDMicros,
	"requests": UnitRequests,
	"credits":  UnitCredits,
	"percent":  UnitPercent,
}

func init() {
	register(SourceManual, Driver{
		Plan:  func(PlanInput) ([]Request, error) { return nil, nil },
		Parse: func(in ParseInput) (Snapshot, error) { return ManualSnapshot(in.Params["provider"], in.Params, in.Now) },
	})
}

// ManualWindowStart returns the start of the current manual window: the last
// renewal day for monthly (clamped to the month's length), the start of the
// ISO week for weekly, midnight UTC for daily.
func ManualWindowStart(window string, renewsDay int, now time.Time) (start, end time.Time) {
	now = now.UTC()
	switch window {
	case "daily":
		start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 0, 1)
	case "weekly":
		wd := (int(now.Weekday()) + 6) % 7 // Monday = 0
		start = time.Date(now.Year(), now.Month(), now.Day()-wd, 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 0, 7)
	}
	if renewsDay < 1 || renewsDay > 31 {
		renewsDay = 1
	}
	day := func(y int, m time.Month) time.Time {
		last := time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
		return time.Date(y, m, min(renewsDay, last), 0, 0, 0, 0, time.UTC)
	}
	start = day(now.Year(), now.Month())
	if start.After(now) {
		prev := now.AddDate(0, 0, -now.Day()) // last day of the previous month
		start = day(prev.Year(), prev.Month())
	}
	next := start.AddDate(0, 0, 32-start.Day()) // a day in the next month
	return start, day(next.Year(), next.Month())
}

// ManualSnapshot builds the estimated snapshot of a manual entry.
func ManualSnapshot(provider string, p map[string]string, now time.Time) (Snapshot, error) {
	s := newSnap(provider, SourceManual, KindManual, ConfEstimated, now)
	s.PlanLabel = SanitizeLabel(p["plan"])
	unit, ok := manualUnits[strings.TrimSpace(p["unit"])]
	if !ok {
		return Snapshot{}, shapeErr("manual: unit", nil)
	}
	allowance, ok := parseAllowance(p["allowance"])
	if !ok {
		return Snapshot{}, shapeErr("manual: allowance", nil)
	}
	window := strings.TrimSpace(p["window"])
	if window == "" {
		window = "monthly"
	}
	rd, _ := strconv.Atoi(strings.TrimSpace(p["renews_day"]))
	start, end := ManualWindowStart(window, rd, now)
	scale := 1.0
	if unit == UnitUSDMicros {
		scale = 1e6 // the user types dollars
	}
	w := Window{Key: window, Unit: unit, Limit: F(allowance * scale), ResetsAt: &end, Length: dur(end.Sub(start))}
	used := strings.TrimSpace(p["used"])
	if used == "" {
		used = strings.TrimSpace(p[ParamObservedUsed])
	}
	if v, err := strconv.ParseFloat(used, 64); err == nil && v >= 0 {
		if unit == UnitUSDMicros && strings.TrimSpace(p["used"]) != "" {
			v *= 1e6 // typed dollars; the observed value is already micros
		}
		w.Used = F(v)
		w.UsedFrac = F(min(v/(allowance*scale), 2))
	}
	s.Windows = []Window{w}
	return s, nil
}
