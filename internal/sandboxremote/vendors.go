package sandboxremote

import (
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	vocab "github.com/marmutapp/superbased-observer/internal/sandboxvocab"
)

// vendors.go is the per-vendor table (sandbox plan v2 sections 7.1, 7.2 and
// 5.7, lane SBX-P7). Every fact is grounded in the vendor's public
// documentation, read 2026-10-22; the docs/sandboxed-terminals.md endpoint
// tables name the page per call. A fact the docs do not state is written
// down as such (Unverified) and never used to claim more.

// The vendor ids. They equal the registry row ids (VendorRow maps them).
const (
	VendorE2B    = "e2b"
	VendorModal  = "modal"
	VendorVercel = "vercel-sandbox"
	// VendorOpenShellOrg is the org-hosted OpenShell gateway (tracker F-019,
	// lane G4): its "vendor" is the organization's own gateway, reached
	// through the org's validating front, which the org publishes in its
	// signed managed sandbox body.
	VendorOpenShellOrg = "openshell-org"
	// VendorSandHold is a SandHold node the user connected with `observer
	// sandbox connect <node-url>` (SandHold notes 41 D2 and D3): its
	// "vendor" is the node itself, reached through its public /v1 API with
	// the API key the node gave at sign-in (NodeEndpoint).
	VendorSandHold = "sandhold"
)

// RegionKind says how a vendor places a box.
type RegionKind string

// The closed RegionKind vocabulary.
const (
	// RegionByEndpoint: each region is its own deployment, chosen by the API
	// endpoint (E2B); a credential belongs to one region.
	RegionByEndpoint RegionKind = "by_endpoint"
	// RegionSelectable: the create request names the region.
	RegionSelectable RegionKind = "selectable"
)

// ProviderChosen is the region a run records when the vendor picks it and
// nothing pins it (an org allowed_regions floor refuses it, S7b).
const ProviderChosen = "provider chosen"

// Regions is a vendor's region model.
type Regions struct {
	Kind RegionKind
	// Default is the region used when none is configured; ProviderChosen
	// when the vendor does not document one.
	Default string
	// Known are the region names the docs list.
	Known []string
	// Endpoints maps a region to its API origin (RegionByEndpoint only).
	Endpoints map[string]string
	// Docs maps every Known region to the vendor page that grounds it. A
	// region with no page is not listed: a by-endpoint region sends the
	// stored credential to its endpoint, so an ungrounded one could send a
	// key to a host nobody checked (TestVendorRegionsGrounded).
	Docs map[string]string
}

// regionDocs maps each region to one grounding page.
func regionDocs(doc string, regions ...string) map[string]string {
	out := make(map[string]string, len(regions))
	for _, r := range regions {
		out[r] = doc
	}
	return out
}

// TierMapping is how a vendor builds one egress tier, or that it cannot.
type TierMapping struct {
	// Mechanism is the vendor setting the tier uses (shown in docs and the
	// panel); empty means the tier is absent for this vendor.
	Mechanism string
	// Source says whether the vendor's tier means what the shipped tier
	// means (refusing host-local, link-local and private ranges for
	// internet): documented only when the vendor's docs say so.
	Source vocab.ClassSource
	// Relay: the run's relay carries the model proxy (and, on internet, the
	// gateway is not used: the vendor's own allow-all is the path out).
	Relay bool
}

// MemoryImagePolicy is what a vendor can do with a live box's memory.
type MemoryImagePolicy struct {
	// AutoPauseExists: the vendor can pause a live box into a memory image.
	AutoPauseExists bool
	// CanDisable: the create request can switch it off (and the driver
	// does).
	CanDisable bool
	Note       string
}

// PlanCap is one plan's longest box lifetime.
type PlanCap struct {
	Plan    string
	Minutes int
}

// Price is a dated list price for a labelled estimate.
type Price struct {
	// USDPerHour is the price of the default box size for one hour.
	USDPerHour float64 `json:"usd_per_hour"`
	Size       string  `json:"size"`
	Date       string  `json:"date"`
	Source     string  `json:"source"`
}

// EndpointDoc grounds one call of a wire client.
type EndpointDoc struct {
	Call   string `json:"call"`
	Method string `json:"method"`
	Path   string `json:"path"`
	Doc    string `json:"doc"`
	// Unverified names what the docs do not state about this call.
	Unverified string `json:"unverified,omitempty"`
}

