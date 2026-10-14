// SPDX-License-Identifier: BUSL-1.1
// Copyright (c) 2026 SuperBased

package cost

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/mcpintel/cost pure (CLAUDE.md rule 1, doc3 11.12b): the callers
// import cost, never the reverse.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		ForbiddenTrees: []string{"database/sql", "net/http", "github.com/fsnotify/fsnotify", "github.com/marmutapp/superbased-observer/internal/store", "github.com/marmutapp/superbased-observer/internal/db", "github.com/marmutapp/superbased-observer/internal/orgserver", "github.com/marmutapp/superbased-observer/internal/mcpgw", "github.com/marmutapp/superbased-observer/internal/intelligence/discover"},
		Reason:         "internal/mcpintel/cost must stay pure",
	})
}
