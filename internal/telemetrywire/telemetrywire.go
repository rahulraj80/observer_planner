package telemetrywire

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/marmutapp/superbased-observer/internal/integration"
)

// Result is one planned edit.
type Result struct {
	// Content is the file's new bytes; equal to the input when Changed is
	// false.
	Content []byte
	// Changed reports that Content differs from the input (the caller
	// writes only then, so an unchanged file is never rewritten).
	Changed bool
	// Conflict, when set, is why the edit was refused: the file already
	// carries an exporter setting the user configured. It names keys only,
	// never a value from the file.
	Conflict string
	// Summary lists what the edit does (or would do), for the preview.
	Summary []string
}

// Op is the edit direction.
type Op int

// Edit directions.
const (
	// OpApply points the exporter at the node.
	OpApply Op = iota + 1
	// OpRemove removes Observer's own exporter wiring.
	OpRemove
)

// Plan computes the edit of one config file. addr is the node receiver's
// loopback OTLP/HTTP address (host:port, e.g. 127.0.0.1:4318; see
// ReceiverAddr). content is the file's current bytes (empty when the file
// does not exist).
//
// owned is the exact endpoint value Observer recorded as installed in this
// file ("" when there is no record). OWNERSHIP IS THAT RECORD, never the
// shape of a value (P3-12): a loopback endpoint the user pointed at their own
// collector is never rewritten or removed. The Codex shape carries its own
// marked block and ignores owned.
func Plan(kind integration.OTLPWireKind, op Op, content []byte, addr, owned string) (Result, error) {
	if strings.TrimSpace(addr) == "" {
		return Result{}, fmt.Errorf("telemetrywire.Plan: no receiver address")
	}
	r, ok := kinds[kind]
	if !ok {
		return Result{}, fmt.Errorf("telemetrywire.Plan: no exporter writer for %q", kind)
	}
	var (
		res Result
		err error
	)
	switch op {
	case OpApply:
		res, err = r.apply(content, addr, owned)
	case OpRemove:
		res, err = r.remove(content, owned)
	default:
		return Result{}, fmt.Errorf("telemetrywire.Plan: unknown op %d", op)
	}
	if err != nil {
		return Result{}, err
	}
	if res.Content == nil {
		res.Content = content
	}
	res.Changed = res.Conflict == "" && !bytes.Equal(res.Content, content)
	if !res.Changed {
		res.Content = content
	}
	return res, nil
}

// Wired reports whether content carries Observer's exporter wiring, for the
// Connected tools status. owned is the recorded endpoint (see Plan).
func Wired(kind integration.OTLPWireKind, content []byte, owned string) bool {
	r, ok := kinds[kind]
	if !ok {
		return false
	}
	return r.wired(content, owned)
}

// Endpoint is the exact endpoint value Observer writes for addr in the JSON
// shapes, the value a caller records as owned after an apply.
func Endpoint(addr string) string { return "http://" + addr }

// ReceiverAddr resolves the address an exporter must send to from the node
// receiver's bind address (P3-12): ALWAYS a loopback host and the bind's
// port. An unspecified bind (0.0.0.0, ::, an empty host) or "localhost" is
// reached on 127.0.0.1; a loopback IP is kept. A specific non-loopback bind
// is refused: an exporter on this machine cannot rely on reaching it, and
// such a receiver requires its token anyway.
func ReceiverAddr(bind string) (string, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(bind))
	if err != nil {
		return "", fmt.Errorf("telemetrywire: receiver address %q is not host:port", bind)
	}
	if n, perr := strconv.Atoi(port); perr != nil || n <= 0 || n > 65535 {
		return "", fmt.Errorf("telemetrywire: receiver address %q has no fixed port", bind)
	}
	if host == "" || host == "localhost" {
		return net.JoinHostPort("127.0.0.1", port), nil
	}
	ip := net.ParseIP(host)
	switch {
	case ip == nil:
		return "", fmt.Errorf("telemetrywire: receiver host %q is not an IP address", host)
	case ip.IsUnspecified():
		return net.JoinHostPort("127.0.0.1", port), nil
	case ip.IsLoopback():
		return net.JoinHostPort(ip.String(), port), nil
	default:
		return "", fmt.Errorf("telemetrywire: the receiver is bound to a non-loopback address; exporter wiring points only at a loopback receiver")
	}
}