// Vendor is one hosted sandbox provider.
type Vendor struct {
	ID   string
	Name string
	// DefaultEndpoint is the API origin an environment-variable credential
	// may be sent to (SBX-GOV-4: an env credential is bound to it only).
	DefaultEndpoint string
	// Domains are the vendor's own registrable domains: every API endpoint
	// and every region page is under one of them.
	Domains []string
	// KeystoreService is the keychain service the credential is stored
	// under.
	KeystoreService string
	// EnvCredential are the environment variable names read at use time,
	// the first one decisive (absent means not configured).
	EnvCredential []string
	Regions       Regions
	Tiers         map[vocab.Tier]TierMapping
	LifetimeCaps  []PlanCap
	MemoryImages  MemoryImagePolicy
	Price         Price
	// RelayStdin: the vendor documents a second process with a streaming
	// stdin, so the relay runs over its own exec stream (exec_stdio).
	RelayStdin bool
	// GuestPTY: the vendor documents no PTY resize, so the box-side guest
	// helper owns the PTY and applies sizes sent on the relay's control
	// stream.
	GuestPTY bool
	// Reattach: the vendor documents reconnecting to a running process.
	Reattach bool
	// GuestReattach (lane SBX-UPD): on a GuestPTY row the guest helper
	// keeps the agent's PTY in the box and a new exec of its attach verb
	// reconnects it, so a run whose home helper went is re-adopted without
	// a vendor reattach. Verification stays doc: whether the vendor keeps
	// the guest helper's process after its exec stream ends is a live check
	// (register REM-13). Held false on every row (SBX-UPD audit finding 2,
	// orchestrator ruling): the keeper-only attach key is built, and the flag
	// is turned on only once REM-13 is checked live; until then no run starts
	// a keeper and the agent ends with its stream, as before.
	GuestReattach bool
	// BoxHome is the writable directory the driver uses as HOME for every
	// process it starts in the box; the workspace, the in-box observer and
	// the relay files live under it.
	BoxHome string
	// Docs grounds every call the wire client makes.
	Docs []EndpointDoc
	// Unverified names what the docs do not state that matters here.
	Unverified []string
	// TemplateRule says what a template must be on this vendor ("" when any
	// image or template name the vendor lists will do); it is named in the
	// refusal of a launch that needs one and of a create the vendor refused
	// for its template.
	TemplateRule string
	// NetworkNotes are what a live run saw the vendor's own network do that
	// Observer does not carry and cannot stop (shown on the consent screen).
	NetworkNotes []string
	// MintedCredential (lane G4, the org-hosted row): the row has no stored
	// or environment credential. Every call carries a short-lived token the
	// org server mints for this member, node and run, through a
	// CredentialSource the hosted core hands the wire client
	// (Credential.Minted); the keystore and environment lookups and the
	// credential_missing probe row do not apply. The endpoint is the one the
	// org published (OrgEndpoint).
	MintedCredential bool
	// UploadViaExec (lane G4): the vendor has no file API, so the hosted
	// core writes files as bounded one-shot exec parts whose credit is each
	// part's exit (flow-controlled: never more than one part in flight).
	UploadViaExec bool
	// Deliveries are the workspace deliveries the row offers
	// (internal/workspace Delivery values); the hosted prepare step resolves
	// the run's source through workspace.DeliveryFor with them. Empty means
	// sync only, what the three file-API vendors do.
	Deliveries []string
	// OrgEndpoint: the API endpoint exists only as one the organization
	// published (front_url), never a default or a local value.
	OrgEndpoint bool
	// BoundedStdin (lane G4): the vendor ends an exec whose client outruns
	// its bounded stdin buffer (OpenShell 0.1.6, exit 74), so a streaming
	// stdin runs under the in-box paced-stdin wrapper (ExecSpec.PacedStdin)
	// and the relay argv is wrapped by it.
	BoundedStdin bool
	// BoxWorkspace and BoxRunDir are the box's fixed workspace and run dir
	// (lane G4 fix round 1, ruling A3: the org row's /sandbox/workspace and
	// /sandbox/run). "" keeps the hosted rule: the workspace at the laptop's
	// same path and the run dir under the box home. With a fixed workspace
	// the laptop's path never reaches the box, so a resume from another
	// machine uses the same paths. With a fixed run dir every box-side file
	// the node writes lives under it (OB-084): the in-box observer at
	// <run dir>/bin/observer, the upload parts, sessions.json, and the
	// guest's baseline.
	BoxWorkspace, BoxRunDir string
	// NoRootExec (ruling A3): the vendor runs every exec as the box user and
	// offers no root exec, so the core never asks for one and the wire
	// client refuses ExecSpec.AsRoot instead of dropping it.
	NoRootExec bool
	// ReadyWaitSeconds bounds the wait for a box the vendor's create (or a
	// resume) answered before it was ready: the hosted core polls Get with
	// backoff until the box runs, before its first exec (FX3 item 1, live
	// finding F-L3P4-1: OpenShell 0.1.6's CreateSandbox answers before the
	// sandbox reaches its ready phase). 0 is DefaultReadyWaitSeconds. A
	// vendor whose create answers a running box never waits.
	ReadyWaitSeconds int
	// OneShotStdinBytes is the most stdin one one-shot exec carries on an
	// UploadViaExec vendor (each upload part is one such exec); 0 keeps the
	// core's quarter-MiB default.
	OneShotStdinBytes int
	// NodeEndpoint (the SandHold row): the API endpoint is the node the user
	// connected this machine to, never a vendor domain or a default; the
	// credential is that node's own sign-in, resolved by the row's driver
	// registration (the hosted core's keystore and environment lookups do
	// not apply), and an environment credential is bound to the node its
	// environment names (SANDHOLD_URL), not to a default endpoint.
	NodeEndpoint bool
	// SeveralIsolations (the SandHold row): the vendor offers more than one
	// isolation, so a create asks for the class the launch needs
	// (BoxSpec.Isolation) and the wire client refuses, and deletes, a box
	// the vendor says got less.
	SeveralIsolations bool
}

// DefaultReadyWaitSeconds is the ready wait of a row that names none.
const DefaultReadyWaitSeconds = 180

// ReadyWait is the row's bounded wait for a box to become ready.
func (v Vendor) ReadyWait() time.Duration {
	if v.ReadyWaitSeconds <= 0 {
		return DefaultReadyWaitSeconds * time.Second
	}
	return time.Duration(v.ReadyWaitSeconds) * time.Second
}

