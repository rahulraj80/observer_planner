package vendorapi

import (
	"errors"
	"time"

	"github.com/marmutapp/superbased-observer/internal/vendorsurface"
)

// AuthScheme is how the egress seam presents a credential on one endpoint.
// It is part of the driver's exact per-endpoint auth contract (plan 12A.6):
// the runner never derives it from a catalog keyword and never retries a 401
// with another scheme.
type AuthScheme string

// Auth schemes.
const (
	// SchemeBearer sends "Authorization: Bearer <secret>".
	SchemeBearer AuthScheme = "bearer"
	// SchemeRawHeader sends the secret as the whole value of Header.
	SchemeRawHeader AuthScheme = "raw_header"
	// SchemeBasicUser sends HTTP Basic with the secret as the user name and
	// an empty password.
	SchemeBasicUser AuthScheme = "basic_user"
	// SchemeOAuthClientCredentials exchanges the secret (an OAuth client
	// secret) for an access token at the surface's catalog token host first,
	// through the egress seam's reviewed token step, then sends
	// "Authorization: Bearer <access token>" (lane ENTAPI-D3, T-692). The
	// client secret never reaches the API host.
	SchemeOAuthClientCredentials AuthScheme = "oauth_client_credentials" //nolint:gosec // G101: an enum value, not a credential

	// The cloud auth kinds (lane ENTAPI-L16C, T-710), each of catalog auth
	// kind cloud_iam and each only for a surface of the egress seam's closed
	// signing or token tables (internal/vendorapi/cloudsign holds the pure
	// signers):
	//
	// SchemeAWSSigV4 signs the exact request with AWS Signature Version 4
	// (access key id from the connection parameters, the secret access key
	// sealed; region and service fixed by the table row).
	SchemeAWSSigV4 AuthScheme = "aws_sigv4"
	// SchemeAlibabaACS3 signs the exact request with Alibaba Cloud
	// ACS3-HMAC-SHA256 (AccessKey id from the connection parameters, the
	// AccessKey secret sealed; action and version fixed by the table row).
	SchemeAlibabaACS3 AuthScheme = "alibaba_acs3"
	// SchemeGoogleServiceAccount signs an RFC 7523 JWT bearer assertion with
	// the sealed service account private key (PEM PKCS#8 RSA), exchanges it
	// at the surface's catalog token host, then sends "Authorization: Bearer
	// <access token>". The private key never leaves the seam.
	SchemeGoogleServiceAccount AuthScheme = "google_service_account"
)

// SigningScheme reports whether s is one of the cloud signing or assertion
// schemes (their credential is never sent as itself).
func SigningScheme(s AuthScheme) bool {
	return s == SchemeAWSSigV4 || s == SchemeAlibabaACS3 || s == SchemeGoogleServiceAccount
}

// AuthContract is a driver's exact auth contract for its endpoint. Kind must
// equal the catalog row's auth kind and the connection's auth binding kind;
// Header names the header a raw-header scheme uses.
type AuthContract struct {
	Kind   vendorsurface.AuthKind
	Scheme AuthScheme
	Header string
	// Prefix is the literal token a SchemePrefixed contract puts before the
	// secret in Header (lane ENTAPI-L16C: JetBrains IDE Services
	// "Automation"); empty for every other scheme.
	Prefix string
}

// Response is what the egress seam hands a driver: the status, the
// response headers the driver declared it reads (and only those), the body,
// the origin of the request (scheme://host:port of the tuple it was sent
// to) and its path. The query string is never carried: a signed URL's query
// is bearer material (plan 6.2). Params are the connection parameters the
// request was built from, so a driver can scope its snapshot.
type Response struct {
	Status int
	Origin string
	Path   string
	Header map[string]string
	Body   []byte
	Params Params
	// Cursor is the cursor the request was built from ("" for the first
	// page), set by the runner's page loop. A page-number family whose
	// response does not echo the page it answered (Cursor's /teams/spend)
	// reads its position here.
	Cursor string
}

