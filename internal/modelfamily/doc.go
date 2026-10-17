// Package modelfamily is the closed model_family normalizer: it maps a raw
// model id to one of a small, versioned family vocabulary so no long-tail
// model string leaves the seam that uses it. It is pure (stdlib only).
//
// It was internal/aggregate's modelfamily.go; it moved here (lane MP-L1E,
// T-715) because core code (the store's structural-insights facts) uses it
// too, and a build without egress.aggregate-share must keep it.
// internal/aggregate re-exports every name, so its callers are unchanged.
package modelfamily
