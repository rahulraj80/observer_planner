package vendorapi

import (
	"fmt"
	"regexp"
	"time"
)

// Activity landings (plan 6.3 typed landings, 5.2 groups G3 to G5, lane F11).
// A driver's Parse fills these on Page beside (or instead of) graph
// observations. Every field is metadata a reviewed driver extracts: never a
// request or response body, never a credential, never free text beyond the
// sanitizer's limits. The runner passes them through vendorsanitize and
// Validate before the store lands them under the run's fence.

// AuditEvent is one vendor audit event (G5 org_vendor_audit_events). The
// actor is a vendor identity (ActorKind plus ActorIdentity, such as an email)
// resolved to an org member at landing where it can be; Detail is a flat
// allowlist the sanitizer keys by Namespace. Vendor audit is never merged
// with Observer's own audit log.
type AuditEvent struct {
	Namespace      string
	EventID        string
	At             time.Time
	ActorKind      string
	ActorIdentity  string
	ActorIsAgent   bool
	AgentSessionID string
	Action         string
	Target         string
	Detail         map[string]string
}

// UsageRow is one per-call usage record (G3 org_vendor_usage). Join* is the
// vendor's own key for the call (kind usage_event_id, a namespace, a value);
// a connection's rows are unique on it. Charged is integer micro-units of
// ChargedUnit and is never summed with another unit (plan 7.5).
type UsageRow struct {
	JoinKind         string
	JoinNamespace    string
	JoinValue        string
	IdentityKind     string
	Identity         string
	SourceAccount    string
	At               time.Time
	Model            string
	SessionRef       string
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	ReasoningTokens  int64
	ChargedUnit      string
	ChargedMicros    int64
	Revision         int64
	Flags            map[string]string
	// Billing is the vendor's per-call billing breakdown when it reports
	// one (T-630); nil otherwise.
	Billing *UsageBilling
}

// UsageBilling is a vendor's per-call billing breakdown (T-630, EA-F2: the
// Cursor usage-event fields the Era 1 parser carried but nothing stored),
// landed in typed org_vendor_usage columns (server migration 220). Every
// money amount is integer micro-units of the row's ChargedUnit; the fee is a
// breakdown already INSIDE ChargedMicros, never an addend. A nil pointer is
// "the vendor did not report it", never zero. Nothing here reaches a spend,
// budget, alert or enforcement path (invariant I4).
type UsageBilling struct {
	// Category is the vendor's billing category ("Usage-based",
	// "Included in Business").
	Category string
	// MaxMode reports the vendor's max-mode flag.
	MaxMode *bool
	// Chargeable is the vendor's chargeable flag. The vendor's own example
	// shows a non-chargeable call with a non-zero charge, so it never zeroes
	// a cost.
	Chargeable *bool
	// RequestUnitsMicros is the legacy request-unit cost (micro-units).
	RequestUnitsMicros *int64
	// ModelCostMicros is the model cost before the vendor's own fee.
	ModelCostMicros *int64
	// FeeMicros is the vendor's own fee (Cursor Token Rate).
	FeeMicros *int64
	// DiscountPercentMicros is the discount percentage applied (micro-units
	// of one percent).
	DiscountPercentMicros *int64
	// ServiceAccountName names the service account that made the call.
	ServiceAccountName string
	// AutomationID is the automation the call is attributed to.
	AutomationID string
}

// FactRow is one aggregate value (G4 org_vendor_facts): one metric of one
// subject over one half-open UTC interval. An empty Identity is an org-level
// fact; otherwise the identity resolves to a member or is held (D-347).
type FactRow struct {
	IntervalStart  time.Time
	IntervalEnd    time.Time
	SourceTZ       string
	CoverageDomain string
	IdentityKind   string
	Identity       string
	ModelRaw       string
	Feature        string
	RepoHash       string
	TrafficSegment string
	Metric         string
	Unit           string
	ValueMicros    int64
	Revision       int64
	Final          bool
}

// VendorSnapshot is one point-in-time body of a class that is not a graph
// resource (G5 org_vendor_snapshots: limits, policy state). Body is a flat
// allowlist the sanitizer keys by Class.
type VendorSnapshot struct {
	Class   string
	Subject string
	Body    map[string]string
}

// Retraction is a vendor correction removing one usage call (G3
// org_vendor_retractions, EA-F3). Request-first and correction-first both
// converge: whichever arrives second finds the other.
type Retraction struct {
	JoinKind      string
	JoinNamespace string
	JoinValue     string
	Reason        string
}

