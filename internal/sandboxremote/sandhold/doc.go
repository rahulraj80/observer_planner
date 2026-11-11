// Package sandhold is Observer's client of a SandHold node's public API
// (SandHold notes 41 D2 and D3, 42): the device sign-in a person approves in
// the node's portal (RFC 8628), who the key belongs to (/v1/whoami), which
// isolation the node offers (/v1/targets), and revoking the key
// (DELETE /v1/keys/{id}), and, through Box, the hosted launch path: a
// sandbox's create, get, list and delete, its files, its streamed exec and
// its terminal WebSocket, as one more sandboxremote.Client. It is the one
// package of Observer that talks to a SandHold node; the cmd layer composes
// it (observer sandbox connect, disconnect, and the sandhold hosted row).
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
//     person sees it (Plain, PlainWriter), except the output of the person's
//     own command, which passes through as it is;
//   - the terminal WebSocket is wss (ws for this machine only), carries the
//     key in a header and never in its address, follows no redirect, and its
//     command's environment travels in an owner-only launch script, never in
//     the address the node logs.
//
// Standard library, the neutral sandbox vocabulary leaf, the hosted core's
// contract (sandboxremote) and coder/websocket.
package sandhold
