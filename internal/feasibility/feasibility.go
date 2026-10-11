package feasibility

import (
	"errors"
	"sort"
	"strings"
)

// Effect is what the caller does with one verdict.
type Effect string

// The closed Effect vocabulary.
const (
	// EffectNone: the option is feasible; nothing to do.
	EffectNone Effect = ""
	// EffectOff: the config asks for the option but it cannot be honoured
	// here. The caller starts with the option switched off and surfaces the
	// reason (log line, doctor row, banner on the relevant page, the
	// settings page showing the option unavailable).
	EffectOff Effect = "off"
	// EffectRefuse: the config asks for the option, it cannot be honoured,
	// and running without it would be unsafe (Rule.Unsafe says why). The
	// caller refuses, naming the cause and the fix.
	EffectRefuse Effect = "refuse"
	// EffectUnavailable: the option is not asked for and cannot be chosen
	// here. A chooser shows it disabled with the reason.
	EffectUnavailable Effect = "unavailable"
)

// Rule is one choosable option and what it needs. Rows are walked in order,
// so a row may name earlier rows in Requires and see their verdicts.
type Rule struct {
	// ID is the stable option id ("aigateway.enabled", a component id, a
	// launch option). It is what Input.Configured and Verdict.ID use.
	ID string
	// Option is the human label a surface shows.
	Option string
	// Needs are requirement ids (component ids or host facts such as
	// "host.bwrap") that must be met. Input.Unavailable says why one is not.
	Needs []string
	// Requires are option ids that must be on and feasible for this one.
	Requires []string
	// Conflicts are option ids that cannot be on together with this one.
	Conflicts []string
	// Unsafe, when set, says why running with this option switched off is
	// unsafe, which turns EffectOff into EffectRefuse. Leave it empty for
	// every ordinary feature: an unavailable feature is simply off.
	Unsafe string
	// Fix is what to change, in one sentence. Empty means the generic
	// "turn the option off or use a build that has it".
	Fix string
	// FixStep names where the choice is made (a setup step, a settings
	// page, a config key), so a chooser can send the user back to it.
	FixStep string
}

// Input is everything Evaluate needs, already reduced to plain data by the
// caller (CLAUDE.md module rule 1: no I/O here).
type Input struct {
	// Configured are the option ids the current config or the user's
	// choices so far turn on.
	Configured map[string]bool
	// Unavailable maps a requirement id to why it cannot be met here. A
	// missing key or an empty value means it is met.
	Unavailable map[string]string
	// Fixes optionally maps a requirement id to how to make it available
	// ("deploy the full image", "install bubblewrap"), used when the rule
	// has no Fix of its own.
	Fixes map[string]string
}

