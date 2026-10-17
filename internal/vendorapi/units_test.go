package vendorapi

import (
	"math"
	"testing"
)

// TestMicrosConversions pins the micro-unit helpers (lane ENTAPI-D1): exact
// decimal conversion with half-away-from-zero rounding, refusal instead of
// overflow, and no exponent or non-finite forms.
func TestMicrosConversions(t *testing.T) {
	for _, c := range []struct {
		s    string
		exp  int
		want int64
		ok   bool
	}{
		{"123.78912", 2, 1_237_891, true}, // cents: 1.2378912 USD
		{"0.06", 0, 60_000, true},         // dollars
		{"1.25", 0, 1_250_000, true},
		{"186", 2, 1_860_000, true},
		{"0.0000005", 0, 1, true},   // half rounds away from zero
		{"0.0000004", 0, 0, true},   // below half rounds down
		{"-0.0000005", 0, -1, true}, // a credit keeps its sign
		{"987654321.123456", 2, 9_876_543_211_235, true},
		{" 7 ", 0, 7_000_000, true},
		{"1e3", 0, 0, false},
		{"NaN", 0, 0, false},
		{"Inf", 0, 0, false},
		{"", 0, 0, false},
		{"abc", 0, 0, false},
		{"1", 7, 0, false},
		{"9223372036854775807", 0, 0, false},
	} {
		got, ok := DecimalMicros(c.s, c.exp)
		if ok != c.ok || got != c.want {
			t.Errorf("DecimalMicros(%q, %d) = %d, %v; want %d, %v", c.s, c.exp, got, ok, c.want, c.ok)
		}
	}
	for _, c := range []struct {
		n    int64
		want int64
		ok   bool
	}{
		{0, 0, true},
		{42, 42_000_000, true},
		{-1, 0, false},
		{math.MaxInt64 / 1_000_000, (math.MaxInt64 / 1_000_000) * 1_000_000, true},
		{math.MaxInt64/1_000_000 + 1, 0, false},
	} {
		got, ok := CountMicros(c.n)
		if ok != c.ok || got != c.want {
			t.Errorf("CountMicros(%d) = %d, %v; want %d, %v", c.n, got, ok, c.want, c.ok)
		}
	}
}

// TestFloatMicros pins the float-serialized number helper (lane ENTAPI-L16E):
// a bounded exponent is admitted (Python writes small floats in exponent
// form), rounding is half away from zero, and every other shape is refused.
func TestFloatMicros(t *testing.T) {
	for _, c := range []struct {
		s    string
		want int64
		ok   bool
	}{
		{"0.0177072", 17_707, true},
		{"1.82e-05", 18, true},
		{"3.4e-05", 34, true},
		{"5e-07", 1, true}, // exactly half a micro rounds away from zero
		{"4e-07", 0, true}, // below half rounds down
		{"2E+3", 2_000_000_000, true},
		{"0", 0, true},
		{"0.7274667", 727_467, true},
		{"1e100", 0, false},  // a three-digit exponent is refused by the pattern
		{"1e-100", 0, false}, // likewise
		{"\"0.1\"", 0, false},
		{"NaN", 0, false},
		{"Infinity", 0, false},
		{"01", 0, false},
		{".5", 0, false},
		{"1.", 0, false},
		{"", 0, false},
		{"9e18", 0, false}, // overflows int64 micro-units
	} {
		got, ok := FloatMicros(c.s)
		if ok != c.ok || got != c.want {
			t.Errorf("FloatMicros(%q) = %d, %v; want %d, %v", c.s, got, ok, c.want, c.ok)
		}
	}
}
