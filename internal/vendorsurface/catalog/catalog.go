package catalog

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"

	"github.com/marmutapp/superbased-observer/internal/vendorsurface"
)

//go:embed data/*.json
var dataFS embed.FS

// Group is one vendor group file.
type Group struct {
	Group        string                  `json:"group"`
	ResearchFile string                  `json:"research_file"`
	CheckedOn    string                  `json:"checked_on"`
	Surfaces     []vendorsurface.Surface `json:"surfaces"`
}

// Catalog is the loaded, validated catalog.
type Catalog struct {
	groups []Group
	byID   map[vendorsurface.SurfaceID]vendorsurface.Surface
	order  []vendorsurface.SurfaceID
}

var (
	loadOnce sync.Once
	loaded   *Catalog
	loadErr  error
)

// Load decodes and validates the embedded catalog once and returns it. A
// decode or validation failure is returned as an error (never a panic); the
// catalog tests make sure the committed data always loads.
func Load() (*Catalog, error) {
	loadOnce.Do(func() { loaded, loadErr = decode(dataFS) })
	return loaded, loadErr
}

func decode(fsys fs.FS) (*Catalog, error) {
	names, err := fs.Glob(fsys, "data/*.json")
	if err != nil {
		return nil, fmt.Errorf("catalog.Load: %w", err)
	}
	sort.Strings(names)
	c := &Catalog{byID: map[vendorsurface.SurfaceID]vendorsurface.Surface{}}
	var all []vendorsurface.Surface
	for _, n := range names {
		raw, err := fs.ReadFile(fsys, n)
		if err != nil {
			return nil, fmt.Errorf("catalog.Load: %s: %w", n, err)
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		var g Group
		if err := dec.Decode(&g); err != nil {
			return nil, fmt.Errorf("catalog.Load: %s: %w", n, err)
		}
		want := strings.TrimSuffix(strings.TrimPrefix(n, "data/"), ".json")
		if g.Group != want {
			return nil, fmt.Errorf("catalog.Load: %s declares group %q", n, g.Group)
		}
		for _, s := range g.Surfaces {
			if s.VendorGroup != g.Group {
				return nil, fmt.Errorf("catalog.Load: %s: row %s has vendor_group %q", n, s.ID, s.VendorGroup)
			}
		}
		c.groups = append(c.groups, g)
		all = append(all, g.Surfaces...)
	}
	if errs := vendorsurface.ValidateCatalog(all); len(errs) > 0 {
		msgs := make([]string, 0, len(errs))
		for i, e := range errs {
			if i == 20 {
				msgs = append(msgs, fmt.Sprintf("... and %d more", len(errs)-20))
				break
			}
			msgs = append(msgs, e.Error())
		}
		return nil, fmt.Errorf("catalog.Load: %d invalid rows:\n%s", len(errs), strings.Join(msgs, "\n"))
	}
	for _, s := range all {
		c.byID[s.ID] = s
		c.order = append(c.order, s.ID)
	}
	sort.Slice(c.order, func(i, j int) bool { return c.order[i] < c.order[j] })
	return c, nil
}

// Surfaces returns every row sorted by id.
func (c *Catalog) Surfaces() []vendorsurface.Surface {
	out := make([]vendorsurface.Surface, 0, len(c.order))
	for _, id := range c.order {
		out = append(out, c.byID[id])
	}
	return out
}

// Surface returns one row by id.
func (c *Catalog) Surface(id vendorsurface.SurfaceID) (vendorsurface.Surface, bool) {
	s, ok := c.byID[id]
	return s, ok
}

// Has reports whether an id is a catalog row.
func (c *Catalog) Has(id vendorsurface.SurfaceID) bool {
	_, ok := c.byID[id]
	return ok
}

// Groups returns the vendor group names in file order.
func (c *Catalog) Groups() []string {
	out := make([]string, 0, len(c.groups))
	for _, g := range c.groups {
		out = append(out, g.Group)
	}
	return out
}

// GroupSurfaces returns the rows of one vendor group.
func (c *Catalog) GroupSurfaces(group string) []vendorsurface.Surface {
	for _, g := range c.groups {
		if g.Group == group {
			return append([]vendorsurface.Surface(nil), g.Surfaces...)
		}
	}
	return nil
}
