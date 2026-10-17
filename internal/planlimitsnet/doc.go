// Package planlimitsnet is the ONLY egress of the Plans & limits module
// (docs/plans-and-limits.md): every HTTPS call to a vendor and every vendor
// CLI or app-server process the module runs goes through this package.
// tests/invariant/planlimits_egress_test.go pins that no other package of the
// module imports net/http or os/exec, that the always-on daemon paths
// (watcher, proxy, store, hook, adapters) never link it, and that only the
// polling service and the observer command import it.
//
// The rules it enforces, before any credential is attached:
//
//   - HTTPS only, no user info, no port other than 443, and the host must be
//     one of the source row's exact Hosts (the registry table decides);
//   - redirects are never followed (a redirect off the vendor host would
//     carry the credential with it);
//   - bodies are capped, every call has a timeout;
//   - a vendor command must be the source row's Command, runs with a fixed
//     argv, no shell, an allow-listed environment, empty stdin and a timeout,
//     and is never resolved to an Observer PATH shim.
//
// Secret header values are never logged or returned.
package planlimitsnet
