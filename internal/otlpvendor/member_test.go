package otlpvendor

import (
	"testing"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
)

// TestTeamExportCarriesMember pins the T-535 mapping the org server reuses:
// a Cursor team export's call names its member from the RESOURCE (vendor
// doc), email normalized, with the billing key; a run with no owner (team
// API key / service account) carries an empty member, which the org server
// holds as unmatched.
func TestTeamExportCarriesMember(t *testing.T) {
	rec := func() *logspb.LogRecord {
		return &logspb.LogRecord{TimeUnixNano: 1, Attributes: kvs("event.name", s("cursor.api.request"),
			"cursor.conversation.id", s("c-1"), "cursor.event.id", s("ev-1"), "cursor.usage_event.id", s("ue-1"),
			"cursor.api.request.input_tokens", i(10), "cursor.api.request.output_tokens", i(2))}
	}
	owned := &collogspb.ExportLogsServiceRequest{ResourceLogs: []*logspb.ResourceLogs{{
		Resource: &resourcepb.Resource{Attributes: kvs("service.name", s("cursor"), "cursor.team.id", s("t-1"),
			"cursor.user.id", s("u-7"), "cursor.user.account_id", s("a-7"), "cursor.user.email", s(" Dev@Example.com"))},
		ScopeLogs: []*logspb.ScopeLogs{{LogRecords: []*logspb.LogRecord{rec()}}},
	}}}
	got := ParseLogs(owned)
	if len(got.Turns) != 1 {
		t.Fatalf("turns = %d", len(got.Turns))
	}
	tr := got.Turns[0]
	if tr.Member != (Member{Email: "dev@example.com", UserID: "u-7", AccountID: "a-7", TeamID: "t-1"}) || tr.BillingID != "ue-1" || tr.RequestID != "ev-1" {
		t.Fatalf("turn = %+v", tr)
	}
	ownerless := ParseLogs(logsExport("cursor", rec()))
	if len(ownerless.Turns) != 1 || ownerless.Turns[0].Member != (Member{}) {
		t.Fatalf("ownerless turn = %+v", ownerless.Turns)
	}
	// A user-grain export never carries a member.
	codex := ParseLogs(logsExport("codex_exec", &logspb.LogRecord{Attributes: kvs("event.name", s("codex.sse_event"),
		"event.kind", s("response.completed"), "input_token_count", s("5"), "conversation.id", s("c"), "user.email", s("x@y"))}))
	if len(codex.Turns) != 1 || codex.Turns[0].Member != (Member{}) {
		t.Fatalf("codex turn = %+v", codex.Turns)
	}
}
