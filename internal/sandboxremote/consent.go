package sandboxremote

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// consent.go is the per-provider consent record (sandbox plan v2 sections
// 5.7 and 7.2): before the first upload the user sees what leaves the
// machine and agrees, per provider. The record is node-local and owner-only
// (the cmd layer writes it); a change to any fact that decides what leaves
// the machine or what it costs invalidates it, so the next launch asks
// again.

// ConsentVersion moves with every change to the record's shape or to the
// set of facts it binds.
const ConsentVersion = 1

// ConsentFacts are what the user agreed to. Each field is shown on the
// consent screen; every one binds the record.
type ConsentFacts struct {
	// Provider is the vendor id (VendorE2B, ...).
	Provider string `json:"provider"`
	// Endpoint is the API origin the credential and the upload go to.
	Endpoint string `json:"endpoint"`
	// Region is the region the box runs in ("provider chosen" when the
	// vendor cannot pin one).
	Region string `json:"region"`
	// Custody is where the model credential sits (section 7.2).
	Custody string `json:"custody"`
	// Tier is the egress tier and Capture the capture level it computes.
	Tier    string `json:"tier"`
	Capture string `json:"capture"`
	// ScopeKey names the upload scope (Manifest.ScopeKey).
	ScopeKey string `json:"scope_key"`
	// BoxBinary names the in-box observer (its version and where it came
	// from: the running binary or box_binary).
	BoxBinary string `json:"box_binary"`
	// MemoryImages is the [terminal.sandbox.hosted].memory_images value.
	MemoryImages string `json:"memory_images"`
	// MaxRunMinutes and DailyBoxMinutes are the spend caps.
	MaxRunMinutes   int `json:"max_run_minutes"`
	DailyBoxMinutes int `json:"daily_box_minutes"`
}

// ConsentRecord is the stored agreement.
type ConsentRecord struct {
	Version int          `json:"version"`
	Facts   ConsentFacts `json:"facts"`
	// At is when the user agreed (RFC 3339, set by the cmd layer).
	At string `json:"at"`
	// Digest is ConsentDigest(Facts), so a hand-edited fact fails.
	Digest string `json:"digest"`
}

// The closed consent verdicts.
const (
	ConsentOK            = "ok"
	ConsentMissing       = "consent_required"
	ConsentStaleVersion  = "consent_stale_version"
	ConsentTampered      = "consent_tampered"
	ConsentChangedPrefix = "consent_changed_"
)

// consentFields are the bound facts with the name a change reports, in the
// order they are compared (table-driven, CLAUDE.md rule 5).
var consentFields = []struct {
	name string
	get  func(ConsentFacts) string
}{
	{"provider", func(f ConsentFacts) string { return f.Provider }},
	{"endpoint", func(f ConsentFacts) string { return f.Endpoint }},
	{"region", func(f ConsentFacts) string { return f.Region }},
	{"custody", func(f ConsentFacts) string { return f.Custody }},
	{"tier", func(f ConsentFacts) string { return f.Tier }},
	{"capture", func(f ConsentFacts) string { return f.Capture }},
	{"scope", func(f ConsentFacts) string { return f.ScopeKey }},
	{"box_binary", func(f ConsentFacts) string { return f.BoxBinary }},
	{"memory_images", func(f ConsentFacts) string { return f.MemoryImages }},
	{"cap", func(f ConsentFacts) string { return fmt.Sprintf("%d/%d", f.MaxRunMinutes, f.DailyBoxMinutes) }},
}