// HasActivity reports whether a page carries any activity landing.
func (p Page) HasActivity() bool {
	return len(p.Audit) > 0 || len(p.Usage) > 0 || len(p.Facts) > 0 || len(p.Snapshots) > 0 || len(p.Retractions) > 0
}

// Activity limits.
const (
	MaxIDLen     = 256
	MaxDetail    = 32
	MaxTargetLen = 512
)

var (
	tokenRE = regexp.MustCompile(`^[a-z][a-z0-9_.:-]{0,127}$`)
	// unitRE is a unit name: lower-case words (credits, cents, usd, acu,
	// tokens, requests, count, seats). The unit is never inferred.
	unitRE = regexp.MustCompile(`^[a-z][a-z_]{0,31}$`)
)

func boundedID(s string) bool { return s != "" && len(s) <= MaxIDLen }

// activityRule is one row of the activity rule table, walked top down per
// landing; the first failing row names the refusal.
type activityRule[T any] struct {
	name string
	ok   func(T) bool
}

var auditRules = []activityRule[AuditEvent]{
	{"audit namespace is a token", func(e AuditEvent) bool { return tokenRE.MatchString(e.Namespace) }},
	{"audit event id is bounded", func(e AuditEvent) bool { return boundedID(e.EventID) }},
	{"audit event time is set", func(e AuditEvent) bool { return !e.At.IsZero() }},
	{"audit action is a token", func(e AuditEvent) bool { return tokenRE.MatchString(e.Action) }},
	{"audit actor kind names its identity", func(e AuditEvent) bool {
		return (e.ActorKind == "") == (e.ActorIdentity == "") && (e.ActorKind == "" || tokenRE.MatchString(e.ActorKind)) &&
			len(e.ActorIdentity) <= MaxIDLen
	}},
	{"audit target within limit", func(e AuditEvent) bool { return len(e.Target) <= MaxTargetLen && len(e.AgentSessionID) <= MaxIDLen }},
	{"audit detail within limits", func(e AuditEvent) bool { return len(e.Detail) <= MaxDetail && attrsWithinLimits(e.Detail) }},
	{"audit detail carries no secret-shaped key", func(e AuditEvent) bool { return !hasSecretShapedAttr(e.Detail) }},
}

var usageRules = []activityRule[UsageRow]{
	{"usage join kind is a token", func(u UsageRow) bool { return tokenRE.MatchString(u.JoinKind) }},
	{"usage join namespace is a token", func(u UsageRow) bool { return tokenRE.MatchString(u.JoinNamespace) }},
	{"usage join value is bounded", func(u UsageRow) bool { return boundedID(u.JoinValue) }},
	{"usage time is set", func(u UsageRow) bool { return !u.At.IsZero() }},
	{"usage identity kind names its identity", func(u UsageRow) bool {
		return u.IdentityKind != "" && tokenRE.MatchString(u.IdentityKind) && boundedID(u.Identity)
	}},
	{"usage counts are not negative", func(u UsageRow) bool {
		return u.InputTokens >= 0 && u.OutputTokens >= 0 && u.CacheReadTokens >= 0 && u.CacheWriteTokens >= 0 &&
			u.ReasoningTokens >= 0 && u.ChargedMicros >= 0 && u.Revision >= 0
	}},
	{"usage charge names its unit", func(u UsageRow) bool {
		return (u.ChargedUnit == "" && u.ChargedMicros == 0) || unitRE.MatchString(u.ChargedUnit)
	}},
	{"usage model and session are bounded", func(u UsageRow) bool {
		return len(u.Model) <= MaxIDLen && len(u.SessionRef) <= MaxIDLen && len(u.SourceAccount) <= MaxIDLen
	}},
	{"usage flags within limits", func(u UsageRow) bool {
		return len(u.Flags) <= MaxDetail && attrsWithinLimits(u.Flags) && !hasSecretShapedAttr(u.Flags)
	}},
	{"usage billing amounts are not negative", func(u UsageRow) bool {
		b := u.Billing
		if b == nil {
			return true
		}
		for _, v := range []*int64{b.RequestUnitsMicros, b.ModelCostMicros, b.FeeMicros, b.DiscountPercentMicros} {
			if v != nil && *v < 0 {
				return false
			}
		}
		return true
	}},
	{"usage billing names are bounded", func(u UsageRow) bool {
		b := u.Billing
		return b == nil || (len(b.Category) <= MaxIDLen && len(b.ServiceAccountName) <= MaxIDLen && len(b.AutomationID) <= MaxIDLen)
	}},
}

