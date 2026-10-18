package telemetry

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/marmutapp/superbased-observer/internal/telemetry/contract"
)

// httpClient is the package-default transport client; tests inject their
// own via Transport.Client.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// Transport posts envelopes to the central ingest. Endpoint comes from the
// resolved config; token from the install-token file.
type Transport struct {
	// Endpoint is the ingest base URL (no trailing slash).
	Endpoint string
	// Token is the bearer install token ("" only before registration).
	Token string
	// Client overrides the HTTP client (tests).
	Client *http.Client
}

// IngestResponse is the POST /v1/ingest body (plan §D3 / server Accepted).
type IngestResponse struct {
	Accepted   int   `json:"accepted"`
	LastSeq    int64 `json:"last_seq"`
	Duplicate  bool  `json:"duplicate"`
	RetryAfter int   `json:"retry_after,omitempty"`
}

// Send builds the gzip JSON body for one envelope and posts it. It returns
// the server's ack so the caller can advance its watermark, and maps HTTP
// failure modes onto the closed error vocabulary (plan §D3).
func (t *Transport) Send(ctx context.Context, env *contract.Envelope) (*IngestResponse, error) {
	wire, err := EncodeEnvelope(env)
	if err != nil {
		return nil, fmt.Errorf("telemetry.Send: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.Endpoint+"/v1/ingest", bytes.NewReader(wire))
	if err != nil {
		return nil, fmt.Errorf("telemetry.Send: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	if t.Token != "" {
		req.Header.Set("Authorization", "Bearer "+t.Token)
	}
	client := t.Client
	if client == nil {
		client = httpClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("telemetry.Send: post: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("telemetry.Send: body: %w", err)
	}
	switch {
	case resp.StatusCode == http.StatusOK:
		var acc IngestResponse
		if err := json.Unmarshal(body, &acc); err != nil {
			return nil, fmt.Errorf("telemetry.Send: ack decode: %w", err)
		}
		return &acc, nil
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, fmt.Errorf("telemetry.Send: %w (http 429)", contract.ErrRateLimited)
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, fmt.Errorf("telemetry.Send: %w (http 401)", contract.ErrInstallRevoked)
	case resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("telemetry.Send: %w (http 403)", contract.ErrConsentMissing)
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		// Schema rejections (bad_json, bad_schema) are permanent for this
		// batch — the caller drops it rather than retrying forever.
		return nil, fmt.Errorf("telemetry.Send: %w (http %d: %s)", contract.ErrBadSchema, resp.StatusCode, truncateErr(body))
	default:
		return nil, fmt.Errorf("telemetry.Send: server error http %d: %s", resp.StatusCode, truncateErr(body))
	}
}

// EncodeEnvelope is the wire body for one envelope: JSON, gzip-compressed,
// refused when the uncompressed JSON exceeds the contract cap. Every transport
// (reporterd, the cloud telemetry lane) sends exactly these bytes.
func EncodeEnvelope(env *contract.Envelope) ([]byte, error) {
	raw, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	if len(raw) > contract.MaxUncompressedBytes {
		return nil, fmt.Errorf("%w: %d bytes", contract.ErrRateLimited, len(raw))
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("gzip close: %w", err)
	}
	return buf.Bytes(), nil
}

// truncateErr bounds a server error body for the local log only (never
// uploaded anywhere).
func truncateErr(b []byte) string {
	if len(b) > 200 {
		return string(b[:200]) + "…"
	}
	return string(b)
}

// BuildEnvelope assembles one contract envelope from the staged rows above
// the DURABLE send watermark (last_ack_seq, read here — never passed in, so
// a caller cannot accidentally re-select acked rows), cut so the COMPRESSED
// body stays under MaxBatchBytes (plan §D3/A3). Rows that do not fit stay
// staged for the next cycle. Returns nil when there is nothing to send.
func (c *Collector) BuildEnvelope(ctx context.Context) (*contract.Envelope, int64, error) {
	iid, err := c.InstallID(ctx)
	if err != nil {
		return nil, 0, err
	}
	lastAck, err := c.LastAckSeq(ctx)
	if err != nil {
		return nil, 0, err
	}
	rows, err := c.db.QueryContext(ctx, `
		SELECT seq, event, payload, created_at
		FROM telemetry_staged
		WHERE install_id = ? AND seq > ?
		ORDER BY seq`, iid, lastAck)
	if err != nil {
		return nil, 0, fmt.Errorf("telemetry.BuildEnvelope: list: %w", err)
	}
	defer rows.Close()
	var events []contract.Event
	var maxSeq int64
	for rows.Next() {
		var seq int64
		var event, payload, created string
		if err := rows.Scan(&seq, &event, &payload, &created); err != nil {
			return nil, 0, fmt.Errorf("telemetry.BuildEnvelope: scan: %w", err)
		}
		var fields map[string]any
		if err := json.Unmarshal([]byte(payload), &fields); err != nil {
			// A malformed staged row (bug, not data) is dropped honestly:
			// counted into dropped_count, never uploaded.
			if _, uerr := c.db.ExecContext(ctx,
				`DELETE FROM telemetry_staged WHERE seq = ?`, seq); uerr == nil {
				_, _ = c.db.ExecContext(ctx,
					`UPDATE telemetry_state SET dropped_count = dropped_count + 1 WHERE id = 1`)
			}
			continue
		}
		events = append(events, contract.Event{
			Event: event, EventVersion: 1,
			SchemaVersion:  SchemaVersion,
			TS:             created,
			InstallID:      iid,
			Seq:            seq,
			Classification: classificationFor(event),
			Purpose:        purposeFor(event),
			Fields:         fields,
		})
		maxSeq = seq
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("telemetry.BuildEnvelope: rows: %w", err)
	}
	if len(events) == 0 {
		return nil, lastAck, nil
	}

	// Byte cap: cut the batch so the compressed body stays under
	// MaxBatchBytes. Gzip of JSON repeats structure well; the estimate
	// below compresses incrementally rather than guessing a ratio.
	env := &contract.Envelope{
		EnvelopeVersion: contract.EnvelopeVersion,
		BatchUUID:       c.newUUID(),
		InstallID:       iid,
		SeqStart:        events[0].Seq,
		SeqEnd:          maxSeq,
		ClientVersion:   c.cfg.ClientVersion,
		OS:              runtimeOS(),
		Arch:            runtimeArch(),
		EventCount:      len(events),
		ClientTS:        c.now().UTC().Format(time.RFC3339),
		Events:          events,
	}
	for {
		raw, gz, err := measureLen(env)
		if err != nil {
			return nil, 0, err
		}
		if (gz <= c.cfg.MaxBatchBytes && raw <= maxUncompressedSend) || len(env.Events) <= 1 {
			// At or under both caps — or a single event already exceeds
			// one of them (oversized singleton; send anyway — the server
			// rejects it and the loop drops the batch honestly).
			return env, maxSeq, nil
		}
		// Drop the newest half and re-cut (drop-newest, keep order).
		keep := len(env.Events) / 2
		env.Events = env.Events[:keep]
		env.EventCount = keep
		env.SeqEnd = env.Events[keep-1].Seq
		maxSeq = env.SeqEnd
	}
}

// maxUncompressedSend is the client-side cap on the RAW (pre-gzip) wire
// size: half the server's contract.MaxUncompressedBytes, leaving headroom
// for envelope-key growth between contract versions. The raw cap matters as
// much as the compressed one: the server bounds the ingest body AFTER
// gunzipping, and repetitive telemetry JSON compresses ~10x, so a batch
// that fits the 256 KB compressed cap can decompress past 1 MiB and be
// rejected as bad_schema (found in the W2 cross-repo wire review).
const maxUncompressedSend = contract.MaxUncompressedBytes / 2

// measureLen returns the envelope's RAW (uncompressed) wire length and its
// gzip-compressed length — the cut loop caps BOTH.
func measureLen(env *contract.Envelope) (rawLen, gzLen int, err error) {
	raw, err := json.Marshal(env)
	if err != nil {
		return 0, 0, fmt.Errorf("telemetry.measureLen: %w", err)
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		return 0, 0, fmt.Errorf("telemetry.measureLen: write: %w", err)
	}
	if err := zw.Close(); err != nil {
		return 0, 0, fmt.Errorf("telemetry.measureLen: close: %w", err)
	}
	return len(raw), buf.Len(), nil
}

// LastAckSeq reads the durable send watermark: the highest seq the server
// has acked. BuildEnvelope excludes staged rows at or below it, so each
// event is uploaded at most once; acked rows stay staged for the derive
// dedup window (idempotent re-derives) and age out via the bounds
// enforcement, never re-sent.
func (c *Collector) LastAckSeq(ctx context.Context) (int64, error) {
	var acked int64
	if err := c.db.QueryRowContext(ctx,
		`SELECT COALESCE(last_ack_seq, 0) FROM telemetry_state WHERE id = 1`).Scan(&acked); err != nil {
		return 0, fmt.Errorf("telemetry.LastAckSeq: %w", err)
	}
	return acked, nil
}

// Ack advances the watermark after a 200 (plan §D3: the client stores
// last_seq; the server dedupes on batch_uuid so a crash between send and
// ack replays the batch idempotently). Acked rows are NOT deleted: the
// derive dedup map reads staged rows within the lookback window, and the
// bounds enforcement evicts them honestly once they age out.
func (c *Collector) Ack(ctx context.Context, lastSeq int64) error {
	if _, err := c.db.ExecContext(ctx,
		`UPDATE telemetry_state SET last_ack_seq = MAX(last_ack_seq, ?), last_batch_at = ? WHERE id = 1`,
		lastSeq, c.now().UTC().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("telemetry.Ack: %w", err)
	}
	return nil
}

// classificationFor returns the contract's classification for an event
// name ("" for unknown — BuildEnvelope never emits unknown names because
// Derive only stages known ones).
func classificationFor(event string) string {
	if pair, ok := contractPairs[event]; ok {
		return pair[0]
	}
	return ""
}

// purposeFor returns the contract's purpose for an event name.
func purposeFor(event string) string {
	if pair, ok := contractPairs[event]; ok {
		return pair[1]
	}
	return ""
}
