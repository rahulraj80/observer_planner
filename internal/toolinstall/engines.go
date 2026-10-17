package toolinstall

import (
	"regexp"
	"strconv"
	"strings"
)

// engines.go reads an npm `engines` range as a whole (review M3-18): every
// `||` alternative, every comparator inside it, and the strictness of each
// operator. A range is a union of half-open version intervals [lo, hi) over
// major.minor.patch; pre-release and build suffixes are ignored, the same way
// ParseRuntimeVersion ignores them on the interpreter side.

// enginesComparatorAt matches one comparator at the start of the remaining
// text of an alternative: an optional operator, then a version whose
// components may be missing or a wildcard (x, X, *), then an ignored
// pre-release / build suffix. Comparators may be glued (">=24.16.0<25").
var enginesComparatorAt = regexp.MustCompile(`^\s*(>=|<=|>|<|\^|~|=)?\s*v?([0-9]+|[xX*])(?:\.([0-9]+|[xX*]))?(?:\.([0-9]+|[xX*]))?(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?`)

// enginesHyphen matches a hyphen range "1.2.3 - 2.3.4" (spaces required, as
// npm requires them).
var enginesHyphen = regexp.MustCompile(`^\s*(\S+)\s+-\s+(\S+)\s*$`)

// enginesInterval is one alternative: versions v with lo <= v < hi, either
// bound absent when its has-flag is false.
type enginesInterval struct {
	lo, hi       [3]int
	hasLo, hasHi bool
}

// contains reports whether v lies in the interval.
func (iv enginesInterval) contains(v [3]int) bool {
	if iv.hasLo && versionLess(v, iv.lo) {
		return false
	}
	if iv.hasHi && !versionLess(v, iv.hi) {
		return false
	}
	return true
}

// intersect narrows iv by other.
func (iv enginesInterval) intersect(other enginesInterval) enginesInterval {
	if other.hasLo && (!iv.hasLo || versionLess(iv.lo, other.lo)) {
		iv.lo, iv.hasLo = other.lo, true
	}
	if other.hasHi && (!iv.hasHi || versionLess(other.hi, iv.hi)) {
		iv.hi, iv.hasHi = other.hi, true
	}
	return iv
}

// bumpAt increments component i of v and zeroes the ones after it.
func bumpAt(v [3]int, i int) [3]int {
	v[i]++
	for j := i + 1; j < 3; j++ {
		v[j] = 0
	}
	return v
}

// enginesVersion is one parsed partial version: the zero-filled triple and
// how many leading components were numeric (0 = a wildcard major).
type enginesVersion struct {
	v [3]int
	n int
}

func parseEnginesVersion(parts []string) enginesVersion {
	var out enginesVersion
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			break // missing or wildcard: zero from here on
		}
		out.v[i] = n
		out.n = i + 1
	}
	return out
}

// upperOf is the exclusive upper bound of a partial version taken as an
// x-range ("20" -> 21.0.0, "20.1" -> 20.2.0, "20.1.2" -> 20.1.3).
func upperOf(ev enginesVersion) [3]int {
	return bumpAt(ev.v, ev.n-1)
}

// comparatorInterval turns one comparator into its interval, per npm's
// semver rules for partial versions.
func comparatorInterval(op string, ev enginesVersion) enginesInterval {
	if ev.n == 0 {
		return enginesInterval{} // "*", "x", ">=x": any version
	}
	switch op {
	case ">=":
		return enginesInterval{lo: ev.v, hasLo: true}
	case ">":
		return enginesInterval{lo: upperOf(ev), hasLo: true}
	case "<":
		return enginesInterval{hi: ev.v, hasHi: true}
	case "<=":
		return enginesInterval{hi: upperOf(ev), hasHi: true}
	case "~":
		hi := bumpAt(ev.v, 0)
		if ev.n >= 2 {
			hi = bumpAt(ev.v, 1)
		}
		return enginesInterval{lo: ev.v, hasLo: true, hi: hi, hasHi: true}
	case "^":
		var hi [3]int
		switch {
		case ev.v[0] != 0 || ev.n == 1:
			hi = bumpAt(ev.v, 0)
		case ev.v[1] != 0 || ev.n == 2:
			hi = bumpAt(ev.v, 1)
		default:
			hi = bumpAt(ev.v, 2)
		}
		return enginesInterval{lo: ev.v, hasLo: true, hi: hi, hasHi: true}
	default: // "" or "=": an x-range
		return enginesInterval{lo: ev.v, hasLo: true, hi: upperOf(ev), hasHi: true}
	}
}

