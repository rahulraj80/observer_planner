// SPDX-License-Identifier: BUSL-1.1
// Copyright (c) 2026 SuperBased

package discover

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/mcpintel/discover pure (CLAUDE.md rule 1, doc3 11.12b): no store and
// no reuse of the SQL-importing internal/intelligence/discover.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{"database/sql", "net/http", "github.com/fsnotify/fsnotify", "github.com/marmutapp/superbased-observer/internal/store", "github.com/marmutapp/superbased-observer/internal/intelligence/discover", "github.com/marmutapp/superbased-observer/internal/orgserver/controlstore"},
		Reason:    "internal/mcpintel/discover must stay pure",
	})
}