// blockAll is the shared mapping text of the none and proxy_only tiers.
func blockAll(mech string, relay bool) TierMapping {
	return TierMapping{Mechanism: mech, Source: vocab.SourceDocumented, Relay: relay}
}

// vendors is the table.
var vendors = map[string]Vendor{
	VendorE2B: {
		ID: VendorE2B, Name: "E2B",
		DefaultEndpoint: "https://api.e2b.app",
		Domains:         []string{"e2b.app", "e2b.dev"},
		KeystoreService: "sbo-sandbox-e2b-v1",
		EnvCredential:   []string{"E2B_API_KEY"},
		// Only the default deployment is grounded in E2B's own documentation
		// (the API reference's server, api.e2b.app). Other regional
		// endpoints are not listed until each has an E2B doc page (the
		// SBX-P7 merge's audit finding 1).
		Regions: Regions{
			Kind: RegionByEndpoint, Default: "us-west-1",
			Known:     []string{"us-west-1"},
			Endpoints: map[string]string{"us-west-1": "https://api.e2b.app"},
			Docs:      regionDocs("https://docs.e2b.dev/api-reference/sandboxes", "us-west-1"),
		},
		Tiers: map[vocab.Tier]TierMapping{
			vocab.TierNone:      blockAll("allow_internet_access = false (deny 0.0.0.0/0); the relay carries only the hook stream", false),
			vocab.TierProxyOnly: blockAll("allow_internet_access = false; the relay carries the hook stream and the model proxy", true),
			// E2B documents that private and link-local ranges (169.254/16
			// included) are refused outside the guest for every sandbox and
			// cannot be disabled (docs.e2b.dev/network/restrict-public-access),
			// but the live run of 2026-10-23 got an answer (HTTP 401) from
			// 169.254.169.254 inside the box on proxy_only and internet, so
			// the claim is not taken as shown: claimed, and disclosed
			// (NetworkNotes).
			vocab.TierInternet: {Mechanism: "the default allow-all egress plus the relay for the model proxy", Source: vocab.SourceClaimed, Relay: true},
		},
		LifetimeCaps: []PlanCap{{"Hobby", 60}, {"Pro", 1440}},
		MemoryImages: MemoryImagePolicy{AutoPauseExists: true, CanDisable: true, Note: "autoPause is false by default and the driver sends false"},
		Price: Price{
			USDPerHour: 0.1656, Size: "2 vCPU, 4 GiB (the pricing page default; docs also name 2 vCPU and 512 MiB)",
			Date: "2026-10-22", Source: "https://e2b.dev/pricing and https://docs.e2b.dev/faq/calculate-sandbox-price",
		},
		RelayStdin: true, GuestPTY: false, Reattach: true, BoxHome: "/home/user",
		Docs: []EndpointDoc{
			{"create", "POST", "/v2/sandboxes", "https://docs.e2b.dev/api-reference/sandboxes; spec/openapi.yml (NewSandboxV2) at e2b-dev/E2B db8d80dad5be", ""},
			{"get", "GET", "/sandboxes/{sandboxID}", "spec/openapi.yml (SandboxDetail)", ""},
			{"list", "GET", "/v2/sandboxes?metadata=", "spec/openapi.yml (ListedSandbox, X-Next-Token)", ""},
			{"delete", "DELETE", "/sandboxes/{sandboxID}", "spec/openapi.yml", ""},
			{"set timeout", "POST", "/sandboxes/{sandboxID}/timeout", "spec/openapi.yml (SandboxTimeoutRequest)", "what a timeout over the plan cap does"},
			{"upload", "POST", "/files?path=&username= (envd, sandbox.<domain>)", "spec/envd/envd.yaml; docs.e2b.dev/openapi-public.yaml", ""},
			{"download", "GET", "/files?path= (envd)", "spec/envd/envd.yaml", ""},
			{"start", "POST", "/process.Process/Start (Connect, server stream)", "spec/envd/process/process.proto; docs.e2b.dev/openapi-public.yaml", "the EndEvent.status vocabulary"},
			{"connect", "POST", "/process.Process/Connect (Connect, server stream)", "spec/envd/process/process.proto", ""},
			{"send input", "POST", "/process.Process/SendInput", "spec/envd/process/process.proto", ""},
			{"resize", "POST", "/process.Process/Update", "spec/envd/process/process.proto", ""},
			{"signal", "POST", "/process.Process/SendSignal", "spec/envd/process/process.proto", ""},
		},
		Unverified: []string{
			"the per-sandbox envd host form 49983-<id>.<domain> (the driver uses the documented sandbox.<domain> with E2b-Sandbox-Id and E2b-Sandbox-Port)",
			"the Keepalive-Ping-Interval header name (behaviour documented, name SDK-only; the driver does not send it)",
			"rate limits of the non-list endpoints and of envd",
			"the refusal of link-local and metadata addresses the docs state (169.254.169.254 answered inside a box in the 2026-10-23 live run)",
		},
		NetworkNotes: []string{
			"in the 2026-10-23 live run the metadata address 169.254.169.254 answered (HTTP 401) inside an E2B box on the proxy_only and internet tiers; that traffic goes through E2B's own network, which Observer does not carry and cannot stop (Observer's relay carries only the hook stream and the model proxy, and refuses link-local and metadata addresses for what it carries)",
		},
	},
	VendorModal: {
		ID: VendorModal, Name: "Modal",
		DefaultEndpoint: "https://api.modal.com:443",
		Domains:         []string{"modal.com"},
		KeystoreService: "sbo-sandbox-modal-v1",
		EnvCredential:   []string{"MODAL_TOKEN_ID", "MODAL_TOKEN_SECRET"},
		Regions: Regions{
			Kind: RegionSelectable, Default: ProviderChosen,
			Known: []string{"us", "eu", "ap", "us-east", "us-central", "us-south", "us-west", "eu-west", "eu-north", "eu-south", "ap-northeast", "ap-southeast", "ap-south", "ap-melbourne", "jp", "au", "uk", "ca", "me", "sa", "af", "mx"},
			Docs:  regionDocs("https://modal.com/docs/guide/region-selection", "us", "eu", "ap", "us-east", "us-central", "us-south", "us-west", "eu-west", "eu-north", "eu-south", "ap-northeast", "ap-southeast", "ap-south", "ap-melbourne", "jp", "au", "uk", "ca", "me", "sa", "af", "mx"),
		},
		Tiers: map[vocab.Tier]TierMapping{
			vocab.TierNone:      blockAll("network_access BLOCKED (block_network); the relay carries only the hook stream", false),
			vocab.TierProxyOnly: blockAll("network_access BLOCKED; the relay carries the hook stream and the model proxy", true),
			// Modal documents outbound access to "any public IP address" but
			// not refusing the metadata endpoint or private ranges: claimed.
			vocab.TierInternet: {Mechanism: "the default open egress plus the relay for the model proxy", Source: vocab.SourceClaimed, Relay: true},
		},
		LifetimeCaps: []PlanCap{{"all plans", 1440}},
		MemoryImages: MemoryImagePolicy{AutoPauseExists: false, Note: "no auto-pause; memory snapshots are opt-in and never requested"},
		Price: Price{
			USDPerHour: 0.19, Size: "1 physical core, 2 GiB (sandbox rates, before any region multiplier)",
			Date: "2026-10-22", Source: "https://modal.com/pricing",
		},
		RelayStdin: true, GuestPTY: true, Reattach: false, GuestReattach: false, BoxHome: "/root",
		Docs: []EndpointDoc{
			{"client", "gRPC", "modal.client.ModalClient at api.modal.com:443 through github.com/modal-labs/modal-client/go v0.11.1 (Apache-2.0)", "https://modal.com/docs/guide/sandboxes; https://github.com/modal-labs/modal-client/tree/main/go", "the gRPC API itself is declared non-public; only the SDK is used"},
			{"app", "SDK", "Apps.FromName(CreateIfMissing)", "modal-client/go app.go", ""},
			{"create", "SDK", "Sandboxes.Create(Timeout, BlockNetwork, Regions, Tags, Runtime gvisor)", "https://modal.com/docs/guide/sandboxes; modal-client/go sandbox.go", ""},
			{"exec", "SDK", "Sandbox.Exec (stdin and stdout pipes, stderr ignored; no PTY flag used)", "modal-client/go sandbox.go", ""},
			{"files", "SDK", "Sandbox.Filesystem.WriteBytes / ReadBytes", "modal-client/go sandbox_fs.go", ""},
			{"list", "SDK", "Sandboxes.List(Tags)", "modal-client/go sandbox.go", ""},
			{"terminate", "SDK", "Sandbox.Terminate", "modal-client/go sandbox.go", ""},
		},
		Unverified: []string{
			"the default region when none is set (recorded as provider chosen)",
			"whether a sandbox keeps the guest helper's process after its exec stream ends (the guest PTY reattach, register REM-13)",
			"refusal of the metadata endpoint and private ranges on the open tier",
			"numeric rate limits",
		},
	},
	VendorVercel: {
		ID: VendorVercel, Name: "Vercel Sandbox",
		DefaultEndpoint: "https://api.vercel.com",
		Domains:         []string{"vercel.com"},
		KeystoreService: "sbo-sandbox-vercel-v1",
		EnvCredential:   []string{"VERCEL_TOKEN", "VERCEL_TEAM_ID", "VERCEL_PROJECT_ID"},
		Regions: Regions{
			Kind: RegionSelectable, Default: "iad1",
			Known: []string{"arn1", "bom1", "cdg1", "cle1", "cpt1", "dub1", "fra1", "gru1", "hkg1", "hnd1", "iad1", "icn1", "kix1", "lhr1", "pdx1", "sfo1", "sin1", "syd1", "yul1"},
			Docs:  regionDocs("https://vercel.com/docs/regions", "arn1", "bom1", "cdg1", "cle1", "cpt1", "dub1", "fra1", "gru1", "hkg1", "hnd1", "iad1", "icn1", "kix1", "lhr1", "pdx1", "sfo1", "sin1", "syd1", "yul1"),
		},
		Tiers: map[vocab.Tier]TierMapping{
			vocab.TierNone:      blockAll("networkPolicy deny-all (DNS included); the relay carries only the hook stream", false),
			vocab.TierProxyOnly: blockAll("networkPolicy deny-all; the relay carries the hook stream and the model proxy", true),
			// The firewall docs do not say whether metadata or private ranges
			// are refused under allow-all: claimed.
			vocab.TierInternet: {Mechanism: "networkPolicy allow-all plus the relay for the model proxy", Source: vocab.SourceClaimed, Relay: true},
		},
		LifetimeCaps: []PlanCap{{"Hobby", 45}, {"Pro", 1440}, {"Enterprise", 1440}},
		MemoryImages: MemoryImagePolicy{AutoPauseExists: false, Note: "no live pause; the filesystem snapshot on stop is switched off with persistent = false"},
		Price: Price{
			USDPerHour: 0.17, Size: "2 vCPU, 4 GB at full CPU in iad1 (active CPU is billed, so most runs cost less)",
			Date: "2026-10-22", Source: "https://vercel.com/docs/sandbox/pricing",
		},
		RelayStdin: true, GuestPTY: true, Reattach: false, GuestReattach: false, BoxHome: "/vercel/sandbox",
		Docs: []EndpointDoc{
			{"create", "POST", "/v3/sandboxes?teamId= (projectId, name, image, timeout ms, persistent false, networkPolicy, region, tags in the body; no ports)", "https://vercel.com/docs/rest-api/sandboxes/create-a-named-sandbox-1; openapi.vercel.sh createSandboxesV3", "the shape of tags (sent as an object of strings, matching the list filter's key:value)"},
			{"get", "GET", "/v2/sandboxes/{name}?teamId=&projectId= (never resume=true)", "https://vercel.com/docs/rest-api/sandboxes/get-a-named-sandbox", ""},
			{"list", "GET", "/v2/sandboxes?teamId=&project=&tags=key:value&limit=&cursor=", "https://vercel.com/docs/rest-api/sandboxes/list-sandboxes", ""},
			{"delete", "DELETE", "/v2/sandboxes/{name}?teamId=&projectId=&deleteOrphanSnapshots=true", "https://vercel.com/docs/rest-api/sandboxes/delete-a-sandbox", ""},
			{"run command", "POST", "/v2/sandboxes/sessions/{sessionId}/cmd?teamId= (attachStdin)", "https://vercel.com/docs/rest-api/sandboxes/execute-a-command", ""},
			{"logs", "GET", "/v2/sandboxes/sessions/{sessionId}/cmd/{cmdId}/logs?teamId= (NDJSON)", "https://vercel.com/docs/rest-api/sandboxes/stream-command-logs", "whether a re-request replays from the start (SDK behaviour only)"},
			{"stdin", "POST", "/v2/sandboxes/sessions/{sessionId}/cmd/{cmdId}/stdin?teamId= (base64, offset)", "https://vercel.com/docs/rest-api/sandboxes/write-to-command-stdin", ""},
			{"wait", "GET", "/v2/sandboxes/sessions/{sessionId}/cmd/{cmdId}?teamId=&wait=true", "https://vercel.com/docs/rest-api/sandboxes/get-a-command", ""},
			{"kill", "POST", "/v2/sandboxes/sessions/{sessionId}/cmd/{cmdId}/kill?teamId=", "https://vercel.com/docs/rest-api/sandboxes/kill-a-command", ""},
			{"write files", "POST", "/v2/sandboxes/sessions/{sessionId}/fs/write?teamId= (application/gzip tar, x-cwd /)", "https://vercel.com/docs/rest-api/sandboxes/write-files", ""},
			{"read file", "POST", "/v2/sandboxes/sessions/{sessionId}/fs/read?teamId=", "https://vercel.com/docs/rest-api/sandboxes/read-a-file", ""},
		},
		Unverified: []string{
			"the /interactive WebSocket PTY (SDK and CLI source only): not used; the guest helper owns the PTY instead",
			"refusal of the metadata endpoint and private ranges under allow-all",
			"whether every 429 carries Retry-After",
			"whether a sandbox keeps the guest helper's process after its command's log stream and stdin end (the guest PTY reattach, register REM-13)",
		},
		// The 2026-10-23 live run: the v3 create starts only an image the
		// team owns; a runtime name answered 404 "Image not found" and no
		// box was made, and the vendor default image holds no agent tool.
		// Observer does not build a team image (that would be a vendor
		// call outside a launch), so the none and proxy_only tiers, which
		// cannot install the tool at first start, need one named here.
		TemplateRule: "Vercel Sandbox's v3 create starts only an image your Vercel team owns (a public runtime name such as node22 answers 404 Image not found); for the none and proxy_only tiers, make a team image that holds the agent tool with Vercel's own tooling and name it in [terminal.sandbox.hosted.vercel].template, or use the internet tier, where the tool installs at first start",
		// Decision (SBX-P7 merge, audit finding 10): Vercel's REST API takes
		// teamId and projectId in the query string, which the vendor requires.
		// They are account and project identifiers, not secrets; the token
		// stays in the Authorization header and never enters a URL.
	},
	// The org-hosted OpenShell gateway (g1-design.md sections 3 and 4). Every
	// fact is the design of record's and the 0.1.6 live gate's (G-L1 and
	// G-L2, decision log OB-077 to OB-079), never a third party's docs.
	VendorOpenShellOrg: {
		ID: VendorOpenShellOrg, Name: "OpenShell (org-hosted)",
		// No default endpoint: the front URL is the org's, published in its
		// signed body (OrgEndpoint).
		KeystoreService: "sbo-openshell-org-grant-v1",
		// RegionByEndpoint with no endpoint: the region a run records is the
		// org's own label, carried beside the published endpoint.
		Regions: Regions{Kind: RegionByEndpoint, Default: RegionOrgPublished, Known: []string{RegionOrgPublished}},
		Tiers: map[vocab.Tier]TierMapping{
			vocab.TierNone:      blockAll("an OpenShell policy with no network rule (the org gateway's supervisor proxy denies every connection); the relay carries only the hook stream", false),
			vocab.TierProxyOnly: blockAll("an OpenShell policy with no network rule; the relay carries the hook stream and the model proxy", true),
			// The org-compiled network rule: the org's hosts narrowed by the
			// node's, private ranges removed, the org's allow_cidrs added and
			// its blocked_cidrs subtracted last (section 6.1).
			vocab.TierInternet: {Mechanism: "the org-compiled OpenShell network rule (org hosts, private ranges removed, blocked_cidrs subtracted last) plus the relay for the model proxy", Source: vocab.SourceDocumented, Relay: true},
		},
		// The row's static cap (7200 s); the org's max_run_minutes lowers it
		// at launch and the front and the reaper enforce it.
		LifetimeCaps: []PlanCap{{"the organization's gateway", 120}},
		MemoryImages: MemoryImagePolicy{AutoPauseExists: false, Note: "no suspend: OpenShell 0.1.6 drivers do not implement it (a held box is a stopped box)"},
		RelayStdin:   true, GuestPTY: false, Reattach: false, BoxHome: "/home/sandbox",
		Docs: []EndpointDoc{
			{"create", "gRPC", "openshell.v1.OpenShell/CreateSandbox through the org's front (the front creates the spec the org compiled at mint)", "docs/plans/openshell/build/g1-design.md section 3.2 step 5", ""},
			{"get", "gRPC", "openshell.v1.OpenShell/GetSandbox", "g1-design.md section 7.6 row F3", ""},
			{"delete", "gRPC", "openshell.v1.OpenShell/DeleteSandbox (a teardown token only)", "g1-design.md section 7.6 row F6", ""},
			{"exec", "gRPC", "openshell.v1.OpenShell/ExecSandbox (one-shot, stdin in the request)", "g1-design.md section 3.3", ""},
			{"interactive", "gRPC", "openshell.v1.OpenShell/ExecSandboxInteractive (bidi; TTY for the agent, TTY off for the relay)", "g1-design.md section 3.3; live gate G-L2 verdict tty_false (OB-079)", ""},
			{"health", "gRPC", "grpc.health.v1.Health/Check (service superbased.openshell.front)", "g1-design.md section 7.6 row F1", ""},
		},
		Unverified: []string{
			"the front's answers through a real org deployment (live checks G-L3 and G-L6)",
		},
		NetworkNotes: []string{
			"the box's own network refusals stay on your organization's machine and are not reported to this machine in this release",
		},
		MintedCredential: true, UploadViaExec: true, OrgEndpoint: true, BoundedStdin: true,
		// The org's fixed box paths (internal/openshellorg compile.go
		// BoxWorkspace and BoxRunDir; a test pins them equal).
		BoxWorkspace: "/sandbox/workspace", BoxRunDir: "/sandbox/run", NoRootExec: true,
		Deliveries: []string{"sync", "remote_clone"},
		// The 0.1.6 create answers before the box is ready (the gateway
		// provisions the container after it answers, then the supervisor
		// dials back; G-L3 run 3 saw 0.3 s to over 1 s): the core waits up to
		// this long, polling the front's GetSandbox (a read, never one of the
		// gateway's admitted mutations).
		ReadyWaitSeconds: 180,
		// internal/openshellorg MaxOneShotStdin (a test pins them equal): the
		// gateway's 1 MiB gRPC decode limit (MAX_GRPC_DECODE_SIZE in the
		// 0.1.6 gateway's multiplex.rs) less 64 KiB for the rest of the
		// request (FX4 item 2, F-P6-2; FX3 had assumed 4 MiB).
		OneShotStdinBytes: 1<<20 - 64<<10,
	},
	// The SandHold row (SandHold notes 41 D2 to D5). Every fact is
	// SandHold's own control/README.md and its API handlers
	// (control/internal/api), read 2026-11-11, never a third party's docs.
	// Built and tested against the node's customer contract vectors and an
	// httptest fake; no live run yet (register SANDHOLD-1).
	VendorSandHold: {
		ID: VendorSandHold, Name: "SandHold",
		// No default endpoint and no vendor domain: the endpoint is the node
		// this machine is connected to (NodeEndpoint).
		KeystoreService: "sbo-sandbox-sandhold-v1",
		// The field name the resolved key is kept under (the key comes from
		// the node's sign-in or SANDHOLD_API_KEY, SANDHOLD_URL naming the
		// node).
		EnvCredential: []string{"SANDHOLD_API_KEY"},
		Regions:       Regions{Kind: RegionByEndpoint, Default: RegionSandHoldNode, Known: []string{RegionSandHoldNode}},
		Tiers: map[vocab.Tier]TierMapping{
			// Every sandbox is deny-by-default (README "Network"); the create
			// sends no allow list, and a box the node reports with any
			// network but none is refused and deleted.
			vocab.TierNone:      blockAll("no network.allow (the node's deny-by-default egress); the relay carries only the hook stream", false),
			vocab.TierProxyOnly: blockAll("no network.allow (the node's deny-by-default egress); the relay carries the hook stream and the model proxy", true),
			// network.open is honoured only by a target that cannot enforce
			// an allow list; elsewhere the box stays deny-by-default (the
			// node's reply says which). Claimed: the host guards (metadata,
			// private ranges, SMTP) are the node's words, not checked here.
			vocab.TierInternet: {Mechanism: "network.open (an open network where the target offers one) plus the relay for the model proxy", Source: vocab.SourceClaimed, Relay: true},
		},
		// The node's own maximum timeout caps a box (its refusal says the
		// number); the row names none.
		MemoryImages: MemoryImagePolicy{AutoPauseExists: false, Note: "the native API has no pause or snapshot of a live sandbox"},
		// The PTY is the node's terminal WebSocket (a resize control frame);
		// the relay's second stream is the same socket's pipe mode (exact
		// bytes, stderr apart, a stdin end of its own), or a second terminal
		// session put in raw mode on a Shared sandbox or a node from before
		// the pipe mode. No reattach: the node ends a terminal session when
		// its socket goes.
		RelayStdin: true, GuestPTY: false, Reattach: false,
		// The node runs every command as its sandbox user, which has no fixed
		// home path; /workspace is made and handed to that user at create.
		BoxHome: "/tmp/observer-home", BoxWorkspace: "/workspace", NoRootExec: true,
		Docs: []EndpointDoc{
			{"create", "POST", "/v1/sandboxes (template, timeoutSeconds, metadata, isolation, network.open)", "SandHold control/README.md; control/internal/api/api.go createSandbox; contract sandbox-create, sandbox-create-refused, sandbox-create-open-not-granted", "an open network not given comes back as networkNote (network_open_not_granted), which the run says"},
			{"get", "GET", "/v1/sandboxes/{id}", "api.go getSandbox; contract sandbox-get, sandbox-missing", ""},
			{"list", "GET", "/v1/sandboxes?metadata=observer-run=<run id>", "api.go listSandboxes; contract sandbox-list, sandbox-list-by-metadata", "a node from before the metadata filter lists every sandbox; that is detected and reported as not supported"},
			{"delete", "DELETE", "/v1/sandboxes/{id}", "api.go deleteSandbox; contract sandbox-delete", ""},
			{"exec", "POST", "/v1/sandboxes/{id}/exec (Accept: text/event-stream; stdinBase64 and output base64 for a one-shot stdin)", "api.go exec, exec_stream.go, exec_pipe.go; contract exec-stream, exec-bytes-stream", "without a stdin the output arrives as text lines; with one, as its exact bytes (a Shared sandbox or an older node refuses it: not supported)"},
			{"upload", "PUT", "/v1/sandboxes/{id}/files?path=&mode= (raw body, 100 MiB at most)", "api.go putFile", "no contract vector"},
			{"download", "GET", "/v1/sandboxes/{id}/files?path=", "api.go getFile", "no contract vector"},
			{"terminal", "GET (WebSocket)", "/v1/sandboxes/{id}/pty?command=[&mode=pipe] (binary frames stdin and stdout, a resize text frame; the pipe's eof frame and tagged output; the exit frame last)", "api.go pty, exec_pipe.go ptyPipe; contract terminal, terminal-pipe", "a node from before 2026-11-11 sends no exit frame and has no pipe mode"},
		},
		Unverified: []string{
			"every call against a real node (register SANDHOLD-1: no live run yet)",
			"whether /tmp in the node's image lets the uploaded observer run (a noexec /tmp would stop it)",
			"the exit status of a terminal session on a node from before the exit frame (the command's wrapper writes it to a file the client reads)",
			"whether a cancelled streamed exec ends the command in the box",
		},
		ReadyWaitSeconds: 60,
		NodeEndpoint:     true, SeveralIsolations: true,
	},
}

