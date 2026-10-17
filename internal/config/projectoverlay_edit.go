package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Project override file EDITS (T-421 item 1): the one owner of planning and
// applying a change to <root>/.observer/config.toml, shared by `observer
// config set --project` and the dashboard's per-project profile card.
//
// The edit is KEY-PRESERVING. The file is read as a generic TOML tree, the
// one key changes, and the tree is written back, so a profiles-only edit
// never serializes the zero-valued [compression] block beside it (the earlier
// struct round trip did, and the proxy's ProjectCompression overlay then
// turned compression off for that project). Keys the allow-listed parse
// target does not know are kept: they are inert, but they are the author's.
//
// Planning never writes: PlanProjectOverlayEdit returns the before and after
// bytes so a caller can show the exact change first, and
// ApplyProjectOverlayEdit refuses a plan whose file changed in between.

// ErrProjectOverlayChanged is returned by ApplyProjectOverlayEdit when the
// file on disk no longer matches the bytes the plan was computed from.
var ErrProjectOverlayChanged = errors.New("config: the project file changed since the preview; preview again")

// ProjectOverlayEditRequest is one requested edit of a project file.
type ProjectOverlayEditRequest struct {
	// Root is the project root (the directory holding .observer/). A
	// relative root resolves against the working directory.
	Root string
	// Key is the dotted key ("profiles.default",
	// "profiles.by_tool.cline", "compression.conversation.target_ratio").
	// Only profiles.* and compression.* keys are accepted.
	Key string
	// Value is the new value (parsed per the key's type). Ignored when
	// Remove is set.
	Value string
	// Remove drops the key instead of setting it, so the project falls
	// back to the global value.
	Remove bool
	// ExcludeFile is the resolved master config path. A root whose
	// override path is that file is refused (the home directory is always
	// refused, which covers the default location).
	ExcludeFile string
}

// ProjectOverlayEdit is a planned edit: the file's bytes before and after.
type ProjectOverlayEdit struct {
	// Path is the override file the edit targets.
	Path string `json:"path"`
	// Before is the file's current content (empty when absent).
	Before []byte `json:"-"`
	// After is the content the apply writes.
	After []byte `json:"-"`
	// Existed reports whether the file existed when planned.
	Existed bool `json:"existed"`
	// Changed is false when the edit changes nothing (setting the value a
	// key already holds, removing a key the file does not set); applying
	// an unchanged plan writes nothing.
	Changed bool `json:"changed"`
}

// ProjectOverlayPath is the override file path for a project root.
func ProjectOverlayPath(root string) string {
	return filepath.Join(root, ProjectOverlayFilename)
}

// ProfileTargetKey is the dotted project-file key of an assignment target:
// "profiles.default", "profiles.by_provider.<class>" or
// "profiles.by_tool.<tool>".
func ProfileTargetKey(t ProfileTarget) string {
	switch t.Kind {
	case ProfileTargetProvider:
		return "profiles.by_provider." + t.Key
	case ProfileTargetTool:
		return "profiles.by_tool." + t.Key
	default:
		return "profiles.default"
	}
}

