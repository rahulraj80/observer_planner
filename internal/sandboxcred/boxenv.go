package sandboxcred

import "strings"

// boxenv.go decides a local run's box environment while its credential
// lane is open (tracker rows F-005 and F-010 A7, finding N3-A7): only the
// lane's own key variable may enter the box, holding the run's sentinel;
// every other variable the credential table (each provider's CredEnv)
// classes as a credential is dropped, whether the tool row or the user's
// pass_env let it in, so the box never holds a real model key beside the
// sentinel. A variable the table does not class is kept as before.

// EnvAction is what happens to one box variable.
type EnvAction string

// The closed EnvAction vocabulary.
const (
	EnvKeep     EnvAction = "keep"
	EnvSentinel EnvAction = "sentinel"
	EnvDrop     EnvAction = "drop"
)

// IsCredentialEnv reports whether the credential table classes name as a
// variable carrying a model provider's credential.
func IsCredentialEnv(name string) bool {
	for _, p := range providers {
		for _, n := range p.CredEnv {
			if n == name {
				return true
			}
		}
	}
	return false
}

// boxEnvRule is one row: when matches holds, the variable gets action.
type boxEnvRule struct {
	name    string
	matches func(lane Provider, name string) bool
	action  EnvAction
}

// boxEnvRules are walked top-down for each variable of the box env.
var boxEnvRules = []boxEnvRule{
	{"the lane's key variable", func(p Provider, n string) bool { return p.KeyEnv != "" && n == p.KeyEnv }, EnvSentinel},
	{"another credential variable", func(_ Provider, n string) bool { return IsCredentialEnv(n) }, EnvDrop},
}

// BoxEnvAction is the action for variable name in the box env of a run
// whose lane injects for provider (an unknown provider injects nothing, so
// every classed variable is dropped).
func BoxEnvAction(provider, name string) EnvAction {
	p, _ := ProviderFor(provider)
	for _, r := range boxEnvRules {
		if r.matches(p, name) {
			return r.action
		}
	}
	return EnvKeep
}

// BoxEnv applies BoxEnvAction to env (KEY=VALUE entries): the lane's key
// variable carries sentinel, every other classed variable is removed. It
// returns the new env and the names it dropped (names only, in env order,
// each once).
func BoxEnv(provider, sentinel string, env []string) ([]string, []string) {
	out := make([]string, 0, len(env))
	var dropped []string
	seen := map[string]bool{}
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		switch BoxEnvAction(provider, name) {
		case EnvSentinel:
			out = append(out, name+"="+sentinel)
		case EnvDrop:
			if !seen[name] {
				seen[name] = true
				dropped = append(dropped, name)
			}
		default:
			out = append(out, kv)
		}
	}
	return out, dropped
}
