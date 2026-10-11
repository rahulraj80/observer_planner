// Package poolside holds the pure drivers of Poolside's inference gateways
// (lane ENTAPI-L16E, plan Phase 8, T-717): the LiteLLM gateway's daily
// activity.
//
// Poolside documents two OPTIONAL gateways a customer may deploy in front of
// its Poolside model inference: LiteLLM and Bifrost. Both are THIRD-PARTY
// software the customer runs on its own domain, so their catalog rows are
// customer_host rows (the connection declares the host tuple; docs/
// vendor-connections.md "Customer-hosted host binding") and the API contract
// is the gateway's own, not Poolside's.
//
// Gateway traffic is any client's traffic, so every fact lands under the
// reserved unattributed value gateway:litellm (vendorapi/gateway.go), never
// under the poolside registry tool.
//
// Written from the public references (https://docs.poolside.ai/deployment/
// inference-gateways/litellm.md, https://docs.litellm.ai/docs/proxy/
// cost_tracking.md and the LiteLLM source's response models), fetched on
// 2026-10-08; nothing here was observed from a live response. The surfaces are
// labelled "synthetic, awaiting live validation" in the catalog.
//
// The package is pure (no SQL, HTTP or file system access; imports_test.go
// pins it).
package poolside
