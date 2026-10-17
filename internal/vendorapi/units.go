package vendorapi

import (
	"math"
	"math/big"
	"regexp"
	"strings"
)

// Micro-unit conversions for aggregate facts (lane ENTAPI-D1). A FactRow's
// ValueMicros is an integer count of millionths of its Unit, so a driver
// never stores a float and never sums units. Each helper reports false
// instead of overflowing or guessing; the driver then fails the page as
// schema drift (a value it cannot represent exactly is a unit change, plan
// 12A.5).

// CountMicros converts a whole count (tokens, requests, lines) to micro-units.
func CountMicros(n int64) (int64, bool) {
	if n < 0 || n > math.MaxInt64/1_000_000 {
		return 0, false
	}
	return n * 1_000_000, true
}

// DecimalMicros converts a decimal string (a JSON number or a quoted decimal)
// expressed in units of 10^-exp of the fact unit to micro-units, rounding
// half away from zero to the nearest micro-unit. exp 0 means the decimal is
// already in whole units (OpenAI dollars); exp 2 means hundredths (Anthropic
// cents of a dollar). Exponents, NaN and infinities are refused.
func DecimalMicros(s string, exp int) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 64 || exp < 0 || exp > 6 || strings.ContainsAny(s, "eEnNiI") {
		return 0, false
	}
	// G113 (CVE-2022-23772) is fixed in the toolchain; the input is also bounded
	// to 64 bytes and exponent forms are refused above.
	r, ok := new(big.Rat).SetString(s) //nolint:gosec // G113: bounded input, no exponent
	if !ok {
		return 0, false
	}
	return ratMicros(r, exp)
}

// floatRE is a JSON number with an optional exponent of at most two digits.
var floatRE = regexp.MustCompile(`^-?(0|[1-9][0-9]{0,17})(\.[0-9]{1,18})?([eE][-+]?[0-9]{1,2})?$`)

// FloatMicros converts a JSON number a vendor serializes from a binary float
// (a Python float: LiteLLM writes 1.82e-05 for 0.0000182 dollars) to
// micro-units of its whole unit, rounding half away from zero (lane
// ENTAPI-L16E). Unlike DecimalMicros it admits an exponent, bounded to two
// digits by the pattern, so the rational it builds is small; a quoted value,
// NaN, an infinity or anything longer than 64 bytes is refused.
func FloatMicros(s string) (int64, bool) {
	if len(s) > 64 || !floatRE.MatchString(s) {
		return 0, false
	}
	r, ok := new(big.Rat).SetString(s) //nolint:gosec // G113: bounded input, exponent at most two digits
	if !ok {
		return 0, false
	}
	return ratMicros(r, 0)
}

// ratMicros scales r (in units of 10^-exp) to micro-units, rounding half away
// from zero; false when the result does not fit an int64.
func ratMicros(r *big.Rat, exp int) (int64, bool) {
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(6-exp)), nil)
	r.Mul(r, new(big.Rat).SetInt(scale))
	num, den := new(big.Int).Set(r.Num()), r.Denom()
	neg := num.Sign() < 0
	num.Abs(num)
	q, rem := new(big.Int).QuoRem(num, den, new(big.Int))
	if new(big.Int).Mul(rem, big.NewInt(2)).Cmp(den) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	if neg {
		q.Neg(q)
	}
	if !q.IsInt64() {
		return 0, false
	}
	return q.Int64(), true
}