// PlanProjectOverlayEdit computes an edit without writing anything.
func PlanProjectOverlayEdit(req ProjectOverlayEditRequest) (ProjectOverlayEdit, error) {
	segs := strings.Split(req.Key, ".")
	if segs[0] != "profiles" && segs[0] != "compression" {
		return ProjectOverlayEdit{}, fmt.Errorf("config: project files accept only profiles.* and compression.* keys (got %q) - daemon-level keys live in the global config.toml", req.Key)
	}
	root, err := filepath.Abs(req.Root)
	if err != nil {
		return ProjectOverlayEdit{}, fmt.Errorf("config: project root %q: %w", req.Root, err)
	}
	path := ProjectOverlayPath(root)
	if err := projectOverlayTargetSafe(root, path, req.ExcludeFile); err != nil {
		return ProjectOverlayEdit{}, err
	}
	before, existed, err := readProjectOverlay(path)
	if err != nil {
		return ProjectOverlayEdit{}, err
	}
	var doc projectOverlayDoc
	tree := map[string]any{}
	if err := toml.Unmarshal(before, &doc); err != nil {
		return ProjectOverlayEdit{}, fmt.Errorf("config: existing %s does not parse; fix it before setting keys: %w", path, err)
	}
	if err := toml.Unmarshal(before, &tree); err != nil {
		return ProjectOverlayEdit{}, fmt.Errorf("config: existing %s does not parse; fix it before setting keys: %w", path, err)
	}
	changed := false
	if req.Remove {
		if err := knownStructKey(reflect.TypeOf(doc), segs, req.Key); err != nil {
			return ProjectOverlayEdit{}, err
		}
		changed = deleteTreeKey(tree, segs)
	} else {
		if err := setStructKey(reflect.ValueOf(&doc).Elem(), segs, req.Key, req.Value); err != nil {
			return ProjectOverlayEdit{}, err
		}
		leaf, err := typedLeaf(doc, segs)
		if err != nil {
			return ProjectOverlayEdit{}, err
		}
		changed, err = setTreeKey(tree, segs, leaf, req.Key)
		if err != nil {
			return ProjectOverlayEdit{}, err
		}
	}
	e := ProjectOverlayEdit{Path: path, Before: before, After: before, Existed: existed, Changed: changed}
	if !changed {
		return e, nil
	}
	after, err := encodeTree(tree)
	if err != nil {
		return ProjectOverlayEdit{}, err
	}
	var check projectOverlayDoc
	if err := toml.Unmarshal(after, &check); err != nil {
		return ProjectOverlayEdit{}, fmt.Errorf("config: edited %s would not parse: %w", path, err)
	}
	e.After = after
	return e, nil
}

// ApplyProjectOverlayEdit writes a planned edit through the shared .bak +
// atomic-rename writer. An unchanged plan writes nothing; a plan whose file
// changed since it was computed returns ErrProjectOverlayChanged.
func ApplyProjectOverlayEdit(e ProjectOverlayEdit) error {
	if !e.Changed {
		return nil
	}
	root := filepath.Dir(filepath.Dir(e.Path))
	if err := projectOverlayTargetSafe(root, e.Path, ""); err != nil {
		return err
	}
	cur, existed, err := readProjectOverlay(e.Path)
	if err != nil {
		return err
	}
	if existed != e.Existed || !bytes.Equal(cur, e.Before) {
		return ErrProjectOverlayChanged
	}
	return writeBytesAtomic(e.Path, e.After)
}

// ReadProjectOverlayProfiles returns the [profiles] keys a project file sets
// (unset keys stay zero: "" default, nil maps), whether the file exists, and
// a parse error for a file that exists but does not parse.
func ReadProjectOverlayProfiles(root string) (ProfilesConfig, bool, error) {
	body, existed, err := readProjectOverlay(ProjectOverlayPath(root))
	if err != nil || !existed {
		return ProfilesConfig{}, existed, err
	}
	var doc projectOverlayDoc
	if err := toml.Unmarshal(body, &doc); err != nil {
		return ProfilesConfig{}, true, fmt.Errorf("config: %s does not parse: %w", ProjectOverlayPath(root), err)
	}
	return doc.Profiles, true, nil
}

// CheckProjectOverlayTarget reports why root's override file may not be
// written (nil when it may): the same refusals PlanProjectOverlayEdit
// applies, so a surface can say so before anyone tries.
func CheckProjectOverlayTarget(root, excludeFile string) error {
	return projectOverlayTargetSafe(root, ProjectOverlayPath(root), excludeFile)
}

