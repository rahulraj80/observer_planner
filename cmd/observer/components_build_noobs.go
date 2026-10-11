package main

import "github.com/marmutapp/superbased-observer/internal/components"

// components_build_noobs.go reports Plane A observability compiled out. A
// build with `-tags sbo_omit_obs` (the catalog's tag for obs, plan 5.1),
// with the one-release alias `-tags no_obs`, or with `-tags
// sbo_omit_judge_client` (obs requires the judge client compiled) links
// obs_wire_stub.go instead of the observability engine, so this binary
// records obs as not compiled in and `observer components` and GET
// /api/components say so (the binary reports its compiled set itself).
//
// PUBLIC TREE NOTE: scripts/release.sh makes this file and obs_wire_stub.go
// unconditional in the public staging tree (PUBLIC_UNCONDITIONAL_STUBS), so
// a public build, which has no internal/obs, reports obs as compiled out.
// The judge tier is public (D-438) and keeps both its variants there.
// Pinned by tests/invariant/release_public_patch_test.go.
func init() { components.MarkOmitted("obs") }