// ConsentDigest hashes the facts in a fixed order.
func ConsentDigest(f ConsentFacts) string {
	h := sha256.New()
	fmt.Fprintf(h, "v%d", ConsentVersion)
	for _, c := range consentFields {
		fmt.Fprintf(h, "\x00%s=%s", c.name, c.get(f))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// NewConsent builds the record the user agreed to at.
func NewConsent(f ConsentFacts, at string) ConsentRecord {
	return ConsentRecord{Version: ConsentVersion, Facts: f, At: at, Digest: ConsentDigest(f)}
}

// CheckConsent compares a stored record (nil when none) with the facts of
// the launch now: ConsentOK, or the verdict naming what changed.
func CheckConsent(rec *ConsentRecord, now ConsentFacts) string {
	switch {
	case rec == nil:
		return ConsentMissing
	case rec.Version != ConsentVersion:
		return ConsentStaleVersion
	case rec.Digest != ConsentDigest(rec.Facts):
		return ConsentTampered
	}
	for _, c := range consentFields {
		if c.get(rec.Facts) != c.get(now) {
			return ConsentChangedPrefix + c.name
		}
	}
	return ConsentOK
}

// DecodeConsent parses a stored record strictly; a record that does not
// parse is treated as missing by the caller.
func DecodeConsent(b []byte) (ConsentRecord, error) {
	var rec ConsentRecord
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rec); err != nil {
		return ConsentRecord{}, fmt.Errorf("sandboxremote.DecodeConsent: %w", err)
	}
	return rec, nil
}

// ConsentLine is one line of the consent screen.
type ConsentLine struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// ConsentScreen is what the panel and `observer sandbox hosted consent`
// show before the user agrees.
type ConsentScreen struct {
	Provider string        `json:"provider"`
	Lines    []ConsentLine `json:"lines"`
	// Excluded lists the credential-shaped and other excluded files.
	Excluded []Exclusion `json:"excluded"`
	// Uploaded names the files that are sent, the first ConsentFileList of
	// them in manifest order; UploadedMore counts the rest (live run
	// 2026-10-23, finding F11: the screen gave only a count).
	Uploaded     []string     `json:"uploaded"`
	UploadedMore int          `json:"uploaded_more"`
	Facts        ConsentFacts `json:"facts"`
}

// ConsentFileList caps the file names the consent screen lists.
const ConsentFileList = 50

// remoteClonePrefix opens the consent scope of a box-side clone.
const remoteClonePrefix = "remote_clone:"

// RemoteCloneScopeKey is the consent scope of a box-side clone of url (a
// remote_clone delivery): the one owner the launch checks and every consent
// surface records (FX3 item 3, live finding F-L6P4-1), so the two never
// differ. The URL is kept exactly as the launch carries it.
func RemoteCloneScopeKey(url string) string { return remoteClonePrefix + url }

// RemoteCloneURL reads the clone URL out of a remote-clone scope key (ok
// false for any other scope).
func RemoteCloneURL(scope string) (string, bool) {
	url, ok := strings.CutPrefix(scope, remoteClonePrefix)
	return url, ok && url != ""
}

// BuildConsentScreen renders the facts, the manifest, the vendor row and an
// estimate into the screen's lines (the same lines on every surface).
func BuildConsentScreen(v Vendor, f ConsentFacts, m Manifest, estimate Estimate) ConsentScreen {
	captureLine := f.Capture
	if f.Capture != "full" {
		captureLine += " (guard advisory for anything judged inside the box)"
	}
	lines := []ConsentLine{
		{"Provider", v.Name + " (" + f.Endpoint + ")"},
		{"Where the box runs", whereLine(v, f.Region)},
		{"What is uploaded", m.Summary() + " from " + m.Root + " (git tracked and untracked-not-ignored files; never your home directory, never .git history)"},
		{"In-box observer", f.BoxBinary},
		{"Model credential", custodyLine(f.Custody)},
		{"Network tier", f.Tier},
		{"Capture", captureLine},
		{"Memory images", memoryLine(v, f.MemoryImages)},
		{"Spend cap", fmt.Sprintf("each box stops after at most %d minutes, sooner when less is left of this machine's own cap of %d box-minutes a day on %s", f.MaxRunMinutes, f.DailyBoxMinutes, v.Name)},
		{"Estimated cost", costLine(v, estimate)},
	}
	if f.Region == RegionOrgPublished {
		// SBX-GOV-2: the endpoint is shown, and what it may not do.
		lines = append(lines, ConsentLine{"Endpoint", f.Endpoint + " was published by your organization: data only (it enables nothing by itself), and your credential is sent there only when you connected one for this endpoint"})
	}
	for _, n := range v.NetworkNotes {
		lines = append(lines, ConsentLine{"Outside Observer's control", n})
	}
	if url, ok := RemoteCloneURL(m.ScopeKey); ok {
		// A box-side clone uploads nothing from this machine; the URL is
		// shown in full, as the digest binds it.
		setConsentLine(lines, "What is uploaded", "nothing from this machine: the box clones "+url+" itself (the clone never runs here)")
	}
	scr := ConsentScreen{Provider: v.ID, Lines: lines, Excluded: m.Excluded, Facts: f, Uploaded: []string{}}
	for i, file := range m.Files {
		if i == ConsentFileList {
			scr.UploadedMore = len(m.Files) - i
			break
		}
		scr.Uploaded = append(scr.Uploaded, file.Path)
	}
	return scr
}

// setConsentLine replaces the value of the line with this label (FX3-A9:
// by label, never by position, so a line added above it cannot shift it).
func setConsentLine(lines []ConsentLine, label, value string) {
	for i := range lines {
		if lines[i].Label == label {
			lines[i].Value = value
			return
		}
	}
}

// whereLine names whose machines run the box (live finding F-L3-7): a
// vendor whose endpoint only an organization publishes (OrgEndpoint) runs
// on that organization's machines, never on a vendor named after the row.
func whereLine(v Vendor, region string) string {
	if v.OrgEndpoint {
		return "on your organization's machines, region " + region
	}
	return "on " + v.Name + "'s machines, region " + region
}

// costLine is the estimate line (live finding F-L3-7): a vendor with no
// recorded list price gets no estimate with empty fields; an
// organization-published one says the organization's compute is not billed
// to the user here.
func costLine(v Vendor, e Estimate) string {
	if e.Price.Date != "" || e.Price.USDPerHour != 0 {
		return e.Label()
	}
	if v.OrgEndpoint {
		return "no estimate: the box runs on your organization's compute, which it provides and pays for; your organization's quotas apply"
	}
	return "no estimate: no list price is recorded for " + v.Name
}

func custodyLine(c string) string {
	switch c {
	case "home_relay":
		return "stays on this machine; the home proxy adds it on the way out"
	case "in_box_api_key":
		return "an API key is placed in the box environment, readable by the agent and by anyone with access to the box; revoke it with its provider after the run if in doubt"
	case "in_box_oauth_seed":
		return "your subscription login (access and refresh token) is copied into the box; revoke the session with its vendor after the run if in doubt"
	case "provider_injected":
		return "the provider's secret broker adds the key on egress; token counts are box-reported"
	}
	return c
}

func memoryLine(v Vendor, policy string) string {
	switch {
	case !v.MemoryImages.AutoPauseExists:
		return v.Name + " takes no memory image of a running box (" + v.MemoryImages.Note + ")"
	case v.MemoryImages.CanDisable:
		return "auto-pause is switched off at create, so no memory image is taken (" + policy + ")"
	}
	return "this provider can take a memory image of the live box (" + policy + ")"
}
