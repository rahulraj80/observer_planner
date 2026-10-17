package cloudsign

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ErrInput is every refusal of a signer: an input outside the restricted
// shapes below. It never carries a key, a secret or a signature.
var ErrInput = errors.New("cloudsign: input refused")

// Request is the exact request a signer signs. Host is the Host header value
// (host, or host:port when the port is not the scheme default), Path a plain
// absolute path, Query the query parameters (rendered by CanonicalQuery, which
// is also what the caller sends), Headers extra headers to sign (lower-case
// names) and Payload the exact body bytes (nil for none).
type Request struct {
	Method  string
	Host    string
	Path    string
	Query   map[string][]string
	Headers map[string]string
	Payload []byte
}

var (
	methodRE  = regexp.MustCompile(`^(GET|POST)$`)
	hostRE    = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?(:[0-9]{1,5})?$`)
	pathRE    = regexp.MustCompile(`^/[A-Za-z0-9._~/-]*$`)
	hdrNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	keyIDRE   = regexp.MustCompile(`^[A-Za-z0-9]{1,128}$`)
	regionRE  = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?$`)
	actionRE  = regexp.MustCompile(`^[A-Za-z0-9]{1,128}$`)
	versionRE = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
	nonceRE   = regexp.MustCompile(`^[0-9a-f]{16,64}$`)
)

// uriEncode is the RFC 3986 percent-encoding both vendors specify: the
// unreserved characters stay, every other byte becomes %XX in upper case (a
// space is %20, never +).
func uriEncode(s string) string {
	const hexd = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hexd[c>>4])
		b.WriteByte(hexd[c&15])
	}
	return b.String()
}

// CanonicalQuery renders a query the way both signatures canonicalise it:
// every name and value RFC 3986 encoded, pairs sorted by encoded name then
// encoded value, joined with "&". A caller sends exactly this string as the
// raw query, so the signed and the sent bytes agree.
func CanonicalQuery(q map[string][]string) string {
	type kv struct{ k, v string }
	var pairs []kv
	for k, vs := range q {
		for _, v := range vs {
			pairs = append(pairs, kv{uriEncode(k), uriEncode(v)})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].k != pairs[j].k {
			return pairs[i].k < pairs[j].k
		}
		return pairs[i].v < pairs[j].v
	})
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, p.k+"="+p.v)
	}
	return strings.Join(parts, "&")
}

// cleanValue trims a header value and collapses inner runs of spaces; a
// control character refuses it.
func cleanValue(v string) (string, bool) {
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return "", false
		}
	}
	return strings.Join(strings.Fields(v), " "), true
}

// canonicalHeaders renders the signed header block and the signed header
// list from a lower-case name map.
func canonicalHeaders(h map[string]string) (block, list string, ok bool) {
	names := make([]string, 0, len(h))
	for n := range h {
		if !hdrNameRE.MatchString(n) || n == "authorization" {
			return "", "", false
		}
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		v, ok := cleanValue(h[n])
		if !ok {
			return "", "", false
		}
		b.WriteString(n + ":" + v + "\n")
	}
	return b.String(), strings.Join(names, ";"), true
}

// checkRequest applies the shared request shape and copies the extra headers
// with the host into a fresh map.
func checkRequest(r Request) (map[string]string, bool) {
	if !methodRE.MatchString(r.Method) || !hostRE.MatchString(r.Host) || !pathRE.MatchString(r.Path) ||
		strings.Contains(r.Path, "//") {
		return nil, false
	}
	h := map[string]string{"host": r.Host}
	for k, v := range r.Headers {
		if k != strings.ToLower(k) || k == "host" {
			return nil, false
		}
		h[k] = v
	}
	return h, true
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func hmacSHA256(key []byte, msg string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(msg))
	return m.Sum(nil)
}

