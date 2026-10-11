package toolinstall

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/integration"
)

// runtime.go is the MINIMUM-RUNTIME table behind the guided install and every
// launcher's preflight: an npm-distributed CLI runs on the host's own `node`,
// and one that needs a newer Node.js than the host has installs fine and then
// crashes at startup (live 2026-10-04 on demo node 1: Gemini CLI 0.62.0 on
// Node.js 18.19.1 died with "SyntaxError: Invalid regular expression flags",
// a regex `v` flag that needs Node.js 20). The registry carries the grounded
// floor (integration.BinaryResolveSpec.Runtime, from the vendor's own
// engines.node); this file turns one observation of the host's interpreter
// into a closed status and the one plain sentence every surface shows.
//
// Pure: the caller resolves the interpreter and runs `<program> --version`
// itself (cmd/observer), and injects the output.

// RuntimeStatus is the closed result of CheckRuntime.
type RuntimeStatus string

const (
	// RuntimeOK: the interpreter meets the floor.
	RuntimeOK RuntimeStatus = "ok"
	// RuntimeTooOld: the interpreter is older than the floor. The only
	// blocking status.
	RuntimeTooOld RuntimeStatus = "too_old"
	// RuntimeUnknown: the version could not be read (probe failed, output not
	// a version, or a floor/runtime this table cannot compare). Never blocks:
	// an install or launch is not refused on a guess.
	RuntimeUnknown RuntimeStatus = "unknown"
	// RuntimeMissing: no interpreter resolves at all. Not blocking HERE: the
	// launch path already reports a missing `#!/usr/bin/env` interpreter
	// (toolresolve InterpreterMissing) and the install path the missing
	// installer program, each with its own sentence.
	RuntimeMissing RuntimeStatus = "missing"
	// RuntimeOutsideRange: the interpreter meets the floor, but the
	// requirement's whole engines range (RuntimeRequirement.Range) leaves this
	// version out, e.g. node 25 against ">=24.16.0 <25 || >=26.1.0" (review
	// M3-18). Reported, never blocking: the check may under-block, never
	// over-block, and npm itself only warns on an engines mismatch.
	RuntimeOutsideRange RuntimeStatus = "outside_range"
)

// RuntimeObservation is what the caller saw of the interpreter a launch or
// install would use.
type RuntimeObservation struct {
	// Path is the interpreter's absolute path; "" when none resolves.
	Path string
	// Output is the raw `<program> --version` output.
	Output string
	// ProbeErr is the error running the probe, if any.
	ProbeErr error
	// DirDependent is true when the interpreter is a version-manager shim
	// whose version depends on the directory it runs in, and the caller does
	// not know the directory the tool will run in (review M3-18). The probe
	// result then says nothing about the launch, so the check is not made.
	DirDependent bool
}

// RuntimeCheck is the evaluated requirement for one launch or install.
type RuntimeCheck struct {
	// Runtime is the requirement's runtime key ("node").
	Runtime string
	// Display is the runtime's product name ("Node.js").
	Display string
	// Product is the tool's product name ("Gemini CLI").
	Product string
	// Min is the floor as shown to people ("20", "22.19").
	Min string
	// Have is the interpreter's version ("18.19.1"); "" when unread.
	Have string
	// Path is the interpreter that was checked.
	Path string
	// Status is the closed verdict.
	Status RuntimeStatus
	// Remedy is the whole fix for a too_old status as plain text, "" otherwise:
	// Summary (with Path), then "Install a newer Node.js first:", then
	// FixCommand on its own indented line, then FixNote. One line per part,
	// so a terminal, `observer doctor` and an HTTP refusal show the command on
	// a line of its own. A UI renders the structured parts below instead.
	Remedy string
	// Summary is the short sentence for a too_old status: "Gemini CLI needs
	// Node.js 20 or newer. This machine has 18.19.1." (no path, no fix).
	Summary string
	// FixCommand is the grounded command that installs a newer runtime on this
	// host family; "" when the only grounded advice is prose (FixNote).
	FixCommand string
	// FixNote is the one-line context for FixCommand (which channel, why not
	// the distribution package), or the whole advice when FixCommand is "".
	FixNote string
	// FloorFrom says where Min came from: "installed" (the installed
	// package's own engines field) or "registry" (the grounded floor).
	FloorFrom string
	// Range is the whole engines range checked for an outside_range status
	// (">=24.16.0 <25 || >=26.1.0"); "" when only a floor is known.
	Range string
	// DirDependent marks an unknown status that is unknown because the
	// interpreter is a version-manager shim and the launch directory is not
	// known here; the launcher checks again in the directory it runs in.
	DirDependent bool
}

