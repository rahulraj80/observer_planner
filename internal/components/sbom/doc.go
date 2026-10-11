// Package sbom renders a CycloneDX 1.5 software bill of materials for one
// observer binary from its Go build information (modular packaging plan
// 5.5, lane MP-REL).
//
// It is pure: it takes a *debug.BuildInfo (read from a file by
// debug/buildinfo.ReadFile in cmd/sbo-components, or from the running
// process by debug.ReadBuildInfo in the dashboard) plus the distro facts
// the caller already knows, and returns JSON bytes. No SQL, HTTP, file or
// process I/O happens here, so the release pipeline's per-artifact SBOM and
// the Components page's download (T-670) are the same document for the
// same binary.
//
// The generation context names the distro, its build tags and the catalog
// version (CISA 2025 draft minimum elements). The document is deterministic
// for a given binary: no wall-clock timestamp unless the caller passes one
// (the release passes SOURCE_DATE_EPOCH), and a serial number derived from
// the binary's own content.
package sbom
