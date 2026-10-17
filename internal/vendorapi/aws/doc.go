// Package aws holds the pure drivers of the Amazon Web Services surfaces
// (enterprise vendor API plan, Phase 8; lane ENTAPI-L16D, T-713). Kiro has no
// public admin API: its organization data reaches an AWS admin through AWS
// account services (CloudTrail, Cost Explorer, CUR 2.0, IAM Identity Center),
// each signed with AWS Signature Version 4 in the egress seam.
//
// The first driver reads CloudTrail Event history (LookupEvents, the AWS JSON
// 1.1 protocol) for the Amazon Q Developer event source q.amazonaws.com, the
// documented source of Amazon Q Developer and Kiro subscription activity. The
// body is the closed vendorapi.CloudTrailLookup; the seam's reviewed POST row
// fixes the content type and X-Amz-Target and its signing row fixes the
// Region and signing name. Like every driver here the package is pure: no SQL,
// no HTTP, no file system (imports_test.go). Nothing here was observed from a
// live response; every surface stays "synthetic, awaiting live validation".
package aws
