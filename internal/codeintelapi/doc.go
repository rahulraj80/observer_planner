// Package codeintelapi is the code-intelligence facade every consumer holds:
// the Provider interface, the plain-data types that cross it, and an empty
// provider for builds or installs without an index. It is a neutral core
// package (lane MP-L1H, T-715) so the MCP retrieval tools and the proxy's
// compression wiring link no code-intelligence engine; the native engine in
// internal/codeintel is the one real implementation.
package codeintelapi
