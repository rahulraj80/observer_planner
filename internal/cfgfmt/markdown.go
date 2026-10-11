package cfgfmt

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// MarkdownDoc is the Markdown / MDC engine. Get / Set / Delete address the
// YAML front matter (through the YAML engine, so a front-matter edit is as
// surgical as a YAML file edit); UpsertBlock / RemoveBlock maintain marked
// Observer blocks in the body (plan section 6.4):
//
//	<!-- observer:org-begin policy=<id> rev=<n> -->
//	...org text...
//	<!-- observer:org-end -->
//
// Everything outside the edited front matter entry or the edited block is
// kept byte for byte.
type MarkdownDoc struct {
	format Format
	bom    bool
	hasFM  bool
	open   line
	fm     *yamlDoc
	close  line
	body   []line
	opts   Options
}

// Block is one marked Observer block.
type Block struct {
	// Kind is the block family, e.g. "org" ([a-z0-9]+).
	Kind string
	// Policy identifies the block within its Kind (the policy id); empty
	// means the file holds at most one block of that Kind.
	Policy string
	// Rev is the policy revision the block was written from.
	Rev string
	// Content is the text between the markers, lines joined with "\n"
	// and newline-terminated ("" for an empty block).
	Content string
	// Line is the 1-based line of the begin marker in the whole file (set
	// by Blocks; ignored by UpsertBlock).
	Line int
}

var (
	mdBeginRE = regexp.MustCompile(`^\s*<!--\s*observer:([a-z0-9]+)-begin((?:\s+[a-z][a-z0-9_]*=[^\s]+)*)\s*-->\s*$`)
	mdEndRE   = regexp.MustCompile(`^\s*<!--\s*observer:([a-z0-9]+)-end\s*-->\s*$`)
	mdAnyRE   = regexp.MustCompile(`<!--\s*observer:`)
	mdAttrRE  = regexp.MustCompile(`([a-z][a-z0-9_]*)=([^\s]+)`)
	mdKindRE  = regexp.MustCompile(`^[a-z0-9]+$`)
	mdValueRE = regexp.MustCompile(`^[A-Za-z0-9._:/@+-]*$`)
	mdFenceRE = regexp.MustCompile("^ {0,3}(```+|~~~+)")
)

// ParseMarkdown parses a Markdown document (FormatMarkdown).
func ParseMarkdown(b []byte) (*MarkdownDoc, error) {
	d, err := Parse(FormatMarkdown, b)
	if err != nil {
		return nil, err
	}
	return d.(*MarkdownDoc), nil
}

func parseMarkdown(f Format, bom bool, body []byte, opts Options) (Doc, error) {
	lines := splitLines(string(body))
	d := &MarkdownDoc{format: f, bom: bom, opts: opts}
	if len(lines) > 0 && strings.TrimRight(lines[0].text, " \t") == "---" {
		for j := 1; j < len(lines); j++ {
			t := strings.TrimRight(lines[j].text, " \t")
			if t != "---" && t != "..." {
				continue
			}
			fmText := joinLines(lines[1:j])
			fmDoc, err := parseYAML(false, []byte(fmText), opts)
			if err != nil {
				var pe *ParseError
				if errors.As(err, &pe) {
					pe.Format = f
					if pe.Line > 0 {
						pe.Line++
					}
					pe.Msg = "front matter: " + pe.Msg
				}
				return nil, err
			}
			d.hasFM = true
			d.open, d.close = lines[0], lines[j]
			d.fm = fmDoc.(*yamlDoc)
			d.body = append([]line(nil), lines[j+1:]...)
			return d, nil
		}
		// An opening fence with no closing fence is a thematic break, not
		// front matter.
	}
	d.body = lines
	return d, nil
}

// Format implements Doc.
func (d *MarkdownDoc) Format() Format { return d.format }

// Disclosures implements Doc.
func (d *MarkdownDoc) Disclosures() []Disclosure {
	if d.fm == nil {
		return nil
	}
	return d.fm.Disclosures()
}

// Bytes implements Doc.
func (d *MarkdownDoc) Bytes() []byte {
	var b strings.Builder
	if d.hasFM {
		b.WriteString(d.open.text + d.open.eol)
		b.Write(d.fm.Bytes())
		b.WriteString(d.close.text + d.close.eol)
	}
	b.WriteString(joinLines(d.body))
	return withBOM(d.bom, []byte(b.String()))
}

