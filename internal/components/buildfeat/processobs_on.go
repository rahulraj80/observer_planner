//go:build !sbo_omit_processobs

package buildfeat

// HasProcessobs reports that process observation (component processobs) is
// compiled into this binary. A size optimisation and a test switch only,
// never the proof of removal (package absence is).
const HasProcessobs = true
