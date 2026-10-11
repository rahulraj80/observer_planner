//go:build sbo_omit_judge_client

package main

import "github.com/marmutapp/superbased-observer/internal/components"

// components_build_nojudge.go reports the judge client compiled out: a
// `-tags sbo_omit_judge_client` build drops judge_wire.go (the only
// importer of internal/judgeclient), alignment_wire.go and
// projects_judge_wire.go, and links projects_judge_wire_stub.go, so the
// Projects alignment tier reports itself unavailable. obs requires the
// judge client compiled (catalog RequiresCompiled), so the same tag also
// selects the obs stub and components_build_noobs.go marks obs omitted.
func init() { components.MarkOmitted("judge.client") }
