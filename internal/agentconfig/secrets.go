package agentconfig

import (
	"errors"
	"sort"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/cfgfmt"
)

// Secret handling for the editor (phases P4 / P5, plan section 5.3): a
// credential in a vendor file is shown as PRESENT or ABSENT, never by
// value, and never appears in a diff, a log or an API response. Detection
// is injected (the boundary wires internal/scrub); this file owns the
// key-name heuristic, the masking of a parsed document and the restore of
// masked values on a raw save.

// SecretDetector reports whether a string value is credential-shaped (the
// boundary wires internal/scrub's pattern-certain detectors). A nil
// detector detects nothing beyond the catalogue's Secret rows and the
// key-name heuristic.
type SecretDetector func(value string) bool

// SecretPlaceholder replaces a hidden value in every masked rendering. A
// raw save that sends it back unchanged keeps the value on disk; a raw save
// that moves or edits it is refused (ErrSecretPlaceholder).
const SecretPlaceholder = "__observer_hidden_secret__" //nolint:gosec // a placeholder marker, never a credential

// ErrSecretPlaceholder: a raw save carries the placeholder somewhere other
// than where Observer put it, so Observer cannot tell which value it
// stands for.
var ErrSecretPlaceholder = errors.New("agentconfig: the hidden-secret placeholder was moved or edited; " +
	"edit that value in the tool's own file, or remove the placeholder")

// secretKeyFragments are key-name fragments (lower case, separators
// dropped) that make a non-empty string value a credential regardless of
// its shape: the env-block case the node 1 grounding found (qwen-code env,
// Prime Agent models.json apiKey).
var secretKeyFragments = []string{
	"apikey", "accesskey", "secretkey", "privatekey", "clientsecret", "secret",
	"token", "password", "passwd", "credential", "authorization", "bearer",
}

// secretKeyExempt are fragments that contain a secret fragment but name a
// non-secret (a token COUNT, a token LIMIT, the env-var NAME of a key).
var secretKeyExempt = []string{
	"maxtoken", "tokens", "tokenlimit", "tokencount", "tokenbudget", "outputtoken", "inputtoken",
	"thinkingtoken", "contexttoken", "keyenv", "envkey", "apikeyenv", "tokenenv", "helper", "file", "path",
	"command", "cmd", "url", "endpoint",
}

// SecretKeyName reports whether a key name says its value is a credential.
func SecretKeyName(key string) bool {
	k := strings.ToLower(key)
	k = strings.NewReplacer("_", "", "-", "", ".", "", " ", "").Replace(k)
	for _, x := range secretKeyExempt {
		if strings.Contains(k, x) {
			return false
		}
	}
	for _, f := range secretKeyFragments {
		if strings.Contains(k, f) {
			return true
		}
	}
	return false
}

// isEnvReference reports a value that only REFERENCES a credential ("${VAR}",
// "$VAR", "env:VAR", "{env:VAR}"): the recommended alternative, never a
// secret itself.
func isEnvReference(v string) bool {
	s := strings.TrimSpace(v)
	switch {
	case s == "":
		return true
	case strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") && !strings.Contains(s[2:len(s)-1], " "):
		return true
	case strings.HasPrefix(s, "{env:") && strings.HasSuffix(s, "}"):
		return true
	case strings.HasPrefix(s, "env:"):
		return true
	case strings.HasPrefix(s, "$") && !strings.ContainsAny(s[1:], " /:"):
		return true
	}
	return s == SecretPlaceholder
}

// secretJudge decides which values of one file shape are secrets.
type secretJudge struct {
	rows   map[string]SettingRow // dotted path -> row, for the shape
	detect SecretDetector
}

func newSecretJudge(shape string, detect SecretDetector) secretJudge {
	j := secretJudge{rows: map[string]SettingRow{}, detect: detect}
	for _, r := range shapeRows(shape) {
		j.rows[r.Path.String()] = r
	}
	return j
}

// shapeRows returns the catalogue rows of one file shape.
func shapeRows(shape string) []SettingRow {
	catalogOnce.Do(build)
	var out []SettingRow
	for _, r := range catalogRows {
		if r.FileID == shape {
			out = append(out, r)
		}
	}
	return out
}

// wholeSecret reports a path whose entire value is a credential: a
// catalogue Secret row, or a string under a credential-named key.
func (j secretJudge) wholeSecret(p KeyPath, v any) bool {
	if r, ok := j.rows[p.String()]; ok && r.Secret {
		return !isEmptyValue(v)
	}
	s, ok := v.(string)
	if !ok || isEnvReference(s) {
		return false
	}
	if len(p) > 0 && SecretKeyName(p[len(p)-1]) {
		return true
	}
	return j.detect != nil && j.detect(s)
}

// containsSecret reports a non-map value (an array, a scalar) holding a
// detected secret string anywhere inside.
func (j secretJudge) containsSecret(v any) bool {
	switch x := v.(type) {
	case string:
		return !isEnvReference(x) && j.detect != nil && j.detect(x)
	case []any:
		for _, e := range x {
			if j.containsSecret(e) {
				return true
			}
		}
	case map[string]any:
		for k, e := range x {
			if s, ok := e.(string); ok && SecretKeyName(k) && !isEnvReference(s) {
				return true
			}
			if j.containsSecret(e) {
				return true
			}
		}
	}
	return false
}

