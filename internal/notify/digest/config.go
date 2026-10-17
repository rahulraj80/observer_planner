package digest

import (
	"time"

	"github.com/marmutapp/superbased-observer/internal/notifycfg"
)

// Frequency is the digest cadence (see internal/notifycfg).
type Frequency = notifycfg.DigestFrequency

const (
	// Daily reports the most-recently-completed local-machine day.
	Daily = notifycfg.Daily
	// Weekly reports the most-recently-completed 7-day window.
	Weekly = notifycfg.Weekly
	// Monthly reports the most-recently-completed calendar month.
	Monthly = notifycfg.Monthly
)

// Config is the [digest] block. Its shape and validation live in
// internal/notifycfg, a neutral core package, so the node and org config
// loaders embed it without importing this package (modular packaging phase
// PB, rev F1). The alias keeps every caller unchanged.
type Config = notifycfg.DigestConfig

// Period identifies one completed reporting window. Its owner is
// internal/notifycfg (core), so the share card (`observer report share`)
// computes the same window in a build without this package (lane MP-L1E).
type Period = notifycfg.Period

// DuePeriod returns the most-recently-COMPLETED reporting window as of now, and
// whether the send-hour gate has passed (see notifycfg.DuePeriod).
func DuePeriod(freq Frequency, sendHour int, now time.Time) (Period, bool) {
	return notifycfg.DuePeriod(freq, sendHour, now)
}