// Value implements Doc: the decoded front matter ({} when there is none).
func (d *MarkdownDoc) Value() (map[string]any, error) {
	if !d.hasFM {
		return map[string]any{}, nil
	}
	return d.fm.Value()
}

// Get implements Doc over the front matter.
func (d *MarkdownDoc) Get(path Path) (any, bool, error) {
	if !d.hasFM {
		return nil, false, nil
	}
	return d.fm.Get(path)
}

// Set implements Doc over the front matter, creating a front matter block
// at the top of the file when there is none.
func (d *MarkdownDoc) Set(path Path, value any) error {
	if !d.hasFM {
		if err := checkPath(path); err != nil {
			return err
		}
		eol := dominantEOL(d.body)
		fm := &yamlDoc{opts: d.opts, unit: 2}
		if err := fm.Set(path, value); err != nil {
			return err
		}
		for i := range fm.lines {
			if fm.lines[i].eol != "" {
				fm.lines[i].eol = eol
			}
		}
		d.hasFM = true
		d.open, d.close = line{text: "---", eol: eol}, line{text: "---", eol: eol}
		d.fm = fm
		return nil
	}
	return d.fm.Set(path, value)
}

// Delete implements Doc over the front matter.
func (d *MarkdownDoc) Delete(path Path) (bool, error) {
	if !d.hasFM {
		return false, checkPath(path)
	}
	return d.fm.Delete(path)
}

// Body returns the text after the front matter.
func (d *MarkdownDoc) Body() string { return joinLines(d.body) }

// bodyLineOffset is the number of file lines before the body.
func (d *MarkdownDoc) bodyLineOffset() int {
	if !d.hasFM {
		return 0
	}
	return 2 + len(d.fm.lines)
}

type mdBlockPos struct {
	Block
	begin, end int // body line indices of the markers
}

// scanBlocks finds the marked blocks in the body, ignoring fenced code.
func (d *MarkdownDoc) scanBlocks() ([]mdBlockPos, error) {
	var out []mdBlockPos
	var cur *mdBlockPos
	fence := ""
	seen := map[string]bool{}
	for i, l := range d.body {
		if m := mdFenceRE.FindStringSubmatch(l.text); m != nil {
			marker := m[1]
			switch {
			case fence == "":
				fence = marker
			case marker[0] == fence[0] && len(marker) >= len(fence):
				fence = ""
			}
			continue
		}
		if fence != "" || !mdAnyRE.MatchString(l.text) {
			continue
		}
		lineNo := d.bodyLineOffset() + i + 1
		if m := mdBeginRE.FindStringSubmatch(l.text); m != nil {
			if cur != nil {
				return nil, fmt.Errorf("%w: line %d opens a block inside the block opened at line %d", ErrMarker, lineNo, cur.Line)
			}
			b := mdBlockPos{Block: Block{Kind: m[1], Line: lineNo}, begin: i}
			for _, a := range mdAttrRE.FindAllStringSubmatch(m[2], -1) {
				switch a[1] {
				case "policy":
					b.Policy = a[2]
				case "rev":
					b.Rev = a[2]
				}
			}
			id := b.Kind + "\x00" + b.Policy
			if seen[id] {
				return nil, fmt.Errorf("%w: line %d repeats block %s %q", ErrMarker, lineNo, b.Kind, b.Policy)
			}
			seen[id] = true
			cur = &b
			continue
		}
		if m := mdEndRE.FindStringSubmatch(l.text); m != nil {
			if cur == nil {
				return nil, fmt.Errorf("%w: line %d closes a block that was never opened", ErrMarker, lineNo)
			}
			if m[1] != cur.Kind {
				return nil, fmt.Errorf("%w: line %d closes %q but the open block is %q", ErrMarker, lineNo, m[1], cur.Kind)
			}
			cur.end = i
			var c strings.Builder
			for _, cl := range d.body[cur.begin+1 : i] {
				c.WriteString(cl.text)
				c.WriteString("\n")
			}
			cur.Content = c.String()
			out = append(out, *cur)
			cur = nil
			continue
		}
		return nil, fmt.Errorf("%w: line %d looks like an observer marker but is malformed", ErrMarker, lineNo)
	}
	if cur != nil {
		return nil, fmt.Errorf("%w: block opened at line %d is never closed", ErrMarker, cur.Line)
	}
	return out, nil
}