// maskValue replaces every secret string inside v with the placeholder.
func (j secretJudge) maskValue(v any) any {
	switch x := v.(type) {
	case string:
		if !isEnvReference(x) && j.detect != nil && j.detect(x) {
			return SecretPlaceholder
		}
		return x
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = j.maskValue(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			if s, ok := e.(string); ok && SecretKeyName(k) && !isEnvReference(s) {
				out[k] = SecretPlaceholder
				continue
			}
			out[k] = j.maskValue(e)
		}
		return out
	}
	return v
}

// maskedPath is one masked value of a document.
type maskedPath struct {
	Path     KeyPath
	Original any
	Masked   any
}

// findSecrets walks a decoded document and returns the values to mask, at
// the shallowest map-addressable path (cfgfmt cannot address array
// elements, so a secret inside an array masks the array value).
func (j secretJudge) findSecrets(tree map[string]any) []maskedPath {
	var out []maskedPath
	var walk func(prefix KeyPath, m map[string]any)
	walk = func(prefix KeyPath, m map[string]any) {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := m[k]
			p := append(append(KeyPath(nil), prefix...), k)
			if j.wholeSecret(p, v) {
				out = append(out, maskedPath{Path: p, Original: v, Masked: SecretPlaceholder})
				continue
			}
			if sub, ok := v.(map[string]any); ok {
				// A catalogue Secret row that is an object (rare) masks whole.
				walk(p, sub)
				continue
			}
			if j.containsSecret(v) {
				out = append(out, maskedPath{Path: p, Original: v, Masked: j.maskValue(v)})
			}
		}
	}
	walk(nil, tree)
	return out
}

// MaskedDoc is a document rendered with its secrets hidden.
type MaskedDoc struct {
	Bytes []byte
	// Paths are the masked values' paths, dotted (presence only).
	Paths []string
	masks []maskedPath
}

// textMasker masks secret-shaped substrings of free text (Markdown).
type textMasker func(string) (masked string, found bool)

// maskDocument renders bytes of format f with every secret replaced. A
// file that does not parse is returned with its text masked line by line
// (detected substrings only) so a raw view of a broken file still never
// shows a credential.
func maskDocument(f cfgfmt.Format, shape string, b []byte, detect SecretDetector, maskText textMasker) (MaskedDoc, error) {
	if f == cfgfmt.FormatMarkdown || f == cfgfmt.FormatMDC {
		return maskMarkdown(b, maskText), nil
	}
	doc, err := cfgfmt.Parse(f, b)
	if err != nil {
		return maskMarkdown(b, maskText), err
	}
	tree, err := doc.Value()
	if err != nil {
		return maskMarkdown(b, maskText), err
	}
	j := newSecretJudge(shape, detect)
	masks := j.findSecrets(tree)
	md := MaskedDoc{masks: masks}
	if len(masks) == 0 {
		md.Bytes = append([]byte(nil), b...)
		return md, nil
	}
	for _, m := range masks {
		if err := doc.Set(cfgfmt.Path(m.Path), m.Masked); err != nil {
			// Never fall back to showing the value: mask the whole text.
			return maskAllText(b), nil
		}
		md.Paths = append(md.Paths, m.Path.String())
	}
	md.Bytes = doc.Bytes()
	return md, nil
}

// maskMarkdown masks detected substrings of free text.
func maskMarkdown(b []byte, maskText textMasker) MaskedDoc {
	if maskText == nil {
		return MaskedDoc{Bytes: append([]byte(nil), b...)}
	}
	s, found := maskText(string(b))
	md := MaskedDoc{Bytes: []byte(s)}
	if found {
		md.Paths = []string{"(text)"}
	}
	return md
}

// maskAllText is the last-resort rendering: nothing of the file is shown.
func maskAllText(b []byte) MaskedDoc {
	return MaskedDoc{Bytes: []byte("# " + SecretPlaceholder + " (this file holds a value Observer cannot hide safely; open it in the tool's own editor)\n"), Paths: []string{"(file)"}}
}

// restoreSecrets puts the original value back wherever a raw save sent the
// masked value unchanged, and refuses a placeholder anywhere else.
func restoreSecrets(orig MaskedDoc, incoming cfgfmt.Doc) error {
	for _, m := range orig.masks {
		v, found, err := incoming.Get(cfgfmt.Path(m.Path))
		if err != nil || !found {
			continue
		}
		if cfgfmt.Equal(v, m.Masked) {
			if err := incoming.Set(cfgfmt.Path(m.Path), m.Original); err != nil {
				return err
			}
		}
	}
	tree, err := incoming.Value()
	if err != nil {
		return err
	}
	if containsPlaceholder(tree) {
		return ErrSecretPlaceholder
	}
	return nil
}

func containsPlaceholder(v any) bool {
	switch x := v.(type) {
	case string:
		return strings.Contains(x, SecretPlaceholder)
	case []any:
		for _, e := range x {
			if containsPlaceholder(e) {
				return true
			}
		}
	case map[string]any:
		for k, e := range x {
			if strings.Contains(k, SecretPlaceholder) || containsPlaceholder(e) {
				return true
			}
		}
	}
	return false
}

func isEmptyValue(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}
