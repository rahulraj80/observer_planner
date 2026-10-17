package managedsettings

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/marmutapp/superbased-observer/internal/agentconfig"
)

// The device-management (MDM) encodings of a managed layer: an Apple
// configuration profile (a .mobileconfig carrying managed preferences) and a
// Windows registry import file (.reg under HKLM\SOFTWARE\Policies). Both are
// rendered from the SAME placed values the plain file artifact carries, so
// every channel of one layer says the same thing. Each vendor reads its
// preferences in its own convention; the mode names it.

// ProfileMode is how a managed-preferences domain carries the values.
type ProfileMode string

// The closed ProfileMode vocabulary.
const (
	// ProfileKeysNative: each top-level key of the document is one
	// preference key; nested objects and lists stay native plist dicts and
	// arrays (Claude Code com.anthropic.claudecode, OpenCode / Kilo
	// ai.opencode.managed).
	ProfileKeysNative ProfileMode = "keys_native"
	// ProfileKeysScalarJSON: each top-level key is one preference key; a
	// bool, number or string stays native, an object or a list becomes its
	// JSON text (the VS Code policy convention).
	ProfileKeysScalarJSON ProfileMode = "keys_scalar_json"
	// ProfileKeysStrings: each top-level key is one preference key and every
	// value is a string: "true" / "false", a decimal number, an object or a
	// list as JSON text (Claude Desktop com.anthropic.claudefordesktop).
	ProfileKeysStrings ProfileMode = "keys_strings"
	// ProfileDottedStrings: each placed setting is one preference key named
	// by its dotted key path, its value a string as in ProfileKeysStrings
	// (GitHub Copilot com.github.copilot).
	ProfileDottedStrings ProfileMode = "dotted_strings"
	// ProfileBase64File: one preference key holds the base64 of the whole
	// rendered file (Codex config_toml_base64 / requirements_toml_base64).
	ProfileBase64File ProfileMode = "base64_file"
)

// RegistryMode is how a Windows policy key carries the values.
type RegistryMode string

// The closed RegistryMode vocabulary.
const (
	// RegistryValues: each top-level key is one registry value; a bool or a
	// whole number is a REG_DWORD (bool as 0 / 1), a string a REG_SZ, an
	// object or a list a REG_SZ holding its JSON text (the ADMX convention).
	RegistryValues RegistryMode = "values"
	// RegistryJSONValue: one REG_SZ value holds the whole document as JSON
	// (Claude Code's "Settings").
	RegistryJSONValue RegistryMode = "json_value"
	// RegistryStrings: each top-level key is one REG_SZ value holding the
	// string form ProfileKeysStrings uses (Claude Desktop).
	RegistryStrings RegistryMode = "strings"
	// RegistryDottedStrings: each placed setting is one REG_SZ value named by
	// its dotted key path (GitHub Copilot).
	RegistryDottedStrings RegistryMode = "dotted_strings"
)

// ProfileChannel is an Apple managed-preferences delivery of a layer.
type ProfileChannel struct {
	// Domain is the preference domain (PayloadType of the inner payload).
	Domain string
	Mode   ProfileMode
	// Key is the single preference key for ProfileBase64File.
	Key string
}

// RegistryChannel is a Windows policy-registry delivery of a layer.
type RegistryChannel struct {
	// Key is the full key path under HKEY_LOCAL_MACHINE, without the hive
	// ("SOFTWARE\\Policies\\ClaudeCode").
	Key  string
	Mode RegistryMode
	// Value is the single value name for RegistryJSONValue.
	Value string
}

// entry is one placed value at its key path in a layer.
type entry struct {
	Key   agentconfig.KeyPath
	Value any
}

// stableUUID derives a deterministic RFC 4122 shaped id from seed, so a
// regenerated profile for the same policy is byte-identical (an MDM then
// sees no change) while a different policy gets a different id.
func stableUUID(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	b := sum[:16]
	b[6] = (b[6] & 0x0f) | 0x50 // name-based version nibble
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%X-%X-%X-%X-%X", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// stringForm is the all-strings encoding: a string as is, a bool as
// "true" / "false", a number in decimal, an object or a list as JSON text.
func stringForm(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case bool:
		return strconv.FormatBool(x), nil
	case int64:
		return strconv.FormatInt(x, 10), nil
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64), nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func dotted(k agentconfig.KeyPath) string { return strings.Join(k, ".") }

// prefsFor maps a layer's values onto preference keys for a mode.
func prefsFor(mode ProfileMode, key string, doc map[string]any, entries []entry, raw []byte) (map[string]any, error) {
	prefs := map[string]any{}
	switch mode {
	case ProfileKeysNative:
		for k, v := range doc {
			prefs[k] = v
		}
	case ProfileKeysScalarJSON:
		for k, v := range doc {
			switch v.(type) {
			case map[string]any, []any:
				b, err := json.Marshal(v)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", k, err)
				}
				prefs[k] = string(b)
			default:
				prefs[k] = v
			}
		}
	case ProfileKeysStrings:
		for k, v := range doc {
			s, err := stringForm(v)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			prefs[k] = s
		}
	case ProfileDottedStrings:
		for _, e := range entries {
			s, err := stringForm(e.Value)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", dotted(e.Key), err)
			}
			prefs[dotted(e.Key)] = s
		}
	case ProfileBase64File:
		if key == "" {
			return nil, fmt.Errorf("base64 mode needs a key")
		}
		prefs[key] = base64Std(raw)
	default:
		return nil, fmt.Errorf("unknown mode %q", mode)
	}
	return prefs, nil
}

