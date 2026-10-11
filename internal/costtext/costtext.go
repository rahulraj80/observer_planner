package costtext

import (
	"regexp"
	"strconv"
	"strings"
)

// The two developer cost displays. The values are the wire values of
// orgcontract.DisplayCostUSD / DisplayCostTokens, restated so this package
// stays dependency-free.
const (
	ModeUSD    = "usd"
	ModeTokens = "tokens"
)

// Hidden is what a dollar amount inside text becomes in token mode. A plain
// ASCII word, so a masked line reads as a deliberate omission rather than as
// a missing value or a typo.
const Hidden = "[hidden]"

// Tokens reports whether figures render as token usage. Only an explicit
// "usd" (or the empty value of a surface that has no org wiring at all) shows
// dollars; any other value, including one this build does not know, is token
// usage, never dollars (D-179: an unknown signed mode resolves to tokens).
func Tokens(mode string) bool {
	return mode != ModeUSD && mode != ""
}

// dollarAmount matches a dollar figure inside prose: "$4", "$1,234.56",
// "$0.0040", "$ 12", "$12k" (a leading sign or tilde stays outside the
// match). It is the same shape as shared/lib/format.ts DOLLAR_AMOUNT; RE2 has
// no lookahead, so the magnitude suffix needs a word boundary after it.
var dollarAmount = regexp.MustCompile(`\$ ?[0-9][0-9,]*(?:\.[0-9]+)?(?:[kKmMbB]\b)?`)

// MaskDollars hides every dollar amount inside s in token mode; the words
// around each amount stay. The identity in dollar mode. Prose cannot be
// re-denominated, so this is a hide, never a conversion.
func MaskDollars(mode, s string) string {
	if !Tokens(mode) || !strings.Contains(s, "$") {
		return s
	}
	return maskAligned(s)
}

// maskAligned replaces each dollar amount with Hidden and keeps a padded
// table column aligned: when the amount is followed by a run of two or more
// spaces (the padding a tabwriter emits), the run absorbs the length
// difference, so the next column starts where it did. Prose (a single space)
// is never padded or squeezed.
func maskAligned(s string) string {
	idx := dollarAmount.FindAllStringIndex(s, -1)
	if len(idx) == 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + len(idx)*len(Hidden))
	prev := 0
	for _, m := range idx {
		start, end := m[0], m[1]
		if start < prev {
			continue
		}
		b.WriteString(s[prev:start])
		b.WriteString(Hidden)
		spaces := 0
		for end+spaces < len(s) && s[end+spaces] == ' ' {
			spaces++
		}
		diff := len(Hidden) - (end - start)
		if spaces >= 2 {
			switch {
			case diff > 0:
				end += min(diff, spaces-1)
			case diff < 0:
				b.WriteString(strings.Repeat(" ", -diff))
			}
		}
		prev = end
	}
	b.WriteString(s[prev:])
	return b.String()
}

// IsDollarKey is the ONE rule for which structured field names carry a dollar
// amount: a key that is "usd" or "cost", or carries a "usd" word ("cost_usd",
// "usd_per_turn", "est_usd_total", "TotalUSD", "costUsd"). A token count
// ("cost_tokens") or a label ("cost_reliability", "cost_source") is not one.
// The web export twin is web/src/lib/costExport.ts isDollarKey; the two are
// pinned by the same table of keys.
func IsDollarKey(key string) bool {
	if strings.HasSuffix(key, "USD") || strings.HasSuffix(key, "Usd") {
		return true
	}
	k := strings.ToLower(key)
	if k == "usd" || k == "cost" {
		return true
	}
	for _, part := range strings.FieldsFunc(k, func(r rune) bool { return r == '_' || r == '-' || r == '.' }) {
		if part == "usd" {
			return true
		}
	}
	return false
}

// KeepColumns returns the columns a token-mode export keeps: every column
// that is not a dollar column. The identity (a copy) in dollar mode.
func KeepColumns(mode string, headers []string) []string {
	out := make([]string, 0, len(headers))
	for _, h := range headers {
		if Tokens(mode) && IsDollarKey(h) {
			continue
		}
		out = append(out, h)
	}
	return out
}

// StripDollarFields returns a copy of a decoded JSON value (the
// map[string]any / []any / string / json.Number / ... shapes encoding/json
// produces) with every dollar-keyed field removed and every remaining string
// masked. Numbers, booleans and nil pass through.
func StripDollarFields(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if IsDollarKey(k) {
				continue
			}
			out[k] = StripDollarFields(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = StripDollarFields(val)
		}
		return out
	case string:
		return MaskDollars(ModeTokens, t)
	default:
		return v
	}
}

// TokenFigure renders a token count the way the node dashboard's compact
// formatter does ("950 tok", "1K tok", "212.4K tok", "1.5M tok"): the token
// counterpart a figure shows in place of dollars.
func TokenFigure(n int64) string {
	neg := ""
	if n < 0 {
		neg, n = "-", -n
	}
	units := []struct {
		div    float64
		suffix string
	}{{1e9, "B"}, {1e6, "M"}, {1e3, "K"}}
	for _, u := range units {
		if float64(n) >= u.div {
			v := strconv.FormatFloat(float64(n)/u.div, 'f', 1, 64)
			return neg + strings.TrimSuffix(v, ".0") + u.suffix + " tok"
		}
	}
	return neg + strconv.FormatInt(n, 10) + " tok"
}
