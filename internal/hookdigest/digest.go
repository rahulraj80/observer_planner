package hookdigest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

// Scope is the value of a hook_checksums.json entry's "digest_scope" key
// when its "hooks_sha256" covers the hooks subtree only.
const Scope = "hooks"

// Entry keys in hook_checksums.json for a scoped (credential-file) entry.
const (
	KeyScope  = "digest_scope"
	KeyDigest = "hooks_sha256"
)

// ErrUnsupportedFormat is returned for a file whose extension is not JSON,
// YAML or TOML.
var ErrUnsupportedFormat = errors.New("hookdigest: unsupported file format")

// ErrUnparseable is returned when the body cannot be decoded. It never
// carries the decoder's own message, which can quote file contents.
var ErrUnparseable = errors.New("hookdigest: file cannot be parsed")

// HooksSubtree returns the hex SHA-256 of the canonical JSON encoding of the
// top-level "hooks" value of body, whose format is chosen by name's
// extension (.json, .yaml, .yml, .toml). An absent or empty document digests
// as JSON null. A TOML hooks value is an array of tables (Kimi Code's
// [[hooks]], T-378).
func HooksSubtree(name string, body []byte) (string, error) {
	var hooks any
	switch strings.ToLower(filepath.Ext(name)) {
	case ".json":
		if len(bytes.TrimSpace(body)) > 0 {
			var top map[string]json.RawMessage
			if json.Unmarshal(body, &top) != nil {
				return "", ErrUnparseable
			}
			if raw, ok := top["hooks"]; ok {
				dec := json.NewDecoder(bytes.NewReader(raw))
				dec.UseNumber()
				if dec.Decode(&hooks) != nil {
					return "", ErrUnparseable
				}
			}
		}
	case ".yaml", ".yml":
		if len(bytes.TrimSpace(body)) > 0 {
			var top map[string]any
			if yaml.Unmarshal(body, &top) != nil {
				return "", ErrUnparseable
			}
			hooks = top["hooks"]
		}
	case ".toml":
		if len(bytes.TrimSpace(body)) > 0 {
			top := map[string]any{}
			if _, err := toml.Decode(string(body), &top); err != nil {
				return "", ErrUnparseable
			}
			hooks = top["hooks"]
		}
	default:
		return "", ErrUnsupportedFormat
	}
	canon, err := json.Marshal(normalize(hooks))
	if err != nil {
		return "", ErrUnparseable
	}
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:]), nil
}

// normalize converts a decoded value into one json.Marshal accepts (a YAML
// mapping with non-string keys becomes a string-keyed map); json.Marshal
// then sorts the keys, which makes the encoding canonical.
func normalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			out[k] = normalize(x)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			ks, _ := k.(string)
			out[ks] = normalize(x)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = normalize(x)
		}
		return out
	default:
		return v
	}
}
