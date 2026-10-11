package otlp

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// OTLP/HTTP carries either binary protobuf (application/x-protobuf) or the
// OTLP JSON encoding (application/json). Several AI coding tools export
// JSON only or by default (Gemini CLI and Qwen Code "http", GitHub Copilot
// CLI, OpenCode), so the receiver accepts both; the signal handlers see the
// same decoded message either way.
const (
	contentTypeProtobuf = "application/x-protobuf"
	contentTypeJSON     = "application/json"
)

// isJSON reports whether the request body is OTLP JSON.
func isJSON(req *http.Request) bool {
	mt, _, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	return err == nil && mt == contentTypeJSON
}

// decodeBody decodes an OTLP/HTTP request body into msg by its content type.
func decodeBody(req *http.Request, raw []byte, msg proto.Message) error {
	if !isJSON(req) {
		return proto.Unmarshal(raw, msg)
	}
	return UnmarshalJSON(raw, msg)
}

// encodeResponse marshals the export response in the request's encoding (the
// OTLP spec answers a JSON request in JSON).
func encodeResponse(req *http.Request, msg proto.Message) ([]byte, string, error) {
	if isJSON(req) {
		out, err := protojson.Marshal(msg)
		return out, contentTypeJSON, err
	}
	out, err := proto.Marshal(msg)
	return out, contentTypeProtobuf, err
}

// otlpIDFields are the bytes fields the OTLP JSON encoding writes as HEX
// strings, where the canonical protobuf JSON mapping expects base64. Both
// the lowerCamelCase and the original field spellings are accepted by
// protojson, so both are rewritten.
var otlpIDFields = map[string]bool{
	"traceId": true, "spanId": true, "parentSpanId": true,
	"trace_id": true, "span_id": true, "parent_span_id": true,
}

// UnmarshalJSON decodes an OTLP JSON body (the OTLP/HTTP JSON encoding): it rewrites the hex trace/span ids to
// base64, then decodes with protojson, ignoring unknown fields as the OTLP
// spec requires of a receiver.
func UnmarshalJSON(raw []byte, msg proto.Message) error {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("otlp json: %w", err)
	}
	rewriteHexIDs(doc)
	fixed, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("otlp json: %w", err)
	}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(fixed, msg); err != nil {
		return fmt.Errorf("otlp json: %w", err)
	}
	return nil
}

// rewriteHexIDs walks a decoded JSON document and converts every hex id
// field to base64 in place. A value that is not valid hex is left as is.
func rewriteHexIDs(v any) {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if s, ok := val.(string); ok && otlpIDFields[k] {
				if b, err := hex.DecodeString(s); err == nil {
					x[k] = base64.StdEncoding.EncodeToString(b)
				}
				continue
			}
			rewriteHexIDs(val)
		}
	case []any:
		for _, e := range x {
			rewriteHexIDs(e)
		}
	}
}

// malformedText is the 400 body for an undecodable request.
func malformedText(req *http.Request) string {
	if isJSON(req) {
		return "bad json"
	}
	return "bad protobuf"
}
