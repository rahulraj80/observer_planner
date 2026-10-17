package vendormock

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/marmutapp/superbased-observer/internal/vendorapi/cloudsign"
)

// The cloud auth mechanics of the mock (lane ENTAPI-L16D, T-713): an AWS
// Signature Version 4 check over the received request, and a Google OAuth
// token host that verifies an RS256 JWT bearer assertion. Keys are generated
// in this process; nothing is a real credential.

var (
	saMu   sync.Mutex
	saKeys = map[string]*rsa.PrivateKey{}
)

// ServiceAccountKey returns the RSA key the suite uses as the service account
// key of a synthetic token string (generated once per process, 2048 bits).
func ServiceAccountKey(token string) *rsa.PrivateKey {
	saMu.Lock()
	defer saMu.Unlock()
	if k, ok := saKeys[token]; ok {
		return k
	}
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil
	}
	saKeys[token] = k
	return k
}

// ServiceAccountPEM is ServiceAccountKey as the PEM PKCS#8 text a sealed
// service account secret holds.
func ServiceAccountPEM(token string) string {
	k := ServiceAccountKey(token)
	if k == nil {
		return ""
	}
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		return ""
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

// sigV4AuthRE parses an AWS4-HMAC-SHA256 Authorization header.
var sigV4AuthRE = regexp.MustCompile(`^AWS4-HMAC-SHA256 Credential=([A-Z0-9]{16,128})/([0-9]{8})/([a-z0-9-]+)/([a-z0-9-]+)/aws4_request, ` +
	`SignedHeaders=([a-z0-9;-]+), Signature=[0-9a-f]{64}$`)

// sigV4OK recomputes the SigV4 signature of a received request with the valid
// token as the secret access key and the profile's scope, signing exactly the
// headers the request lists, and compares the Authorization header whole.
func (m *Mock) sigV4OK(r *http.Request, body []byte) bool {
	got := r.Header.Get("Authorization")
	sm := sigV4AuthRE.FindStringSubmatch(got)
	if sm == nil || sm[3] != m.profile.SigV4Region || sm[4] != m.profile.SigV4Service {
		return false
	}
	at, err := time.Parse("20060102T150405Z", r.Header.Get("X-Amz-Date"))
	if err != nil || at.Format("20060102") != sm[2] {
		return false
	}
	hdr := map[string]string{}
	signed := map[string]bool{}
	for _, n := range strings.Split(sm[5], ";") {
		signed[n] = true
		if n == "host" || n == "x-amz-date" {
			continue
		}
		hdr[n] = r.Header.Get(n)
	}
	// The vendor reads the content type and every required header (the
	// X-Amz-Target names the operation), so each must be under the signature.
	need := []string{"host", "x-amz-date"}
	if r.Header.Get("Content-Type") != "" {
		need = append(need, "content-type")
	}
	for k := range m.profile.Required {
		need = append(need, strings.ToLower(k))
	}
	for _, n := range need {
		if !signed[n] {
			return false
		}
	}
	want, err := cloudsign.SignSigV4(cloudsign.Request{
		Method: r.Method, Host: r.Host, Path: r.URL.Path, Query: r.URL.Query(), Headers: hdr, Payload: body,
	}, cloudsign.AccessKey{ID: sm[1], Secret: m.opts.ValidToken}, cloudsign.SigV4Params{
		Region: m.profile.SigV4Region, Service: m.profile.SigV4Service, Time: at,
	})
	return err == nil && want["Authorization"] == got
}

// jwtGrant is the RFC 7523 grant type.
const jwtGrant = "urn:ietf:params:oauth:grant-type:jwt-bearer"

// verifyAssertion checks an RS256 assertion against a key and returns its
// claims.
func verifyAssertion(a string, k *rsa.PrivateKey) (map[string]any, bool) {
	parts := strings.Split(a, ".")
	if k == nil || len(parts) != 3 {
		return nil, false
	}
	hb, err1 := base64.RawURLEncoding.DecodeString(parts[0])
	cb, err2 := base64.RawURLEncoding.DecodeString(parts[1])
	sig, err3 := base64.RawURLEncoding.DecodeString(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return nil, false
	}
	var h map[string]any
	if json.Unmarshal(hb, &h) != nil || h["alg"] != "RS256" {
		return nil, false
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(&k.PublicKey, crypto.SHA256, sum[:], sig) != nil {
		return nil, false
	}
	var c map[string]any
	if json.Unmarshal(cb, &c) != nil {
		return nil, false
	}
	return c, true
}

// serveJWTToken is the mock Google OAuth token endpoint (POST /token, form
// grant_type jwt-bearer and assertion; documented at
// https://developers.google.com/identity/protocols/oauth2/service-account):
// an assertion signed by the valid or the wrong-plane token's key, for the
// profile's scope and this endpoint's URL, gets the documented success shape
// with a minted token; anything else gets 400 invalid_grant.
func (m *Mock) serveJWTToken(w http.ResponseWriter, r *http.Request) {
	rec := Recorded{Method: r.Method, Path: r.URL.Path, HasAuth: r.Header.Get("Authorization") != ""}
	_ = r.ParseForm()
	minted := ""
	if r.Method == http.MethodPost && r.URL.Path == "/token" && !rec.HasAuth &&
		strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") &&
		r.PostForm.Get("grant_type") == jwtGrant {
		for _, tok := range []string{m.opts.ValidToken, m.opts.WrongPlaneToken} {
			c, ok := verifyAssertion(r.PostForm.Get("assertion"), ServiceAccountKey(tok))
			if !ok {
				continue
			}
			iat, _ := c["iat"].(float64)
			exp, _ := c["exp"].(float64)
			if c["scope"] == m.profile.TokenScope && c["aud"] == m.TokenURL+"/token" && c["iss"] != "" && exp > iat && exp-iat <= 3600 {
				minted = AccessTokenFor(tok)
			}
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if minted == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"synthetic"}`))
		rec.Status = http.StatusBadRequest
	} else {
		_, _ = w.Write([]byte(`{"access_token":"` + minted + `","expires_in":3599,"token_type":"Bearer"}`))
		rec.Status = http.StatusOK
	}
	m.mu.Lock()
	m.tokReqs = append(m.tokReqs, rec)
	m.mu.Unlock()
}
