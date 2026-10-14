package planlimits

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// drv_cli.go holds the vendor-CLI reporters (rung 1, "via <tool> CLI"): the
// vendor's own CLI prints its own usage, Observer parses the text. None of
// them offers a machine-readable flag, so every parser fails closed: no match
// is an error, never zero. Output shapes are from the vendors' CLIs as
// captured in CodexBar's fixtures (discovery source).

func init() {
	register(SourceKiroCLI, cliDriver([]string{"kiro-cli", "chat", "--no-interactive", "/usage"}, 20*time.Second,
		[]string{"TERM=dumb", "NO_COLOR=1"}, ParseKiroUsage))
	register(SourceAmpCLI, cliDriver([]string{"amp", "usage"}, 15*time.Second, []string{"NO_COLOR=1"}, ParseAmpUsage))
	register(SourceAugmentCLI, cliDriver([]string{"auggie", "account", "status"}, 15*time.Second, []string{"NO_COLOR=1"}, ParseAuggieStatus))
}

// cliDriver builds a driver that runs one fixed argv and parses its output.
func cliDriver(argv []string, timeout time.Duration, env []string, parse func(string, time.Time) (Snapshot, error)) Driver {
	return Driver{
		Plan: func(PlanInput) ([]Request, error) {
			return []Request{{Key: "out", Kind: ReqExec, Argv: append([]string(nil), argv...), Env: append([]string(nil), env...), Timeout: timeout}}, nil
		},
		Parse: func(in ParseInput) (Snapshot, error) {
			r, ok := in.Responses["out"]
			if !ok {
				return Snapshot{}, fmt.Errorf("%w: no output", ErrTransient)
			}
			if r.Err != nil {
				return Snapshot{}, r.Err
			}
			return parse(string(r.Body), in.Now)
		},
	}
}

// errNotSignedIn is the CLI's "please sign in" answer.
func errNotSignedIn(tool string) error { return fmt.Errorf("%w: %s is not signed in", ErrAuth, tool) }

// errNoMatch is an output the parser does not recognise.
func errNoMatch(tool string) error { return shapeErr(tool+" output format not recognised", nil) }

var ansiRe = regexp.MustCompile(`\x1B\[[0-9;?]*[A-Za-z]|\x1B\][^\x07]*\x07`)

// StripANSI removes terminal escapes.
func StripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

func numText(s string) float64 {
	f, _ := strconv.ParseFloat(strings.ReplaceAll(s, ",", ""), 64)
	return f
}

var (
	kiroPlan2x    = regexp.MustCompile(`Estimated Usage[ \t]*\|[^\n|]*\|[ \t]*([A-Z][A-Z0-9 ]+)`)
	kiroPlanColon = regexp.MustCompile(`(?m)^[ \t]*Plan:[ \t]*([^|\r\n]+?)[ \t]*(?:\||$)`)
	kiroPlanOld   = regexp.MustCompile(`\|[ \t]*(KIRO[ \t]+\w+)`)
	kiroPct       = regexp.MustCompile(`\x{2588}+\s*(\d+(?:\.\d+)?)%`)
	kiroCredits   = regexp.MustCompile(`\((\d+\.?\d*)\s+of\s+(\d+)\s+covered`)
	kiroReset     = regexp.MustCompile(`resets on (\d{4}-\d{2}-\d{2})`)
	kiroBonus     = regexp.MustCompile(`Bonus credits:\s*(\d+\.?\d*)/(\d+)`)
	kiroOverUsed  = regexp.MustCompile(`(?i)Credits used:\s*(\d+\.?\d*)`)
	kiroOverCost  = regexp.MustCompile(`(?i)Est\.\s*cost:\s*\$?(\d+\.?\d*)\s*USD`)
)

