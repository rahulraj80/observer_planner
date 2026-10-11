package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/marmutapp/superbased-observer/internal/models"
)

// unknownLaneStatus is the status a refused /up/<id>/ request receives. 502:
// the proxy (the gateway the client addressed) has no route to the upstream
// the request named. It is answered locally; nothing is dialed.
const unknownLaneStatus = http.StatusBadGateway

// unknownLaneCode is the machine-readable error code in the refusal body.
const unknownLaneCode = "observer_unknown_upstream_lane"

// writeUnknownLaneRefusal answers a request whose `/up/<id>/` prefix names a
// lane this node cannot route (Sol A5-02). The body is provider-shaped so the
// calling client renders the message instead of a parse error, and it names
// the lane and where to configure it. No upstream request is built, so no
// credential the client sent can leave the node.
func writeUnknownLaneRefusal(w http.ResponseWriter, provider, laneID string) {
	msg := fmt.Sprintf("observer: upstream lane %q is not configured on this node ([proxy.upstreams] / [proxy.agent_upstreams], or [proxy].auto_default_lane for \"auto\"); the request was not forwarded", laneID)
	writeLaneRefusal(w, provider, unknownLaneStatus, unknownLaneCode, msg)
}

// malformedLaneStatus is the status a malformed lane address receives: the
// client's own base URL is wrong, so this is a client error, not a missing
// route.
const malformedLaneStatus = http.StatusBadRequest

// malformedLaneCode is the machine-readable code for a malformed lane address.
const malformedLaneCode = "observer_malformed_upstream_lane"

// writeMalformedLaneRefusal answers a lane address with no id or no path
// after the id (pass-2 review C156-18). Like writeUnknownLaneRefusal it is
// answered locally, so no credential the client sent can leave the node.
func writeMalformedLaneRefusal(w http.ResponseWriter, provider string) {
	msg := "observer: the request path starts with /up/ but names no upstream lane, or no API path after the lane id (expected /up/<lane>/<api path>, e.g. /up/openrouter/api/v1/chat/completions); check the client's base URL. The request was not forwarded"
	writeLaneRefusal(w, provider, malformedLaneStatus, malformedLaneCode, msg)
}

// writeLaneRefusal writes a provider-shaped local error body.
func writeLaneRefusal(w http.ResponseWriter, provider string, status int, code, msg string) {
	var body []byte
	if provider == models.ProviderOpenAI || provider == models.ProviderGoogle {
		body, _ = json.Marshal(map[string]any{
			"error": map[string]any{
				"message": msg,
				"type":    "invalid_request_error",
				"code":    code,
			},
		})
	} else {
		body, _ = json.Marshal(map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":    "invalid_request_error",
				"message": msg,
			},
		})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