// Floor sources for RuntimeCheck.FloorFrom.
const (
	// FloorFromInstalled: the installed package's own engines range.
	FloorFromInstalled = "installed"
	// FloorFromRegistry: the registry's grounded floor.
	FloorFromRegistry = "registry"
)

// Blocking reports whether the install or launch must be refused.
func (c RuntimeCheck) Blocking() bool { return c.Status == RuntimeTooOld }

// runtimeRow is one runtime in the closed vocabulary.
type runtimeRow struct {
	display string
	// programs maps a GOOS to the executable spelling ("" key = every other
	// OS).
	programs map[string]string
	// hints maps a host family (PackageManager.Name, or "windows") to a
	// grounded way to install a newer release; "" is the fallback.
	hints map[string]runtimeHint
}

// runtimeHint is one grounded install route: the command (may be "") and its
// one-line note.
type runtimeHint struct {
	command string
	note    string
}

// runtimeTable is THE table. Hints grounded 2026-10-04: NodeSource's
// deb/rpm setup_lts.x scripts (both served 200; each pins NODE_VERSION="24.x",
// the current LTS line, nodejs.org/dist/index.json: v24.21.0 "Krypton"); the
// Homebrew `node` formula (homebrew/core, formulae.brew.sh/api/formula/node.json:
// node 26, current); winget's OpenJS.NodeJS.LTS (microsoft/winget-pkgs
// manifests/o/OpenJS/NodeJS/LTS). The Debian/Ubuntu distribution package is
// deliberately NOT suggested: Ubuntu 24.04 ships Node.js 18, which is exactly
// the too-old runtime this check exists for.
var runtimeTable = map[string]runtimeRow{
	integration.RuntimeNode: {
		display:  "Node.js",
		programs: map[string]string{"windows": "node.exe", "": "node"},
		hints: map[string]runtimeHint{
			"apt": {
				command: "curl -fsSL https://deb.nodesource.com/setup_lts.x | sudo -E bash - && sudo apt-get install -y nodejs",
				note:    "Node.js LTS from NodeSource (Debian/Ubuntu). The distribution's own nodejs package is too old on many releases.",
			},
			"dnf": {
				command: "curl -fsSL https://rpm.nodesource.com/setup_lts.x | sudo bash - && sudo dnf install -y nodejs",
				note:    "Node.js LTS from NodeSource (Fedora/RHEL).",
			},
			"brew":    {command: "brew install node", note: "Current Node.js from Homebrew (macOS)."},
			"windows": {command: "winget install OpenJS.NodeJS.LTS", note: "Node.js LTS from the Windows Package Manager."},
			"":        {note: "Download the LTS release from https://nodejs.org/en/download."},
		},
	},
}

// RuntimeProgram is the executable spelling of runtime on goos ("" when the
// runtime is outside the table).
func RuntimeProgram(runtime, goos string) string {
	row, ok := runtimeTable[runtime]
	if !ok {
		return ""
	}
	if p, ok := row.programs[goos]; ok {
		return p
	}
	return row.programs[""]
}

// RuntimeDisplay is runtime's product name ("Node.js"), or the key itself.
func RuntimeDisplay(runtime string) string {
	if row, ok := runtimeTable[runtime]; ok {
		return row.display
	}
	return runtime
}

// runtimeInstallHint picks the grounded install guidance for the host.
func runtimeInstallHint(row runtimeRow, goos string, pm PackageManager) runtimeHint {
	key := pm.Name
	if goos == "windows" {
		key = "windows"
	}
	if h, ok := row.hints[key]; ok {
		return h
	}
	return row.hints[""]
}

// ParseRuntimeVersion reads "v18.19.1", "20" or "v26.0.0-nightly..." into
// major/minor/patch. Missing components are zero; a pre-release or build
// suffix is ignored. ok is false when no leading number exists.
func ParseRuntimeVersion(s string) ([3]int, bool) {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimPrefix(strings.TrimPrefix(s, "v"), "V")
	if i := strings.IndexAny(s, "-+ "); i >= 0 {
		s = s[:i]
	}
	var out [3]int
	parts := strings.Split(s, ".")
	if len(parts) == 0 || parts[0] == "" {
		return out, false
	}
	for i := 0; i < len(parts) && i < 3; i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil || n < 0 {
			if i == 0 {
				return [3]int{}, false
			}
			break
		}
		out[i] = n
	}
	return out, true
}

