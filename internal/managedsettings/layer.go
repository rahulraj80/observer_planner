package managedsettings

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/marmutapp/superbased-observer/internal/agentconfig"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// Layer is one vendor-managed settings layer Observer renders. The file's
// paths, format and key shape come from the catalogue row FileID names (the
// catalogue is the one owner of where a vendor reads its files); this row
// adds what only lever A needs: which policy modes land here, how firmly the
// vendor holds the value, the MDM channels and the grounding.
type Layer struct {
	FileID string
	// AlsoShapes are further catalogue shapes whose settings this layer
	// takes, for a vendor whose two products read one managed file (Kilo
	// Code and the Kilo CLI share /etc/kilo).
	AlsoShapes []string
	// Modes are the policy modes whose values land in this layer: enforced
	// values go to an override layer, defaults to the vendor's lowest admin
	// layer (never an override layer, which would turn a default into a lock).
	Modes []string
	// Strength is the hold of the file channel on every OS it reaches,
	// except the OSes in Weak.
	Strength Strength
	// ChannelStrength overrides Strength for one MDM channel (an OS policy
	// store the user cannot redirect while the file can be).
	ChannelStrength map[Channel]Strength
	// PerSetting overrides the strength for a setting id, or for every id
	// under a prefix ending in "." ("copilot-cli/sandbox.").
	PerSetting map[string]Strength
	// Weak names the OSes where the file channel's path is user-writable or
	// relocatable (os -> why): tamper-evident there.
	Weak map[string]string
	// FileName is the artifact file name when the catalogue path is a
	// directory the vendor does not name a file in ("" = the path's base).
	FileName string
	// PolicyKeys renames a setting to the layer's own key (setting id ->
	// key). Nil means the layer uses the user file's key paths verbatim; a
	// non-nil map carries only the settings it names (an empty map: only
	// derived values land here).
	PolicyKeys map[string]string
	// Check refuses a value this layer must not hold (a restrict-only file
	// that fails closed on a grant); nil accepts every value.
	Check func(settingID string, v any) error
	// Profile / Registry are the MDM channels (nil when the vendor has none).
	Profile  *ProfileChannel
	Registry *RegistryChannel
	// FileChannelOS limits the plain-file artifact to these OSes (nil = every
	// OS the catalogue row has a path for).
	FileChannelOS []string
	// Header writes a leading comment (formats that keep comments).
	Header bool
	// Sources are the vendor pages the row is grounded in; Verified says
	// they were fetched on 2026-10-06 and state what the row claims.
	Sources  []string
	Verified bool
	Note     string
}

func (l Layer) carries(mode string) bool {
	for _, m := range l.Modes {
		if m == mode {
			return true
		}
	}
	return false
}

// takesShape reports whether settings of a catalogue shape land here.
func (l Layer) takesShape(fileShape, settingShape string) bool {
	return fileShape == settingShape || contains(l.AlsoShapes, settingShape)
}

// keyFor returns the key a setting takes in this layer.
func (l Layer) keyFor(row agentconfig.SettingRow) (agentconfig.KeyPath, bool) {
	if l.PolicyKeys == nil {
		return row.Path, true
	}
	k, ok := l.PolicyKeys[row.ID]
	if !ok || k == "" {
		return nil, false
	}
	return agentconfig.KeyPath{k}, true
}

// settingRule is the vendor's own hold for one key (ok=false: the layer's).
func (l Layer) settingRule(settingID string) (Strength, bool) {
	if s, ok := l.PerSetting[settingID]; ok {
		return s, true
	}
	var best Strength
	bestLen := 0
	for k, s := range l.PerSetting {
		if strings.HasSuffix(k, ".") && strings.HasPrefix(settingID, k) && len(k) > bestLen {
			best, bestLen = s, len(k)
		}
	}
	return best, bestLen > 0
}

// effective is the hold one setting gets through one channel on one OS: a
// weak channel stays tamper-evident; otherwise the vendor's per-key rule
// (a lock, a default, restrict only) replaces the layer's own strength.
func (l Layer) effective(settingID string, ch Channel, os string) Strength {
	s := l.channelStrength(ch, os)
	if s == StrengthTamperEvident {
		return s
	}
	if r, ok := l.settingRule(settingID); ok {
		return r
	}
	return s
}