// kindRows is one writer shape.
type kindRows struct {
	apply  func(content []byte, addr, owned string) (Result, error)
	remove func(content []byte, owned string) (Result, error)
	wired  func(content []byte, owned string) bool
}

// kinds is the closed table of writer shapes, keyed by the registry's
// OTLPWireKind.
var kinds = map[integration.OTLPWireKind]kindRows{
	integration.OTLPWireEnvSettingsJSON: {apply: applyEnv, remove: removeEnv, wired: wiredEnv},
	integration.OTLPWireTelemetryJSON:   {apply: applyTelemetry, remove: removeTelemetry, wired: wiredTelemetry},
	integration.OTLPWireCodexTOML:       {apply: applyCodex, remove: removeCodex, wired: wiredCodex},
}

// ownedEndpoint reports whether an endpoint value in the file is the one
// Observer recorded installing. An empty record owns nothing.
func ownedEndpoint(got, owned string) bool {
	return owned != "" && got == owned
}

// applyEndpointOK reports whether apply may (re)write an endpoint currently
// set to got: it already is the wanted value, or it is Observer's recorded
// one (a receiver port change). Anything else is the user's.
func applyEndpointOK(got, want, owned string) bool {
	return got == want || ownedEndpoint(got, owned)
}

// ---- env block (Claude Code settings.json "env") ----

// envKeys are the four keys the env-block writer owns. The endpoint value is
// filled per call; the others are fixed.
const envEndpointKey = "OTEL_EXPORTER_OTLP_ENDPOINT"

func envWant(addr string) [][2]string {
	return [][2]string{
		{"CLAUDE_CODE_ENABLE_TELEMETRY", "1"},
		{"OTEL_LOGS_EXPORTER", "otlp"},
		{"OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf"},
		{envEndpointKey, Endpoint(addr)},
	}
}

// envApplyOK reports whether apply may set key, currently got.
func envApplyOK(key, got, owned string, want [2]string) bool {
	if key == envEndpointKey {
		return applyEndpointOK(got, want[1], owned)
	}
	return got == want[1]
}

// envRemoveOK reports whether remove may delete key, currently got: the
// endpoint only when it is the recorded one, a fixed key only at its fixed
// value.
func envRemoveOK(key, got, owned string, want [2]string) bool {
	if key == envEndpointKey {
		return ownedEndpoint(got, owned)
	}
	return got == want[1]
}

// envEndpointOwned reports whether env's endpoint is Observer's recorded one.
func envEndpointOwned(env object, owned string) bool {
	j := env.find(envEndpointKey)
	if j < 0 {
		return false
	}
	var got string
	return json.Unmarshal(env.members[j].raw, &got) == nil && ownedEndpoint(got, owned)
}

func docOrEmpty(content []byte) []byte {
	if len(bytes.TrimSpace(content)) == 0 {
		return []byte("{}\n")
	}
	return content
}

