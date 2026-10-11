package nodegov

import (
	"fmt"
	"reflect"
	"strings"
)

// SchemaHint returns a one-line, allowed-keys descriptor for the
// node.governance body (Body), for use as a PROMPT AID by a self-hosted
// policy-evolution reviewer so a small model does not emit stray keys that
// DecodeBody's DisallowUnknownFields would reject. It is NOT a validator —
// CompileBody / the lint gate remain the sole authority.
//
// It names the structural KEYS only, not the closed vocabularies of section
// ids / pinnable keys / share keys / feature ids (those are values the
// reviewer copies from the current body). The schema number and the
// "sections" / "notice" keys are DERIVED from MaxSchema and the wire structs'
// json tags (wire.go), so a new schema or a new section list (schema 3's
// expose lists, Sol F6) cannot leave the hint behind; the drift guard is
// TestSchemaHintDecodes (schemahint_test.go).
func SchemaHint() string {
	return `A JSON object with keys (use ONLY these): ` +
		fmt.Sprintf(`"schema" (number, currently %d; use the lowest schema the body needs: 3 only when an expose list is non-empty), `, MaxSchema) +
		`"sections" (object), "pinned" (object mapping a dotted config key to a value), ` +
		`"share" (object), "features" (object mapping a feature id to a bool), "notice" (object). ` +
		`The "sections" object has keys: ` + quotedJSONKeys(reflect.TypeOf(SectionsV1{})) + ` (each an array of section ids; ` +
		`"visible" and "settings_visible" are the expose lists: keep every id they hold, since dropping one hides that page again). ` +
		`The "notice" object has keys: ` + quotedJSONKeys(reflect.TypeOf(NoticeV1{})) + `. ` +
		`Do not introduce any other key; reuse only the section ids / config keys / feature ids already present in the current body.`
}

// quotedJSONKeys lists a struct's json field names, quoted, in declaration
// order.
func quotedJSONKeys(t reflect.Type) string {
	var keys []string
	for i := 0; i < t.NumField(); i++ {
		name := strings.Split(t.Field(i).Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			keys = append(keys, `"`+name+`"`)
		}
	}
	return strings.Join(keys, ", ")
}
