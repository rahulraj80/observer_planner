// Package buildfeat will hold one generated build-feature constant per
// removable component (plan 5.1): `const HasCodeintel = true` in the
// component's on file and `false` in its off file, for the few call sites
// inside shared packages, so the linker can drop guarded code (the
// Tailscale buildfeatures lesson).
//
// It is a skeleton in phase P0 (lane MP-CAT): nothing is compiled out yet,
// so there is no constant to generate. Lanes MP-L1A and MP-L1B add the
// constants with each wiring pair, generated from internal/components so
// the names cannot drift from the catalog. The constants are a size
// optimisation only and are never the proof of removal: package absence
// in `go list -deps` is the only L1 proof.
package buildfeat
