package agentconfigsvc

import (
	"encoding/json"

	"github.com/marmutapp/superbased-observer/internal/agentconfig"
	"github.com/marmutapp/superbased-observer/internal/scrub"
)

// What a MANAGED node tells its organization about a value it found in an AI
// tool's file (D-112, "the org sees everything on managed nodes, values
// included"; T-428). The ruling allows SCRUBBING, not withholding: a value
// from a personal (local-only) file is kept like any other, and a value that
// looks like a credential is kept in its scrubbed form, never dropped. The
// raw secret is never stored node-side in the org state and never leaves the
// machine. Whether a kept value ships at all is still the push seam's
// decision (store.ShareOptions.shipsRawContent, managed enrolments only).

// orgValueScrubber is the structure-aware scrubber (Scrubber.JSONValue scrubs
// each decoded string in isolation, so a JSON value can never be truncated
// the way a raw-body regex pass can).
var orgValueScrubber = scrub.New()

// ScrubOrgValue returns v with every secret replaced and whether anything
// was replaced. It walks the DECODED value: a string under a credential-
// named key is redacted, every string goes through internal/scrub's patterns
// and the pattern-certain detector mask. Non-string leaves are unchanged.
func (s *Service) ScrubOrgValue(v any) (any, bool) {
	out := scrubOrgValue(orgValueScrubber.JSONValue(v), s.o.MaskText, "")
	before, err1 := json.Marshal(v)
	after, err2 := json.Marshal(out)
	return out, err1 != nil || err2 != nil || string(before) != string(after)
}

func scrubOrgValue(v any, mask func(string) (string, bool), key string) any {
	switch x := v.(type) {
	case string:
		if key != "" && x != "" && agentconfig.SecretKeyName(key) {
			return scrub.Redacted
		}
		if mask != nil {
			if m, found := mask(x); found {
				return m
			}
		}
		return x
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = scrubOrgValue(e, mask, key)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = scrubOrgValue(e, mask, k)
		}
		return out
	}
	return v
}

// ScrubOrgText scrubs free text (an instruction file's body) the same way.
func (s *Service) ScrubOrgText(text string) (string, bool) {
	out := orgValueScrubber.String(text)
	if s.o.MaskText != nil {
		if m, found := s.o.MaskText(out); found {
			out = m
		}
	}
	return out, out != text
}
