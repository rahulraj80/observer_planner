//go:build sbo_omit_cloud_intelligence

package main

import (
	"context"
	"errors"

	"github.com/marmutapp/superbased-observer/internal/cloudcontract"
	"github.com/marmutapp/superbased-observer/internal/intelligence/cost"
	"github.com/marmutapp/superbased-observer/internal/orgintel"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// intelsubmit_omit.go stands in for intelsubmit.go in a build without
// cloud.intelligence (-tags sbo_omit_cloud_intelligence, lane MP-L1I,
// T-715, ruling D-512). The node-built org-plane evidence uses the
// cloudevidence builder, which is not linked, so this build never builds or
// queues an envelope: a developer preview or submit answers that
// intelligence is not in this build, and the org client gets no background
// submit source. The org intel consent and grant rails (core) are unchanged.

// errOrgIntelNotInBuild is the refusal every envelope path returns here.
var errOrgIntelNotInBuild = errors.New("org session enrichment is not available on this machine: Cloud Intelligence (cloud.intelligence) is not in this build")

// buildOrgIntelEnvelope refuses: no envelope is ever built in this build.
func buildOrgIntelEnvelope(context.Context, *store.Store, string, string, orgintel.SubmitPlan, *cloudcontract.EvidenceSettings, *cost.Engine) (orgIntelBuilt, error) {
	return orgIntelBuilt{}, errOrgIntelNotInBuild
}

// queueOrgIntelSubmission refuses: nothing reaches it, since no envelope is
// built, and it never queues one.
func queueOrgIntelSubmission(context.Context, *store.Store, orgIntelBuilt, string, string) (store.OrgIntelSubmission, bool, error) {
	return store.OrgIntelSubmission{}, false, errOrgIntelNotInBuild
}

// orgIntelSubmitSource is nil: the org client skips the background submit
// cycle (orgclient.Client runs a source only when one is set).
func orgIntelSubmitSource(*store.Store, *liveOrgEnrichment, func() bool, *cost.Engine) func(ctx context.Context) error {
	return nil
}
