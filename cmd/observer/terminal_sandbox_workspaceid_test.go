package main

import (
	"regexp"
	"testing"
)

// A workspace id is also a box's label value on hosted sandboxes, which
// must start and end with a letter or digit.
func TestWorkspaceIDIsALabelValue(t *testing.T) {
	label := regexp.MustCompile(`^[A-Za-z0-9]([-A-Za-z0-9_.]{0,61}[A-Za-z0-9])?$`)
	for i := 0; i < 5000; i++ {
		id, err := newWorkspaceID()
		if err != nil {
			t.Fatal(err)
		}
		if !label.MatchString(id) {
			t.Fatalf("id %q is not a label value", id)
		}
	}
}
