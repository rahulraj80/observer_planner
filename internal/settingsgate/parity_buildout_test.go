package settingsgate

import (
	"bufio"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/configschema"
	"github.com/marmutapp/superbased-observer/internal/testsupport/publictree"
)

// schemaBaselineFile freezes the machine schema's leaf paths at the commit
// before the Plane A build-out (fc5ce34e86). The build-out's machine keys are
// DERIVED from it (review E-52): every current schema leaf not in the
// baseline. The hand list this replaces had already missed
// observability.alerts.pagerduty_routing_key (lane C9).
const schemaBaselineFile = "testdata/schema-leaves-before-buildout.txt"

// notBuildOutKeys are leaves added after the baseline by work outside the
// Plane A build-out (a merge from main), each with its reason. A key here is
// outside principle 10's Plane A scope on purpose.
var notBuildOutKeys = map[string]string{
	"licenses.licenses":                                     "this fork's license usage records (Licenses page), not a Plane A build-out key",
	"llm.connections":                                       "this fork's local LLM connections for analysis (Settings > LLM connections card), not a Plane A build-out key",
	"telemetry.batch_interval_seconds":                      "this fork's usage telemetry rail (PRIVACY.md), not a Plane A build-out key; set in config.toml or with `observer telemetry consent`",
	"telemetry.enabled":                                     "this fork's usage telemetry rail (PRIVACY.md), not a Plane A build-out key; set in config.toml or with `observer telemetry consent`",
	"telemetry.endpoint":                                    "this fork's usage telemetry rail (PRIVACY.md), not a Plane A build-out key; set in config.toml or with `observer telemetry consent`",
	"telemetry.install_token_file":                          "this fork's usage telemetry rail (PRIVACY.md), not a Plane A build-out key; set in config.toml or with `observer telemetry consent`",
	"telemetry.max_batch_bytes":                             "this fork's usage telemetry rail (PRIVACY.md), not a Plane A build-out key; set in config.toml or with `observer telemetry consent`",
	"telemetry.tier":                                        "this fork's usage telemetry rail (PRIVACY.md), not a Plane A build-out key; set in config.toml or with `observer telemetry consent`",
	"terminal.launch.auto_confirm_agent":                    "this fork's owner switch for agent launch confirmation (off by default), not a Plane A build-out key",
	"terminal.sandbox.hosted.sandhold.allow_private_ranges": "this fork's SandHold vendor block; its node is the one `observer sandbox connect` signed in to, not a Plane A build-out key",
	"terminal.sandbox.hosted.sandhold.box_binary":           "this fork's SandHold vendor block; its node is the one `observer sandbox connect` signed in to, not a Plane A build-out key",
	"terminal.sandbox.hosted.sandhold.endpoint":             "this fork's SandHold vendor block; its node is the one `observer sandbox connect` signed in to, not a Plane A build-out key",
	"terminal.sandbox.hosted.sandhold.endpoint_spki_sha256": "this fork's SandHold vendor block; its node is the one `observer sandbox connect` signed in to, not a Plane A build-out key",
	"terminal.sandbox.hosted.sandhold.region":               "this fork's SandHold vendor block; its node is the one `observer sandbox connect` signed in to, not a Plane A build-out key",
	"terminal.sandbox.hosted.sandhold.template":             "this fork's SandHold vendor block; its node is the one `observer sandbox connect` signed in to, not a Plane A build-out key",
}

// buildOutMachineKeys derives the build-out's machine keys from the schema:
// every current leaf that is not in the frozen baseline.
func buildOutMachineKeys(t *testing.T) []string {
	t.Helper()
	f, err := os.Open(schemaBaselineFile)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	before := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" && !strings.HasPrefix(line, "#") {
			before[line] = true
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(before) < 500 {
		t.Fatalf("%s holds %d leaves; the baseline is incomplete", schemaBaselineFile, len(before))
	}
	var out []string
	for _, l := range configschema.Leaves() {
		if !before[l.Path] {
			out = append(out, l.Path)
		}
	}
	sort.Strings(out)
	return out
}