// ParseKiroUsage parses `kiro-cli chat --no-interactive /usage`.
func ParseKiroUsage(out string, now time.Time) (Snapshot, error) {
	t := StripANSI(out)
	low := strings.ToLower(t)
	for _, k := range []string{"not logged in", "login required", "failed to initialize auth portal", "kiro-cli login"} {
		if strings.Contains(low, k) {
			return Snapshot{}, errNotSignedIn("kiro-cli")
		}
	}
	if strings.Contains(low, "could not retrieve usage information") {
		return Snapshot{}, errNoMatch("kiro-cli")
	}
	s := newSnap(ProviderKiro, SourceKiroCLI, KindVendorCLI, ConfExact, now)
	for _, re := range []*regexp.Regexp{kiroPlanOld, kiroPlan2x, kiroPlanColon} {
		if m := re.FindStringSubmatch(t); m != nil {
			s.PlanLabel = SanitizeLabel(m[1])
		}
	}
	w := Window{Key: "monthly", Unit: UnitCredits}
	if m := kiroCredits.FindStringSubmatch(t); m != nil {
		used, total := numText(m[1]), numText(m[2])
		w.Used, w.Limit = F(used), F(total)
		if total > 0 {
			w.UsedFrac = F(used / total)
		}
	}
	if m := kiroPct.FindStringSubmatch(t); m != nil && w.UsedFrac == nil {
		w.UsedFrac = fracFromPercent(numText(m[1]))
	}
	if m := kiroReset.FindStringSubmatch(t); m != nil {
		if r, err := time.Parse("2006-01-02", m[1]); err == nil {
			w.ResetsAt = &r
		}
	}
	if w.UsedFrac != nil || w.Used != nil {
		s.Windows = append(s.Windows, w)
	}
	if m := kiroBonus.FindStringSubmatch(t); m != nil && numText(m[2]) > 0 {
		s.Windows = append(s.Windows, Window{
			Key: "bonus", Unit: UnitCredits, Used: F(numText(m[1])), Limit: F(numText(m[2])),
			UsedFrac: F(numText(m[1]) / numText(m[2])),
		})
	}
	if m := kiroOverUsed.FindStringSubmatch(t); m != nil {
		s.Spend = append(s.Spend, Spend{Period: "cycle", Unit: UnitCredits, Amount: int64(numText(m[1])), Scope: "account"})
	}
	if m := kiroOverCost.FindStringSubmatch(t); m != nil {
		s.Spend = append(s.Spend, Spend{Period: "cycle", Unit: UnitUSDMicros, Amount: microsFromFloat(numText(m[1])), Scope: "account"})
	}
	if len(s.Windows) == 0 {
		if s.PlanLabel != "" && (strings.Contains(low, "managed by admin") || strings.Contains(low, "managed by organization") || strings.Contains(low, "usage breakdown")) {
			s.Status = StatusPlanOnly // a managed seat shows the plan but no meter
			return s, nil
		}
		return Snapshot{}, errNoMatch("kiro-cli")
	}
	return s, nil
}

const ampAmt = `([0-9][0-9,]*(?:\.[0-9]+)?)`

var (
	ampSigned    = regexp.MustCompile(`(?im)^\s*Signed in as\s+([^\s(]+)`)
	ampFreeUSD   = regexp.MustCompile(`(?im)^\s*Amp Free:\s*\$?` + ampAmt + `\s*/\s*\$?` + ampAmt + `\s+remaining`)
	ampFreePct   = regexp.MustCompile(`(?im)^\s*Amp Free:\s*` + ampAmt + `\s*%\s+remaining`)
	ampSub       = regexp.MustCompile(`(?im)^\s*(?:Subscription\s+(.+?)|Amp\s+(.+?)\s+Subscription):\s*` + ampAmt + `\s*%\s+other\s+usage\s+and\s+` + ampAmt + `\s*%\s+orb\s+usage\s+remaining\s*-\s*resets\s+upon\s+renewal\s+in\s+([0-9]+)\s+(days?|months?)`)
	ampTier      = regexp.MustCompile(`(?im)^\s*Amp\s+([^\r\n]+?)\s+Tier:\s*agent\s+usage\s+\$` + ampAmt + `\s+of\s+\$` + ampAmt + `\s+remaining\b([^\r\n]*?)resets\s+upon\s+renewal\s+in\s+([0-9]+)\s+(days?|months?)\b`)
	ampPeriod    = regexp.MustCompile(`\bperiod\s+(\d{4}-\d{2}-\d{2})\s+to\s+(\d{4}-\d{2}-\d{2})\b`)
	ampIndiv     = regexp.MustCompile(`(?im)^\s*Individual credits:\s*\$?` + ampAmt + `\s+remaining`)
	ampWorkspace = regexp.MustCompile(`(?im)^\s*Workspace\s+(.+?):\s*\$?` + ampAmt + `\s+remaining`)
)

func ampRenewal(now time.Time, n int, unit string) time.Time {
	if strings.HasPrefix(unit, "month") {
		return now.AddDate(0, n, 0)
	}
	return now.AddDate(0, 0, n)
}

