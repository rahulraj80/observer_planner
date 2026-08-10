package briefing

import (
	"fmt"
	"sort"
)

// Claim is a structured efficiency claim: the title, the action nudge, the
// data source, and the evidence math. Justify turns it into a fully-stamped
// Justification with the window and pricing basis bound in.
type Claim struct {
	Detector       string
	Title          string
	Nudge          string
	Source         string
	GroupBy        string
	SavingsUSD     float64
	SavingsMin     float64
	Confidence     float64
	Tool           string
	Project        string
	FlipConditions string
	Evidence       Evidence
	Reprice        []RepriceTurn
}

// Justify is the single formatter that binds a Claim to the Facts window and
// pricing basis, producing the Justification rendered identically by the
// dashboard, the digest email, and MCP tools (§2.4 of the briefing spec).
func Justify(c Claim, f Facts) Justification {
	j := Justification{
		Claim:           c.Title,
		Source:          c.Source,
		GroupBy:         c.GroupBy,
		Since:           f.Window.Since.UTC().Format("2006-01-02T15:04:05Z07:00"),
		Until:           f.Window.Until.UTC().Format("2006-01-02T15:04:05Z07:00"),
		Tool:            c.Tool,
		Project:         c.Project,
		PriceBasis:      f.Basis,
		EstimateVersion: EstimateVersion,
		Evidence:        normalizeEvidence(c.Evidence),
		Confidence:      clamp01(c.Confidence),
		FlipConditions:  c.FlipConditions,
		Reprice:         c.Reprice,
	}
	return j
}

// normalizeEvidence sorts evidence items deterministically and fills a
// deterministic ordering for the numbers map (keys sorted).
func normalizeEvidence(e Evidence) Evidence {
	items := make([]EvidenceItem, len(e.Items))
	copy(items, e.Items)
	sort.SliceStable(items, func(i, j int) bool { return items[i].Label < items[j].Label })
	nums := e.Numbers
	if nums != nil {
		keys := make([]string, 0, len(nums))
		for k := range nums {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		sorted := make(map[string]float64, len(nums))
		for _, k := range keys {
			sorted[k] = nums[k]
		}
		nums = sorted
	}
	return Evidence{Numbers: nums, Items: items, Math: e.Math}
}

// RepriceSummary is a compact human line for the counterfactual replay:
// "session models → candidate on N steps at ~$X (candidate cost)". The
// original cost lives in the parent opportunity's SavingsUSD.
func RepriceSummary(reprice []RepriceTurn) string {
	if len(reprice) == 0 {
		return ""
	}
	from := reprice[0].OriginalModel
	to := reprice[0].SelectedModel
	var candidate float64
	for _, r := range reprice {
		candidate += r.EstimatedCost
	}
	return fmt.Sprintf("%d step(s) replayed %s → %s; candidate cost ~$%.2f",
		len(reprice), from, to, round2(candidate))
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