// Verdict is one evaluated option. JSON field names are snake_case and
// additive only: the node API, the org API and the CLI all emit it.
type Verdict struct {
	ID         string `json:"id"`
	Option     string `json:"option"`
	Configured bool   `json:"configured"`
	Feasible   bool   `json:"feasible"`
	Effect     Effect `json:"effect,omitempty"`
	// Cause is the requirement or option id that made the option
	// infeasible.
	Cause   string `json:"cause,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Fix     string `json:"fix,omitempty"`
	FixStep string `json:"fix_step,omitempty"`
}

// Report is the evaluated table, in rule order.
type Report struct {
	Verdicts []Verdict `json:"verdicts"`
}

// Evaluate walks rules top-down and returns one verdict per row. It is
// deterministic and pure: the same input always gives the same report, so
// a chooser re-evaluates after each choice by calling it again with the
// updated Configured set.
func Evaluate(rules []Rule, in Input) Report {
	rep := Report{Verdicts: make([]Verdict, 0, len(rules))}
	// Every option's label is known up front, so a reason that names an
	// option evaluated LATER (a conflict partner further down the table)
	// still uses its label, not its raw id. The placeholder is feasible with
	// no fix step, which is exactly how the checks treated an unseen option.
	seen := make(map[string]Verdict, len(rules))
	for _, r := range rules {
		seen[r.ID] = Verdict{ID: r.ID, Option: label(r), Feasible: true}
	}
	for _, r := range rules {
		v := Verdict{ID: r.ID, Option: label(r), Configured: in.Configured[r.ID], Feasible: true, FixStep: r.FixStep}
		for _, check := range checks {
			cause, reason, fix, step, bad := check(r, in, seen)
			if !bad {
				continue
			}
			v.Feasible = false
			v.Cause, v.Reason = cause, reason
			v.Fix = r.Fix
			if v.Fix == "" {
				v.Fix = fix
			}
			if step != "" && r.FixStep == "" {
				v.FixStep = step
			}
			break
		}
		v.Effect = effectOf(r, v)
		if v.Feasible {
			v.FixStep = ""
		}
		seen[r.ID] = v
		rep.Verdicts = append(rep.Verdicts, v)
	}
	return rep
}

// check is one row of the ordered infeasibility table (CLAUDE.md module rule
// 5): the first check that reports bad decides the reason.
type check func(r Rule, in Input, seen map[string]Verdict) (cause, reason, fix, step string, bad bool)

var checks = []check{
	// A requirement (component or host fact) is not met here.
	func(r Rule, in Input, _ map[string]Verdict) (string, string, string, string, bool) {
		for _, n := range r.Needs {
			if why := strings.TrimSpace(in.Unavailable[n]); why != "" {
				fix := in.Fixes[n]
				if fix == "" {
					fix = "Turn " + label(r) + " off, or use a build that includes " + n + "."
				}
				return n, why, fix, "", true
			}
		}
		return "", "", "", "", false
	},
	// An option this one needs is off or itself infeasible.
	func(r Rule, in Input, seen map[string]Verdict) (string, string, string, string, bool) {
		for _, q := range r.Requires {
			pv, ok := seen[q]
			name := q
			if ok {
				name = pv.Option
			}
			switch {
			case ok && !pv.Feasible:
				return q, label(r) + " needs " + name + ", which is unavailable: " + pv.Reason, pv.Fix, pv.FixStep, true
			case !in.Configured[q]:
				return q, label(r) + " needs " + name + " to be on.", "Turn " + name + " on first.", stepOf(pv, ok), true
			}
		}
		return "", "", "", "", false
	},
	// A conflicting option is on.
	func(r Rule, in Input, seen map[string]Verdict) (string, string, string, string, bool) {
		for _, c := range r.Conflicts {
			if !in.Configured[c] {
				continue
			}
			pv, ok := seen[c]
			name := c
			if ok {
				name = pv.Option
			}
			return c, label(r) + " cannot be on together with " + name + ".", "Turn one of them off.", stepOf(pv, ok), true
		}
		return "", "", "", "", false
	},
}

func stepOf(v Verdict, ok bool) string {
	if !ok {
		return ""
	}
	return v.FixStep
}

func effectOf(r Rule, v Verdict) Effect {
	switch {
	case v.Feasible:
		return EffectNone
	case !v.Configured:
		return EffectUnavailable
	case r.Unsafe != "":
		return EffectRefuse
	default:
		return EffectOff
	}
}

func label(r Rule) string {
	if r.Option != "" {
		return r.Option
	}
	return r.ID
}

// Get returns the verdict for one option id.
func (r Report) Get(id string) (Verdict, bool) {
	for _, v := range r.Verdicts {
		if v.ID == id {
			return v, true
		}
	}
	return Verdict{}, false
}

// With returns the verdicts whose effect is e, in rule order.
func (r Report) With(e Effect) []Verdict {
	var out []Verdict
	for _, v := range r.Verdicts {
		if v.Effect == e {
			out = append(out, v)
		}
	}
	return out
}

// Off returns the configured options the caller must switch off.
func (r Report) Off() []Verdict { return r.With(EffectOff) }

// Refusals returns the configured options whose absence is unsafe.
func (r Report) Refusals() []Verdict { return r.With(EffectRefuse) }

// Line renders one verdict as the single sentence every surface shows: the
// option, the reason and the fix, plus where to change it.
func (v Verdict) Line() string {
	var b strings.Builder
	b.WriteString(v.Option)
	switch v.Effect {
	case EffectOff:
		b.WriteString(" is off: ")
	case EffectRefuse:
		b.WriteString(" cannot run here: ")
	case EffectUnavailable:
		b.WriteString(" is unavailable: ")
	default:
		return v.Option + " is available."
	}
	b.WriteString(strings.TrimSuffix(strings.TrimSpace(v.Reason), "."))
	b.WriteString(".")
	if v.Fix != "" {
		b.WriteString(" Fix: ")
		b.WriteString(strings.TrimSpace(v.Fix))
	}
	if v.FixStep != "" {
		b.WriteString(" (change it in ")
		b.WriteString(v.FixStep)
		b.WriteString(")")
	}
	return b.String()
}

// RefusalError joins the refusals into one error naming each cause, why
// running without it is unsafe, and the fix; nil when nothing refuses.
func RefusalError(rules []Rule, r Report) error {
	refs := r.Refusals()
	if len(refs) == 0 {
		return nil
	}
	unsafe := map[string]string{}
	for _, ru := range rules {
		unsafe[ru.ID] = ru.Unsafe
	}
	parts := make([]string, 0, len(refs))
	for _, v := range refs {
		s := v.Line()
		if u := unsafe[v.ID]; u != "" {
			s += " Refusing because " + strings.TrimSuffix(strings.TrimSpace(u), ".") + "."
		}
		parts = append(parts, s)
	}
	return errors.New(strings.Join(parts, " "))
}

// Ignored turns selection problems (a [components] delta that names an
// unknown id, removes core, or adds what this build lacks) into verdicts
// with EffectOff: the delta is ignored, the rest of the selection runs, and
// the reason is surfaced instead of refusing the start. The problems are
// sorted for a deterministic report.
func Ignored(problems []string, fixStep string) []Verdict {
	ps := append([]string(nil), problems...)
	sort.Strings(ps)
	out := make([]Verdict, 0, len(ps))
	for _, p := range ps {
		out = append(out, Verdict{
			ID: "components.selection", Option: "[components] entry", Configured: true,
			Effect: EffectOff, Reason: "ignored: " + p,
			Fix: "Edit or remove the entry.", FixStep: fixStep,
		})
	}
	return out
}
