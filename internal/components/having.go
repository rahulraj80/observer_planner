package components

// DistrosHaving names the prebuilt node distros that compile id in, as the
// one phrase every surface uses ("builds that include it: developer, full").
// It is the exported form of the resolver's own wording, so the feasibility
// seam (internal/feasibility/nodefeas) and the resolver's selection errors
// can never word the same fact differently (D-514).
func DistrosHaving(id string) string { return distrosHaving(id) }
