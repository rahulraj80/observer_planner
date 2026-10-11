package orgcontract

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

// TestPolicyVersionsDocCarriesVersionsOnly pins T-533 and POLICY-PROBE-1:
// the probe document declares the two version maps and nothing else. The
// dead ProbeAfterSeconds field (wire name probe_after_seconds, never sent by
// a server, never read by a node) is gone and its name stays reserved.
func TestPolicyVersionsDocCarriesVersionsOnly(t *testing.T) {
	var keys []string
	typ := reflect.TypeOf(PolicyVersionsDoc{})
	for i := 0; i < typ.NumField(); i++ {
		keys = append(keys, typ.Field(i).Tag.Get("json"))
	}
	sort.Strings(keys)
	want := []string{"policy_versions,omitempty", "rail_versions"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("PolicyVersionsDoc json fields = %v, want %v", keys, want)
	}
	// A body from an older server that still names the reserved field decodes
	// (the node reads this document leniently) and the field is ignored.
	var doc PolicyVersionsDoc
	if err := json.Unmarshal([]byte(`{"rail_versions":{"budget":3},"probe_after_seconds":60}`), &doc); err != nil {
		t.Fatalf("decode with the reserved name: %v", err)
	}
	if doc.RailVersions["budget"] != 3 {
		t.Fatalf("rail_versions lost: %+v", doc)
	}
}
