package config

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/marmutapp/superbased-observer/internal/cfgfmt"
)

// Structural (per-key) patches: the comment-preserving write for a whole-
// section save (T-524).
//
// A section save (PUT /api/config/section/<id> and the dedicated editors'
// commitConfigEdit) hands PatchFile a config whose changed values are not
// all scalars (a list, a table, a block of rules), which used to force the
// Tier-B path: the whole Config struct re-encoded, every comment and the
// operator's formatting gone, and every default written out. Instead the
// save now names its changes as TOML paths: StructuralPatches diffs the TOML
// trees of the loaded config and the edited one, and RenderPatch applies each
// difference to the file through internal/cfgfmt, the comment-preserving
// engine the adapter-config editor uses. Keys the save did not change keep
// their bytes; a key the file never set and the save did not touch stays
// unset (its default still applies).
//
// The result is proven before it is used: the edited text must decode, and
// the config it loads to (defaults, then the file) must equal the config the
// unedited file loads to with exactly these changes applied. Anything the
// line editor cannot express (an array of tables, an inline table on the
// path) or that does not round-trip falls back to the old re-serialize, with
// the reason reported, never silently.

// StructuralPatches returns the per-key edits that turn prev into cfg, as
// TOML paths with their new values (Patch.Path + Patch.Value) or deletions
// (Patch.Delete). A table both sides have is compared key by key; any other
// value (a scalar, a list, an array of tables) is one edit. The result is in
// a stable order. An empty result means the two encode identically.
func StructuralPatches(prev, cfg Config) ([]Patch, error) {
	a, err := configTree(prev)
	if err != nil {
		return nil, fmt.Errorf("config.StructuralPatches: %w", err)
	}
	b, err := configTree(cfg)
	if err != nil {
		return nil, fmt.Errorf("config.StructuralPatches: %w", err)
	}
	var out []Patch
	diffTrees(nil, a, b, &out)
	return out, nil
}

// configTree encodes c through the TOML encoder and decodes it back into a
// normalized tree, so both sides of a diff use the vocabulary the file uses.
func configTree(c Config) (map[string]any, error) {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(c); err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}
	return decodeTree(buf.String())
}

