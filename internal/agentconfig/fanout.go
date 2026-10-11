package agentconfig

import (
	"errors"
	"fmt"
	"sort"

	"github.com/marmutapp/superbased-observer/internal/cfgfmt"
)

// The common form (plan sections 5.1 and 7, phases P4 / P5): one value per
// common key, read back from every tool (P4) and written to every tool's
// USER-scope file as the developer's L1 all-adapters value (P5), or, for one
// project, to every tool's PROJECT file at the project rank (T-429, D-106:
// all adapters -> per adapter -> project), with a per-tool fan-out preview:
// the native key and value each tool gets, which are lossy, which do not
// expose the key, which are refused.

// CommonToolValue is one tool's current value for a common key.
type CommonToolValue struct {
	Tool      string      `json:"tool"`
	Settings  []string    `json:"settings"`
	Transform TransformID `json:"transform"`
	Lossy     bool        `json:"lossy,omitempty"`
	Note      string      `json:"note,omitempty"`
	// Set: at least one native setting holds a value; Value is the
	// canonical projection of the effective native values.
	Set   bool `json:"set"`
	Value any  `json:"value,omitempty"`
	// Masked: a native value holds a hidden secret.
	Masked bool `json:"masked,omitempty"`
	// Error is a projection failure (a native value outside the transform).
	Error string     `json:"error,omitempty"`
	Layer ValueLayer `json:"layer,omitempty"`
	// Writable: the tool has a user-scope file Observer may write for every
	// native setting.
	Writable bool `json:"writable"`
}

// CommonKeyView is one common key across every tool.
type CommonKeyView struct {
	Spec    CommonSpec        `json:"spec"`
	Exposed int               `json:"exposed"`
	Lossy   int               `json:"lossy"`
	SetIn   int               `json:"set_in"`
	Tools   []CommonToolValue `json:"tools"`
	// NotExposed lists the catalogued tools with no mapping for the key.
	NotExposed []string `json:"not_exposed,omitempty"`
}

// CommonView projects every tool view onto the common vocabulary.
func CommonView(views map[string]ToolView) []CommonKeyView {
	var out []CommonKeyView
	for _, spec := range CommonSpecs() {
		kv := CommonKeyView{Spec: spec}
		mapped := map[string]bool{}
		for _, m := range MapRowsFor(spec.Key) {
			mapped[m.Tool] = true
			view, ok := views[m.Tool]
			if !ok {
				continue
			}
			kv.Exposed++
			if m.Lossy {
				kv.Lossy++
			}
			tv := CommonToolValue{Tool: m.Tool, Settings: m.Settings, Transform: m.Transform, Lossy: m.Lossy, Note: m.Note, Writable: true}
			byID := map[string]SettingView{}
			for _, s := range view.Settings {
				byID[s.ID] = s
			}
			natives := make([]any, len(m.Settings))
			for i, id := range m.Settings {
				s := byID[id]
				if !s.Writable || userFileFor(id) == "" {
					tv.Writable = false
				}
				if !s.Effective.Set || s.Effective.Layer == ValueVendorDefault {
					continue
				}
				if s.Effective.SecretPresent || s.Effective.Masked {
					tv.Masked = true
					continue
				}
				natives[i] = s.Effective.Value
				if tv.Layer == "" {
					tv.Layer = s.Effective.Layer
				}
			}
			v, present, err := m.Reverse(natives)
			switch {
			case err != nil:
				tv.Set, tv.Error = true, err.Error()
			case present:
				tv.Set, tv.Value = true, v
			}
			if tv.Set || tv.Masked {
				kv.SetIn++
			}
			kv.Tools = append(kv.Tools, tv)
		}
		for t := range views {
			if !mapped[t] {
				kv.NotExposed = append(kv.NotExposed, t)
			}
		}
		sort.Strings(kv.NotExposed)
		out = append(out, kv)
	}
	return out
}

// userFileFor returns the user-scope file row id an L1 / L2 write of a
// setting lands in ("" when the tool has no user file Observer may write).
func userFileFor(settingID string) string {
	r, ok := Setting(settingID)
	if !ok {
		return ""
	}
	if !scopeAllowed(r.Scopes, ScopeUser) {
		return ""
	}
	for _, f := range filesTable() {
		if f.Shape == r.FileID && f.Scope == ScopeUser && f.WriteNeeds == PrivUser {
			if _, ok := f.Format.Engine(); ok {
				return f.ID
			}
		}
	}
	return ""
}

// FanOutStatus says what an L1 write does for one tool.
type FanOutStatus string

// The closed FanOutStatus vocabulary.
const (
	FanOutWrite        FanOutStatus = "write"
	FanOutNotInstalled FanOutStatus = "not_installed"
	FanOutRefused      FanOutStatus = "refused"
	FanOutNotWritable  FanOutStatus = "not_writable"
	FanOutNoUserFile   FanOutStatus = "no_user_file"
	// FanOutNoProjectFile: the tool has no project file of the chosen
	// placement for the key (T-429).
	FanOutNoProjectFile FanOutStatus = "no_project_file"
)

// FanOutScope says which file of each tool a common value lands in: the
// user-scope file (the zero value), or a project file (Project), the
// vendor's personal, git-excluded one when LocalOnly and the shared one a
// commit carries otherwise.
type FanOutScope struct {
	Project   bool
	LocalOnly bool
}

