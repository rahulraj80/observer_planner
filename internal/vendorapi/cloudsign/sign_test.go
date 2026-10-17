package cloudsign

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"strings"
	"testing"
	"time"
)

// The AWS vectors are the published AWS Signature Version 4 test suite as
// shipped in awslabs/aws-c-auth (tests/aws-signing-test-suite/v4), fetched
// 2026-10-08 from
// https://raw.githubusercontent.com/awslabs/aws-c-auth/main/tests/aws-signing-test-suite/v4/<case>/
// context.json (both cases, sha256 5f8a32a7...16a2f1): AKIDEXAMPLE,
// wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY, us-east-1, service "service",
// 2015-08-30T12:36:00Z, sign_body false.
//   - get-vanilla: request.txt sha256 9b87a070...ad63e4f, header-signature.txt
//     sha256 fa5655a3...5b57352.
//   - get-vanilla-query-order-key-case: request.txt sha256 97810c49...a6d6d4,
//     header-signature.txt sha256 20158c75...8b83b.
var awsSuiteKey = AccessKey{ID: "AKIDEXAMPLE", Secret: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"}

var awsSuiteTime = time.Date(2015, 8, 30, 12, 36, 0, 0, time.UTC)

// TestSigV4PublishedSuite: the signer reproduces the published signatures
// exactly, including the query ordering case.
func TestSigV4PublishedSuite(t *testing.T) {
	p := SigV4Params{Region: "us-east-1", Service: "service", Time: awsSuiteTime}
	cases := []struct {
		name  string
		query map[string][]string
		want  string
	}{
		{"get-vanilla", nil, "5fa00fa31553b73ebf1942676e86291e8372ff2a2260956d9b8aae1d763fbf31"},
		{
			"get-vanilla-query-order-key-case",
			map[string][]string{"Param2": {"value2"}, "Param1": {"value1"}},
			"b97d918cfa904a5beff61c982a1b6f458b799221646efd99d3219ec94cdf2500",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, err := SignSigV4(Request{Method: "GET", Host: "example.amazonaws.com", Path: "/", Query: c.query}, awsSuiteKey, p)
			if err != nil {
				t.Fatal(err)
			}
			want := "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20150830/us-east-1/service/aws4_request, " +
				"SignedHeaders=host;x-amz-date, Signature=" + c.want
			if h["Authorization"] != want || h["X-Amz-Date"] != "20150830T123600Z" || len(h) != 2 {
				t.Fatalf("headers %v\nwant Authorization %s", h, want)
			}
		})
	}
	if got := CanonicalQuery(map[string][]string{"Param2": {"value2"}, "Param1": {"value1"}}); got != "Param1=value1&Param2=value2" {
		t.Fatalf("canonical query %q", got)
	}
}

// TestACS3PublishedExample: Alibaba Cloud's "V3 request structure and
// signature" page ("Verify your signature implementation"), fetched
// 2026-10-08 from
// https://www.alibabacloud.com/help/en/sdk/product-overview/v3-request-structure-and-signature
// (HTML sha256 f016bf8a5f785bc7b56bac493410a2ce753c55e729df042ac790ece30754a3bd):
// YourAccessKeyId / YourAccessKeySecret, POST / with ImageId and RegionId,
// host ecs.cn-shanghai.aliyuncs.com, RunInstances 2014-05-26, nonce
// 3156853299f313e23d1673dc12e1703d, 2023-10-26T10:22:32Z, empty body. Both the
// hashed canonical request and the signature match the published values.
func TestACS3PublishedExample(t *testing.T) {
	r := Request{
		Method: "POST", Host: "ecs.cn-shanghai.aliyuncs.com", Path: "/",
		Query: map[string][]string{
			"ImageId":  {"win2019_1809_x64_dtc_zh-cn_40G_alibase_20230811.vhd"},
			"RegionId": {"cn-shanghai"},
		},
	}
	p := ACS3Params{
		Action: "RunInstances", Version: "2014-05-26", Time: time.Date(2023, 10, 26, 10, 22, 32, 0, time.UTC),
		Nonce: "3156853299f313e23d1673dc12e1703d",
	}
	_, canonical, _, ok := acs3Parts(r, p)
	if !ok || sha256Hex([]byte(canonical)) != "7ea06492da5221eba5297e897ce16e55f964061054b7695beedaac1145b1e259" {
		t.Fatalf("hashed canonical request differs; canonical:\n%s", canonical)
	}
	h, err := SignACS3(r, AccessKey{ID: "YourAccessKeyId", Secret: "YourAccessKeySecret"}, p)
	if err != nil {
		t.Fatal(err)
	}
	want := "ACS3-HMAC-SHA256 Credential=YourAccessKeyId,SignedHeaders=host;x-acs-action;x-acs-content-sha256;" +
		"x-acs-date;x-acs-signature-nonce;x-acs-version,Signature=06563a9e1b43f5dfe96b81484da74bceab24a1d853912eee15083a6f0f3283c0"
	if h["Authorization"] != want || h["x-acs-date"] != "2023-10-26T10:22:32Z" ||
		h["x-acs-content-sha256"] != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("headers %v", h)
	}
}