func versionLess(a, b [3]int) bool {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

func versionString(v [3]int) string {
	return strconv.Itoa(v[0]) + "." + strconv.Itoa(v[1]) + "." + strconv.Itoa(v[2])
}

// DisplayRuntimeMin shows a floor without trailing zero components: "20.0.0" -> "20",
// "22.19.0" -> "22.19".
func DisplayRuntimeMin(min string) string {
	parts := strings.Split(strings.TrimSpace(min), ".")
	for len(parts) > 1 && parts[len(parts)-1] == "0" {
		parts = parts[:len(parts)-1]
	}
	return strings.Join(parts, ".")
}

// runtimeRule is one row of the ordered status table: first match wins.
type runtimeRule struct {
	status RuntimeStatus
	match  func(in runtimeInputs) bool
}

type runtimeInputs struct {
	known, found, probed, floorOK, haveOK bool
	dirDependent                          bool
	// rangeRead: the requirement's Range parsed; inRange: have lies in it.
	rangeRead, inRange bool
	have, floor        [3]int
}

var runtimeRules = []runtimeRule{
	// 1. A runtime outside the table cannot be compared.
	{RuntimeUnknown, func(in runtimeInputs) bool { return !in.known }},
	// 2. Nothing resolves: other sentences own this case.
	{RuntimeMissing, func(in runtimeInputs) bool { return !in.found }},
	// 2b. A version-manager shim probed outside the launch directory: its
	// answer is about some other directory, so nothing is claimed.
	{RuntimeUnknown, func(in runtimeInputs) bool { return in.dirDependent }},
	// 3. The probe failed or printed no version, or the floor does not parse.
	{RuntimeUnknown, func(in runtimeInputs) bool { return !in.probed || !in.haveOK || !in.floorOK }},
	// 4. Older than the floor.
	{RuntimeTooOld, func(in runtimeInputs) bool { return versionLess(in.have, in.floor) }},
	// 4b. Meets the floor but falls in a hole of the whole range.
	{RuntimeOutsideRange, func(in runtimeInputs) bool { return in.rangeRead && !in.inRange }},
	// 5. Meets the floor.
	{RuntimeOK, func(runtimeInputs) bool { return true }},
}

// CheckRuntime evaluates one requirement against one observation of the host
// interpreter. goos and pm pick the install guidance in the remedy.
func CheckRuntime(req integration.RuntimeRequirement, obs RuntimeObservation, goos string, pm PackageManager) RuntimeCheck {
	row, known := runtimeTable[req.Runtime]
	in := runtimeInputs{
		known:        known,
		found:        strings.TrimSpace(obs.Path) != "",
		probed:       obs.ProbeErr == nil,
		dirDependent: obs.DirDependent,
	}
	if strings.TrimSpace(req.Range) != "" {
		in.inRange, in.rangeRead = EnginesSatisfies(req.Range, obs.Output)
	}
	in.have, in.haveOK = ParseRuntimeVersion(obs.Output)
	in.floor, in.floorOK = ParseRuntimeVersion(req.MinVersion)
	if !runtimeVersionShapeOK(req.MinVersion) {
		in.floorOK = false
	}

	out := RuntimeCheck{
		Runtime:   req.Runtime,
		Display:   RuntimeDisplay(req.Runtime),
		Product:   req.Product,
		Min:       DisplayRuntimeMin(req.MinVersion),
		Path:      obs.Path,
		FloorFrom: FloorFromRegistry,
	}
	if in.found && in.probed && in.haveOK && known && !in.dirDependent {
		out.Have = versionString(in.have)
	}
	for _, r := range runtimeRules {
		if r.match(in) {
			out.Status = r.status
			break
		}
	}
	switch out.Status {
	case RuntimeUnknown:
		out.DirDependent = in.dirDependent
	case RuntimeOutsideRange:
		out.Range = strings.TrimSpace(req.Range)
		out.Summary = outsideRangeSummary(out)
	}
	if out.Status == RuntimeTooOld {
		h := runtimeInstallHint(row, goos, pm)
		out.Summary = runtimeSummary(out)
		out.FixCommand = h.command
		out.FixNote = h.note
		out.Remedy = runtimeRemedy(out)
	}
	return out
}

// runtimeVersionShapeOK rejects a floor that is not dotted numeric ("lts").
func runtimeVersionShapeOK(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	for _, p := range strings.Split(s, ".") {
		if p == "" {
			return false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

// runtimeSummary is the short sentence: what the tool needs and what this
// machine has.
func runtimeSummary(c RuntimeCheck) string {
	product := strings.TrimSpace(c.Product)
	if product == "" {
		product = "This tool"
	}
	return product + " needs " + c.Display + " " + c.Min + " or newer. " +
		"This machine has " + c.Have + "."
}

// outsideRangeSummary is the short sentence for an outside_range status.
func outsideRangeSummary(c RuntimeCheck) string {
	product := strings.TrimSpace(c.Product)
	if product == "" {
		product = "This tool"
	}
	return product + " declares " + c.Display + " " + c.Range + ". This machine has " +
		c.Have + ", which that range leaves out, so the tool may fail to start."
}

// runtimeRemedy is the plain-text whole fix, one part per line, the command on
// a line of its own (see RuntimeCheck.Remedy).
func runtimeRemedy(c RuntimeCheck) string {
	lines := []string{c.Summary}
	if c.Path != "" {
		lines[0] = strings.TrimSuffix(c.Summary, ".") + " (" + c.Path + ")."
	}
	if c.FixCommand != "" {
		lines = append(lines, "Install a newer "+c.Display+" first:", "  "+c.FixCommand)
		if c.FixNote != "" {
			lines = append(lines, c.FixNote)
		}
	} else {
		lines = append(lines, "Install a newer "+c.Display+" first. "+c.FixNote)
	}
	return strings.Join(lines, "\n")
}

// ChannelHostRuntime reports which runtime an install channel's program runs
// on once installed ("" when the channel brings its own runtime or none). Data
// on the channel table, so a newly grounded channel decides it in one place.
func ChannelHostRuntime(channel string) string {
	return channelTable[channel].HostRuntime
}

// EnginesFloor returns the lowest version an npm `engines` range accepts, as
// "major.minor.patch": the minimum over its `||` alternatives of each
// alternative's lower bound (">=24.16.0 <25 || >=26.1.0" -> "24.16.0",
// "^20.0.0 || >=22" -> "20.0.0", "20.x" -> "20.0.0"). A strict `>` is exact
// (">20" -> "21.0.0", ">20.1.2" -> "20.1.3"), review M3-18. ok is false when
// the range does not parse or any alternative has no lower bound ("*", "<20",
// "latest", empty): the caller then falls back to the registry floor rather
// than guess. The floor alone can miss a hole between alternatives; the
// whole range is checked by EnginesSatisfies (RuntimeRequirement.Range).
func EnginesFloor(rng string) (string, bool) {
	alts, ok := parseEnginesRange(rng)
	if !ok {
		return "", false
	}
	var floor [3]int
	for i, iv := range alts {
		if !iv.hasLo {
			return "", false
		}
		if i == 0 || versionLess(iv.lo, floor) {
			floor = iv.lo
		}
	}
	return versionString(floor), true
}

// PackageEnginesNode returns the `engines.node` range of a package.json, or
// ok=false when the file does not parse or declares none (an engines value of
// another shape, as some old packages carry, counts as none).
func PackageEnginesNode(packageJSON []byte) (string, bool) {
	var pkg struct {
		Engines json.RawMessage `json:"engines"`
	}
	if err := json.Unmarshal(packageJSON, &pkg); err != nil || len(pkg.Engines) == 0 {
		return "", false
	}
	var engines map[string]json.RawMessage
	if err := json.Unmarshal(pkg.Engines, &engines); err != nil {
		return "", false
	}
	var node string
	if err := json.Unmarshal(engines[integration.RuntimeNode], &node); err != nil || strings.TrimSpace(node) == "" {
		return "", false
	}
	return node, true
}

// InstalledEngines resolves what an installed package declares for itself:
// its package.json `engines.node` range and that range's floor (see
// EnginesFloor). ok is false exactly when InstalledFloor's is.
func InstalledEngines(packageJSON []byte) (floor, rng string, ok bool) {
	if len(packageJSON) == 0 {
		return "", "", false
	}
	rng, ok = PackageEnginesNode(packageJSON)
	if !ok {
		return "", "", false
	}
	floor, ok = EnginesFloor(rng)
	if !ok {
		return "", "", false
	}
	return floor, strings.TrimSpace(rng), true
}

// InstalledFloor resolves the floor an installed package declares for itself:
// its package.json `engines.node`, reduced by EnginesFloor. ok is false when
// the bytes are absent, do not parse, declare no node engine, or the range has
// no lower bound; the caller then keeps the registry floor.
func InstalledFloor(packageJSON []byte) (string, bool) {
	if len(packageJSON) == 0 {
		return "", false
	}
	rng, ok := PackageEnginesNode(packageJSON)
	if !ok {
		return "", false
	}
	return EnginesFloor(rng)
}
