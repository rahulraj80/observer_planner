package notifycfg

import (
	"fmt"
	"time"
)

// Period identifies one completed reporting window: a stable string key (used
// for the digest's send-once de-dup marker) plus the [Start, End) bounds and a human
// label for the email.
type Period struct {
	Key   string    // stable de-dup key, e.g. "2026-W28" or "2026-06"
	Start time.Time // inclusive
	End   time.Time // exclusive
	Label string    // human label for the email, e.g. "Jun 30 - Jul 6, 2026"
}

// DuePeriod returns the most-recently-COMPLETED reporting window as of now, and
// whether the send-hour gate has passed. The window is always closed (it never
// includes the in-progress period), so a digest reports settled data.
//
// ready is true when now.Hour() >= sendHour. Combined with the caller's
// persisted de-dup marker (send once per Key), this yields at-most-once
// delivery per period that is also restart-safe: the first tick of a new period
// at/after the send hour fires; every later tick sees Key already sent.
func DuePeriod(freq DigestFrequency, sendHour int, now time.Time) (Period, bool) {
	now = now.UTC()
	var p Period
	switch freq {
	case Daily:
		// One COMPLETED local-machine day: yesterday's local midnight to
		// today's local midnight, keyed by the local date so the same stored
		// day reads back identically across the local zone.
		localNow := now.In(time.Local)
		today := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, time.Local)
		p.Start = today.AddDate(0, 0, -1)
		p.End = today
		p.Key = p.Start.Format("2006-01-02")
		p.Label = p.Start.Format("Mon, Jan 2, 2006")
	case Monthly:
		curMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		p.Start = curMonthStart.AddDate(0, -1, 0)
		p.End = curMonthStart
		p.Key = p.Start.Format("2006-01")
		p.Label = p.Start.Format("January 2006")
	default: // Weekly
		curWeekStart := StartOfISOWeek(now)
		p.Start = curWeekStart.AddDate(0, 0, -7)
		p.End = curWeekStart
		y, wk := p.Start.ISOWeek()
		p.Key = fmt.Sprintf("%04d-W%02d", y, wk)
		lastDay := p.End.AddDate(0, 0, -1)
		p.Label = fmt.Sprintf("%s - %s", p.Start.Format("Jan 2"), lastDay.Format("Jan 2, 2006"))
	}
	return p, now.Hour() >= sendHour
}

// StartOfISOWeek returns 00:00 UTC of the Monday of t's ISO week.
func StartOfISOWeek(t time.Time) time.Time {
	t = t.UTC()
	// Go's Weekday: Sunday=0 … Saturday=6. ISO week starts Monday.
	offset := (int(t.Weekday()) + 6) % 7 // days since Monday
	monday := t.AddDate(0, 0, -offset)
	return time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, time.UTC)
}
