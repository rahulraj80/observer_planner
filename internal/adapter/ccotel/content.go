package ccotel

import (
	"github.com/marmutapp/superbased-observer/internal/otlpvendor"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
)

// Content kinds for a ContentRecord. They equal otlpvendor's content kinds.
const (
	KindPrompt     = otlpvendor.ContentPrompt
	KindToolInput  = otlpvendor.ContentToolInput
	KindToolOutput = otlpvendor.ContentToolOutput
	KindRawBody    = otlpvendor.ContentRawBody
)

// ContentRecord is one captured content body from the OTel stream (a user
// prompt, tool input/output, or raw API body). Content is the RAW text as
// emitted — the caller scrubs it for secrets and computes the hash before
// storage. Records are produced only for content events Claude Code emits when
// the admin enabled the OTEL_LOG_* flags; an install without them yields none.
type ContentRecord struct {
	RequestID string
	SessionID string
	ToolUseID string
	Kind      string
	Content   string
	// TimeUnixNano is the record's emit time (0 when unset); the caller maps it
	// to a timestamp.
	TimeUnixNano uint64
}

// ParseContent extracts Claude Code content bodies from an OTLP logs export.
// Resources tagged as Observer-emitted are skipped (echo guard). Records
// without any content text are dropped. The caller gates this on
// [ingest.otel].content_capture and scrubs/hashes each Content before storage.
// The event table is the claude-code row of internal/otlpvendor.
func ParseContent(req *collogspb.ExportLogsServiceRequest) []ContentRecord {
	if req == nil {
		return nil
	}
	var out []ContentRecord
	for _, c := range otlpvendor.ParseLogs(req).Contents {
		if c.Tool != toolClaudeCode {
			continue
		}
		out = append(out, ContentRecord{
			RequestID: c.RequestID, SessionID: c.SessionID, ToolUseID: c.ToolUseID,
			Kind: c.Kind, Content: c.Content, TimeUnixNano: c.TimeUnixNano,
		})
	}
	return out
}
