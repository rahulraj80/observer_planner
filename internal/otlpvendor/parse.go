package otlpvendor

import (
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

// Echo guard: Observer stamps sbo.emitted_by=observer on the telemetry it
// exports itself (internal/exporter/otel); such a resource is never read
// back in.
const (
	attrEmittedBy = "sbo.emitted_by"
	emittedBySelf = "observer"
	attrService   = "service.name"
)

// IsEcho reports whether a resource's attributes mark it Observer-emitted.
func IsEcho(res []*commonpb.KeyValue) bool {
	return attrMap(res)[attrEmittedBy].GetStringValue() == emittedBySelf
}

// claim returns the vendor whose ServiceNames contains svc.
func claim(svc string) (*Vendor, bool) {
	if svc == "" {
		return nil, false
	}
	for i := range vendors {
		for _, n := range vendors[i].ServiceNames {
			if n == svc {
				return &vendors[i], true
			}
		}
	}
	return nil, false
}

// candidatesFor returns the vendors whose rules read a resource: the
// claiming vendor alone, or (unclaimed) every ClaimByEventName vendor.
func candidatesFor(res attrs) []*Vendor {
	if v, ok := claim(res[attrService].GetStringValue()); ok {
		return []*Vendor{v}
	}
	var out []*Vendor
	for i := range vendors {
		if vendors[i].ClaimByEventName {
			out = append(out, &vendors[i])
		}
	}
	return out
}

// ClaimsService reports whether a mapping row claims resources with this
// service.name. The daemon uses it to keep a coding tool's spans away from
// the Plane A trace handler.
func ClaimsService(svc string) bool {
	_, ok := claim(svc)
	return ok
}

// ParseLogs maps an OTLP logs export.
func ParseLogs(req *collogspb.ExportLogsServiceRequest) Result {
	var out Result
	if req == nil {
		return out
	}
	seen := map[string]bool{}
	for _, rl := range req.GetResourceLogs() {
		if IsEcho(rl.GetResource().GetAttributes()) {
			continue
		}
		res := attrMap(rl.GetResource().GetAttributes())
		cands := candidatesFor(res)
		matched := false
		for _, sl := range rl.GetScopeLogs() {
			for ord, rec := range sl.GetLogRecords() {
				names := logNames(rec)
				ts := logTime(rec)
				v := view{rec: attrMap(rec.GetAttributes()), res: res}
				for _, vd := range cands {
					for ri := range vd.Rules {
						r := &vd.Rules[ri]
						if r.Signal != SignalLogs || !nameMatch(r, names) || !condsHold(v, r.Where) {
							continue
						}
						matched = true
						body := rec.GetBody().GetStringValue()
						apply(&out, vd, r, v, ts, rec.GetTimeUnixNano(), recordKey(ts, nil, ord), body)
					}
				}
			}
		}
		noteUnclaimed(&out, seen, res, matched)
	}
	return out
}

// ParseSpans maps an OTLP trace export.
func ParseSpans(req *coltracepb.ExportTraceServiceRequest) Result {
	var out Result
	if req == nil {
		return out
	}
	seen := map[string]bool{}
	for _, rs := range req.GetResourceSpans() {
		if IsEcho(rs.GetResource().GetAttributes()) {
			continue
		}
		res := attrMap(rs.GetResource().GetAttributes())
		cands := candidatesFor(res)
		matched := false
		for _, ss := range rs.GetScopeSpans() {
			for _, sp := range ss.GetSpans() {
				v := view{rec: attrMap(sp.GetAttributes()), res: res}
				ts := spanTime(sp)
				for _, vd := range cands {
					for ri := range vd.Rules {
						r := &vd.Rules[ri]
						if r.Signal != SignalSpans || !nameMatch(r, []string{sp.GetName()}) || !condsHold(v, r.Where) {
							continue
						}
						matched = true
						apply(&out, vd, r, v, ts, sp.GetStartTimeUnixNano(), recordKey(ts, sp.GetSpanId(), 0), "")
						if r.Kind == KindTurn || r.Kind == KindTool {
							setSpanDuration(&out, r.Kind, sp)
						}
					}
				}
			}
		}
		noteUnclaimed(&out, seen, res, matched)
	}
	return out
}

// noteUnclaimed records a resource's service.name when no row read it.
func noteUnclaimed(out *Result, seen map[string]bool, res attrs, matched bool) {
	svc := res[attrService].GetStringValue()
	if matched || svc == "" || seen[svc] {
		return
	}
	if _, ok := claim(svc); ok {
		return
	}
	seen[svc] = true
	out.Unclaimed = append(out.Unclaimed, svc)
}

// logNames returns the candidate event names of a log record: the typed
// EventName and the event.name attribute (Codex sets the typed field to its
// tracing target and the real name in the attribute).
func logNames(rec interface {
	GetEventName() string
	GetAttributes() []*commonpb.KeyValue
},
) []string {
	names := []string{rec.GetEventName()}
	for _, kv := range rec.GetAttributes() {
		if kv.GetKey() == "event.name" {
			names = append(names, kv.GetValue().GetStringValue())
			break
		}
	}
	return names
}

// nameMatch reports whether any candidate name satisfies the rule.
func nameMatch(r *Rule, names []string) bool {
	for _, n := range names {
		if n == "" {
			continue
		}
		for _, want := range r.Names {
			if n == want || (r.NamePrefix && strings.HasPrefix(n, want)) {
				return true
			}
		}
	}
	return false
}

// condsHold reports whether every condition holds on the record.
func condsHold(v view, conds []Cond) bool {
	for _, c := range conds {
		val, ok := v.rec[c.Key]
		present := ok && val != nil && val.GetValue() != nil
		if c.Absent {
			if present {
				return false
			}
			continue
		}
		if !present {
			return false
		}
		if c.Equals != "" && val.GetStringValue() != c.Equals {
			return false
		}
	}
	return true
}

// apply maps one matched record by the rule's kind.
func apply(out *Result, vd *Vendor, r *Rule, v view, ts time.Time, tsNano uint64, key, body string) {
	f := &r.F
	session := v.str(f.Session)
	switch r.Kind {
	case KindTurn:
		request := v.str(f.Request)
		synthetic := false
		if request == "" {
			if r.RequireRequest {
				out.Skipped++
				return
			}
			request = synthID(vd.Tool, session, key)
			synthetic = true
		}
		t := Turn{
			Tool: vd.Tool, SourceTag: vd.SourceTag, Provider: firstNonEmpty(v.str(f.Provider), vd.Provider),
			SessionID: session, RequestID: request, RequestSynthetic: synthetic,
			Model: v.str(f.Model), Timestamp: ts,
			CacheReadTokens:  v.int64Of(f.CacheRead),
			CacheWriteTokens: v.int64Of(f.CacheWrite),
			ReasoningTokens:  v.int64Of(f.Reasoning),
			DurationMS:       v.int64Of(f.Duration),
			ViaProxyRoute: v.res[RouteResourceAttr].GetStringValue() == RouteProxy &&
				v.res[RouteToolResourceAttr].GetStringValue() == vd.Tool,
			BillingID: v.str(f.BillingID),
			Member: Member{
				Email: strings.ToLower(strings.TrimSpace(v.str(f.MemberEmail))), UserID: v.str(f.MemberUserID),
				AccountID: v.str(f.MemberAccountID), TeamID: v.str(f.MemberTeamID),
			},
		}
		t.CostUSD, _ = v.num(f.Cost)
		t.InputTokens, t.OutputTokens = normalize(vd.Norm, v.int64Of(f.Input), v.int64Of(f.Output), v.int64Of(f.Total), t)
		out.Turns = append(out.Turns, t)
	case KindTool:
		tc := ToolCall{
			Tool: vd.Tool, SessionID: session, Name: v.str(f.ToolName),
			CallID: v.str(f.CallID), DurationMS: v.int64Of(f.Duration), Timestamp: ts,
		}
		if tc.CallID == "" {
			tc.CallID = synthID(vd.Tool, session, key)
		}
		if ok, known := v.boolOf(f.Success); known {
			tc.Success, tc.SuccessKnown = ok, true
		} else if len(f.Failure) > 0 {
			tc.Success, tc.SuccessKnown = !v.has(f.Failure), true
		}
		out.Tools = append(out.Tools, tc)
	case KindContent:
		content := v.str(f.Content)
		if content == "" && f.ContentFromBody {
			content = body
		}
		if content == "" || isPlaceholder(content, f.ContentPlaceholders) {
			return
		}
		out.Contents = append(out.Contents, Content{
			Tool: vd.Tool, SourceTag: vd.SourceTag, SessionID: session,
			RequestID: v.str(f.Request), ToolUseID: v.str(f.ToolUseID),
			Kind: f.ContentKind, Content: content, TimeUnixNano: tsNano,
		})
	}
}

// normalize converts raw input/output counts to the proxy-row convention.
// total is the vendor's own total (0 when absent), read only under
// Normalize.ReasoningByTotal.
func normalize(n Normalize, input, output, total int64, t Turn) (netInput, grossOutput int64) {
	netInput, grossOutput = input, output
	if n.InputIncludesCacheRead {
		netInput -= t.CacheReadTokens
	}
	if n.InputIncludesCacheWrite {
		netInput -= t.CacheWriteTokens
	}
	if netInput < 0 {
		netInput = 0
	}
	if outputExcludesReasoning(n, input, output, total, t.ReasoningTokens) {
		grossOutput += t.ReasoningTokens
	}
	return netInput, grossOutput
}

// outputExcludesReasoning resolves whether a record's output count leaves
// its reasoning out (so the gross output adds it back). Under
// ReasoningByTotal the record's own arithmetic decides when it can; the
// static OutputExcludesReasoning flag is the fallback.
func outputExcludesReasoning(n Normalize, input, output, total, reasoning int64) bool {
	if n.ReasoningByTotal && total > 0 && reasoning > 0 {
		switch total {
		case input + output + reasoning:
			return true
		case input + output:
			return false
		}
	}
	return n.OutputExcludesReasoning
}

// setSpanDuration fills a span-derived turn/tool's duration from the span's
// own start/end when no duration attribute was mapped.
func setSpanDuration(out *Result, k Kind, sp *tracepb.Span) {
	end, start := sp.GetEndTimeUnixNano(), sp.GetStartTimeUnixNano()
	if end <= start {
		return
	}
	ms := int64((end - start) / uint64(time.Millisecond))
	switch k {
	case KindTurn:
		if t := &out.Turns[len(out.Turns)-1]; t.DurationMS == 0 {
			t.DurationMS = ms
		}
	case KindTool:
		if tc := &out.Tools[len(out.Tools)-1]; tc.DurationMS == 0 {
			tc.DurationMS = ms
		}
	}
}

// logTime resolves a log record's timestamp: emit time, else observed time,
// else the event.timestamp attribute; zero when none is set.
func logTime(rec interface {
	GetTimeUnixNano() uint64
	GetObservedTimeUnixNano() uint64
	GetAttributes() []*commonpb.KeyValue
},
) time.Time {
	if ns := rec.GetTimeUnixNano(); ns != 0 {
		return time.Unix(0, int64(ns)).UTC()
	}
	if ns := rec.GetObservedTimeUnixNano(); ns != 0 {
		return time.Unix(0, int64(ns)).UTC()
	}
	for _, kv := range rec.GetAttributes() {
		if kv.GetKey() == "event.timestamp" {
			if t, ok := parseTimestamp(kv.GetValue().GetStringValue()); ok {
				return t
			}
		}
	}
	return time.Time{}
}

// spanTime is a span's end time (when the call's usage is known), else its
// start time.
func spanTime(sp *tracepb.Span) time.Time {
	if ns := sp.GetEndTimeUnixNano(); ns != 0 {
		return time.Unix(0, int64(ns)).UTC()
	}
	if ns := sp.GetStartTimeUnixNano(); ns != 0 {
		return time.Unix(0, int64(ns)).UTC()
	}
	return time.Time{}
}

// recordKey is the stable per-record discriminator synthesized ids are built
// from: the span id when there is one, else the record timestamp plus the
// record's ordinal inside its scope (Gemini stamps millisecond times, so two
// records can share one). A retried export resends the same batch, so the
// same record keeps the same key.
func recordKey(ts time.Time, spanID []byte, ordinal int) string {
	if len(spanID) > 0 {
		return hex.EncodeToString(spanID)
	}
	n := int64(0)
	if !ts.IsZero() {
		n = ts.UnixNano()
	}
	return strconv.FormatInt(n, 10) + "-" + strconv.Itoa(ordinal)
}

// synthID builds a deterministic id for a record the export gives no id
// for, so a redelivered record maps to the same row.
func synthID(tool, session, key string) string {
	return "otel:" + tool + ":" + session + ":" + key
}

func isPlaceholder(s string, placeholders []string) bool {
	for _, p := range placeholders {
		if s == p {
			return true
		}
	}
	return false
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
