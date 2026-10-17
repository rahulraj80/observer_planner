package notifycfg

import (
	"fmt"
	"strings"
)

// DigestFrequency is the digest cadence.
type DigestFrequency string

const (
	// Daily reports the most-recently-completed local-machine day (the
	// observer_planner Daily Briefing email).
	Daily DigestFrequency = "daily"
	// Weekly reports the most-recently-completed 7-day window.
	Weekly DigestFrequency = "weekly"
	// Monthly reports the most-recently-completed calendar month.
	Monthly DigestFrequency = "monthly"
)

// DigestConfig is the [digest] block shared by the node (~/.observer/config.toml) and
// the org server (/etc/observer-org/config.toml). Like EmailConfig it is the
// SINGLE owner of the [digest] shape - both config packages embed this type as
// their [digest] surface, so the shape and its validation live in one place.
//
// LOCAL-ONLY, never distributed over the org wire. Zero value = disabled: a
// default install schedules no digest and makes no SMTP connection. A digest
// send additionally requires [email].enabled - the digest rides the same
// fail-soft email channel the alert evaluators use.
type DigestConfig struct {
	// Enabled gates the scheduler. Default false (opt-in; a fired digest makes
	// an outbound SMTP call).
	Enabled bool `toml:"enabled"`
	// Frequency ∈ daily | weekly | monthly. Empty defaults to weekly.
	Frequency string `toml:"frequency"`
	// SendHour is the UTC hour (0-23) at/after which a due digest is sent on the
	// first tick of a new period. Default 8.
	SendHour int `toml:"send_hour"`
	// To overrides the recipient list for digests. Empty falls back to
	// [email].to (resolved by the caller before sending).
	To []string `toml:"to"`
}

// Validate checks the block only when Enabled - a stale disabled section never
// fails the daemon (digests are opt-in). It never touches I/O.
func (c DigestConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(c.Frequency)) {
	case "", string(Daily), string(Weekly), string(Monthly):
	default:
		return fmt.Errorf("digest: frequency %q not in {daily, weekly, monthly}", c.Frequency)
	}
	if c.SendHour < 0 || c.SendHour > 23 {
		return fmt.Errorf("digest: send_hour %d out of range 0..23", c.SendHour)
	}
	return nil
}

// FrequencyOrDefault returns the effective cadence, defaulting to weekly.
func (c DigestConfig) FrequencyOrDefault() DigestFrequency {
	switch strings.ToLower(strings.TrimSpace(c.Frequency)) {
	case string(Daily):
		return Daily
	case string(Monthly):
		return Monthly
	default:
		return Weekly
	}
}

// SendHourOrDefault returns the effective send hour, defaulting to 8.
func (c DigestConfig) SendHourOrDefault() int {
	if c.SendHour < 0 || c.SendHour > 23 {
		return 8
	}
	if c.SendHour == 0 {
		// The zero value is indistinguishable from an unset block, so 0 is
		// treated as the default 8 (mid-morning UTC) rather than midnight. Set
		// any hour 1-23 for a specific send time.
		return 8
	}
	return c.SendHour
}