// renderProfile renders a .mobileconfig carrying the layer's values under one
// managed-preferences payload. identifier is the reverse-DNS profile id.
func renderProfile(ch ProfileChannel, identifier, displayName, description string, doc map[string]any, entries []entry, raw []byte) (string, error) {
	prefs, err := prefsFor(ch.Mode, ch.Key, doc, entries, raw)
	if err != nil {
		return "", fmt.Errorf("managedsettings.renderProfile: %s: %w", ch.Domain, err)
	}
	seed := ch.Domain + "\x00" + identifier + "\x00" + string(mustJSON(prefs))
	inner := map[string]any{
		"PayloadType":         ch.Domain,
		"PayloadIdentifier":   identifier + "." + ch.Domain,
		"PayloadUUID":         stableUUID("payload\x00" + seed),
		"PayloadVersion":      int64(1),
		"PayloadDisplayName":  displayName,
		"PayloadDescription":  description,
		"PayloadOrganization": "Observer",
	}
	for k, v := range prefs {
		inner[k] = v
	}
	outer := map[string]any{
		"PayloadType":              "Configuration",
		"PayloadVersion":           int64(1),
		"PayloadIdentifier":        identifier,
		"PayloadUUID":              stableUUID("profile\x00" + seed),
		"PayloadDisplayName":       displayName,
		"PayloadDescription":       description,
		"PayloadOrganization":      "Observer",
		"PayloadScope":             "System",
		"PayloadRemovalDisallowed": true,
		"PayloadContent":           []any{inner},
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0">` + "\n")
	if err := writePlist(&b, outer, 0); err != nil {
		return "", err
	}
	b.WriteString("</plist>\n")
	return b.String(), nil
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// writePlist writes v as an XML property list value, keys sorted so the
// output is stable.
func writePlist(b *strings.Builder, v any, depth int) error {
	ind := strings.Repeat("\t", depth)
	switch x := v.(type) {
	case nil:
		return fmt.Errorf("managedsettings: a property list has no null")
	case bool:
		if x {
			b.WriteString(ind + "<true/>\n")
		} else {
			b.WriteString(ind + "<false/>\n")
		}
	case int64:
		b.WriteString(ind + "<integer>" + strconv.FormatInt(x, 10) + "</integer>\n")
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1e15 {
			b.WriteString(ind + "<integer>" + strconv.FormatInt(int64(x), 10) + "</integer>\n")
		} else {
			b.WriteString(ind + "<real>" + strconv.FormatFloat(x, 'g', -1, 64) + "</real>\n")
		}
	case string:
		b.WriteString(ind + "<string>" + xmlEscape(x) + "</string>\n")
	case time.Time:
		b.WriteString(ind + "<date>" + x.UTC().Format(time.RFC3339) + "</date>\n")
	case []any:
		b.WriteString(ind + "<array>\n")
		for _, e := range x {
			if err := writePlist(b, e, depth+1); err != nil {
				return err
			}
		}
		b.WriteString(ind + "</array>\n")
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteString(ind + "<dict>\n")
		for _, k := range keys {
			b.WriteString(ind + "\t<key>" + xmlEscape(k) + "</key>\n")
			if err := writePlist(b, x[k], depth+1); err != nil {
				return err
			}
		}
		b.WriteString(ind + "</dict>\n")
	default:
		return fmt.Errorf("managedsettings: unsupported property list value %T", v)
	}
	return nil
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

var regEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

func regString(name, v string) string {
	return `"` + regEscaper.Replace(name) + `"="` + regEscaper.Replace(v) + `"`
}

func regTyped(name string, v any) (string, error) {
	quoted := `"` + regEscaper.Replace(name) + `"`
	switch x := v.(type) {
	case bool:
		d := 0
		if x {
			d = 1
		}
		return fmt.Sprintf("%s=dword:%08x", quoted, d), nil
	case int64:
		if x < 0 || x > math.MaxUint32 {
			return "", fmt.Errorf("managedsettings: %s: %d does not fit a REG_DWORD", name, x)
		}
		return fmt.Sprintf("%s=dword:%08x", quoted, x), nil
	case float64:
		if x != math.Trunc(x) || x < 0 || x > math.MaxUint32 {
			return regString(name, strconv.FormatFloat(x, 'g', -1, 64)), nil
		}
		return fmt.Sprintf("%s=dword:%08x", quoted, int64(x)), nil
	case string:
		return regString(name, x), nil
	case map[string]any, []any:
		b, err := json.Marshal(x)
		if err != nil {
			return "", fmt.Errorf("managedsettings: %s: %w", name, err)
		}
		return regString(name, string(b)), nil
	}
	return "", fmt.Errorf("managedsettings: %s: unsupported registry value %T", name, v)
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// renderRegistry renders a "Windows Registry Editor Version 5.00" import
// file as text with CRLF line ends (Artifact.Encoding says the bytes are
// UTF-16LE with a BOM, which is what regedit reads for this header).
func renderRegistry(ch RegistryChannel, comment []string, doc map[string]any, entries []entry) (string, error) {
	lines := []string{"Windows Registry Editor Version 5.00", ""}
	for _, c := range comment {
		lines = append(lines, "; "+c)
	}
	lines = append(lines, "", `[HKEY_LOCAL_MACHINE\`+ch.Key+`]`)
	switch ch.Mode {
	case RegistryJSONValue:
		if ch.Value == "" {
			return "", fmt.Errorf("managedsettings.renderRegistry: %s: json mode needs a value name", ch.Key)
		}
		b, err := json.Marshal(doc)
		if err != nil {
			return "", fmt.Errorf("managedsettings.renderRegistry: %w", err)
		}
		lines = append(lines, regString(ch.Value, string(b)))
	case RegistryValues:
		for _, k := range sortedKeys(doc) {
			line, err := regTyped(k, doc[k])
			if err != nil {
				return "", err
			}
			lines = append(lines, line)
		}
	case RegistryStrings:
		for _, k := range sortedKeys(doc) {
			s, err := stringForm(doc[k])
			if err != nil {
				return "", fmt.Errorf("managedsettings.renderRegistry: %s: %w", k, err)
			}
			lines = append(lines, regString(k, s))
		}
	case RegistryDottedStrings:
		names := map[string]string{}
		for _, e := range entries {
			s, err := stringForm(e.Value)
			if err != nil {
				return "", fmt.Errorf("managedsettings.renderRegistry: %s: %w", dotted(e.Key), err)
			}
			names[dotted(e.Key)] = s
		}
		keys := make([]string, 0, len(names))
		for k := range names {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			lines = append(lines, regString(k, names[k]))
		}
	default:
		return "", fmt.Errorf("managedsettings.renderRegistry: unknown mode %q", ch.Mode)
	}
	return strings.Join(lines, "\r\n") + "\r\n", nil
}

// EncodeUTF16LE returns s as UTF-16LE bytes with a byte-order mark (the
// encoding a version 5.00 .reg file is read in).
func EncodeUTF16LE(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, 0, 2+2*len(units))
	out = append(out, 0xFF, 0xFE)
	for _, u := range units {
		out = append(out, byte(u), byte(u>>8))
	}
	return out
}

// Bytes returns the artifact's deploy-ready bytes in its declared encoding.
func (a Artifact) Bytes() []byte {
	if a.Encoding == EncodingUTF16LE {
		return EncodeUTF16LE(a.Content)
	}
	return []byte(a.Content)
}

// Zip packs every artifact of the bundle plus the READMEs into one archive,
// in a stable order with a fixed timestamp so the same policy yields the same
// bytes.
func (b Bundle) Zip() ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	stamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	add := func(name string, data []byte) error {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: stamp})
		if err != nil {
			return fmt.Errorf("managedsettings.Zip: %s: %w", name, err)
		}
		if _, err := w.Write(data); err != nil {
			return fmt.Errorf("managedsettings.Zip: %s: %w", name, err)
		}
		return nil
	}
	if err := add("README.md", []byte(b.Readme)); err != nil {
		return nil, err
	}
	for _, t := range b.Tools {
		if t.Readme != "" {
			if err := add(t.Tool+"/README.md", []byte(t.Readme)); err != nil {
				return nil, err
			}
		}
		for _, a := range t.Artifacts {
			if err := add(a.Name, a.Bytes()); err != nil {
				return nil, err
			}
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("managedsettings.Zip: %w", err)
	}
	return buf.Bytes(), nil
}