// decodeTree decodes TOML text into a normalized tree.
func decodeTree(text string) (map[string]any, error) {
	raw := map[string]any{}
	if _, err := toml.Decode(text, &raw); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	n, err := cfgfmt.Normalize(raw)
	if err != nil {
		return nil, fmt.Errorf("normalize: %w", err)
	}
	m, _ := n.(map[string]any)
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

// diffTrees appends the edits that turn a into b under path.
func diffTrees(path []string, a, b map[string]any, out *[]Patch) {
	keys := make([]string, 0, len(a)+len(b))
	seen := map[string]bool{}
	for k := range a {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	for k := range b {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		p := append(append([]string(nil), path...), k)
		av, inA := a[k]
		bv, inB := b[k]
		switch {
		case !inB:
			*out = append(*out, Patch{Dotted: strings.Join(p, "."), Path: p, Delete: true})
		case !inA:
			*out = append(*out, Patch{Dotted: strings.Join(p, "."), Path: p, Value: bv})
		default:
			am, aIsMap := av.(map[string]any)
			bm, bIsMap := bv.(map[string]any)
			if aIsMap && bIsMap {
				diffTrees(p, am, bm, out)
				continue
			}
			if !cfgfmt.Equal(av, bv) {
				*out = append(*out, Patch{Dotted: strings.Join(p, "."), Path: p, Value: bv})
			}
		}
	}
}

// isStructural reports whether p is a structural (per-key) edit.
func (p Patch) isStructural() bool { return len(p.Path) > 0 }

// allStructural reports whether every patch is a structural edit.
func allStructural(patches []Patch) bool {
	if len(patches) == 0 {
		return false
	}
	for _, p := range patches {
		if !p.isStructural() {
			return false
		}
	}
	return true
}

// structuralText applies structural patches to the current file text through
// cfgfmt and proves the result (see the package comment above). reason names
// why it could not be used.
func structuralText(current string, patches []Patch) (text, reason string, ok bool) {
	doc, err := cfgfmt.ParseWith(cfgfmt.FormatTOML, []byte(current), cfgfmt.Options{ForbidReserialize: true})
	if err != nil {
		return "", "the file could not be edited key by key (" + err.Error() + ") - the file is re-serialized", false
	}
	for _, p := range groupUnderAbsentTables(doc, patches) {
		var eerr error
		if p.Delete {
			_, eerr = doc.Delete(cfgfmt.Path(p.Path))
		} else {
			eerr = doc.Set(cfgfmt.Path(p.Path), p.Value)
		}
		if eerr != nil {
			return "", p.Dotted + " could not be edited in place (" + eerr.Error() + ") - the file is re-serialized", false
		}
	}
	text = string(doc.Bytes())
	var probe Config
	if _, err := toml.Decode(text, &probe); err != nil {
		return "", "the edited file does not decode (" + err.Error() + ") - the file is re-serialized", false
	}
	// The edited file must load to the unedited file's config with exactly
	// these changes applied: defaults hold for every key neither the file nor
	// the save sets, so a deleted key that a default fills back in is caught
	// here rather than on the next start.
	before, err := loadedTree(current)
	if err != nil {
		return "", "the current file does not decode (" + err.Error() + ") - the file is re-serialized", false
	}
	after, err := loadedTree(text)
	if err != nil {
		return "", "the edited file does not load (" + err.Error() + ") - the file is re-serialized", false
	}
	for _, p := range patches {
		if p.Delete {
			treeDeletePath(before, p.Path)
		} else {
			treeSetPath(before, p.Path, p.Value)
		}
	}
	if !cfgfmt.Equal(any(before), any(after)) {
		return "", "the key-by-key edit does not load to the saved config - the file is re-serialized", false
	}
	return text, "", true
}

// groupUnderAbsentTables folds every set whose parent table the file does not
// have yet into ONE table-valued set at the shallowest absent table (D-407).
// cfgfmt places a lone new key after the deepest existing ancestor header as
// a dotted relative key (process.enabled under [observer]); that is valid
// TOML, but this file is the operator's, and a later [observer.process]
// header (hand-written, or appended by another writer) would define the
// table twice and the file would stop loading. A table value is written as
// its own [observer.process] section instead. A table the file already
// writes as dotted keys is found by Get and keeps that shape. Deletes, and
// sets whose parent exists, pass through in order; a grouped set takes the
// position of its first member. The proof in structuralText still checks
// the result against the original, ungrouped patches.
func groupUnderAbsentTables(doc cfgfmt.Doc, patches []Patch) []Patch {
	out := make([]Patch, 0, len(patches))
	at := map[string]int{} // absent table path -> index in out
	for _, p := range patches {
		k := 0
		if !p.Delete {
			k = absentPrefix(doc, p.Path)
		}
		if k == 0 {
			out = append(out, p)
			continue
		}
		table := p.Path[:k]
		key := strings.Join(table, "\x00")
		i, seen := at[key]
		if !seen {
			i = len(out)
			at[key] = i
			out = append(out, Patch{Dotted: strings.Join(table, "."), Path: append([]string(nil), table...), Value: map[string]any{}})
		}
		treeSetPath(out[i].Value.(map[string]any), p.Path[k:], p.Value)
	}
	return out
}

// absentPrefix returns the length of the shallowest prefix of path's parent
// that doc does not hold, or 0 when the parent table exists, path is a root
// key, or the lookup fails (the set then passes through unchanged).
func absentPrefix(doc cfgfmt.Doc, path []string) int {
	for k := 1; k < len(path); k++ {
		_, found, err := doc.Get(cfgfmt.Path(path[:k]))
		if err != nil {
			return 0
		}
		if !found {
			return k
		}
	}
	return 0
}

// loadedTree is the tree of the config text loads to: defaults, then text.
func loadedTree(text string) (map[string]any, error) {
	c := Default()
	if _, err := toml.Decode(text, &c); err != nil {
		return nil, err
	}
	return configTree(c)
}

// treeSetPath sets path in root, creating intermediate tables.
func treeSetPath(root map[string]any, path []string, v any) {
	m := root
	for _, k := range path[:len(path)-1] {
		next, ok := m[k].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[k] = next
		}
		m = next
	}
	m[path[len(path)-1]] = v
}

// treeDeletePath removes path from root (a missing path is a no-op).
func treeDeletePath(root map[string]any, path []string) {
	m := root
	for _, k := range path[:len(path)-1] {
		next, ok := m[k].(map[string]any)
		if !ok {
			return
		}
		m = next
	}
	delete(m, path[len(path)-1])
}