// TestSignersRefuseShapes: every input outside the restricted shapes is
// ErrInput, and no refusal carries the secret.
func TestSignersRefuseShapes(t *testing.T) {
	base := Request{Method: "GET", Host: "example.amazonaws.com", Path: "/"}
	p4 := SigV4Params{Region: "us-east-1", Service: "service", Time: awsSuiteTime}
	pa := ACS3Params{Action: "DescribeX", Version: "2014-05-26", Time: awsSuiteTime, Nonce: "3156853299f313e23d1673dc12e1703d"}
	mut := []struct {
		name string
		r    func(*Request)
		k    func(*AccessKey)
	}{
		{"PUT", func(r *Request) { r.Method = "PUT" }, nil},
		{"upper-case host", func(r *Request) { r.Host = "Example.amazonaws.com" }, nil},
		{"host with userinfo", func(r *Request) { r.Host = "u@example.amazonaws.com" }, nil},
		{"escaped path", func(r *Request) { r.Path = "/a%2Fb" }, nil},
		{"relative path", func(r *Request) { r.Path = "a" }, nil},
		{"double slash", func(r *Request) { r.Path = "//x" }, nil},
		{"authorization signed", func(r *Request) { r.Headers = map[string]string{"authorization": "x"} }, nil},
		{"upper-case header name", func(r *Request) { r.Headers = map[string]string{"Accept": "x"} }, nil},
		{"header with a line break", func(r *Request) { r.Headers = map[string]string{"accept": "a\r\nb"} }, nil},
		{"empty secret", nil, func(k *AccessKey) { k.Secret = "" }},
		{"secret with a space", nil, func(k *AccessKey) { k.Secret = "a b" }},
		{"key id with a slash", nil, func(k *AccessKey) { k.ID = "AKID/X" }},
	}
	for _, m := range mut {
		r, k := base, awsSuiteKey
		if m.r != nil {
			m.r(&r)
		}
		if m.k != nil {
			m.k(&k)
		}
		for name, err := range map[string]error{
			"sigv4": func() error { _, e := SignSigV4(r, k, p4); return e }(),
			"acs3":  func() error { _, e := SignACS3(r, k, pa); return e }(),
		} {
			if !errors.Is(err, ErrInput) || strings.Contains(err.Error(), awsSuiteKey.Secret) {
				t.Errorf("%s %s: err %v", name, m.name, err)
			}
		}
	}
	for name, p := range map[string]ACS3Params{
		"short nonce":  {Action: "X", Version: "2014-05-26", Time: awsSuiteTime, Nonce: "abc"},
		"bad version":  {Action: "X", Version: "v1", Time: awsSuiteTime, Nonce: pa.Nonce},
		"bad action":   {Action: "X Y", Version: "2014-05-26", Time: awsSuiteTime, Nonce: pa.Nonce},
		"no time":      {Action: "X", Version: "2014-05-26", Nonce: pa.Nonce},
		"header clash": pa,
	} {
		r := base
		if name == "header clash" {
			r.Headers = map[string]string{"x-acs-date": "x"}
		}
		if _, err := SignACS3(r, awsSuiteKey, p); !errors.Is(err, ErrInput) {
			t.Errorf("acs3 %s: err %v", name, err)
		}
	}
	if _, err := SignSigV4(base, awsSuiteKey, SigV4Params{Region: "US", Service: "s", Time: awsSuiteTime}); !errors.Is(err, ErrInput) {
		t.Errorf("sigv4 bad region: %v", err)
	}
}