// fileFor is the scope's file rule for one native setting, with the status
// and reason when the tool has no such file.
func (sc FanOutScope) fileFor(settingID string) (string, FanOutStatus, string) {
	if !sc.Project {
		if f := userFileFor(settingID); f != "" {
			return f, "", ""
		}
		return "", FanOutNoUserFile, "The tool has no user-level settings file Observer may write for " + settingID + "."
	}
	if f := ProjectFileFor(settingID, sc.LocalOnly); f != "" {
		return f, "", ""
	}
	kind := "shared"
	if sc.LocalOnly {
		kind = "personal (local-only)"
	}
	return "", FanOutNoProjectFile, "The tool has no " + kind + " project file Observer may write for " + settingID + "."
}

// FanOutTarget is one tool's part of an L1 write.
type FanOutTarget struct {
	Tool   string       `json:"tool"`
	Status FanOutStatus `json:"status"`
	Reason string       `json:"reason,omitempty"`
	FileID string       `json:"file_id,omitempty"`
	Edits  []Edit       `json:"edits,omitempty"`
	// Natives pair each native setting with the value it receives.
	Natives []NativeWrite `json:"natives,omitempty"`
	Lossy   bool          `json:"lossy,omitempty"`
	// Reprojected is what reading the written natives back gives; it
	// differs from the requested value for a lossy transform.
	Reprojected any `json:"reprojected,omitempty"`
}

// NativeWrite is one native setting an L1 write sets.
type NativeWrite struct {
	SettingID string `json:"setting_id"`
	Path      string `json:"path"`
	Value     any    `json:"value,omitempty"`
}

// ErrUnknownCommonKey: the key is not in the closed vocabulary or is
// Observer-owned (never a common-plane write).
var ErrUnknownCommonKey = errors.New("agentconfig: unknown or Observer-owned common key")

// PlanFanOut plans an L1 write of one common key. installed reports whether
// a tool is installed on this machine (a tool that is not gets no file
// created). clear deletes the native settings instead of setting them.
func PlanFanOut(key CommonKey, value any, clear bool, installed func(tool string) bool) ([]FanOutTarget, error) {
	return PlanFanOutAt(key, value, clear, installed, FanOutScope{})
}

// PlanFanOutAt is PlanFanOut at a scope: the user-scope files (L1) or one
// project's files (the project rank, T-429).
func PlanFanOutAt(key CommonKey, value any, clear bool, installed func(tool string) bool, scope FanOutScope) ([]FanOutTarget, error) {
	spec, ok := CommonSpecFor(key)
	if !ok || spec.Tier == TierObserverOwned {
		return nil, fmt.Errorf("%w: %q", ErrUnknownCommonKey, key)
	}
	if !clear {
		if err := checkCanonical(spec, value); err != nil {
			return nil, err
		}
	}
	var out []FanOutTarget
	for _, m := range MapRowsFor(key) {
		t := FanOutTarget{Tool: m.Tool, Lossy: m.Lossy}
		if installed != nil && !installed(m.Tool) {
			t.Status, t.Reason = FanOutNotInstalled, "Not installed on this machine; no file is created for it."
			out = append(out, t)
			continue
		}
		var natives []any
		if !clear {
			var err error
			natives, err = m.Forward(value)
			if err != nil {
				t.Status, t.Reason = FanOutRefused, err.Error()
				out = append(out, t)
				continue
			}
		}
		fileID := ""
		for i, id := range m.Settings {
			r, ok := Setting(id)
			if !ok || !r.Writable {
				t.Status, t.Reason = FanOutNotWritable, "Setting "+id+" is not editable: "+readOnlyReason(r, Access{})
				break
			}
			f, st, why := scope.fileFor(id)
			if f == "" {
				t.Status, t.Reason = st, why
				break
			}
			if fileID != "" && f != fileID {
				t.Status, t.Reason = FanOutNotWritable, "The native settings live in different files; set it per tool instead."
				break
			}
			fileID = f
			nw := NativeWrite{SettingID: id, Path: r.Path.String()}
			if clear {
				t.Edits = append(t.Edits, Edit{Path: r.Path, Op: OpDelete})
			} else {
				nw.Value = natives[i]
				t.Edits = append(t.Edits, Edit{Path: r.Path, Op: OpSet, Value: natives[i]})
			}
			t.Natives = append(t.Natives, nw)
		}
		if t.Status == "" {
			t.Status, t.FileID = FanOutWrite, fileID
			if !clear {
				if back, present, err := m.Reverse(natives); err == nil && present {
					t.Reprojected = back
					if !cfgfmt.Equal(normalizeLoose(back), normalizeLoose(value)) {
						t.Lossy = true
					}
				}
			}
		} else {
			t.Edits, t.Natives = nil, nil
		}
		out = append(out, t)
	}
	return out, nil
}

func normalizeLoose(v any) any {
	n, err := cfgfmt.Normalize(v)
	if err != nil {
		return v
	}
	return n
}

// checkCanonical validates a canonical value against its spec.
func checkCanonical(spec CommonSpec, v any) error {
	n, err := cfgfmt.Normalize(v)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBadValue, err)
	}
	r := SettingRow{Type: spec.Type, Enum: spec.Enum}
	if msg := checkValue(r, n); msg != "" {
		return fmt.Errorf("%w: %s", ErrBadValue, msg)
	}
	return nil
}

// CommonDanger reports whether a canonical value is in the key's dangerous
// set (the confirm step of the section 5.4 table).
func CommonDanger(key CommonKey, v any) bool {
	spec, ok := CommonSpecFor(key)
	if !ok {
		return false
	}
	s := fmt.Sprint(v)
	for _, d := range spec.Danger {
		if d == s {
			return true
		}
	}
	return false
}