// validSecret refuses an empty secret or one with whitespace or a control
// character (a stored secret is one opaque token).
func validSecret(s string) bool {
	if s == "" || len(s) > 256 {
		return false
	}
	for _, r := range s {
		if r <= 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// AccessKey is a cloud access key pair: the public key id and its secret.
type AccessKey struct {
	ID     string
	Secret string
}

// SigV4Params are the scope of an AWS Signature Version 4 signature.
type SigV4Params struct {
	Region  string
	Service string
	Time    time.Time
}

// sigV4Parts returns the canonical request and the signed header list.
func sigV4Parts(r Request, amzDate string) (canonical, signed string, ok bool) {
	h, ok := checkRequest(r)
	if !ok {
		return "", "", false
	}
	if _, clash := h["x-amz-date"]; clash {
		return "", "", false
	}
	h["x-amz-date"] = amzDate
	block, list, ok := canonicalHeaders(h)
	if !ok {
		return "", "", false
	}
	canonical = strings.Join([]string{r.Method, r.Path, CanonicalQuery(r.Query), block, list, sha256Hex(r.Payload)}, "\n")
	return canonical, list, true
}

// SignSigV4 signs r with AWS Signature Version 4 (single-chunk, header form)
// and returns the headers to set: X-Amz-Date and Authorization. The canonical
// URI is the path as given: the path shape is restricted to unreserved
// characters, for which AWS's per-segment encoding (once or twice) is the
// identity. The payload hash is always part of the canonical request; the
// x-amz-content-sha256 header (an S3 requirement) is not added.
func SignSigV4(r Request, k AccessKey, p SigV4Params) (map[string]string, error) {
	if !keyIDRE.MatchString(k.ID) || !validSecret(k.Secret) || !regionRE.MatchString(p.Region) ||
		!regionRE.MatchString(p.Service) || p.Time.IsZero() {
		return nil, ErrInput
	}
	t := p.Time.UTC()
	amzDate, day := t.Format("20060102T150405Z"), t.Format("20060102")
	canonical, signed, ok := sigV4Parts(r, amzDate)
	if !ok {
		return nil, ErrInput
	}
	scope := day + "/" + p.Region + "/" + p.Service + "/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + sha256Hex([]byte(canonical))
	key := hmacSHA256([]byte("AWS4"+k.Secret), day)
	key = hmacSHA256(key, p.Region)
	key = hmacSHA256(key, p.Service)
	key = hmacSHA256(key, "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(key, toSign))
	return map[string]string{
		"X-Amz-Date":    amzDate,
		"Authorization": "AWS4-HMAC-SHA256 Credential=" + k.ID + "/" + scope + ", SignedHeaders=" + signed + ", Signature=" + sig,
	}, nil
}

// ACS3Params are the RPC operation and the per-request values of an Alibaba
// Cloud ACS3-HMAC-SHA256 signature. Nonce must be fresh per request (the
// caller draws it from a cryptographic source).
type ACS3Params struct {
	Action  string
	Version string
	Time    time.Time
	Nonce   string
}

// acs3Parts returns the headers to set (without Authorization), the
// canonical request and the signed header list.
func acs3Parts(r Request, p ACS3Params) (map[string]string, string, string, bool) {
	h, ok := checkRequest(r)
	if !ok {
		return nil, "", "", false
	}
	payload := sha256Hex(r.Payload)
	set := map[string]string{
		"x-acs-action":          p.Action,
		"x-acs-version":         p.Version,
		"x-acs-date":            p.Time.UTC().Format("2006-01-02T15:04:05Z"),
		"x-acs-signature-nonce": p.Nonce,
		"x-acs-content-sha256":  payload,
	}
	for k, v := range set {
		if _, clash := h[k]; clash {
			return nil, "", "", false
		}
		h[k] = v
	}
	block, list, ok := canonicalHeaders(h)
	if !ok {
		return nil, "", "", false
	}
	canonical := strings.Join([]string{r.Method, r.Path, CanonicalQuery(r.Query), block, list, payload}, "\n")
	return set, canonical, list, true
}

// SignACS3 signs r with Alibaba Cloud signature V3 (ACS3-HMAC-SHA256) and
// returns the headers to set: the five x-acs-* headers and Authorization.
// Every header it sets, the host and the extra headers are signed.
func SignACS3(r Request, k AccessKey, p ACS3Params) (map[string]string, error) {
	if !keyIDRE.MatchString(k.ID) || !validSecret(k.Secret) || !actionRE.MatchString(p.Action) ||
		!versionRE.MatchString(p.Version) || !nonceRE.MatchString(p.Nonce) || p.Time.IsZero() {
		return nil, ErrInput
	}
	set, canonical, signed, ok := acs3Parts(r, p)
	if !ok {
		return nil, ErrInput
	}
	toSign := "ACS3-HMAC-SHA256\n" + sha256Hex([]byte(canonical))
	sig := hex.EncodeToString(hmacSHA256([]byte(k.Secret), toSign))
	set["Authorization"] = "ACS3-HMAC-SHA256 Credential=" + k.ID + ",SignedHeaders=" + signed + ",Signature=" + sig
	return set, nil
}
