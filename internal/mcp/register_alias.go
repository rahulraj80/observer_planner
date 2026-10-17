package mcp

import "github.com/marmutapp/superbased-observer/internal/mcpregister"

// The client-config registrar lives in internal/mcpregister (lane MP-L1H,
// T-715), so it stays in a build without the MCP server. These aliases keep
// existing callers of the old names compiling unchanged.
type (
	// Registrar is mcpregister.Registrar.
	Registrar = mcpregister.Registrar
	// RegisterOptions is mcpregister.RegisterOptions.
	RegisterOptions = mcpregister.RegisterOptions
	// RegistrationResult is mcpregister.RegistrationResult.
	RegistrationResult = mcpregister.RegistrationResult
	// UnregistrationResult is mcpregister.UnregistrationResult.
	UnregistrationResult = mcpregister.UnregistrationResult
)

// ServerName is mcpregister.ServerName.
const ServerName = mcpregister.ServerName

// NewRegistrar is mcpregister.NewRegistrar.
func NewRegistrar(opts RegisterOptions) (*Registrar, error) { return mcpregister.NewRegistrar(opts) }
