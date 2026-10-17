package orgintel

import (
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/cloudcontract"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

func TestSweepFor(t *testing.T) {
	off := false
	on := true
	def := Sweep{On: true, Quiet: DefaultSweepQuiet, MinActions: DefaultSweepMinActions, MaxAge: DefaultSweepMaxAge}
	cases := []struct {
		name string
		req  *orgcontract.IntelRequirementBody
		want Sweep
	}{
		{"no terms keeps the defaults", nil, def},
		{"an older server keeps the defaults", &orgcontract.IntelRequirementBody{}, def},
		{"on demand only", &orgcontract.IntelRequirementBody{Background: &off}, Sweep{Quiet: DefaultSweepQuiet, MinActions: DefaultSweepMinActions, MaxAge: DefaultSweepMaxAge}},
		{
			"the org's bounds", &orgcontract.IntelRequirementBody{Background: &on, QuietMinutes: 45, MinActions: 9, MaxAgeDays: 2},
			Sweep{On: true, Quiet: 45 * time.Minute, MinActions: 9, MaxAge: 48 * time.Hour},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SweepFor(tc.req); got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestEvidenceSettingsFor(t *testing.T) {
	if EvidenceSettingsFor(nil, cloudcontract.EvidenceLevelExcerpts) != nil ||
		EvidenceSettingsFor(&orgcontract.IntelRequirementBody{}, cloudcontract.EvidenceLevelExcerpts) != nil {
		t.Fatal("no org selection must keep the builder's defaults (nil)")
	}
	es := cloudcontract.DefaultEvidenceSettings()
	es.UserMessages, es.AssistantMessages, es.FailureClasses = 6, 4, 2
	req := &orgcontract.IntelRequirementBody{EvidenceSettings: &es}
	if got := EvidenceSettingsFor(req, cloudcontract.EvidenceLevelExcerpts); got == nil || *got != es {
		t.Fatalf("excerpts narrowed the org selection: %+v", got)
	}
	titles := EvidenceSettingsFor(req, cloudcontract.EvidenceLevelTitles)
	if titles == nil || titles.UserMessages != 1 || titles.AssistantMessages != 0 || titles.FailureClasses != 0 {
		t.Fatalf("titles = %+v", titles)
	}
	structural := EvidenceSettingsFor(req, cloudcontract.EvidenceLevelStructural)
	if structural == nil || structural.UserMessages+structural.AssistantMessages+structural.FailureClasses != 0 || structural.Validate() != nil {
		t.Fatalf("structural = %+v", structural)
	}
}
