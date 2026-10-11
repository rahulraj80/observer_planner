// Package agentconfigorg is the pure logic of the organization's AI tool
// settings policy (adapter config phases P6 and P7, backlog item 8;
// docs/agent-config.md "Organization policy"; plan
// docs/plans/adapter-config-and-common-vocabulary-plan-2026-10-03.md
// sections 3 and 6).
//
// Three halves, all pure (no os, database/sql, net/http or fsnotify; pinned
// by imports_test.go):
//
//   - Spec + Lint: one org-authored policy document (settings by catalogue id
//     or common key, mode, project, placement, and the org knobs) and the
//     publish-time sanity check, which runs the SAME catalogue checks a
//     developer's save runs and refuses anything a node would refuse, plus
//     every secret (secrets are never distributed).
//   - Compose: the server-side composition of the org-wide, team and member
//     documents that address one member into the ONE signed body that member's
//     managed node receives (orgcontract.AgentConfigPolicyBody), through the
//     agentconfig precedence resolver (D-122 / D-123: most specific wins,
//     ordered-danger keys strictest-wins, conflicting teams need a priority).
//   - Decide: the node-side decision table for one target file of one
//     directive (apply, re-assert, report drift, back off after three drifts
//     in an hour, D-101), and the closed applied-state vocabulary the managed
//     node reports to the org (P7).
package agentconfigorg