// Driver binds one surface to its pure functions (plan 6.3). Push-only
// verification (VerifyInbound) joins with the receivers lane.
//
// Parse reads a body alone; ParseResponse, when set, is used instead and also
// sees the declared response headers (a Link-header paging family needs
// them). Exactly one of them is required.
type Driver struct {
	Surface       vendorsurface.SurfaceID
	Build         func(p Params, w Window, cursor string) (Request, error)
	Parse         func(body []byte) (Page, error)
	ParseResponse func(r Response) (Page, error)
	Canary        func(Page) []DriftSignal

	// Auth is the exact per-endpoint auth contract (never keyword-derived).
	Auth AuthContract
	// Headers are static, non-secret request headers the endpoint documents
	// (an API version, an Accept type). A header that carries a credential
	// is never here: the egress seam injects it from Auth.
	Headers map[string]string
	// ResponseHeaders lists the response header names (lower case) the
	// driver reads; every other response header is dropped at the seam.
	ResponseHeaders []string
	// ContentTypes lists the accepted response media types (lower case,
	// without parameters); empty means application/json.
	ContentTypes []string
	// Supplies are the supplies one run of the driver produces, by class.
	Supplies []vendorsurface.DataClass
	// AuthoritativeSnapshot reports that a complete page set of this
	// surface lists every resource in the snapshot's scope, so a complete
	// run may tombstone what it did not see.
	AuthoritativeSnapshot bool
	// LegacyPoller names the legacy poller that already reads this surface
	// (cursoradminpoll, copilotanalytics, ...). While that poller owns it the
	// runner never runs the surface (invariant I8).
	LegacyPoller string
	// ClosedDTO reports that the driver decodes into closed types with no
	// value field and rejects unknown fields; a may_return_secret surface
	// runs only with a ClosedDTO driver (plan 4.5 R-E-06).
	ClosedDTO bool
	// ContractVersion is recorded on every snapshot manifest the driver
	// opens (a change marks live rungs for revalidation, plan 12A.7).
	ContractVersion string
	// Tool is the registry tool an activity surface's rows are attributed
	// to (usage, facts and held identities carry it). Graph-only drivers
	// leave it empty.
	Tool string
	// MaxWindow caps one run's fetch window (the vendor's documented
	// maximum date range, such as 30 days); zero means no cap. A backfill
	// longer than it is executed as consecutive runs (lane F11).
	MaxWindow time.Duration
	// MaxLookback is how far back the vendor serves data; a backfill never
	// asks for more. Zero means the runner's default.
	MaxLookback time.Duration
	// KnownPaths are the schema property paths of the surface's documented
	// responses, in the fixture field ledger's notation; the runner's shape
	// digest keeps these member names and wildcards every other one (plan
	// 12A.7). Pinned equal to the corpus ledgers by the drivers test.
	KnownPaths []string
	// Report, when set, makes the surface a two-step report (plan 6.2
	// second hop, lane L8): Build and the API host answer a document of
	// signed download links, and ParseResponse parses ONE downloaded file
	// into a page. Nil for every one-step surface.
	Report *ReportContract
}

// ReportContract is the pure half of a two-step report surface. The runner
// owns both hops: the first through the egress seam with the credential, the
// second through the seam's download path with NO credential, to a catalog
// download host only. A signed link is bearer material: the driver returns
// it to the runner and nothing else ever sees it (never a page, a run row or
// a log line).
type ReportContract struct {
	// Links reads the first hop's response and returns the signed download
	// links in order. A missing or mistyped links field is schema drift.
	Links func(r Response) ([]string, error)
	// Next returns the cursor of the next first-hop request for the window
	// ("" when the window is done). Report families that page by day use
	// it; a single "latest" report returns "".
	Next func(p Params, w Window, cursor string) string
	// ContentTypes lists the accepted download media types (lower case).
	ContentTypes []string
	// MaxLinks caps the links one first-hop answer may name; more is drift.
	MaxLinks int
}

// ErrSchemaDrift wraps every Parse refusal the field-policy matrix (plan
// 12A.5) calls a page failure: a declared required field is missing, a JSON
// type is wrong, the pagination contract changed. The runner lands nothing
// for that page and keeps the watermark.
var ErrSchemaDrift = errors.New("vendorapi: schema drift")