func applyEnv(content []byte, addr, owned string) (Result, error) {
	doc := docOrEmpty(content)
	top, err := parseObject(doc)
	if err != nil {
		return Result{}, fmt.Errorf("settings file is not a JSON object: %w", err)
	}
	nl, unit := newline(doc), indentUnit(doc, top)
	topIndent := memberIndent(doc, top, unit)
	want := envWant(addr)
	var env object
	envRaw := []byte("{}")
	if i := top.find("env"); i >= 0 {
		envRaw = top.members[i].raw
		if env, err = parseObject(envRaw); err != nil {
			return Result{Conflict: "the settings file's env block is not an object; it was left as it is"}, nil
		}
	}
	// Refuse to overwrite a key the user set to something else.
	var conflicts []string
	for _, w := range want {
		if j := env.find(w[0]); j >= 0 {
			var got string
			if json.Unmarshal(env.members[j].raw, &got) != nil || !envApplyOK(w[0], got, owned, w) {
				conflicts = append(conflicts, w[0])
			}
		}
	}
	if len(conflicts) > 0 {
		return Result{Conflict: "env already sets " + strings.Join(conflicts, ", ") + " to a value you configured; nothing was changed"}, nil
	}
	newEnv := envRaw
	envIndent := memberIndent(envRaw, env, topIndent+unit)
	var summary []string
	for _, w := range want {
		cur, perr := parseObject(newEnv)
		if perr != nil {
			return Result{}, perr
		}
		if j := cur.find(w[0]); j >= 0 {
			var got string
			_ = json.Unmarshal(cur.members[j].raw, &got)
			if got == w[1] {
				continue
			}
		}
		newEnv = setMember(newEnv, cur, w[0], quote(w[1]), envIndent, topIndent, nl)
		summary = append(summary, fmt.Sprintf("set env.%s = %s", w[0], w[1]))
	}
	if len(summary) == 0 {
		return Result{Content: content, Summary: []string{"already wired: env sends Claude Code's telemetry to " + "http://" + addr}}, nil
	}
	out := setMember(doc, top, "env", newEnv, topIndent, "", nl)
	return Result{Content: out, Summary: summary}, nil
}

func removeEnv(content []byte, owned string) (Result, error) {
	if len(bytes.TrimSpace(content)) == 0 {
		return Result{Summary: []string{"nothing to remove: no settings file"}}, nil
	}
	top, err := parseObject(content)
	if err != nil {
		return Result{}, fmt.Errorf("settings file is not a JSON object: %w", err)
	}
	i := top.find("env")
	if i < 0 {
		return Result{Summary: []string{"nothing to remove: no env block"}}, nil
	}
	envRaw := top.members[i].raw
	env, err := parseObject(envRaw)
	if err != nil {
		return Result{Summary: []string{"nothing to remove: the env block is not an object"}}, nil
	}
	if !envEndpointOwned(env, owned) {
		// Ownership is the recorded endpoint (P3-12): without it every key
		// here may be the user's own exporter, so none is touched.
		return Result{Content: content, Summary: []string{"nothing to remove: env does not carry the endpoint Observer installed"}}, nil
	}
	newEnv := envRaw
	var summary, kept []string
	removed := 0
	for _, w := range envWant("127.0.0.1:0") {
		cur, perr := parseObject(newEnv)
		if perr != nil {
			return Result{}, perr
		}
		j := cur.find(w[0])
		if j < 0 {
			continue
		}
		var got string
		if json.Unmarshal(cur.members[j].raw, &got) != nil || !envRemoveOK(w[0], got, owned, w) {
			kept = append(kept, w[0])
			continue
		}
		newEnv = removeMember(newEnv, cur, w[0])
		summary = append(summary, "remove env."+w[0])
		removed++
	}
	if len(kept) > 0 {
		summary = append(summary, "kept env "+strings.Join(kept, ", ")+" (set to a value Observer did not write)")
	}
	if removed == 0 {
		if len(summary) == 0 {
			summary = []string{"nothing to remove: no Observer telemetry keys in env"}
		}
		return Result{Content: content, Summary: summary}, nil
	}
	var out []byte
	if remaining, perr := parseObject(newEnv); perr == nil && len(remaining.members) == 0 {
		// An empty env is the same as none: drop the key rather than leave
		// "env": {} behind.
		out = removeMember(content, top, "env")
	} else {
		out = splice(content, top.members[i].valStart, top.members[i].valEnd, newEnv)
	}
	return Result{Content: out, Summary: summary}, nil
}

