package nodecomponents

import "github.com/marmutapp/superbased-observer/internal/components"

// LegacyUpdateState is what a node's update state means before it records
// any distro (plan 5.6.1, fourth-pass M8): every pre-modular binary is the
// full build, its components digest is taken from the running binary at
// first start, and its distro epoch is zero. Lane MP-REL's agent migration
// backfills exactly these values and its tests compare against this value,
// so the meaning has one owner.
type LegacyUpdateState struct {
	Distro          components.Distro
	DistroEpoch     int64
	DigestFromBuild bool
}

// LegacyDefaults returns the backfill values.
func LegacyDefaults() LegacyUpdateState {
	return LegacyUpdateState{Distro: components.DistroFull, DistroEpoch: 0, DigestFromBuild: true}
}

// LegacyFullFallbackAllowed answers the new-node / old-server cell of the
// update negotiation (plan 5.6.1): when the org server has no distro-aware
// manifest, a node may take the v1 artifact (always the full build) only
// when it already runs full, the update is a same-distro update, and its
// ceiling (when pinned) permits every component of the full build.
// Otherwise it refuses with posture reason server_lacks_distro_updates.
func LegacyFullFallbackAllowed(running components.Distro, ceilingPinned bool, ceiling []string) bool {
	if running != components.DistroFull {
		return false
	}
	if !ceilingPinned {
		return true
	}
	allowed := map[string]bool{}
	for _, id := range ceiling {
		allowed[id] = true
	}
	for _, id := range components.InDistro(components.DistroFull) {
		c, ok := components.Get(id)
		if ok && c.Class == components.ClassCore {
			continue
		}
		if !allowed[id] {
			return false
		}
	}
	return true
}

// ReasonServerLacksDistroUpdates is the posture reason for a refused
// legacy fallback (plan 5.6.1).
const ReasonServerLacksDistroUpdates = "server_lacks_distro_updates"
