// Package sandhold is Observer's client of a SandHold node's public API
// (SandHold notes 41 D2 and D3, 42): the device sign-in a person approves in
// the node's portal (RFC 8628), who the key belongs to (/v1/whoami), which
// isolation the node offers (/v1/targets), and revoking the key
// (DELETE /v1/keys/{id}). It is the one package of Observer that talks to a
// SandHold node; the cmd layer composes it (observer sandbox connect,
// disconnect, and the sandhold row of the backend list).
//
// Observer is a customer of the same public API every customer uses: no
// private endpoint, no shared code with SandHold. The client side of the
// security review of note 42 section 5 is kept here:
//
//   - a node address is https, or http for this machine only (NodeURL);
//   - no redirect is followed, so the key goes only to the node named
//     (HTTPClient); Go keeps X-API-Key on a redirect;
//   - a saved key is sent only to the node that issued it (Resolve);
//   - a key never prints: Credential's String and Format name the node only;
//   - every string a node sends is stripped of control characters before a
//     person sees it (Plain, PlainWriter).
//
// Standard library plus the neutral sandbox vocabulary leaf only.
package sandhold
