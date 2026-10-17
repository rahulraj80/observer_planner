// Package vendorschema validates a vendor settings document against the
// vendor's own pinned JSON schema (the adapter-config hygiene pipeline,
// step 2: docs/plans/adapter-config-and-common-vocabulary-plan-2026-10-03.md
// section 5.2; operator ruling D-108 approved the Go JSON-schema validator).
//
// The schemas are the SAME pinned copies the catalogue generator reads
// (testdata/agentconfig/schemas), embedded here because a testdata tree
// cannot be embedded; TestEmbeddedSchemasMatchPins byte-checks every copy
// against the sha256 the generator recorded (agentconfig.SchemaPins), so
// the two can never drift silently.
//
// No network, ever: the compiler's loader refuses every URL. The two
// schemas that reference models.dev's model schema (OpenCode, Kilo) get a
// permissive local stand-in for it, so a model entry is not checked
// field by field (disclosed, never a false error).
package vendorschema

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/marmutapp/superbased-observer/internal/agentconfig"
)

//go:embed schemas/*.json
var schemaFS embed.FS

// modelsDevStub stands in for https://models.dev/model-schema.json.
const modelsDevURL = "https://models.dev/model-schema.json"

var modelsDevStub = map[string]any{"$defs": map[string]any{"Model": map[string]any{}}}

type compiled struct {
	once sync.Once
	sch  *jsonschema.Schema
	err  error
}

var (
	mu       sync.Mutex
	byFile   = map[string]*compiled{}
	errNoNet = errors.New("vendorschema: remote schema loading is disabled")
)

type noNetLoader struct{}

func (noNetLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("%w: %s", errNoNet, url)
}

// schemaFor returns the compiled schema of a file shape (nil when the shape
// has no pinned schema or it failed to compile).
func schemaFor(shape string) (*jsonschema.Schema, error) {
	file := ""
	for _, p := range agentconfig.SchemaPins() {
		if p.Shape == shape {
			file = p.File
		}
	}
	if file == "" {
		return nil, nil
	}
	mu.Lock()
	c := byFile[file]
	if c == nil {
		c = &compiled{}
		byFile[file] = c
	}
	mu.Unlock()
	c.once.Do(func() { c.sch, c.err = compile(file) })
	return c.sch, c.err
}

func compile(file string) (*jsonschema.Schema, error) {
	raw, err := schemaFS.ReadFile("schemas/" + file)
	if err != nil {
		return nil, fmt.Errorf("vendorschema: %s not embedded: %w", file, err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("vendorschema: %s: %w", file, err)
	}
	c := jsonschema.NewCompiler()
	c.UseLoader(noNetLoader{})
	loc := "file:///vendorschema/" + file
	if err := c.AddResource(loc, doc); err != nil {
		return nil, fmt.Errorf("vendorschema: %s: %w", file, err)
	}
	if err := c.AddResource(modelsDevURL, modelsDevStub); err != nil {
		return nil, fmt.Errorf("vendorschema: models.dev stand-in: %w", err)
	}
	sch, err := c.Compile(loc)
	if err != nil {
		return nil, fmt.Errorf("vendorschema: compile %s: %w", file, err)
	}
	return sch, nil
}

// Check validates a decoded document of the shape. It never errors: a
// shape without a schema (or one that failed to compile) yields nothing,
// so step 2 degrades to the catalogue checks of step 3.
func Check(shape string, tree map[string]any) []agentconfig.SchemaIssue {
	sch, err := schemaFor(shape)
	if err != nil || sch == nil {
		return nil
	}
	inst, err := toInstance(tree)
	if err != nil {
		return nil
	}
	verr := sch.Validate(inst)
	if verr == nil {
		return nil
	}
	var ve *jsonschema.ValidationError
	if !errors.As(verr, &ve) {
		return nil
	}
	p := message.NewPrinter(language.English)
	var out []agentconfig.SchemaIssue
	seen := map[string]bool{}
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) > 0 {
			for _, c := range e.Causes {
				walk(c)
			}
			return
		}
		is := agentconfig.SchemaIssue{
			Path:     keyPath(tree, e.InstanceLocation),
			Message:  e.ErrorKind.LocalizedString(p),
			Location: "/" + joinPointer(e.InstanceLocation),
		}
		if ap, ok := e.ErrorKind.(*kind.AdditionalProperties); ok {
			is.Unknown = true
			if len(ap.Properties) == 1 {
				is.Path = append(is.Path, ap.Properties[0])
			}
		}
		key := is.Location + "\x00" + is.Message
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, is)
	}
	walk(ve)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Location < out[j].Location })
	return out
}

// toInstance converts a normalized cfgfmt tree into the validator's JSON
// value space (json.Number numbers, RFC 3339 times).
func toInstance(tree map[string]any) (any, error) {
	b, err := json.Marshal(convertTimes(tree))
	if err != nil {
		return nil, err
	}
	return jsonschema.UnmarshalJSON(bytes.NewReader(b))
}

func convertTimes(v any) any {
	switch x := v.(type) {
	case time.Time:
		return x.Format(time.RFC3339Nano)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = convertTimes(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = convertTimes(e)
		}
		return out
	}
	return v
}

// keyPath maps an instance location onto a catalogue KeyPath: map keys
// only, cut where the location descends into an array.
func keyPath(tree map[string]any, loc []string) agentconfig.KeyPath {
	var out agentconfig.KeyPath
	var cur any = tree
	for _, seg := range loc {
		m, ok := cur.(map[string]any)
		if !ok {
			break
		}
		out = append(out, seg)
		cur = m[seg]
	}
	return out
}

func joinPointer(loc []string) string { return strings.Join(loc, "/") }

// Shapes lists the file shapes with an embedded schema, sorted.
func Shapes() []string {
	var out []string
	for _, p := range agentconfig.SchemaPins() {
		out = append(out, p.Shape)
	}
	sort.Strings(out)
	return out
}