// ProfileProviderClasses is the provider-class vocabulary a "provider"
// assignment target accepts ("anthropic", "openai"), sorted.
func ProfileProviderClasses() []string {
	out := make([]string, 0, len(defaultProfiles().ByProvider))
	for k := range defaultProfiles().ByProvider {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// readProjectOverlay reads the file, reporting absence as existed=false.
func readProjectOverlay(path string) ([]byte, bool, error) {
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("config: read %s: %w", path, err)
	}
	return body, true, nil
}

// projectOverlayTargetSafe refuses a write that would land somewhere other
// than a repo-local override file: the home directory (whose .observer/
// config.toml is the daemon's master config), the resolved master config
// path, a root that is not a directory, and a .observer directory or file
// that is a symbolic link (a repository can commit one pointing anywhere).
func projectOverlayTargetSafe(root, path, excludeFile string) error {
	if home, err := os.UserHomeDir(); err == nil && home != "" && filepath.Clean(home) == filepath.Clean(root) {
		return fmt.Errorf("config: %s is the home directory; its .observer/config.toml is the daemon's own config, not a project file", root)
	}
	if excludeFile != "" && filepath.Clean(excludeFile) == filepath.Clean(path) {
		return fmt.Errorf("config: %s is the daemon's own config, not a project file", path)
	}
	fi, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("config: project root %s: %w", root, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("config: project root %s is not a directory", root)
	}
	for _, p := range []string{filepath.Dir(path), path} {
		li, err := os.Lstat(p)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("config: %s: %w", p, err)
		}
		if li.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("config: %s is a symbolic link; refusing to write a project file through it", p)
		}
		if p == path && li.IsDir() {
			return fmt.Errorf("config: %s is a directory", p)
		}
	}
	return nil
}

// knownStructKey checks that segs names a key of t (struct fields by toml
// tag; a string map consumes one trailing segment) without setting it.
func knownStructKey(t reflect.Type, segs []string, dotted string) error {
	for i, seg := range segs {
		switch t.Kind() {
		case reflect.Struct:
			found := false
			for f := range t.NumField() {
				if strings.Split(t.Field(f).Tag.Get("toml"), ",")[0] == seg {
					t = t.Field(f).Type
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("config: unknown key segment %q in %q", seg, dotted)
			}
		case reflect.Map:
			if i != len(segs)-1 {
				return fmt.Errorf("config: map entry %q takes exactly one trailing segment", dotted)
			}
			return nil
		default:
			return fmt.Errorf("config: key %q descends past a leaf at %q", dotted, seg)
		}
	}
	return nil
}

// typedLeaf returns the TOML-typed value doc holds at segs, by encoding doc
// and walking the decoded tree (so the value carries exactly the type the
// encoder would write).
func typedLeaf(doc projectOverlayDoc, segs []string) (any, error) {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(doc); err != nil {
		return nil, fmt.Errorf("config: encode project file: %w", err)
	}
	var tree map[string]any
	if err := toml.Unmarshal(buf.Bytes(), &tree); err != nil {
		return nil, fmt.Errorf("config: decode project file: %w", err)
	}
	var cur any = tree
	for _, seg := range segs {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("config: %q is not a table", strings.Join(segs, "."))
		}
		cur, ok = m[seg]
		if !ok {
			return nil, fmt.Errorf("config: %q has no value after the edit", strings.Join(segs, "."))
		}
	}
	return cur, nil
}

// setTreeKey sets segs to leaf in tree, creating tables on the way, and
// reports whether the value changed.
func setTreeKey(tree map[string]any, segs []string, leaf any, dotted string) (bool, error) {
	cur := tree
	for _, seg := range segs[:len(segs)-1] {
		next, ok := cur[seg]
		if !ok {
			m := map[string]any{}
			cur[seg] = m
			cur = m
			continue
		}
		m, ok := next.(map[string]any)
		if !ok {
			return false, fmt.Errorf("config: %q: %q is not a table in the existing file", dotted, seg)
		}
		cur = m
	}
	last := segs[len(segs)-1]
	if old, ok := cur[last]; ok && reflect.DeepEqual(old, leaf) {
		return false, nil
	}
	cur[last] = leaf
	return true, nil
}

// deleteTreeKey removes segs from tree, pruning tables the removal empties,
// and reports whether anything was removed.
func deleteTreeKey(tree map[string]any, segs []string) bool {
	if len(segs) == 1 {
		if _, ok := tree[segs[0]]; !ok {
			return false
		}
		delete(tree, segs[0])
		return true
	}
	child, ok := tree[segs[0]].(map[string]any)
	if !ok {
		return false
	}
	removed := deleteTreeKey(child, segs[1:])
	if removed && len(child) == 0 {
		delete(tree, segs[0])
	}
	return removed
}

// encodeTree renders a tree; an empty tree is an empty file.
func encodeTree(tree map[string]any) ([]byte, error) {
	if len(tree) == 0 {
		return []byte{}, nil
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(tree); err != nil {
		return nil, fmt.Errorf("config: encode project file: %w", err)
	}
	return buf.Bytes(), nil
}
