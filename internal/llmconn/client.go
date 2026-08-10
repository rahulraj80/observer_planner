package llmconn

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Defaults for the shared client (mirrors the judge client's posture).
const (
	// defaultMaxTokens caps a single analysis reply (0 = omit the field and
	// use the provider default).
	defaultMaxTokens = 1024
	// defaultRetries is the number of ADDITIONAL attempts on a retryable
	// failure (transport error, request timeout, 5xx) beyond the first.
	defaultRetries = 1
	// retryBackoff is the pause between attempts (context-bounded).
	retryBackoff = 400 * time.Millisecond
	// defaultTimeout bounds a single call.
	defaultTimeout = 60 * time.Second
)

// Result is one completed analysis call: the reply plus the observed usage and
// latency so callers can record it to the analysis_calls accounting table.
type Result struct {
	Text         string `json:"text"`
	Model        string `json:"model"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	LatencyMS    int64  `json:"latency_ms"`
}

// Client calls one connection. Credentials are resolved by the caller (hv is
// the Authorization/x-api-key value sourced from the connection's env ref —
// never written to config or disk).
type Client struct {
	conn  Connection
	hv    string
	httpc *http.Client
	// scrubPrompt redacts secrets from the prompt BEFORE it is sent upstream.
	// Injected for a REMOTE connection so raw secrets never egress; nil for a
	// loopback connection (no egress, no overhead).
	scrubPrompt func(string) string
	// system is an optional system message prepended to the request (the judge
	// binding uses it to pin the JSON-verdict instruction). Omitted when empty.
	system string
	// maxPromptBytes caps the prompt sent upstream (0 = uncapped).
	maxPromptBytes int
	// maxTokens caps the reply length (0 = omit the field).
	maxTokens int
	// numCtx, when >0 AND the connection is local, is passed through as an
	// Ollama-style context-window hint. Never sent to a remote host.
	numCtx int
	// retries is the number of ADDITIONAL attempts on a retryable failure.
	retries int
}

// NewClient returns a client for conn with defaults. hv is the resolved
// credential (empty is allowed for loopback connections).
func NewClient(conn Connection, hv string) *Client {
	if conn.BaseURL == "" {
		switch conn.Kind {
		case KindAnthropic:
			conn.BaseURL = "https://api.anthropic.com"
		default:
			conn.BaseURL = "https://openrouter.ai/api/v1"
		}
	}
	if conn.Kind == "" {
		conn.Kind = KindOpenAICompat
	}
	return &Client{
		conn:      conn,
		hv:        hv,
		httpc:     &http.Client{Timeout: defaultTimeout},
		maxTokens: defaultMaxTokens,
		retries:   defaultRetries,
	}
}

// WithTuning overrides the reply cap and the local-only context-window hint;
// zero/negative leaves the constructor default untouched. Returns a copy.
func (c *Client) WithTuning(maxTokens, numCtx int) *Client {
	nc := *c
	if maxTokens > 0 {
		nc.maxTokens = maxTokens
	}
	if numCtx > 0 {
		nc.numCtx = numCtx
	}
	return &nc
}

// WithEgressScrub returns a copy that caps and secret-redacts the prompt
// before sending — applied for a REMOTE connection. scrub may be nil (cap
// only).
func (c *Client) WithEgressScrub(scrub func(string) string, maxBytes int) *Client {
	nc := *c
	nc.scrubPrompt = scrub
	nc.maxPromptBytes = maxBytes
	return &nc
}

// WithSystem returns a copy carrying a system message prepended to the
// request. Empty system resets it (the default). The judge binding pins its
// JSON-verdict instruction here.
func (c *Client) WithSystem(system string) *Client {
	nc := *c
	nc.system = system
	return &nc
}

// WithRetries overrides the retry count (0 = single attempt).
func (c *Client) WithRetries(n int) *Client {
	nc := *c
	nc.retries = n
	return &nc
}

// WithHTTPClient overrides the underlying http.Client (a test seam — the
// default is a 60s-timeout client). Returns a copy.
func (c *Client) WithHTTPClient(httpc *http.Client) *Client {
	nc := *c
	nc.httpc = httpc
	return &nc
}

// Connection returns the connection the client is bound to.
func (c *Client) Connection() Connection { return c.conn }

// Complete runs one analysis turn for model (defaulting to the connection's
// DefaultModel) with the given prompt, returning the reply, usage, and
// latency. A loopback connection needs no credential; a remote connection
// requires one. Egress guardrails (payload cap + secret redaction) apply only
// when WithEgressScrub was applied at construction.
func (c *Client) Complete(ctx context.Context, model, prompt string) (Result, error) {
	if model == "" {
		model = c.conn.DefaultModel
	}
	if model == "" {
		return Result{}, fmt.Errorf("llmconn: %s: no model (set default_model or pass one)", c.conn.ID)
	}
	if c.hv == "" && c.conn.Remote() {
		return Result{}, fmt.Errorf("llmconn: %s: remote connection needs a credential (env %s)", c.conn.ID, c.conn.APIKeyEnv)
	}
	p := prompt
	if c.maxPromptBytes > 0 && len(p) > c.maxPromptBytes {
		p = p[:c.maxPromptBytes] + "\n…[truncated]"
	}
	if c.scrubPrompt != nil {
		p = c.scrubPrompt(p)
	}

	start := time.Now()
	url, hdr, body, err := c.buildRequest(model, p)
	if err != nil {
		return Result{}, err
	}
	raw, status, err := c.request(ctx, http.MethodPost, url, hdr, body)
	if err != nil {
		return Result{}, err
	}
	if status != http.StatusOK {
		return Result{}, fmt.Errorf("llmconn: %s: upstream %d: %s", c.conn.ID, status, truncate(string(raw), 300))
	}
	res, err := c.parseResponse(model, raw)
	if err != nil {
		return Result{}, err
	}
	res.LatencyMS = time.Since(start).Milliseconds()
	return res, nil
}

// Test checks connectivity and lists available models. A 2xx from the models
// listing is success; latency is recorded. The discovered model list is
// returned so the caller can refresh the connection's Models.
func (c *Client) Test(ctx context.Context) (Health, []string) {
	start := time.Now()
	url := c.endpoint() + "/models"
	if c.conn.Kind == KindAnthropic {
		url = c.conn.BaseURL + "/v1/models"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Health{OK: false, Message: err.Error()}, nil
	}
	c.applyExtraHeaders(req.Header)
	c.applyAuth(req.Header)
	resp, err := c.httpc.Do(req)
	if err != nil {
		return Health{OK: false, Message: err.Error(), LatencyMS: time.Since(start).Milliseconds()}, nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
	h := Health{OK: resp.StatusCode >= 200 && resp.StatusCode < 300, LatencyMS: time.Since(start).Milliseconds()}
	if !h.OK {
		h.Message = fmt.Sprintf("upstream %d: %s", resp.StatusCode, truncate(string(body), 300))
		return h, nil
	}
	var models []string
	var listing struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &listing); err == nil {
		for _, m := range listing.Data {
			if m.ID != "" {
				models = append(models, m.ID)
			}
		}
	}
	h.Message = fmt.Sprintf("ok (%d models)", len(models))
	return h, models
}

func (c *Client) endpoint() string {
	if c.conn.Kind == KindAnthropic {
		return strings.TrimRight(c.conn.BaseURL, "/") + "/v1"
	}
	return strings.TrimRight(c.conn.BaseURL, "/")
}

func (c *Client) applyAuth(hdr http.Header) {
	if c.hv == "" {
		return
	}
	if c.conn.Kind == KindAnthropic {
		hdr.Set("x-api-key", c.hv)
		hdr.Set("anthropic-version", "2023-06-01")
	} else {
		hdr.Set("Authorization", "Bearer "+c.hv)
	}
}

// applyExtraHeaders merges the connection's static Headers into hdr — but
// ONLY into fields the standard request did not already set (Content-Type,
// attribution, and the dedicated credential via applyAuth all win). Header
// names/values were validated (CR/LF-free) at config load.
func (c *Client) applyExtraHeaders(hdr http.Header) {
	for k, v := range c.conn.Headers {
		if hdr.Get(k) == "" {
			hdr.Set(k, v)
		}
	}
}

func (c *Client) buildRequest(model, prompt string) (string, http.Header, []byte, error) {
	url := c.endpoint()
	hdr := http.Header{}
	hdr.Set("Content-Type", "application/json")
	// Attribution headers (spec §3.3): identify the caller to the upstream so
	// analysis traffic is distinguishable from tool traffic on the provider's
	// dashboard (mirrors the judge client's X-SBO-Eval / X-Title posture).
	hdr.Set("X-SBO-Analysis", "1")
	if c.conn.Kind != KindAnthropic {
		hdr.Set("X-Title", "superbased-observer analysis")
	}
	// Extra connection headers fill everything the caller did not already set
	// (Set keeps the existing value) — so the Content-Type / attribution
	// defaults above always win, and applyAuth below wins for the standard
	// Authorization / x-api-key fields.
	c.applyExtraHeaders(hdr)
	c.applyAuth(hdr)
	if c.conn.Kind == KindAnthropic {
		url += "/messages"
		body := map[string]any{
			"model":       model,
			"max_tokens":  c.maxTokens,
			"temperature": 0,
			"messages":    []map[string]string{{"role": "user", "content": prompt}},
		}
		if c.maxTokens <= 0 {
			delete(body, "max_tokens")
		}
		if c.system != "" {
			body["system"] = c.system
		}
		raw, err := json.Marshal(body)
		return url, hdr, raw, err
	}
	url += "/chat/completions"
	messages := []map[string]string{}
	if c.system != "" {
		messages = append(messages, map[string]string{"role": "system", "content": c.system})
	}
	messages = append(messages, map[string]string{"role": "user", "content": prompt})
	body := map[string]any{
		"model":       model,
		"temperature": 0,
		"messages":    messages,
	}
	if c.maxTokens > 0 {
		body["max_tokens"] = c.maxTokens
	}
	if c.conn.Local() && c.numCtx > 0 {
		body["num_ctx"] = c.numCtx
	}
	raw, err := json.Marshal(body)
	return url, hdr, raw, err
}

func (c *Client) parseResponse(model string, raw []byte) (Result, error) {
	res := Result{Model: model}
	if c.conn.Kind == KindAnthropic {
		var parsed struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return res, fmt.Errorf("llmconn: %s: decode response: %w", c.conn.ID, err)
		}
		for _, block := range parsed.Content {
			if block.Type == "text" {
				res.Text += block.Text
			}
		}
		res.InputTokens, res.OutputTokens = parsed.Usage.InputTokens, parsed.Usage.OutputTokens
		return res, nil
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return res, fmt.Errorf("llmconn: %s: decode response: %w", c.conn.ID, err)
	}
	if len(parsed.Choices) == 0 {
		return res, fmt.Errorf("llmconn: %s: empty choices in response", c.conn.ID)
	}
	res.Text = parsed.Choices[0].Message.Content
	res.InputTokens, res.OutputTokens = parsed.Usage.PromptTokens, parsed.Usage.CompletionTokens
	return res, nil
}

// request posts raw, retrying on a retryable failure (transport error, request
// timeout, 5xx) up to c.retries additional attempts with a short backoff. A
// 4xx is returned immediately. The context bounds every attempt and each
// backoff sleep.
func (c *Client) request(ctx context.Context, method, url string, hdr http.Header, raw []byte) ([]byte, int, error) {
	attempts := c.retries + 1
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, 0, fmt.Errorf("llmconn: %s: request: %w", c.conn.ID, ctx.Err())
			case <-time.After(retryBackoff):
			}
		}
		req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(raw))
		if err != nil {
			return nil, 0, fmt.Errorf("llmconn: %s: new request: %w", c.conn.ID, err)
		}
		req.Header = hdr.Clone()
		resp, err := c.httpc.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("llmconn: %s: request: %w", c.conn.ID, err)
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if resp.StatusCode >= 500 && attempt < attempts-1 {
			lastErr = fmt.Errorf("llmconn: %s: upstream %d: %s", c.conn.ID, resp.StatusCode, truncate(string(body), 300))
			continue
		}
		return body, resp.StatusCode, nil
	}
	return nil, 0, lastErr
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