func wiredEnv(content []byte, owned string) bool {
	top, err := parseObject(docOrEmpty(content))
	if err != nil {
		return false
	}
	i := top.find("env")
	if i < 0 {
		return false
	}
	env, err := parseObject(top.members[i].raw)
	if err != nil {
		return false
	}
	for _, w := range envWant("127.0.0.1:0") {
		j := env.find(w[0])
		if j < 0 {
			return false
		}
		var got string
		if json.Unmarshal(env.members[j].raw, &got) != nil || !envRemoveOK(w[0], got, owned, w) {
			return false
		}
	}
	return true
}

// ---- telemetry object (Gemini CLI / Qwen Code settings.json) ----

// telemetryBlock is the exact telemetry object Observer writes: export to a
// local collector over OTLP/HTTP (JSON encoding, which the node accepts).
type telemetryBlock struct {
	Enabled      bool   `json:"enabled"`
	Target       string `json:"target"`
	OTLPEndpoint string `json:"otlpEndpoint"`
	OTLPProtocol string `json:"otlpProtocol"`
}

// shape reports whether a decoded telemetry object has exactly Observer's
// fixed fields (the endpoint is judged separately, by ownership).
func (t telemetryBlock) shape() bool {
	return t.Enabled && t.Target == "local" && t.OTLPProtocol == "http"
}

// ours reports whether a decoded telemetry object is Observer's: its shape
// and the endpoint Observer recorded installing.
func (t telemetryBlock) ours(owned string) bool {
	return t.shape() && ownedEndpoint(t.OTLPEndpoint, owned)
}

// decodeTelemetry decodes a telemetry value strictly: an extra key means the
// user customised it, so it is not Observer's.
func decodeTelemetry(raw []byte) (telemetryBlock, bool) {
	var t telemetryBlock
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		return t, false
	}
	return t, true
}

func applyTelemetry(content []byte, addr, owned string) (Result, error) {
	doc := docOrEmpty(content)
	top, err := parseObject(doc)
	if err != nil {
		return Result{}, fmt.Errorf("settings file is not a JSON object: %w", err)
	}
	want := telemetryBlock{Enabled: true, Target: "local", OTLPEndpoint: Endpoint(addr), OTLPProtocol: "http"}
	if i := top.find("telemetry"); i >= 0 {
		got, ok := decodeTelemetry(top.members[i].raw)
		if !ok || !got.shape() || !applyEndpointOK(got.OTLPEndpoint, want.OTLPEndpoint, owned) {
			return Result{Conflict: "the settings file already has a telemetry block you configured; nothing was changed"}, nil
		}
		if got == want {
			return Result{Content: content, Summary: []string{"already wired: telemetry exports to " + want.OTLPEndpoint}}, nil
		}
	}
	nl, unit := newline(doc), indentUnit(doc, top)
	topIndent := memberIndent(doc, top, unit)
	val, err := renderValue(want, topIndent, unit, nl)
	if err != nil {
		return Result{}, err
	}
	out := setMember(doc, top, "telemetry", val, topIndent, "", nl)
	return Result{Content: out, Summary: []string{"set telemetry = {enabled: true, target: local, otlpEndpoint: " + want.OTLPEndpoint + ", otlpProtocol: http}"}}, nil
}

func removeTelemetry(content []byte, owned string) (Result, error) {
	if len(bytes.TrimSpace(content)) == 0 {
		return Result{Summary: []string{"nothing to remove: no settings file"}}, nil
	}
	top, err := parseObject(content)
	if err != nil {
		return Result{}, fmt.Errorf("settings file is not a JSON object: %w", err)
	}
	i := top.find("telemetry")
	if i < 0 {
		return Result{Content: content, Summary: []string{"nothing to remove: no telemetry block"}}, nil
	}
	if got, ok := decodeTelemetry(top.members[i].raw); !ok || !got.ours(owned) {
		return Result{Content: content, Summary: []string{"kept the telemetry block: it is not the one Observer writes"}}, nil
	}
	return Result{Content: removeMember(content, top, "telemetry"), Summary: []string{"remove telemetry"}}, nil
}