// parseEnginesAlternative reads one `||` alternative. ok is false when any
// part of it is not a comparator this reader understands.
func parseEnginesAlternative(alt string) (enginesInterval, bool) {
	alt = strings.TrimSpace(alt)
	if alt == "" {
		return enginesInterval{}, false
	}
	if m := enginesHyphen.FindStringSubmatch(alt); m != nil {
		a := enginesComparatorAt.FindStringSubmatch(m[1])
		b := enginesComparatorAt.FindStringSubmatch(m[2])
		if a == nil || b == nil || a[1] != "" || b[1] != "" || len(a[0]) != len(m[1]) || len(b[0]) != len(m[2]) {
			return enginesInterval{}, false
		}
		lo := comparatorInterval(">=", parseEnginesVersion(a[2:5]))
		hi := comparatorInterval("<=", parseEnginesVersion(b[2:5]))
		return lo.intersect(hi), true
	}
	var iv enginesInterval
	rest := alt
	for strings.TrimSpace(rest) != "" {
		m := enginesComparatorAt.FindStringSubmatch(rest)
		if m == nil {
			return enginesInterval{}, false
		}
		iv = iv.intersect(comparatorInterval(m[1], parseEnginesVersion(m[2:5])))
		rest = rest[len(m[0]):]
	}
	return iv, true
}

// parseEnginesRange reads a whole range into its alternatives. ok is false
// when the range is empty or any alternative does not parse.
func parseEnginesRange(rng string) ([]enginesInterval, bool) {
	rng = strings.TrimSpace(rng)
	if rng == "" {
		return nil, false
	}
	var out []enginesInterval
	for _, alt := range strings.Split(rng, "||") {
		iv, ok := parseEnginesAlternative(alt)
		if !ok {
			return nil, false
		}
		out = append(out, iv)
	}
	return out, true
}

// EnginesSatisfies reports whether version (as ParseRuntimeVersion reads it)
// lies in the npm engines range rng: inside at least one `||` alternative,
// meeting every comparator of it. ok is false when the range or the version
// cannot be read, in which case no verdict is given.
func EnginesSatisfies(rng, version string) (satisfied, ok bool) {
	alts, ok := parseEnginesRange(rng)
	if !ok {
		return false, false
	}
	v, vok := ParseRuntimeVersion(version)
	if !vok {
		return false, false
	}
	for _, iv := range alts {
		if iv.contains(v) {
			return true, true
		}
	}
	return false, true
}

// dirDependentShimMarkers are path-segment sequences that identify a
// version-manager shim: an executable that picks the real interpreter from
// the directory it runs in (.tool-versions, .node-version, package.json
// "volta", ...), so `node --version` answers for the current directory only.
// Grounded from each manager's documented shim location: asdf
// (~/.asdf/shims), mise and rtx (~/.local/share/mise/shims), nodenv
// (~/.nodenv/shims) all use a "shims" directory; Volta puts its shims in
// ~/.volta/bin. nvm and fnm switch PATH per shell instead and are not shims.
var dirDependentShimMarkers = [][]string{
	{"shims"},
	{".volta", "bin"},
}

// DirDependentInterpreter reports whether the interpreter at path is a
// version-manager shim whose version depends on the working directory.
func DirDependentInterpreter(path string) bool {
	segs := strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' })
	if len(segs) < 2 {
		return false
	}
	dirs := segs[:len(segs)-1]
	for _, marker := range dirDependentShimMarkers {
		if len(dirs) < len(marker) {
			continue
		}
		tail := dirs[len(dirs)-len(marker):]
		match := true
		for i := range marker {
			if !strings.EqualFold(tail[i], marker[i]) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