// orgFileOnlyKeys are the org server keys the build-out added that are
// edited in the org config file only, each with its reason (principle 10's
// org-side allow-list; the org config is outside the machine schema, so it
// is checked against the generated org key inventory).
var orgFileOnlyKeys = map[string]string{
	"gateway.public_url": "the address members' SDKs are told to send to; a deployment fact the operator sets with the gateway listener and the TLS terminator, not a dashboard choice (lane B6)",
}

// TestTheParityScopeCoversEveryBuildOutKey: each machine key the build-out
// added (derived from the schema, not listed by hand) is inside the parity
// gate's scope, so TestEveryPlaneAKeyIsReachableFromUICLIAndMCP checks it,
// or is named in notBuildOutKeys with a reason. A new key under another
// block fails here until isPlaneAKey covers it (review R-B28, E-52).
func TestTheParityScopeCoversEveryBuildOutKey(t *testing.T) {
	derived := buildOutMachineKeys(t)
	have := map[string]bool{}
	for _, k := range derived {
		have[k] = true
		if why, ok := notBuildOutKeys[k]; ok {
			if strings.TrimSpace(why) == "" {
				t.Errorf("%s is excepted with no reason", k)
			}
			continue
		}
		l, _ := configschema.Lookup(k)
		if !isPlaneAKey(l) {
			t.Errorf("build-out key %s is outside the parity gate's scope (isPlaneAKey); widen the scope or name it in notBuildOutKeys with a reason", k)
		}
	}
	for k := range notBuildOutKeys {
		if !have[k] {
			t.Errorf("notBuildOutKeys names %s, which is not a key added after the baseline", k)
		}
	}
	// The keys the build-out is known to have added, the one the old hand
	// list missed included: the derivation must find each.
	for _, k := range []string{
		"observability.alerts.pagerduty_routing_key",
		"observability.alerts.slack_webhook_url",
		"observability.sdk.destination",
		"observability.sdk.gateway_url",
		"observability.store_content",
	} {
		if !have[k] {
			t.Errorf("the derived build-out key set misses %s", k)
		}
	}
}

// TestACardRenderedLeafIsClaimedByItsCard narrows "UI" from "the section id
// appears in Settings.tsx" to "a control renders it": the generic section
// renders every writable leaf except the field paths a bespoke card claims,
// so each claimed path must be a real leaf of the section and must be named
// by the card's own source.
func TestACardRenderedLeafIsClaimedByItsCard(t *testing.T) {
	src, err := os.ReadFile("../../web/src/pages/settings/SdkDestinationCard.tsx")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`SDK_DESTINATION_FIELDS = \[([^\]]*)\]`).FindSubmatch(src)
	if m == nil {
		t.Fatal("SDK_DESTINATION_FIELDS not found in SdkDestinationCard.tsx")
	}
	byField := map[string]configschema.Leaf{}
	for _, l := range configschema.Leaves() {
		byField[l.FieldPath] = l
	}
	for _, q := range regexp.MustCompile(`"([A-Za-z.]+)"`).FindAllSubmatch(m[1], -1) {
		fp := string(q[1])
		l, ok := byField[fp]
		if !ok || l.Section != "observability" {
			t.Errorf("the SDK destination card claims %s, which is no observability leaf; the generic section would hide a key nothing renders", fp)
			continue
		}
		last := fp[strings.LastIndex(fp, ".")+1:]
		if strings.Count(string(src), last) < 2 {
			t.Errorf("the SDK destination card claims %s but its source never renders %s", fp, last)
		}
	}
}

// TestOrgFileOnlyKeysExist: every org-side exception names a key in the
// generated org key inventory, so a renamed key cannot leave a stale reason.
func TestOrgFileOnlyKeysExist(t *testing.T) {
	publictree.Skip(t, "the org server config inventory (internal/orgserver/config/keyclass_inventory.tsv)")
	f, err := os.Open("../orgserver/config/keyclass_inventory.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	have := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := sc.Text(); line != "" && !strings.HasPrefix(line, "#") {
			have[strings.SplitN(line, "\t", 2)[0]] = true
		}
	}
	for k, why := range orgFileOnlyKeys {
		if !have[k] {
			t.Errorf("org exception %s names no org config key", k)
		}
		if strings.TrimSpace(why) == "" {
			t.Errorf("org exception %s has no reason", k)
		}
	}
}