func wiredTelemetry(content []byte, owned string) bool {
	top, err := parseObject(docOrEmpty(content))
	if err != nil {
		return false
	}
	i := top.find("telemetry")
	if i < 0 {
		return false
	}
	got, ok := decodeTelemetry(top.members[i].raw)
	return ok && got.ours(owned)
}

// ---- marked [otel] table (Codex config.toml) ----

// Codex block markers. The block is appended at the end of the file and is
// removed byte-exact (shellwrap's marked-block pattern).
const (
	codexBegin = "# >>> observer otlp export (observer init --otlp-export) >>>"
	codexEnd   = "# <<< observer otlp export <<<"
)

func codexBlock(addr, nl string) string {
	lines := []string{
		codexBegin,
		"[otel]",
		`exporter = { otlp-http = { endpoint = "http://` + addr + `/v1/logs", protocol = "binary" } }`,
		codexEnd,
	}
	return strings.Join(lines, nl) + nl
}

// findCodexBlock returns the byte span of Observer's marked block, from the
// start of its begin line to just past its end line's newline.
func findCodexBlock(content []byte) (start, end int, ok bool) {
	b := bytes.Index(content, []byte(codexBegin))
	if b < 0 {
		return 0, 0, false
	}
	e := bytes.Index(content[b:], []byte(codexEnd))
	if e < 0 {
		return 0, 0, false
	}
	end = b + e + len(codexEnd)
	switch {
	case bytes.HasPrefix(content[end:], []byte("\r\n")):
		end += 2
	case bytes.HasPrefix(content[end:], []byte("\n")):
		end++
	}
	return b, end, true
}

// userOTel reports whether the TOML outside Observer's block configures an
// otel table or key of its own.
func userOTel(content []byte) (bool, error) {
	outside := content
	if s, e, ok := findCodexBlock(content); ok {
		outside = splice(content, s, e, nil)
	}
	var doc map[string]any
	if err := toml.Unmarshal(outside, &doc); err != nil {
		return false, fmt.Errorf("config.toml does not parse as TOML: %w", err)
	}
	_, has := doc["otel"]
	return has, nil
}

func applyCodex(content []byte, addr, _ string) (Result, error) {
	user, err := userOTel(content)
	if err != nil {
		return Result{}, err
	}
	if user {
		return Result{Conflict: "config.toml already has an [otel] section you configured; nothing was changed"}, nil
	}
	nl := newline(content)
	block := codexBlock(addr, nl)
	if s, e, ok := findCodexBlock(content); ok {
		if string(content[s:e]) == block {
			return Result{Content: content, Summary: []string{"already wired: [otel] exports to http://" + addr + "/v1/logs"}}, nil
		}
		return Result{Content: splice(content, s, e, []byte(block)), Summary: []string{"update Observer's [otel] block to http://" + addr + "/v1/logs"}}, nil
	}
	out := append([]byte(nil), content...)
	if len(out) > 0 && !bytes.HasSuffix(out, []byte("\n")) {
		out = append(out, nl...)
	}
	if len(out) > 0 {
		out = append(out, nl...)
	}
	out = append(out, block...)
	return Result{Content: out, Summary: []string{"append an [otel] table exporting logs to http://" + addr + "/v1/logs (marked block)"}}, nil
}

func removeCodex(content []byte, _ string) (Result, error) {
	s, e, ok := findCodexBlock(content)
	if !ok {
		return Result{Content: content, Summary: []string{"nothing to remove: no Observer [otel] block"}}, nil
	}
	// Take the blank separator line the apply added, when it is there.
	nl := newline(content)
	if s >= 2*len(nl) && string(content[s-2*len(nl):s]) == nl+nl {
		s -= len(nl)
	}
	return Result{Content: splice(content, s, e, nil), Summary: []string{"remove Observer's [otel] block"}}, nil
}

func wiredCodex(content []byte, _ string) bool {
	_, _, ok := findCodexBlock(content)
	return ok
}
