package invariant

import (
	"reflect"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// TestComponentPostureRowWireShapeIsEnumOnly pins the node component posture
// (modular packaging plan 6.4, T-682) to closed enums, counts, digests and
// booleans, by exact field set: a new field is a deliberate edit here. A
// path, a hostname, a config value, a policy body or an org name must never
// join it.
func TestComponentPostureRowWireShapeIsEnumOnly(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		typ     reflect.Type
		allowed map[string]reflect.Kind
	}{
		{reflect.TypeOf(orgcontract.ComponentPostureRow{}), map[string]reflect.Kind{
			"Distro":           reflect.String, // closed: minimal|developer|managed|full|custom
			"CatalogVersion":   reflect.Int,    // the catalog's own version number
			"ComponentsDigest": reflect.String, // 64-hex digest of the compiled set
			"ActiveDigest":     reflect.String, // 64-hex digest of the active set
			"CompiledCount":    reflect.Int,
			"ActiveCount":      reflect.Int,
			"GovernanceSource": reflect.String, // closed nodecomponents source
			"OrgManaged":       reflect.Bool,
			"CeilingPinned":    reflect.Bool,
			"Inactive":         reflect.Slice,   // of ComponentStateEntry, below
			"ScheduledPurges":  reflect.Pointer, // to a slice of ComponentPurgeEntry, below (T-711)
		}},
		{reflect.TypeOf(orgcontract.ComponentPurgeEntry{}), map[string]reflect.Kind{
			"ID":    reflect.String, // a catalog id; the server drops unknown ids
			"State": reflect.String, // closed scheduled purge state
			"DueOn": reflect.String, // a YYYY-MM-DD date, validated on both sides
		}},
		{reflect.TypeOf(orgcontract.ComponentStateEntry{}), map[string]reflect.Kind{
			"ID":     reflect.String, // a catalog id; the server drops unknown ids
			"State":  reflect.String, // closed component state
			"Reason": reflect.String, // closed component reason
		}},
	} {
		if tc.typ.NumField() != len(tc.allowed) {
			t.Errorf("%s has %d fields, the pinned set has %d", tc.typ.Name(), tc.typ.NumField(), len(tc.allowed))
		}
		for i := 0; i < tc.typ.NumField(); i++ {
			f := tc.typ.Field(i)
			kind, ok := tc.allowed[f.Name]
			if !ok {
				t.Errorf("%s.%s is not in the pinned enum-only set", tc.typ.Name(), f.Name)
				continue
			}
			if f.Type.Kind() != kind {
				t.Errorf("%s.%s kind = %s, want %s", tc.typ.Name(), f.Name, f.Type.Kind(), kind)
			}
		}
	}
}
