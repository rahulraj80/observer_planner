//go:build !sbo_omit_org_agentaccess && !sbo_omit_agentaccess_relay

package policyfam

import "github.com/marmutapp/superbased-observer/internal/policyfam/mcpaccess"

// family_mcpaccess.go links the tools.mcp_access compiler (Agent Access,
// internal/policyfam/mcpaccess) through the linked-family seam (linked.go).
// It is owned by org.agentaccess (modular packaging lane MP-L1G): an image
// built with -tags sbo_omit_org_agentaccess leaves it out, and with it
// internal/policyfam/mcpaccess and internal/mcpaccess.

func init() {
	RegisterLinkedFamily(mcpaccess.Family, LinkedFamily{
		Compile: func(raw []byte, maxBytes int64) (any, []byte, error) {
			s, canon, err := mcpaccess.CompileBody(raw, maxBytes)
			if err != nil {
				return nil, nil, err
			}
			return s, canon, nil
		},
		Enforces: func(spec any) bool {
			return mcpaccess.Enforces(spec.(mcpaccess.PolicySpec)) //nolint:forcetypeassert // caller contract: spec came from CompileFamilyBody(family, ...)
		},
	})
}