func pkcs8PEM(t *testing.T, k *rsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

// TestGoogleAssertion: an RS256 assertion from a key generated in this test
// verifies with its public key and carries exactly the RFC 7523 claims,
// exp - iat at most one hour; the key parser takes only an unencrypted PKCS#8
// RSA key of at least 2048 bits.
func TestGoogleAssertion(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pk, err := ParseRSAKeyPEM(pkcs8PEM(t, key))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	c := JWTClaims{
		Issuer:   "observer-reader@acme-proj-1.iam.gserviceaccount.com",
		Scopes:   []string{"https://www.googleapis.com/auth/cloud-platform.read-only"},
		Audience: "https://oauth2.googleapis.com/token", IssuedAt: now, Lifetime: time.Hour,
	}
	a, err := GoogleAssertion(pk, c)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(a, ".")
	if len(parts) != 3 {
		t.Fatalf("assertion has %d parts", len(parts))
	}
	enc := base64.RawURLEncoding
	hdr, _ := enc.DecodeString(parts[0])
	if string(hdr) != `{"alg":"RS256","typ":"JWT"}` {
		t.Fatalf("header %s", hdr)
	}
	sig, _ := enc.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, sum[:], sig); err != nil {
		t.Fatalf("signature does not verify: %v", err)
	}
	cb, _ := enc.DecodeString(parts[1])
	var got map[string]any
	if err := json.Unmarshal(cb, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 || got["iss"] != c.Issuer || got["scope"] != c.Scopes[0] || got["aud"] != c.Audience ||
		got["iat"].(float64) != float64(now.Unix()) || got["exp"].(float64)-got["iat"].(float64) != 3600 {
		t.Fatalf("claims %v", got)
	}

	for name, m := range map[string]func(*JWTClaims){
		"lifetime over an hour": func(c *JWTClaims) { c.Lifetime = time.Hour + time.Second },
		"no lifetime":           func(c *JWTClaims) { c.Lifetime = 0 },
		"not a service account": func(c *JWTClaims) { c.Issuer = "someone@gmail.com" },
		"foreign scope":         func(c *JWTClaims) { c.Scopes = []string{"https://evil.example.com/auth/x"} },
		"no scope":              func(c *JWTClaims) { c.Scopes = nil },
		"audience with query":   func(c *JWTClaims) { c.Audience = "https://oauth2.googleapis.com/token?x=1" },
		"no issue time":         func(c *JWTClaims) { c.IssuedAt = time.Time{} },
	} {
		cc := c
		m(&cc)
		if _, err := GoogleAssertion(pk, cc); !errors.Is(err, ErrInput) {
			t.Errorf("%s: err %v", name, err)
		}
	}

	small, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	pkcs1 := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	good := pkcs8PEM(t, key)
	withHeader := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Headers: map[string]string{"Proc-Type": "4,ENCRYPTED"}, Bytes: []byte{1}})
	for name, b := range map[string][]byte{
		"1024-bit key":   pkcs8PEM(t, small),
		"PKCS#1 block":   pkcs1,
		"trailing bytes": append(append([]byte{}, good...), []byte("x")...),
		"two blocks":     append(append([]byte{}, good...), good...),
		"encrypted":      withHeader,
		"empty":          nil,
		"not PEM":        []byte("synthetic-not-a-key"),
	} {
		if _, err := ParseRSAKeyPEM(b); !errors.Is(err, ErrInput) {
			t.Errorf("%s: err %v", name, err)
		}
	}
}
