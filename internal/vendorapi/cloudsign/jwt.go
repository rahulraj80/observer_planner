package cloudsign

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"regexp"
	"strings"
	"time"
)

// MaxAssertionLifetime is the longest JWT bearer assertion Google accepts
// (exp at most one hour after iat).
const MaxAssertionLifetime = time.Hour

// maxKeyPEM bounds a stored service account key (a 4096-bit PKCS#8 RSA key
// is about 3.3 KiB of PEM).
const maxKeyPEM = 8 << 10

var (
	// saEmailRE is a Google service account e-mail (account id 6 to 30
	// characters, a project id, the iam.gserviceaccount.com domain).
	saEmailRE = regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]@[a-z][a-z0-9-]{4,28}[a-z0-9]\.iam\.gserviceaccount\.com$`)
	// scopeRE is one OAuth scope URL of the googleapis.com scope space.
	scopeRE = regexp.MustCompile(`^https://www\.googleapis\.com/auth/[a-z0-9._-]{1,96}$`)
	// audRE is a token endpoint audience: an absolute URL with no query.
	audRE = regexp.MustCompile(`^https?://[a-z0-9.-]+(:[0-9]{1,5})?/[A-Za-z0-9/._-]*$`)
)

// ValidServiceAccountEmail reports whether s is a Google service account
// e-mail (the assertion's issuer; an identifier, not a secret).
func ValidServiceAccountEmail(s string) bool { return saEmailRE.MatchString(s) }

// ValidScope reports whether s is one googleapis.com OAuth scope URL.
func ValidScope(s string) bool { return scopeRE.MatchString(s) }

// ParseRSAKeyPEM parses a stored service account private key: exactly one
// unencrypted PEM "PRIVATE KEY" block (PKCS#8) holding an RSA key of at
// least 2048 bits, with nothing but whitespace around it. Every refusal is
// ErrInput and never echoes the input.
func ParseRSAKeyPEM(b []byte) (*rsa.PrivateKey, error) {
	if len(b) == 0 || len(b) > maxKeyPEM {
		return nil, ErrInput
	}
	blk, rest := pem.Decode(b)
	if blk == nil || blk.Type != "PRIVATE KEY" || len(blk.Headers) != 0 || len(bytes.TrimSpace(rest)) != 0 {
		return nil, ErrInput
	}
	k, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		return nil, ErrInput
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok || rk.N.BitLen() < 2048 {
		return nil, ErrInput
	}
	return rk, nil
}

// JWTClaims are the claims of a Google service account JWT bearer
// assertion: the issuer (the service account e-mail), the space-separated
// scopes, the audience (the token endpoint URL), the issue time and the
// lifetime (at most MaxAssertionLifetime).
type JWTClaims struct {
	Issuer   string
	Scopes   []string
	Audience string
	IssuedAt time.Time
	Lifetime time.Duration
}

// GoogleAssertion builds and signs (RS256) the RFC 7523 JWT bearer assertion
// a service account exchanges for an access token at Google's OAuth token
// endpoint. Header {"alg":"RS256","typ":"JWT"}; claims iss, scope, aud, iat,
// exp. The assertion is itself a credential: the caller sends it only to the
// token host and never keeps or logs it.
func GoogleAssertion(key *rsa.PrivateKey, c JWTClaims) (string, error) {
	if key == nil || !saEmailRE.MatchString(c.Issuer) || !audRE.MatchString(c.Audience) || len(c.Scopes) == 0 ||
		c.IssuedAt.IsZero() || c.Lifetime <= 0 || c.Lifetime > MaxAssertionLifetime || c.Lifetime%time.Second != 0 {
		return "", ErrInput
	}
	for _, s := range c.Scopes {
		if !scopeRE.MatchString(s) {
			return "", ErrInput
		}
	}
	iat := c.IssuedAt.Unix()
	claims := struct {
		Iss   string `json:"iss"`
		Scope string `json:"scope"`
		Aud   string `json:"aud"`
		Iat   int64  `json:"iat"`
		Exp   int64  `json:"exp"`
	}{c.Issuer, strings.Join(c.Scopes, " "), c.Audience, iat, iat + int64(c.Lifetime/time.Second)}
	cb, err := json.Marshal(claims)
	if err != nil {
		return "", ErrInput
	}
	enc := base64.RawURLEncoding
	input := enc.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`)) + "." + enc.EncodeToString(cb)
	sum := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", ErrInput
	}
	return input + "." + enc.EncodeToString(sig), nil
}
