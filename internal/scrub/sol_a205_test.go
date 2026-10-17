package scrub

import (
	"encoding/base64"
	"strings"
	"testing"
)

// solA205JWT builds a JWS whose header and payload JSON carry whitespace
// after the opening brace, so they encode as eyA..., not eyJ... (Sol
// A2-05). Assembled at runtime so no literal here is a credential shape.
func solA205JWT(header, payload string) string {
	enc := base64.RawURLEncoding
	return enc.EncodeToString([]byte(header)) + "." + enc.EncodeToString([]byte(payload)) + "." + strings.Repeat("Sg9_", 8)
}

// solA205JWTNoSig is an unsecured (alg none) token: empty signature.
func solA205JWTNoSig(header, payload string) string {
	enc := base64.RawURLEncoding
	return enc.EncodeToString([]byte(header)) + "." + enc.EncodeToString([]byte(payload)) + "."
}

// solA205Fixtures are valid secrets the detector table missed before Sol
// A2-05, with the detector each must be reported as.
func solA205Fixtures() map[string]string {
	return map[string]string{
		"slack xoxo":        "xoxo-" + "1111111111-2222222222",
		"slack xoxe":        "xoxe-" + "1-" + strings.Repeat("Ab9", 12),
		"slack xoxe.xoxp":   "xoxe.xoxp-" + "1-" + strings.Repeat("Ab9", 12),
		"jwt spaced header": solA205JWT(`{ "alg":"HS256","typ":"JWT"}`, `{ "sub":"1234567890"}`),
		"jwt newline json":  solA205JWT("{\n \"alg\": \"RS256\"\n}", "{\n \"sub\": \"u1\"\n}"),
		"jwt tab json":      solA205JWT("{\t\"alg\":\"none\"}", `{"iss":"x","sub":"y"}`),
		"jwt legacy shape":  "eyJ" + strings.Repeat("h", 12) + ".eyJ" + strings.Repeat("p", 12) + "." + strings.Repeat("s", 12),
		"jwt unsecured":     solA205JWTNoSig(`{ "alg":"none"}`, `{ "sub":"u1"}`),
		"jwt binary payload": func() string {
			enc := base64.RawURLEncoding
			return enc.EncodeToString([]byte(`{ "alg":"HS256"}`)) + "." + enc.EncodeToString([]byte("raw bytes")) + "." + strings.Repeat("Sg9_", 8)
		}(),
	}
}

func solA205Type(name string) string {
	if strings.HasPrefix(name, "slack") {
		return "slack_token"
	}
	return "jwt"
}

// TestSolA205SharedFixtures runs every fixture through all four scrub
// surfaces: the typed detector, the storage scrub, the forward scrub and
// the prompt masker. Each must catch the value.
func TestSolA205SharedFixtures(t *testing.T) {
	s := New()
	for name, val := range solA205Fixtures() {
		t.Run(name, func(t *testing.T) {
			want := solA205Type(name)
			text := "here is my key " + val + " please use it"

			found := false
			for _, f := range DetectSecrets(text) {
				if f.Type == want {
					found = true
				}
			}
			if !found {
				t.Errorf("DetectSecrets missed %s: %+v", want, DetectSecrets(text))
			}
			if out := s.String(text); strings.Contains(out, val) {
				t.Errorf("String left the value: %q", out)
			}
			body := []byte(`{"messages":[{"role":"user","content":"` + text + `"}]}`)
			if out := string(s.ScrubForward(body)); strings.Contains(out, val) {
				t.Errorf("ScrubForward left the value: %q", out)
			}
			masked, _ := MaskSecrets(text, func(TypedFinding) bool { return true })
			if strings.Contains(masked, val) {
				t.Errorf("MaskSecrets left the value: %q", masked)
			}
		})
	}
}

// TestSolA205JWTNearMisses pins that the widened candidate pattern is
// decided structurally: a dotted base64-ish run that does not decode to a
// JWS header (no "alg", not JSON) is not reported or redacted as a JWT.
func TestSolA205JWTNearMisses(t *testing.T) {
	enc := base64.RawURLEncoding
	s := New()
	for name, val := range map[string]string{
		"undecodable":          "eyA" + "bcdefghij.eabc.0123456789",
		"header no alg":        solA205JWT(`{ "typ":"JWT"}`, `{ "sub":"1"}`),
		"header not an object": enc.EncodeToString([]byte(`{ "alg"`)) + "." + enc.EncodeToString([]byte(`{"sub":"1"}`)) + "." + strings.Repeat("Sg9_", 8),
		"header array":         enc.EncodeToString([]byte(`[ "alg" ]`)) + ".e30." + strings.Repeat("Sg9_", 8),
	} {
		t.Run(name, func(t *testing.T) {
			text := "value " + val + " end"
			for _, f := range DetectSecrets(text) {
				if f.Type == "jwt" {
					t.Errorf("reported as jwt: %+v", f)
				}
			}
			if out := s.String(text); strings.Contains(out, TypedRedactedMarker("jwt")) {
				t.Errorf("storage scrub wrote a jwt marker: %q", out)
			}
		})
	}
}