// bestFor is the firmest hold a setting gets through any channel and OS of
// the layer.
func (l Layer) bestFor(settingID string, f agentconfig.FileRow) Strength {
	best := StrengthTamperEvident
	for o, chans := range l.reach(f) {
		for _, ch := range chans {
			if s := l.effective(settingID, ch, o); Stronger(s, best) {
				best = s
			}
		}
	}
	return best
}

// channelStrength is the hold one channel gives on one OS, before any
// per-setting override.
func (l Layer) channelStrength(ch Channel, os string) Strength {
	if s, ok := l.ChannelStrength[ch]; ok {
		return s
	}
	if ch == ChannelFile {
		if _, weak := l.Weak[os]; weak {
			return StrengthTamperEvident
		}
	}
	return l.Strength
}

// header is the leading comment of a file artifact ("" when the format
// keeps no comments).
func (l Layer) header(in PolicyInput) string {
	if !l.Header {
		return ""
	}
	return fmt.Sprintf("# Generated by Observer from the org AI tool policy for %s, version %d.\n"+
		"# Deploy it with administrator rights only (see README.md). Regenerate it from\n"+
		"# the org dashboard after each policy change; do not edit by hand.\n\n", in.Audience, in.Version)
}

func (l Layer) fileName(f agentconfig.FileRow) string {
	if l.FileName != "" {
		return l.FileName
	}
	for _, p := range f.Paths.All() {
		p = strings.ReplaceAll(p, `\`, "/")
		if !strings.HasSuffix(p, "/") {
			return path.Base(p)
		}
	}
	return path.Base(f.ID)
}

// osPaths lists the catalogue path per OS ("linux", "darwin", "windows").
func osPaths(f agentconfig.FileRow) map[string]string {
	out := map[string]string{}
	if f.Paths.Linux != "" {
		out["linux"] = f.Paths.Linux
	}
	if f.Paths.Darwin != "" {
		out["darwin"] = f.Paths.Darwin
	}
	if f.Paths.Windows != "" {
		out["windows"] = f.Paths.Windows
	}
	return out
}

var osOrder = []string{"linux", "darwin", "windows"}

func (l Layer) fileOS(f agentconfig.FileRow) []string {
	paths := osPaths(f)
	var out []string
	for _, o := range osOrder {
		if paths[o] == "" {
			continue
		}
		if l.FileChannelOS != nil && !contains(l.FileChannelOS, o) {
			continue
		}
		out = append(out, o)
	}
	return out
}

// reach is every (channel, os) the layer delivers through.
func (l Layer) reach(f agentconfig.FileRow) map[string][]Channel {
	out := map[string][]Channel{}
	for _, o := range l.fileOS(f) {
		out[o] = append(out[o], ChannelFile)
	}
	if l.Profile != nil {
		out["darwin"] = append(out["darwin"], ChannelProfile)
	}
	if l.Registry != nil {
		out["windows"] = append(out["windows"], ChannelRegistry)
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// destination resolves a catalogue path to the path a deploy step names:
// a directory row gets the artifact's file name appended.
func destination(p, name string) string {
	if strings.HasSuffix(p, "/") || strings.HasSuffix(p, `\`) {
		return p + name
	}
	return p
}

// fileInstall returns the per-OS steps that put the file artifact in place
// with administrator ownership (the precondition for a tamper-proof hold).
func (l Layer) fileInstall(f agentconfig.FileRow, name string) []InstallStep {
	paths := osPaths(f)
	var out []InstallStep
	for _, o := range l.fileOS(f) {
		dst := destination(paths[o], name)
		var steps []string
		switch o {
		case "windows":
			win := windowsPath(dst)
			dir := win
			if i := strings.LastIndex(win, `\`); i > 0 {
				dir = win[:i]
			}
			steps = []string{
				"From an elevated PowerShell:",
				fmt.Sprintf(`New-Item -ItemType Directory -Force -Path "%s" | Out-Null`, dir),
				fmt.Sprintf(`Copy-Item -Force "%s" "%s"`, name, win),
				fmt.Sprintf(`icacls "%s" /inheritance:r /grant:r "Administrators:(OI)(CI)F" "SYSTEM:(OI)(CI)F" "Users:(OI)(CI)RX"`, dir),
				"Fleet: ship the same file with Intune (a Win32 app or a remediation script) or Group Policy Preferences (Files).",
			}
		default:
			group := "root"
			if o == "darwin" {
				group = "wheel"
			}
			steps = []string{
				fmt.Sprintf(`sudo install -d -m 0755 -o root -g %s "%s"`, group, path.Dir(dst)),
				fmt.Sprintf(`sudo install -m 0644 -o root -g %s "%s" "%s"`, group, name, dst),
			}
			if o == "darwin" {
				steps = append(steps, "Fleet: ship the same file with your MDM's script or package payload (Jamf policy, Kandji custom script, Intune shell script).")
			} else {
				steps = append(steps, "Fleet: ship the same file with your configuration management (Ansible, Puppet, Chef, Salt) or a package.")
			}
		}
		if why, weak := l.Weak[o]; weak {
			steps = append(steps, "Tamper-evident only through this file on this OS: "+why)
		}
		out = append(out, InstallStep{OS: o, Path: dst, Steps: steps})
	}
	return out
}

// windowsPath expands the catalogue's Windows tokens for a deploy step.
func windowsPath(p string) string {
	return strings.NewReplacer(
		"%ProgramData%", `C:\ProgramData`,
		"%ProgramFiles%", `C:\Program Files`,
		"/", `\`,
	).Replace(p)
}

var (
	filesOnce sync.Once
	filesByID map[string]agentconfig.FileRow
)

// fileRow finds a catalogue file row by id.
func fileRow(id string) (agentconfig.FileRow, bool) {
	filesOnce.Do(func() {
		filesByID = map[string]agentconfig.FileRow{}
		for _, f := range agentconfig.Files() {
			filesByID[f.ID] = f
		}
	})
	f, ok := filesByID[id]
	return f, ok
}

// layerByFile finds the layer row for a catalogue file id.
func layerByFile(id string) (Layer, bool) {
	for _, l := range Layers() {
		if l.FileID == id {
			return l, true
		}
	}
	return Layer{}, false
}

// Derived is a value an enforced setting implies in an enforcement file
// whose keys are constraints rather than the setting itself (Codex
// requirements.toml: an enforced approval_policy becomes the one-entry
// allowed_approval_policies list).
type Derived struct {
	Name string
	From string // setting id
	Mode string
	To   string // layer FileID
	Path agentconfig.KeyPath
	// Transform maps the setting's value to the constraint's value; ok is
	// false when the value has no constraint form.
	Transform func(any) (any, bool)
}

// oneOf returns a Transform admitting exactly the enforced scalar value when
// it is one the constraint list accepts.
func oneOf(allowed ...string) func(any) (any, bool) {
	return func(v any) (any, bool) {
		s, ok := v.(string)
		if !ok || (len(allowed) > 0 && !contains(allowed, s)) {
			return nil, false
		}
		return []any{s}, true
	}
}

// same is a Transform that pins the value itself.
func same(v any) (any, bool) {
	switch v.(type) {
	case string, bool, int64, float64:
		return v, true
	}
	return nil, false
}

func derivedFor(setting, mode string) []Derived {
	var out []Derived
	for _, d := range derivedRules() {
		if d.From == setting && d.Mode == mode {
			out = append(out, d)
		}
	}
	return out
}

// toolsOfLayer names the catalogue tools whose settings a layer takes.
func toolsOfLayer(l Layer) []string {
	f, ok := fileRow(l.FileID)
	if !ok {
		return nil
	}
	shapes := append([]string{f.Shape}, l.AlsoShapes...)
	seen := map[string]bool{}
	var out []string
	for _, g := range agentconfig.Files() {
		if contains(shapes, g.Shape) && !seen[g.Tool] {
			seen[g.Tool] = true
			out = append(out, g.Tool)
		}
	}
	sort.Strings(out)
	return out
}

// Locks is the honest lock posture of every catalogued tool: the firmest
// hold its rendered managed layers give an ENFORCED value the org AI tool
// policy can author, per OS, or tamper-evident (lever B only) with the
// reason.
func Locks() []ToolLock {
	byTool := map[string][]Layer{}
	for _, l := range Layers() {
		if !l.carries(orgcontract.AgentConfigModeEnforced) {
			continue
		}
		for _, t := range toolsOfLayer(l) {
			byTool[t] = append(byTool[t], l)
		}
	}
	var out []ToolLock
	for _, c := range agentconfig.Coverage() {
		if c.Status == agentconfig.CoverageNoSurface {
			continue
		}
		lk := ToolLock{Tool: c.Tool, Best: StrengthTamperEvident, PerOS: map[string]Strength{}}
		for _, o := range osOrder {
			lk.PerOS[o] = StrengthTamperEvident
		}
		var notes []string
		verified := "doc"
		for _, l := range byTool[c.Tool] {
			ids := layerAuthorable(l, c.Tool)
			if len(ids) == 0 {
				continue
			}
			f, _ := fileRow(l.FileID)
			lk.Layers = append(lk.Layers, l.FileID)
			if !l.Verified {
				verified = "unverified"
			}
			for o, chans := range l.reach(f) {
				for _, ch := range chans {
					for _, id := range ids {
						if s := l.effective(id, ch, o); Stronger(s, lk.PerOS[o]) {
							lk.PerOS[o] = s
						}
					}
				}
			}
			if l.Note != "" {
				notes = append(notes, l.Note)
			}
		}
		for _, s := range lk.PerOS {
			if Stronger(s, lk.Best) {
				lk.Best = s
			}
		}
		if len(lk.Layers) == 0 {
			lk.Note = noLayerNote(c.Tool, len(byTool[c.Tool]) > 0)
		} else {
			lk.Verified = verified
			lk.Note = strings.Join(notes, " ")
		}
		sort.Strings(lk.Layers)
		out = append(out, lk)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tool < out[j].Tool })
	return out
}

// layerAuthorable lists the settings of a tool the org policy can publish
// as an ENFORCED value that land in the layer (directly, renamed, or as a
// derived constraint).
func layerAuthorable(l Layer, tool string) []string {
	f, ok := fileRow(l.FileID)
	if !ok {
		return nil
	}
	var out []string
	for _, r := range agentconfig.SettingsFor(tool) {
		if !Authorable(r) {
			continue
		}
		if l.takesShape(f.Shape, r.FileID) && l.admits(f, r) {
			if _, ok := l.keyFor(r); ok {
				out = append(out, r.ID)
				continue
			}
		}
		for _, d := range derivedRules() {
			if d.From == r.ID && d.To == l.FileID && d.Mode == orgcontract.AgentConfigModeEnforced {
				out = append(out, r.ID)
				break
			}
		}
	}
	return out
}

// Authorable reports whether the org AI tool policy may set a catalogue
// setting at all: user-owned, never a secret, no Observer-owned span inside,
// and writable by the user (lever B can deliver it).
func Authorable(r agentconfig.SettingRow) bool {
	return r.Owner == agentconfig.OwnerUser && !r.Secret && r.Writable && len(agentconfig.SpansFor(r.ID)) == 0
}

// noLayerNote explains a tool with nothing lever A can lock.
func noLayerNote(tool string, hasLayer bool) string {
	if n, ok := vendorRailNotes[tool]; ok {
		return n
	}
	if hasLayer {
		return "Its managed layer holds only administrator keys the org AI tool policy does not set; Observer's lever B keeps the policy (tamper-evident)."
	}
	return "No administrator-owned settings layer is documented for this tool; Observer's lever B keeps the policy (tamper-evident)."
}

// admits reports whether a setting may be set at the layer's file scope: on
// its own shape the catalogue scope decides; through AlsoShapes (another
// product reading the same managed file) a user-level setting qualifies,
// since the shared file takes the same keys the user file does.
func (l Layer) admits(f agentconfig.FileRow, r agentconfig.SettingRow) bool {
	if r.FileID == f.Shape {
		return scopeAllowed(r.Scopes, f.Scope)
	}
	return scopeAllowed(r.Scopes, agentconfig.ScopeUser)
}
