package agentconfigorg

import (
	"time"

	"github.com/marmutapp/superbased-observer/internal/agentconfig"
	"github.com/marmutapp/superbased-observer/internal/cfgfmt"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// State is the closed applied-state vocabulary of one org directive on one
// target file of a managed node (P7). It is what the node reports to the
// org: an enum plus value hashes, never a secret.
type State string

const (
	// StateApplied: the file holds the org's value.
	StateApplied State = "applied"
	// StateReasserted: the value drifted and the node wrote it back.
	StateReasserted State = "reasserted"
	// StateDrifted: the file no longer holds the org's value and the node is
	// not re-applying it (the policy says report, or a default the developer
	// may override).
	StateDrifted State = "drifted"
	// StateBackedOff: the value drifted three times within an hour; the node
	// stopped re-applying it and keeps reporting (D-101).
	StateBackedOff State = "backed_off"
	// StateRefused: the sanity check refused the org's write (a value that
	// would switch off Observer's capture, an Observer-owned entry, a file
	// that does not parse).
	StateRefused State = "refused_by_hygiene"
	// StateNotInstalled: the tool is not installed here; nothing is created.
	StateNotInstalled State = "not_installed"
	// StateNoFile: no file of the tool takes this setting here (a project
	// directive for a project this machine does not have is not a state: it
	// has no target at all).
	StateNoFile State = "no_file"
	// StateDeveloperValue: an org default the developer holds a grant for,
	// overridden by the developer's own value (legitimate, D-122).
	StateDeveloperValue State = "developer_value"
	// StateUnknownSetting: this build's catalogue does not know the setting
	// (a newer org server); nothing is written.
	StateUnknownSetting State = "unknown_setting"
)

// States is the closed set, in display order.
func States() []State {
	return []State{StateApplied, StateReasserted, StateDrifted, StateBackedOff, StateRefused, StateNotInstalled, StateNoFile, StateDeveloperValue, StateUnknownSetting}
}

// KnownState reports a value of the closed set (the org server's ingest
// refuses anything else).
func KnownState(s string) bool {
	for _, k := range States() {
		if string(k) == s {
			return true
		}
	}
	return false
}

// IsDrift reports a state the org treats as drift evidence.
func IsDrift(s State) bool { return s == StateDrifted || s == StateBackedOff || s == StateReasserted }

// BackoffWindow and BackoffDrifts are D-101: after three drifts of the same
// setting within an hour the node stops re-applying it.
const (
	BackoffWindow = time.Hour
	BackoffDrifts = 3
)

// Prior is what the node recorded for this target on its last pass.
type Prior struct {
	Known bool
	// ExpectedHash is the hash of the org value last applied (a different
	// hash means the policy changed: a fresh initial apply).
	ExpectedHash string
	State        State
	// DriftTimes are the drift events within the back-off window.
	DriftTimes []time.Time
}

// DecideInput is one directive on one target file.
type DecideInput struct {
	KnownSetting bool
	Installed    bool
	// HasFile: a writable file of the tool takes this setting here.
	HasFile bool
	Mode    string
	// Granted: the developer holds an edit grant for the setting (D-122).
	Granted bool
	// LockLifted: an honoured AI tool setting edit grant (T-436) names this
	// ENFORCED setting, so the developer's value wins until it ends.
	LockLifted   bool
	Expected     any
	Current      any
	CurrentFound bool
	OnDrift      string
	Prior        Prior
	Now          time.Time
}

// Decision is what to do and the state to record.
type Decision struct {
	Write      bool
	State      State
	DriftTimes []time.Time
	// DriftEvent: this pass observed a new drift (an equal-to-different
	// transition), the event the org counts for findings.
	DriftEvent bool
	Rule       string
}

// decideRule is one row of the decision table, walked top-down (CLAUDE.md
// #5): the first row whose match holds decides.
type decideRule struct {
	name   string
	match  func(in DecideInput, d derived) bool
	decide func(in DecideInput, d derived) Decision
}

// derived are the facts every row reads.
type derived struct {
	equal       bool
	sameExpect  bool
	wasDrifting bool // the prior pass already reported drift for this value
	recent      []time.Time
}

var decideTable = []decideRule{
	{
		"unknown_setting", func(in DecideInput, _ derived) bool { return !in.KnownSetting },
		func(DecideInput, derived) Decision { return Decision{State: StateUnknownSetting} },
	},
	{
		"not_installed", func(in DecideInput, _ derived) bool { return !in.Installed },
		func(DecideInput, derived) Decision { return Decision{State: StateNotInstalled} },
	},
	{
		"no_file", func(in DecideInput, _ derived) bool { return !in.HasFile },
		func(DecideInput, derived) Decision { return Decision{State: StateNoFile} },
	},
	{
		"in_place", func(_ DecideInput, d derived) bool { return d.equal },
		func(_ DecideInput, d derived) Decision {
			return Decision{State: StateApplied, DriftTimes: d.recent}
		},
	},
	// An edit grant lifts an enforced lock until it ends (T-436): the
	// developer's value (or their removal of the key) stands and is never
	// written back; once the grant ends this row stops matching and the
	// usual drift rows reassert the org value. A value never applied yet is
	// still written first.
	{"grant_lifts_lock", func(in DecideInput, d derived) bool {
		return in.Mode == orgcontract.AgentConfigModeEnforced && in.LockLifted && (in.CurrentFound || d.sameExpect)
	}, func(DecideInput, derived) Decision { return Decision{State: StateDeveloperValue} }},
	// A granted developer beats an org default (D-122): with their own value,
	// or by deleting the default after it was applied (review M3-17: removing
	// the key chooses the tool's own default; it is not drift and is never
	// written back). A default never applied yet is still written first.
	{"developer_override", func(in DecideInput, d derived) bool {
		return in.Mode == orgcontract.AgentConfigModeDefault && in.Granted && (in.CurrentFound || d.sameExpect)
	}, func(DecideInput, derived) Decision { return Decision{State: StateDeveloperValue} }},
	{
		"initial_apply", func(_ DecideInput, d derived) bool { return !d.sameExpect },
		func(DecideInput, derived) Decision { return Decision{Write: true, State: StateApplied} },
	},
	{
		"still_drifted", func(in DecideInput, d derived) bool {
			return d.wasDrifting && (in.Prior.State == StateBackedOff || in.OnDrift == orgcontract.AgentConfigOnDriftReport)
		},
		func(in DecideInput, d derived) Decision {
			return Decision{State: in.Prior.State, DriftTimes: d.recent}
		},
	},
	{
		"report_only", func(in DecideInput, _ derived) bool { return in.OnDrift == orgcontract.AgentConfigOnDriftReport },
		func(in DecideInput, d derived) Decision {
			return Decision{State: StateDrifted, DriftTimes: append(d.recent, in.Now), DriftEvent: true}
		},
	},
	{
		"back_off", func(_ DecideInput, d derived) bool { return len(d.recent)+1 >= BackoffDrifts },
		func(in DecideInput, d derived) Decision {
			return Decision{State: StateBackedOff, DriftTimes: append(d.recent, in.Now), DriftEvent: true}
		},
	},
	{
		"reassert", func(DecideInput, derived) bool { return true },
		func(in DecideInput, d derived) Decision {
			return Decision{Write: true, State: StateReasserted, DriftTimes: append(d.recent, in.Now), DriftEvent: true}
		},
	},
}

// Decide walks the table for one target.
func Decide(in DecideInput) Decision {
	d := derived{
		equal:      in.CurrentFound && cfgfmt.Equal(normalize(in.Current), normalize(in.Expected)),
		sameExpect: in.Prior.Known && in.Prior.ExpectedHash == ValueHash(in.Expected),
	}
	if d.sameExpect {
		d.wasDrifting = in.Prior.State == StateDrifted || in.Prior.State == StateBackedOff
		for _, t := range in.Prior.DriftTimes {
			if in.Now.Sub(t) < BackoffWindow {
				d.recent = append(d.recent, t)
			}
		}
	}
	for _, r := range decideTable {
		if r.match(in, d) {
			out := r.decide(in, d)
			out.Rule = r.name
			return out
		}
	}
	return Decision{State: StateNoFile, Rule: "unreachable"}
}

// ValueHash is the fingerprint of an org value (the layer store's hash).
func ValueHash(v any) string { return agentconfig.ValueHash(v) }

func normalize(v any) any {
	n, err := cfgfmt.Normalize(v)
	if err != nil {
		return v
	}
	return n
}