// Blocks returns the marked blocks in body order.
func (d *MarkdownDoc) Blocks() ([]Block, error) {
	pos, err := d.scanBlocks()
	if err != nil {
		return nil, err
	}
	out := make([]Block, len(pos))
	for i, p := range pos {
		out[i] = p.Block
	}
	return out, nil
}

func validateBlock(b Block) error {
	if !mdKindRE.MatchString(b.Kind) {
		return fmt.Errorf("%w: kind %q", ErrMarker, b.Kind)
	}
	if !mdValueRE.MatchString(b.Policy) || !mdValueRE.MatchString(b.Rev) {
		return fmt.Errorf("%w: policy / rev may only hold [A-Za-z0-9._:/@+-]", ErrMarker)
	}
	if strings.IndexByte(b.Content, 0) >= 0 || !utf8.ValidString(b.Content) {
		return ErrBinary
	}
	if mdAnyRE.MatchString(b.Content) {
		return fmt.Errorf("%w: block content may not contain an observer marker", ErrMarker)
	}
	return nil
}

func beginMarker(b Block) string {
	s := "<!-- observer:" + b.Kind + "-begin"
	if b.Policy != "" {
		s += " policy=" + b.Policy
	}
	if b.Rev != "" {
		s += " rev=" + b.Rev
	}
	return s + " -->"
}

func endMarker(kind string) string { return "<!-- observer:" + kind + "-end -->" }

// UpsertBlock writes b: an existing block with the same Kind and Policy is
// replaced in place (its markers re-rendered with the new Rev); otherwise
// the block is appended at the end of the body after one blank line.
// Writing identical content is a no-op.
func (d *MarkdownDoc) UpsertBlock(b Block) error {
	if err := validateBlock(b); err != nil {
		return err
	}
	pos, err := d.scanBlocks()
	if err != nil {
		return err
	}
	eol := dominantEOL(d.body)
	content := strings.TrimSuffix(strings.ReplaceAll(b.Content, "\r\n", "\n"), "\n")
	var inner []line
	if content != "" {
		inner = textLines(content, eol)
	}
	for _, p := range pos {
		if p.Kind != b.Kind || p.Policy != b.Policy {
			continue
		}
		newBegin := beginMarker(b)
		if strings.TrimSpace(d.body[p.begin].text) == newBegin && p.Content == joinContent(inner) {
			return nil
		}
		repl := make([]line, 0, len(inner)+2)
		repl = append(repl, line{text: newBegin, eol: d.body[p.begin].eol})
		repl = append(repl, inner...)
		repl = append(repl, d.body[p.end])
		d.body = spliceLines(d.body, p.begin, p.end+1, repl, eol)
		return nil
	}
	var repl []line
	if n := len(d.body); n > 0 && !isBlank(d.body[n-1].text) {
		repl = append(repl, line{text: "", eol: eol})
	}
	repl = append(repl, line{text: beginMarker(b), eol: eol})
	repl = append(repl, inner...)
	repl = append(repl, line{text: endMarker(b.Kind), eol: eol})
	d.body = spliceLines(d.body, len(d.body), len(d.body), repl, eol)
	return nil
}

func joinContent(ls []line) string {
	var c strings.Builder
	for _, l := range ls {
		c.WriteString(l.text)
		c.WriteString("\n")
	}
	return c.String()
}

// RemoveBlock deletes the block with the given Kind and Policy (markers
// included). A block that ends the file takes the blank separator line
// UpsertBlock added in front of it, so append-then-remove restores the
// original text.
func (d *MarkdownDoc) RemoveBlock(kind, policy string) (bool, error) {
	pos, err := d.scanBlocks()
	if err != nil {
		return false, err
	}
	for _, p := range pos {
		if p.Kind != kind || p.Policy != policy {
			continue
		}
		from, to := p.begin, p.end+1
		atEnd := true
		for _, l := range d.body[to:] {
			if !isBlank(l.text) {
				atEnd = false
				break
			}
		}
		if atEnd && from > 0 && isBlank(d.body[from-1].text) {
			from--
		}
		d.body = append(append([]line(nil), d.body[:from]...), d.body[to:]...)
		return true, nil
	}
	return false, nil
}
