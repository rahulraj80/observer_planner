//go:build sbo_omit_agentaccess_relay

package store

// relayInBuild is false: mcprelay_omit.go composes no relay rows in a build
// without agentaccess.relay (D-535).
const relayInBuild = false
