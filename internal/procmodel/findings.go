package procmodel

import "strings"

// FindingRuleID names a built-in finding. Stable IDs (the §14 catalog), used
// as the surfaced rule id and as data, never branched on as control flow.
type FindingRuleID string

const (
	// FindingPrivilegedExec: an attributed process runs with effective root
	// (euid 0) from a non-root real uid — a privilege elevation (setuid-root:
	// sudo / su / pkexec) inside an AI session.
	FindingPrivilegedExec FindingRuleID = "process.privileged_exec"
	// FindingExecutableFromTmp: an attributed process's executable lives under
	// a temp / cache / downloads directory — code run from a scratch location.
	FindingExecutableFromTmp FindingRuleID = "process.executable_from_tmp"
)

// Finding severities — compact strings mirroring the guard severity vocabulary
// (info < warn < high). Stored/surfaced as data.
const (
	SeverityInfo = "info"
	SeverityWarn = "warn"
	SeverityHigh = "high"
)

// RunFacts is the pure, SQL-free view of a process run the findings engine
// reasons over. The store loads a process_runs row into this at the boundary;
// this package never imports database/sql.
type RunFacts struct {
	ProcessKey  string
	SessionID   string
	Attributed  bool
	ExePath     string
	ExeBasename string
	UID         int
	EUID        int
}

// Finding is one derived observe-only signal about a process run.
type Finding struct {
	RuleID     FindingRuleID
	Severity   string
	ProcessKey string
	SessionID  string
	TargetKind string // "exe" | "capability" | …
	Target     string
	Detail     string // one-sentence human explanation
}

// findingRule is one row of the table-driven rule set (CLAUDE.md rule 5): an
// ordered set walked top-down, one test per row — data, not a conditional
// ladder. test reports whether the rule fires and, if so, the target and a
// human detail.
type findingRule struct {
	id       FindingRuleID
	severity string
	test     func(f RunFacts) (hit bool, targetKind, target, detail string)
}

var findingRules = []findingRule{
	{
		id:       FindingPrivilegedExec,
		severity: SeverityHigh,
		test: func(f RunFacts) (bool, string, string, string) {
			if f.EUID == 0 && f.UID != 0 {
				return true, "exe", orQuestion(f.ExeBasename), "effective root (euid 0) from a non-root real uid - a privilege elevation"
			}
			return false, "", "", ""
		},
	},
	{
		id:       FindingExecutableFromTmp,
		severity: SeverityWarn,
		test: func(f RunFacts) (bool, string, string, string) {
			if marker, ok := tmpishDir(f.ExePath); ok {
				return true, "exe", f.ExePath, "executable runs from a scratch location (" + marker + ")"
			}
			return false, "", "", ""
		},
	},
}

// DeriveFindings walks the rule table over each attributed run and returns the
// findings that fire, preserving run order then catalog (rule) order. Pure.
// Unattributed runs are skipped — a finding with no session owner is noise.
func DeriveFindings(runs []RunFacts) []Finding {
	var out []Finding
	for _, f := range runs {
		if !f.Attributed || f.SessionID == "" {
			continue
		}
		for _, rule := range findingRules {
			hit, targetKind, target, detail := rule.test(f)
			if !hit {
				continue
			}
			out = append(out, Finding{
				RuleID:     rule.id,
				Severity:   rule.severity,
				ProcessKey: f.ProcessKey,
				SessionID:  f.SessionID,
				TargetKind: targetKind,
				Target:     target,
				Detail:     detail,
			})
		}
	}
	return out
}

// tmpishDir reports whether an executable path lives under a temp / cache /
// downloads directory, returning the matched marker. Cross-OS: process paths
// are remote-OS shaped, so the path is lowercased and back-slashes folded to
// forward before matching slash-delimited markers (so "C:\\Users\\x\\AppData\\
// Local\\Temp\\x.exe" matches like a POSIX temp path).
func tmpishDir(exePath string) (string, bool) {
	if exePath == "" {
		return "", false
	}
	p := strings.ToLower(strings.ReplaceAll(exePath, "\\", "/"))
	for _, m := range []string{
		"/tmp/", "/var/tmp/", "/dev/shm/", "/.cache/", "/downloads/",
		"/appdata/local/temp/", "/windows/temp/",
	} {
		if strings.Contains(p, m) {
			return strings.Trim(m, "/"), true
		}
	}
	return "", false
}

func orQuestion(s string) string {
	if s == "" {
		return "?"
	}
	return s
}