// VendorFor returns the vendor of a registry row id.
func VendorFor(id string) (Vendor, bool) {
	v, ok := vendors[id]
	return v, ok
}

// VendorIDs returns the vendor ids, sorted.
func VendorIDs() []string {
	out := make([]string, 0, len(vendors))
	for id := range vendors {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// TierList returns the tiers the vendor can express, most open first (host is
// never one: unsatisfiable remotely).
func (v Vendor) TierList() []vocab.Tier {
	var out []vocab.Tier
	for _, t := range vocab.Tiers() {
		if m, ok := v.Tiers[t]; ok && m.Mechanism != "" {
			out = append(out, t)
		}
	}
	return out
}

// MinLifetimeMinutes is the shortest plan cap the vendor documents (the
// registry row's LifetimeCapSeconds, S13).
func (v Vendor) MinLifetimeMinutes() int {
	m := 0
	for _, c := range v.LifetimeCaps {
		if m == 0 || c.Minutes < m {
			m = c.Minutes
		}
	}
	return m
}

// ResolveRegion returns the region a run records and the API endpoint it
// uses, for the configured region ("" means the vendor default). ok is false
// for a region the vendor does not list.
func (v Vendor) ResolveRegion(configured string) (region, endpoint string, ok bool) {
	r := strings.TrimSpace(configured)
	if r == "" {
		r = v.Regions.Default
	}
	if r != ProviderChosen && !contains(v.Regions.Known, r) {
		return "", "", false
	}
	endpoint = v.DefaultEndpoint
	if v.Regions.Kind == RegionByEndpoint {
		endpoint = v.Regions.Endpoints[r]
	}
	return r, endpoint, true
}

// RegionOrgPublished is the region a run records when it uses an
// org-published endpoint (SBX-GOV-2).
const RegionOrgPublished = "org-published"

// RegionCustomEndpoint is the region a run records for a configured
// endpoint on the vendor's own domain that no listed region names.
const RegionCustomEndpoint = "custom-endpoint"

// ResolveEndpoint is ResolveRegion with a configured API endpoint (E2B's
// endpoint key, lane SBX-UPD): "" is the region's own endpoint. Only a
// vendor whose region IS its endpoint takes one. A local endpoint must be on
// the vendor's own domain (the P-090 F1 rule); only an org-published one
// (orgPublished, set by the managed pull on a managed node) may leave it,
// and a credential is still sent there only when it was connected for it
// (CheckBinding, SBX-GOV-4). ok is false otherwise.
func (v Vendor) ResolveEndpoint(configuredRegion, endpoint string, orgPublished bool) (region, ep string, ok bool) {
	endpoint = strings.TrimSuffix(strings.TrimSpace(endpoint), "/")
	if v.NodeEndpoint {
		return resolveNodeEndpoint(endpoint)
	}
	if v.OrgEndpoint {
		// Only the organization's published endpoint, never a default or a
		// local value; the region is the org's own label (section 8.2).
		return v.resolveOrgEndpoint(configuredRegion, endpoint, orgPublished)
	}
	if endpoint == "" {
		return v.ResolveRegion(configuredRegion)
	}
	if v.Regions.Kind != RegionByEndpoint {
		return "", "", false
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || (u.Path != "" && u.Path != "/") {
		return "", "", false
	}
	if orgPublished {
		return RegionOrgPublished, endpoint, true
	}
	if !v.OwnsHost(u.Hostname()) {
		return "", "", false
	}
	for r, e := range v.Regions.Endpoints {
		if strings.EqualFold(strings.TrimSuffix(e, "/"), endpoint) {
			return r, endpoint, true
		}
	}
	return RegionCustomEndpoint, endpoint, true
}

// resolveOrgEndpoint is ResolveEndpoint for an OrgEndpoint row: an https
// origin the org published, recorded with the org's region label (empty is
// kept empty, so an allowed_regions floor refuses it, S7b).
func (v Vendor) resolveOrgEndpoint(region, endpoint string, orgPublished bool) (string, string, bool) {
	if !orgPublished || endpoint == "" {
		return "", "", false
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || (u.Path != "" && u.Path != "/") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(endpoint, "?#") {
		return "", "", false
	}
	return strings.TrimSpace(region), endpoint, true
}

// RegionSandHoldNode is the region a SandHold run records: the node this
// machine is connected to (its address is the run's endpoint).
const RegionSandHoldNode = "your-node"

// resolveNodeEndpoint is ResolveEndpoint for a NodeEndpoint row: the
// connected node's address, https, or http for this machine only (a key
// travels on every call), with no user, query or fragment.
func resolveNodeEndpoint(endpoint string) (string, string, bool) {
	if endpoint == "" {
		return "", "", false
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(endpoint, "?#") {
		return "", "", false
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !loopbackHost(u.Hostname()) {
			return "", "", false
		}
	default:
		return "", "", false
	}
	return RegionSandHoldNode, endpoint, true
}

// loopbackHost reports localhost or a loopback address, without a resolver
// (this package does no I/O).
func loopbackHost(h string) bool {
	h = strings.ToLower(h)
	if h == "localhost" || h == "::1" {
		return true
	}
	parts := strings.Split(h, ".")
	if len(parts) != 4 || parts[0] != "127" {
		return false
	}
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 255 {
			return false
		}
	}
	return true
}

// OwnsHost reports whether host is one of the vendor's own domains or under
// one.
func (v Vendor) OwnsHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, d := range v.Domains {
		d = strings.ToLower(d)
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

// Estimate is a labelled cost estimate.
type Estimate struct {
	Minutes int     `json:"minutes"`
	USD     float64 `json:"usd"`
	Price   Price   `json:"price"`
}

// EstimateCost prices minutes of the default box size.
func (v Vendor) EstimateCost(minutes int) Estimate {
	return Estimate{Minutes: minutes, USD: v.Price.USDPerHour * float64(minutes) / 60, Price: v.Price}
}

// Label renders the estimate with its source and date, always marked an
// estimate. no-bare-usd:allow: a remote sandbox vendor's compute list price
// (box-minutes), not model spend, so the token display has no equivalent.
func (e Estimate) Label() string {
	return "about $" + strconv.FormatFloat(e.USD, 'f', 2, 64) + " for " + strconv.Itoa(e.Minutes) +
		" box-minutes (estimate: " + e.Price.Size + ", list price read " + e.Price.Date + "; the vendor's bill is the truth)"
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// BoxLayout is where the driver puts things in a box.
type BoxLayout struct {
	// Home is HOME for every process the driver starts.
	Home string
	// Workspace is the workspace's path in the box: the SAME absolute path
	// as on the host (the bind_same_path rule of section 6), so a hook's
	// cwd, a transcript's project root and the relay's run roots agree with
	// no translation. The driver creates it as root and hands it to the box
	// user.
	Workspace string
	Binary    string
	// RunDir holds the relay's per-run files (the mux secret).
	RunDir string
}

// Layout returns the box paths for a workspace at hostRoot (an absolute,
// cleaned slash path).
func (v Vendor) Layout(hostRoot string) BoxLayout {
	root := v.BoxHome + "/.observer-box"
	l := BoxLayout{Home: v.BoxHome, Workspace: hostRoot, Binary: root + "/bin/observer", RunDir: root + "/run"}
	if v.BoxWorkspace != "" {
		l.Workspace = v.BoxWorkspace
	}
	if v.BoxRunDir != "" {
		l.RunDir = v.BoxRunDir
		l.Binary = v.BoxRunDir + "/bin/observer"
	}
	return l
}
