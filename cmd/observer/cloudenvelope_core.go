package main

import "github.com/marmutapp/superbased-observer/internal/cloudcontract"

// cloudenvelope_core.go holds the envelope result types the org intel rail
// (intelactions.go) and the Cloud Intelligence wiring (cloud.go,
// intelsubmit.go) share. They speak only internal/cloudcontract, which is
// core (operator ruling D-512: the org wire speaks it), so they stay in
// every build; a build without cloud.intelligence (-tags
// sbo_omit_cloud_intelligence) never fills them (intelsubmit_omit.go).

// cloudEnvelopeResult is the output of buildCloudEnvelope: the exact bytes a
// node previews AND uploads, their two digests, and a content-free summary.
type cloudEnvelopeResult struct {
	EvidenceSettingsJSON string
	Bytes                []byte
	Digests              cloudcontract.Digests
	CloudSession         string
	Actions              int
	Excerpts             int
	Purpose              cloudcontract.Purpose
	// Envelope is the built envelope these bytes were serialized from, so a
	// surface can summarize it without re-parsing the bytes. It is node-local
	// and is never a second serialization: Bytes above stays the one truth.
	Envelope *cloudcontract.Envelope
}

// orgIntelBuilt is one built org-plane envelope.
type orgIntelBuilt struct {
	OrgID   string
	Level   cloudcontract.EvidenceLevel
	Purpose cloudcontract.Purpose
	Result  cloudEnvelopeResult
}
