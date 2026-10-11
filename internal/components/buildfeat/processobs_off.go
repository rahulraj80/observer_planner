//go:build sbo_omit_processobs

package buildfeat

// HasProcessobs is false in a `-tags sbo_omit_processobs` build: process
// observation is compiled out.
const HasProcessobs = false
