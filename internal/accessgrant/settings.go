package accessgrant

import (
	"fmt"
	"sort"
	"time"
)

// Locked-rule approver vocabulary (D-082).
const (
	// ApproverSuperAdmin: exempting an org-locked rule needs a super-admin
	// (the default).
	ApproverSuperAdmin = "super_admin"
	// ApproverPolicyWriter: a write:policy holder may exempt a locked rule.
	ApproverPolicyWriter = "policy_writer"
)

// Settings is the org's grant configuration (the org_access_grant_settings
// singleton). The zero value is NOT the default; use DefaultSettings.
type Settings struct {
	// LockedRuleApprover is ApproverSuperAdmin or ApproverPolicyWriter.
	LockedRuleApprover string `json:"locked_rule_approver"`
	// MaxDurationS optionally LOWERS a type's maximum window (seconds). A
	// value above the D-081 maximum is refused by Normalize.
	MaxDurationS map[string]int64 `json:"max_duration_s,omitempty"`
	// MaxOfflineS is how long a node honours a grant it cannot re-verify
	// (D-087: 24h; 0 = online only; at most 7d).
	MaxOfflineS int64 `json:"max_offline_s"`
	// UnansweredTTLS lapses an open request nobody answered (default 14d).
	UnansweredTTLS int64 `json:"unanswered_ttl_s"`
	// RequireDistinctApprover is FIXED ON (D-086: no self-approval, except a
	// solo super-admin, audited). It stays on the wire so a released admin
	// page that sends it still saves, but Normalize always sets it true and
	// Decide never reads it: no org setting may switch the rule off.
	RequireDistinctApprover bool `json:"require_distinct_approver"`
	// DisabledTypes are request types developers may not raise (the page and
	// existing requests stay visible either way, D-089).
	DisabledTypes []string `json:"disabled_types,omitempty"`
	// RequireMachineProof (T-656, default off) withholds every machine-scoped
	// grant from a fetch that does not prove its machine with a registered
	// machine key. Off, a machine without a registered key is judged by the
	// T-389 attribution check alone; a machine WITH a registered key always
	// needs its proof, whatever this says.
	RequireMachineProof bool `json:"require_machine_proof,omitempty"`
}

// Defaults (D-081, D-086, D-087).
const (
	DefaultMaxOffline    = 24 * time.Hour
	MaxMaxOffline        = 7 * 24 * time.Hour
	DefaultUnansweredTTL = 14 * 24 * time.Hour
	MinUnansweredTTL     = 24 * time.Hour
	MaxUnansweredTTL     = 90 * 24 * time.Hour
	// OnlineOnlyWindow is what MaxOfflineS 0 ("online only") means on the
	// node: a grant is honoured only while the last verified sync is this
	// recent. Two push ceilings plus slack.
	OnlineOnlyWindow = 10 * time.Minute
)

// DefaultSettings is the org's configuration before any admin edit.
func DefaultSettings() Settings {
	return Settings{
		LockedRuleApprover:      ApproverSuperAdmin,
		MaxOfflineS:             int64(DefaultMaxOffline / time.Second),
		UnansweredTTLS:          int64(DefaultUnansweredTTL / time.Second),
		RequireDistinctApprover: true,
	}
}

// MaxFor returns the maximum window of a fixed-max type under these settings
// (0 for a budget-period type; the caller bounds it by the period end).
func (s Settings) MaxFor(t RequestType) time.Duration {
	if t.Max != MaxFixed {
		return 0
	}
	max := t.MaxDuration
	if v, ok := s.MaxDurationS[t.ID]; ok && v > 0 {
		if d := time.Duration(v) * time.Second; d < max {
			max = d
		}
	}
	return max
}

// MaxOffline returns the offline window as a duration (0 = online only).
func (s Settings) MaxOffline() time.Duration { return time.Duration(s.MaxOfflineS) * time.Second }

// TypeDisabled reports whether developers may not raise typeID.
func (s Settings) TypeDisabled(typeID string) bool {
	for _, d := range s.DisabledTypes {
		if d == typeID {
			return true
		}
	}
	return false
}

// Normalize validates an admin-authored Settings (the PUT sanity check: known
// keys, closed vocabularies, dangerous values) and returns the canonical form.
// Any problem means nothing is written.
func (s Settings) Normalize() (Settings, []Problem) {
	var probs []Problem
	out := s
	// D-086 is not an org choice: a stored or sent false is read as true
	// rather than refused, so a document saved before the rule was fixed
	// keeps every other setting (Store.Settings falls back to the defaults
	// on any problem).
	out.RequireDistinctApprover = true
	switch out.LockedRuleApprover {
	case "":
		out.LockedRuleApprover = ApproverSuperAdmin
	case ApproverSuperAdmin, ApproverPolicyWriter:
	default:
		probs = append(probs, Problem{
			Field: "locked_rule_approver", Code: CodeBadValue,
			Message: "locked_rule_approver must be super_admin or policy_writer",
		})
	}
	if len(out.MaxDurationS) > 0 {
		clean := map[string]int64{}
		keys := make([]string, 0, len(out.MaxDurationS))
		for k := range out.MaxDurationS {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := out.MaxDurationS[k]
			t, ok := TypeByID(k)
			if !ok || !t.Grantable() {
				probs = append(probs, Problem{
					Field: "max_duration_s." + k, Code: CodeUnknown,
					Message: fmt.Sprintf("%q is not a grantable request type", k),
				})
				continue
			}
			if t.Max != MaxFixed {
				probs = append(probs, Problem{
					Field: "max_duration_s." + k, Code: CodeBadValue,
					Message: fmt.Sprintf("%s is bounded by the budget period, not by a fixed maximum", k),
				})
				continue
			}
			if v <= 0 || time.Duration(v)*time.Second > t.MaxDuration {
				probs = append(probs, Problem{
					Field: "max_duration_s." + k, Code: CodeOutOfRange,
					Message: fmt.Sprintf("%s maximum must be between 1 second and %s", k, HumanDuration(t.MaxDuration)),
				})
				continue
			}
			clean[k] = v
		}
		out.MaxDurationS = clean
	}
	if out.MaxOfflineS < 0 || time.Duration(out.MaxOfflineS)*time.Second > MaxMaxOffline {
		probs = append(probs, Problem{
			Field: "max_offline_s", Code: CodeOutOfRange,
			Message: "max_offline_s must be between 0 (online only) and 7 days",
		})
	}
	ttl := time.Duration(out.UnansweredTTLS) * time.Second
	if ttl < MinUnansweredTTL || ttl > MaxUnansweredTTL {
		probs = append(probs, Problem{
			Field: "unanswered_ttl_s", Code: CodeOutOfRange,
			Message: "unanswered_ttl_s must be between 1 and 90 days",
		})
	}
	seen := map[string]bool{}
	disabled := []string{}
	for _, d := range out.DisabledTypes {
		if _, ok := TypeByID(d); !ok {
			probs = append(probs, Problem{
				Field: "disabled_types", Code: CodeUnknown,
				Message: fmt.Sprintf("%q is not a request type", d),
			})
			continue
		}
		if !seen[d] {
			seen[d] = true
			disabled = append(disabled, d)
		}
	}
	sort.Strings(disabled)
	out.DisabledTypes = disabled
	return out, probs
}

// HumanDuration renders a duration in the coarsest whole unit (30d, 8h, 15m).
func HumanDuration(d time.Duration) string {
	switch {
	case d <= 0:
		return "0s"
	case d%(24*time.Hour) == 0:
		return fmt.Sprintf("%dd", d/(24*time.Hour))
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	default:
		return d.Truncate(time.Second).String()
	}
}
