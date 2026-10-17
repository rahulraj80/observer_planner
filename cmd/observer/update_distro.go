//go:build !sbo_omit_org_update

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/managedconfig"
	"github.com/marmutapp/superbased-observer/internal/policyfam/nodecomponents"
	"github.com/marmutapp/superbased-observer/internal/update"
)

// update_distro.go is the cmd boundary of the distro-aware update rail
// (modular packaging plan 5.6 / 5.6.1, lane MP-REL2). It reads what only I/O
// can (the running binary's own distro, the local component ceiling with its
// last-known set, the verified node.components policy in force, the epoch the
// last apply recorded) and hands update.Verify plain data through
// orgclient.SetUpdateDistroSource. Every decision is update.Verify's rule
// 10-12 or resolveUpdateDistroContext below, which is pure.

// updateDistroInputs is everything the distro context is computed from.
type updateDistroInputs struct {
	// Running is the distro this binary was built as ("" = full, the
	// unstamped build).
	Running components.Distro
	// Ceiling is the local ceiling loader's verdict.
	Ceiling managedconfig.Result
	// PolicyDistro is the distro the node.components policy in force names
	// ("" when no policy governs or it names none).
	PolicyDistro string
	// Epoch is the distro epoch the last apply recorded on the channel.
	Epoch int64
}

// resolveUpdateDistroContext is the pure half: the context update.Verify
// receives. The target is the policy's distro, else the running one (an
// individual node never switches distro through the org rail; Q3's opt-in
// switch is a separate, vendor-signed path). A transition is allowed only
// when every component the target ADDS is within the pinned ceiling (plan
// 5.6: "a transition that adds compiled components is subject to the local
// ceiling"); with no ceiling the policy's naming of the target is the
// allowance.
func resolveUpdateDistroContext(in updateDistroInputs) update.DistroContext {
	running := update.NormalizeDistro(string(in.Running))
	pinned, ceiling := in.Ceiling.Pinned()
	dc := update.DistroContext{
		Aware:             true,
		Current:           running,
		Target:            running,
		Epoch:             in.Epoch,
		UpdatesAllowed:    in.Ceiling.UpdatesAllowed,
		LegacyFullAllowed: nodecomponents.LegacyFullFallbackAllowed(components.Distro(running), pinned, ceiling),
	}
	if t := strings.TrimSpace(in.PolicyDistro); t != "" && components.Distro(t).Valid() {
		dc.Target = t
		dc.TargetAllowed = distroAdditionsWithinCeiling(components.Distro(running), components.Distro(t), pinned, ceiling)
	}
	return dc
}

// distroAdditionsWithinCeiling reports whether every non-core component the
// target distro compiles in and the running one does not is within the
// pinned ceiling. Without a ceiling there is no local limit.
func distroAdditionsWithinCeiling(running, target components.Distro, pinned bool, ceiling []string) bool {
	if !pinned {
		return true
	}
	allowed := map[string]bool{}
	for _, id := range ceiling {
		allowed[id] = true
	}
	have := map[string]bool{}
	for _, id := range components.InDistro(running) {
		have[id] = true
	}
	for _, id := range components.InDistro(target) {
		if have[id] || allowed[id] {
			continue
		}
		if c, ok := components.Get(id); ok && c.Class == components.ClassCore {
			continue
		}
		return false
	}
	return true
}

// runningUpdateDistro is the distro the update rail selects artifacts for: a
// prebuilt stamp, else a custom build's custom distro id
// (custom-<manifest digest>, plan 5.3, lane MP-CUST), else "" (full). A
// custom binary whose stamp does not resolve reports its id as the empty
// manifest's, so it matches no artifact and every update refuses (fail
// closed) instead of reading as full.
func runningUpdateDistro() components.Distro {
	return runningUpdateDistroFrom(components.BuildDistro, components.BuildCustom)
}

// runningUpdateDistroFrom is runningUpdateDistro with its two reads injected.
func runningUpdateDistroFrom(prebuilt func() components.Distro, custom func() (components.CustomBuild, bool, error)) components.Distro {
	if d := prebuilt(); d != "" {
		return d
	}
	cb, stamped, err := custom()
	switch {
	case !stamped:
		return ""
	case err != nil:
		return components.Distro(update.CustomDistro(customStampErrorDigest))
	}
	return components.Distro(update.CustomDistro(cb.ManifestDigest))
}

// customStampErrorDigest is the custom distro id a binary with an unusable
// stamp reports to the update rail: 64 zeros, a manifest digest no vendor
// index can name, so no artifact is ever selected.
const customStampErrorDigest = "0000000000000000000000000000000000000000000000000000000000000000"

// distroContext loads the inputs for one fetch. It runs in the daemon on the
// nudge path, so it reads the already-open store and the same signed inputs
// componentGovernanceFor reads; a read error resolves to the legacy defaults
// (full, epoch 0) with the ceiling still applied, the fail-closed direction
// for every boolean update.DistroContext carries.
func (u *updateRuntime) distroContext(ctx context.Context, channel update.Channel) update.DistroContext {
	in := updateDistroInputs{Running: runningUpdateDistro()}
	gin := componentGovernanceInputs{Now: time.Now()}
	if u == nil || u.store == nil {
		in.Ceiling = loadCeilingWithLastKnown(ctx, managedconfig.Load, nil)
		return resolveUpdateDistroContext(in)
	}
	in.Ceiling = loadCeilingWithLastKnown(ctx, managedconfig.Load, u.store)
	gin.Ceiling = in.Ceiling
	_ = loadComponentGovernanceInputs(ctx, u.cfg, u.store, &gin)
	if out := nodeComponentsOutcome(gin); out.Spec != nil {
		in.PolicyDistro = out.Spec.Distro
	}
	if row, err := u.store.LoadUpdateState(ctx); err == nil {
		in.Epoch = row.DistroEpoch(channel)
	}
	return resolveUpdateDistroContext(in)
}

// applyDistroRefusal re-applies verify rule 12 at apply time: a transition
// lands only to the distro the node should run now, with the policy and
// ceiling allowance, under an epoch ahead of the one recorded. An apply can
// run from a manifest accepted before any of those moved, and a rule that
// guards only the fetch is not a rule (the update_apply.go downgrade gate's
// reasoning).
func applyDistroRefusal(dc update.DistroContext, transition bool, art update.Artifact, man update.Manifest) update.Reason {
	if !transition {
		return ""
	}
	if update.NormalizeDistro(art.Distro) != update.NormalizeDistro(dc.Target) || !dc.TargetAllowed {
		return update.ReasonDistroNotAllowed
	}
	if man.DistroEpoch <= dc.Epoch {
		return update.ReasonDistroEpochStale
	}
	return ""
}

// probeBinaryComponentsDigest asks a staged binary for its own compiled-set
// digest (`components list --json`, which reads the binary alone and never
// opens the database). The apply compares it with the signed manifest's
// components_digest before the swap, so an archive whose binary is not the
// distro the vendor index claims is refused while the running binary is
// still untouched (plan 5.6: the self-check comparison, done pre-swap).
func probeBinaryComponentsDigest(ctx context.Context, binPath string) (string, error) {
	pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(pctx, binPath, "components", "list", "--json") //nolint:gosec // a staged artifact this node hash- and signature-verified
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("components list: %w", err)
	}
	var rep struct {
		ComponentsDigest string `json:"components_digest"`
	}
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil || strings.TrimSpace(rep.ComponentsDigest) == "" {
		return "", fmt.Errorf("components list: no components_digest in the report")
	}
	return strings.TrimSpace(rep.ComponentsDigest), nil
}