var factRules = []activityRule[FactRow]{
	{"fact interval is a non-empty UTC interval", func(f FactRow) bool {
		return !f.IntervalStart.IsZero() && f.IntervalEnd.After(f.IntervalStart)
	}},
	{"fact metric is a token", func(f FactRow) bool { return tokenRE.MatchString(f.Metric) }},
	{"fact unit is a unit name", func(f FactRow) bool { return unitRE.MatchString(f.Unit) }},
	{"fact identity kind names its identity", func(f FactRow) bool {
		return (f.IdentityKind == "") == (f.Identity == "") && (f.IdentityKind == "" || tokenRE.MatchString(f.IdentityKind)) &&
			len(f.Identity) <= MaxIDLen
	}},
	{"fact dimensions are bounded", func(f FactRow) bool {
		for _, d := range []string{f.ModelRaw, f.Feature, f.RepoHash, f.TrafficSegment, f.CoverageDomain, f.SourceTZ} {
			if len(d) > MaxIDLen {
				return false
			}
		}
		return true
	}},
	{"fact revision is not negative", func(f FactRow) bool { return f.Revision >= 0 }},
}

var snapshotRules = []activityRule[VendorSnapshot]{
	{"snapshot class is a token", func(s VendorSnapshot) bool { return tokenRE.MatchString(s.Class) }},
	{"snapshot subject is bounded", func(s VendorSnapshot) bool { return len(s.Subject) <= MaxIDLen }},
	{"snapshot body within limits", func(s VendorSnapshot) bool { return attrsWithinLimits(s.Body) }},
	{"snapshot body carries no secret-shaped key", func(s VendorSnapshot) bool { return !hasSecretShapedAttr(s.Body) }},
}

var retractionRules = []activityRule[Retraction]{
	{"retraction join kind is a token", func(r Retraction) bool { return tokenRE.MatchString(r.JoinKind) }},
	{"retraction join namespace is a token", func(r Retraction) bool { return tokenRE.MatchString(r.JoinNamespace) }},
	{"retraction join value is bounded", func(r Retraction) bool { return boundedID(r.JoinValue) }},
	{"retraction reason is a token or empty", func(r Retraction) bool { return r.Reason == "" || tokenRE.MatchString(r.Reason) }},
}

func walk[T any](what string, rows []T, rules []activityRule[T], key func(T) string) error {
	seen := map[string]bool{}
	for i, row := range rows {
		for _, r := range rules {
			if !r.ok(row) {
				return fmt.Errorf("%w: %s %d: %s", ErrInvalidPage, what, i, r.name)
			}
		}
		k := key(row)
		if seen[k] {
			return fmt.Errorf("%w: %s %d repeats its key", ErrInvalidPage, what, i)
		}
		seen[k] = true
	}
	return nil
}

// ValidateActivity is the structural gate an activity landing passes before
// the store lands it. A key repeated inside one page is refused (the store
// is idempotent across pages, never ambiguous within one).
func ValidateActivity(p Page) error {
	if err := walk("audit event", p.Audit, auditRules, func(e AuditEvent) string { return e.Namespace + "\x00" + e.EventID }); err != nil {
		return err
	}
	if err := walk("usage row", p.Usage, usageRules, func(u UsageRow) string {
		return u.JoinKind + "\x00" + u.JoinNamespace + "\x00" + u.JoinValue
	}); err != nil {
		return err
	}
	if err := walk("fact", p.Facts, factRules, func(f FactRow) string {
		return f.IntervalStart.UTC().Format(time.RFC3339) + "\x00" + f.IntervalEnd.UTC().Format(time.RFC3339) + "\x00" +
			f.IdentityKind + "\x00" + f.Identity + "\x00" + f.ModelRaw + "\x00" + f.Feature + "\x00" + f.RepoHash + "\x00" +
			f.TrafficSegment + "\x00" + f.Metric + "\x00" + f.Unit
	}); err != nil {
		return err
	}
	if err := walk("snapshot", p.Snapshots, snapshotRules, func(s VendorSnapshot) string { return s.Class + "\x00" + s.Subject }); err != nil {
		return err
	}
	return walk("retraction", p.Retractions, retractionRules, func(r Retraction) string {
		return r.JoinKind + "\x00" + r.JoinNamespace + "\x00" + r.JoinValue
	})
}
