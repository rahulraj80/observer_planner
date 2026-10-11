package mcpaccess

import (
	"reflect"
	"strings"
	"testing"
)

// TestStrictClassTableCoversVocabulary: every accepted class has exactly one
// predicate row and no row names a value outside the vocabulary, so an
// accepted value can never again be one the PDP does not act on (AA-69).
func TestStrictClassTableCoversVocabulary(t *testing.T) {
	rows := map[string]int{}
	for _, r := range strictClassTable {
		rows[r.class]++
	}
	for _, c := range StrictClasses {
		if rows[c] != 1 {
			t.Errorf("class %q has %d predicate rows, want 1", c, rows[c])
		}
	}
	if len(rows) != len(StrictClasses) {
		t.Errorf("predicate table has %d classes, vocabulary %d", len(rows), len(StrictClasses))
	}
	for _, r := range RetiredStrictClasses() {
		if contains(StrictClasses, r.Value) {
			t.Errorf("retired %q is still in the accepted vocabulary", r.Value)
		}
		for _, c := range r.Into {
			if !contains(StrictClasses, c) {
				t.Errorf("retired %q converts into unknown class %q", r.Value, c)
			}
		}
	}
}

// TestMatchStrictClass is one case per (class, call shape): a call the class
// names matches; one it does not name does not.
func TestMatchStrictClass(t *testing.T) {
	governedCall := StrictCall{DecisionClass: StrictClassGoverned, Action: ActionCall}
	destructiveCall := StrictCall{DecisionClass: StrictClassGoverned, Action: ActionCall, Destructive: true}
	governedRead := StrictCall{DecisionClass: StrictClassGoverned, Action: ActionRead}
	governedTask := StrictCall{DecisionClass: StrictClassGoverned, Action: ActionTasksCancel}
	catalogueList := StrictCall{DecisionClass: StrictClassCatalogue, Action: ActionList}
	catalogueDiscover := StrictCall{DecisionClass: StrictClassCatalogue, Action: ActionDiscover}
	cases := []struct {
		class string
		call  StrictCall
		want  bool
	}{
		{StrictClassGoverned, governedCall, true},
		{StrictClassGoverned, governedRead, true},
		{StrictClassGoverned, governedTask, true},
		{StrictClassGoverned, catalogueList, false},
		{StrictClassCatalogue, catalogueList, true},
		{StrictClassCatalogue, catalogueDiscover, true},
		{StrictClassCatalogue, governedCall, false},
		{StrictClassDestructive, destructiveCall, true},
		{StrictClassDestructive, governedCall, false},
		{StrictClassDestructive, governedRead, false},
		{StrictClassWrite, governedCall, true},
		{StrictClassWrite, destructiveCall, true},
		{StrictClassWrite, governedRead, false},
		{StrictClassWrite, catalogueList, false},
	}
	for _, tc := range cases {
		got, known := MatchStrictClass(tc.class, tc.call)
		if !known || got != tc.want {
			t.Errorf("MatchStrictClass(%q, %+v) = (%v, known %v), want (%v, true)", tc.class, tc.call, got, known, tc.want)
		}
	}
	if _, known := MatchStrictClass("egress", governedCall); known {
		t.Error("retired egress must not be a known (matchable) class")
	}
}

func TestNormalizeStrictClasses(t *testing.T) {
	cases := []struct {
		name                     string
		in                       []string
		classes, retired, unknwn string
	}{
		{"empty", nil, "", "", ""},
		{"sorted and de-duplicated", []string{" write", "governed", "write", ""}, "governed,write", "", ""},
		{"egress becomes every decision class", []string{"egress"}, "catalogue,governed", "egress", ""},
		{"egress merges with the rest", []string{"write", "egress", "governed"}, "catalogue,governed,write", "egress", ""},
		{"unknown is kept, never dropped", []string{"everything", "write"}, "everything,write", "", "everything"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := NormalizeStrictClasses(tc.in)
			if got := n.CSV(); got != tc.classes {
				t.Errorf("classes = %q, want %q", got, tc.classes)
			}
			if got := strings.Join(n.Retired, ","); got != tc.retired {
				t.Errorf("retired = %q, want %q", got, tc.retired)
			}
			if got := strings.Join(n.Unknown, ","); got != tc.unknwn {
				t.Errorf("unknown = %q, want %q", got, tc.unknwn)
			}
			if n.Changed() != (tc.retired != "") {
				t.Errorf("Changed() = %v", n.Changed())
			}
		})
	}
	if got := RetiredStrictClasses(); !reflect.DeepEqual(got[0].Into, []string{StrictClassCatalogue, StrictClassGoverned}) {
		t.Errorf("RetiredStrictClasses()[0].Into = %v", got[0].Into)
	}
}

// TestRetiredConversionIsNeverWeaker: every call a retired value could have
// meant (any decision class, any action) is matched by its replacement set.
func TestRetiredConversionIsNeverWeaker(t *testing.T) {
	for _, r := range RetiredStrictClasses() {
		for _, dc := range []string{StrictClassGoverned, StrictClassCatalogue} {
			for a := range actionResource {
				call := StrictCall{DecisionClass: dc, Action: a, Destructive: a == ActionCall}
				hit := false
				for _, c := range r.Into {
					if m, _ := MatchStrictClass(c, call); m {
						hit = true
					}
				}
				if !hit {
					t.Errorf("retired %q -> %v misses %+v", r.Value, r.Into, call)
				}
			}
		}
	}
}