// ParseAmpUsage parses `amp usage`. The "Signed in as" identity line is
// read only to tell a signed-out CLI apart; it is never stored.
func ParseAmpUsage(out string, now time.Time) (Snapshot, error) {
	t := strings.ReplaceAll(StripANSI(out), "**", "")
	if ampSigned.FindStringSubmatch(t) == nil {
		low := strings.ToLower(t)
		if strings.Contains(low, "sign in") || strings.Contains(low, "log in") || strings.Contains(low, "login") {
			return Snapshot{}, errNotSignedIn("amp")
		}
	}
	s := newSnap(ProviderAmp, SourceAmpCLI, KindVendorCLI, ConfExact, now)
	if m := ampFreeUSD.FindStringSubmatch(t); m != nil && numText(m[2]) > 0 {
		rem, quota := numText(m[1]), numText(m[2])
		s.Windows = append(s.Windows, Window{
			Key: "daily:free", Unit: UnitUSDMicros,
			Used: F(float64(microsFromFloat(quota - rem))), Limit: F(float64(microsFromFloat(quota))),
			Remaining: F(float64(microsFromFloat(rem))), UsedFrac: F((quota - rem) / quota), Rolling: true,
		})
	} else if m := ampFreePct.FindStringSubmatch(t); m != nil {
		s.Windows = append(s.Windows, Window{Key: "daily:free", Unit: UnitPercent, UsedFrac: F((100 - numText(m[1])) / 100), Length: dur(24 * time.Hour)})
	}
	if m := ampTier.FindStringSubmatch(t); m != nil {
		rem, lim := numText(m[2]), numText(m[3])
		s.PlanLabel = SanitizeLabel(m[1])
		reset := ampRenewal(now, int(numText(m[5])), m[6])
		if p := ampPeriod.FindStringSubmatch(m[4]); p != nil {
			if e, err := time.Parse("2006-01-02", p[2]); err == nil {
				reset = e
			}
		}
		if lim > 0 {
			s.Windows = append(s.Windows, Window{
				Key: "cycle:agent", Unit: UnitUSDMicros,
				Used: F(float64(microsFromFloat(lim - rem))), Limit: F(float64(microsFromFloat(lim))),
				UsedFrac: F((lim - rem) / lim), ResetsAt: &reset,
			})
		}
	} else if m := ampSub.FindStringSubmatch(t); m != nil {
		s.PlanLabel = SanitizeLabel(m[1] + m[2])
		reset := ampRenewal(now, int(numText(m[5])), m[6])
		s.Windows = append(s.Windows,
			Window{Key: "cycle:agent", Unit: UnitPercent, UsedFrac: F((100 - numText(m[3])) / 100), ResetsAt: &reset},
			Window{Key: "cycle:orb", Unit: UnitPercent, UsedFrac: F((100 - numText(m[4])) / 100), ResetsAt: &reset})
	}
	if m := ampIndiv.FindStringSubmatch(t); m != nil {
		s.Balances = append(s.Balances, Balance{Kind: "prepaid", Unit: UnitUSDMicros, Amount: microsFromFloat(numText(m[1]))})
	}
	for _, m := range ampWorkspace.FindAllStringSubmatch(t, -1) {
		s.Balances = append(s.Balances, Balance{Kind: "workspace_pool", Unit: UnitUSDMicros, Amount: microsFromFloat(numText(m[2]))})
	}
	if len(s.Windows)+len(s.Balances) == 0 {
		return Snapshot{}, errNoMatch("amp")
	}
	return s, nil
}

var (
	augPerMonth  = regexp.MustCompile(`([\d,]+)\s+credits\s*/\s*month`)
	augRemaining = regexp.MustCompile(`([\d,]+)\s+(?:credits\s+)?remaining`)
	augUsedTotal = regexp.MustCompile(`([\d,]+)\s*/\s*([\d,]+)\s+credits used`)
	augEnds      = regexp.MustCompile(`ends\s+(\d{1,2}/\d{1,2}/\d{4})`)
)

// ParseAuggieStatus parses `auggie account status` (box and legacy formats).
func ParseAuggieStatus(out string, now time.Time) (Snapshot, error) {
	t := StripANSI(out)
	if strings.Contains(t, "Authentication failed") || strings.Contains(t, "auggie login") {
		return Snapshot{}, errNotSignedIn("auggie")
	}
	var total, remaining, used *float64
	var reset *time.Time
	for _, line := range strings.Split(t, "\n") {
		switch {
		case strings.Contains(line, "billing cycle"):
			if m := augEnds.FindStringSubmatch(line); m != nil {
				if r, err := time.Parse("1/2/2006", m[1]); err == nil {
					reset = &r
				}
			}
		case strings.Contains(line, "credits used"):
			if m := augUsedTotal.FindStringSubmatch(line); m != nil {
				used, total = F(numText(m[1])), F(numText(m[2]))
			}
			if m := augRemaining.FindStringSubmatch(line); m != nil {
				remaining = F(numText(m[1]))
			}
		default:
			if m := augPerMonth.FindStringSubmatch(line); m != nil && total == nil {
				total = F(numText(m[1]))
			}
			if strings.Contains(line, "credits remaining") {
				if m := augRemaining.FindStringSubmatch(line); m != nil {
					remaining = F(numText(m[1]))
				}
			}
		}
	}
	if remaining == nil || total == nil || *total <= 0 {
		return Snapshot{}, errNoMatch("auggie")
	}
	if used == nil {
		used = F(*total - *remaining)
	}
	s := newSnap(ProviderAugment, SourceAugmentCLI, KindVendorCLI, ConfExact, now)
	s.Windows = []Window{{Key: "monthly", Unit: UnitCredits, Used: used, Limit: total, Remaining: remaining, UsedFrac: F(*used / *total), ResetsAt: reset}}
	return s, nil
}
