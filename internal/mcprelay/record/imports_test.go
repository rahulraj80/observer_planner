// SPDX-License-Identifier: BUSL-1.1
// Copyright (c) 2026 SuperBased

package record

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestRecordImportsStayAtTheStoreSeam pins the boundary: a store seam over database/sql and nothing more. Never
// internal/store, which imports this package to compose the wire (a cycle and a second push-seam owner).
func TestRecordImportsStayAtTheStoreSeam(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Custom: func(_, p string) string {
			for _, bad := range []string{"net/http", "github.com/fsnotify/fsnotify", "/internal/store", "/internal/orgclient", "/internal/orgserver", "/internal/proxy"} {
				if strings.Contains(p, bad) {
					return "contains " + bad
				}
			}
			return ""
		},
		Reason: "the record store seam must stay free of it",
	})
}
