package cfgfmt

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMarkdownBlocks(t *testing.T) {
	orig, err := os.ReadFile(filepath.Join(fixtureDir, "markdown", "opencode-AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := ParseMarkdown(orig)
	if err != nil {
		t.Fatal(err)
	}
	b := Block{Kind: "org", Policy: "pol-1", Rev: "3", Content: "Use the company lint rules.\nNever push to main.\n"}
	if err := d.UpsertBlock(b); err != nil {
		t.Fatal(err)
	}
	want := string(orig) + "\n<!-- observer:org-begin policy=pol-1 rev=3 -->\nUse the company lint rules.\nNever push to main.\n<!-- observer:org-end -->\n"
	if got := string(d.Bytes()); got != want {
		t.Fatalf("append:\n%s", firstDiff(want, got))
	}
	// Idempotent.
	if err := d.UpsertBlock(b); err != nil || string(d.Bytes()) != want {
		t.Fatalf("re-upsert changed bytes (err=%v)", err)
	}
	// Replace in place with a new revision; text outside is untouched.
	b2 := Block{Kind: "org", Policy: "pol-1", Rev: "4", Content: "Only one rule now."}
	if err := d.UpsertBlock(b2); err != nil {
		t.Fatal(err)
	}
	want2 := string(orig) + "\n<!-- observer:org-begin policy=pol-1 rev=4 -->\nOnly one rule now.\n<!-- observer:org-end -->\n"
	if got := string(d.Bytes()); got != want2 {
		t.Fatalf("replace:\n%s", firstDiff(want2, got))
	}
	blocks, err := d.Blocks()
	if err != nil || len(blocks) != 1 || blocks[0].Rev != "4" || blocks[0].Content != "Only one rule now.\n" {
		t.Fatalf("blocks = %+v err = %v", blocks, err)
	}
	// Remove restores the original bytes.
	found, err := d.RemoveBlock("org", "pol-1")
	if err != nil || !found {
		t.Fatalf("remove: %v %v", found, err)
	}
	if got := string(d.Bytes()); got != string(orig) {
		t.Fatalf("remove did not restore the original:\n%s", firstDiff(string(orig), got))
	}
}

func TestMarkdownBlockWithFrontMatterAndCRLF(t *testing.T) {
	in := "---\r\nname: x\r\n---\r\nbody\r\n"
	d, err := ParseMarkdown([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.UpsertBlock(Block{Kind: "org", Content: "a\nb"}); err != nil {
		t.Fatal(err)
	}
	want := in + "\r\n<!-- observer:org-begin -->\r\na\r\nb\r\n<!-- observer:org-end -->\r\n"
	if got := string(d.Bytes()); got != want {
		t.Fatalf("got %q", got)
	}
	blocks, _ := d.Blocks()
	if len(blocks) != 1 || blocks[0].Line != 6 {
		t.Fatalf("blocks = %+v", blocks)
	}
	if err := d.Set(Path{"name"}, "z"); err != nil {
		t.Fatal(err)
	}
	if got := string(d.Bytes()); got != "---\r\nname: z\r\n---\r\nbody\r\n\r\n<!-- observer:org-begin -->\r\na\r\nb\r\n<!-- observer:org-end -->\r\n" {
		t.Fatalf("front matter edit disturbed the body: %q", got)
	}
}

// TestMarkdownMalformedBlocks: a damaged block structure refuses every
// block edit (fail closed) but the file still parses and round-trips.
func TestMarkdownMalformedBlocks(t *testing.T) {
	cases := map[string]string{
		"unterminated":   "a\n<!-- observer:org-begin policy=p -->\nx\n",
		"end-without":    "a\n<!-- observer:org-end -->\n",
		"nested":         "<!-- observer:org-begin policy=a -->\n<!-- observer:org-begin policy=b -->\n<!-- observer:org-end -->\n<!-- observer:org-end -->\n",
		"kind-mismatch":  "<!-- observer:org-begin -->\n<!-- observer:team-end -->\n",
		"duplicate":      "<!-- observer:org-begin policy=p -->\n<!-- observer:org-end -->\n<!-- observer:org-begin policy=p -->\n<!-- observer:org-end -->\n",
		"malformed":      "<!-- observer:org-begin policy=has space -->\n<!-- observer:org-end -->\n",
		"inline-mention": "see <!-- observer:org-begin --> here\n",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			d, err := ParseMarkdown([]byte(in))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if string(d.Bytes()) != in {
				t.Fatal("round trip changed bytes")
			}
			if _, err := d.Blocks(); !errors.Is(err, ErrMarker) {
				t.Fatalf("Blocks err = %v", err)
			}
			if err := d.UpsertBlock(Block{Kind: "org", Policy: "p", Content: "x"}); !errors.Is(err, ErrMarker) {
				t.Fatalf("Upsert err = %v", err)
			}
			if string(d.Bytes()) != in {
				t.Fatal("refused upsert changed bytes")
			}
		})
	}
}

// TestMarkdownBlockSafety: markers inside fenced code are documentation,
// and block content can never forge a marker.
func TestMarkdownBlockSafety(t *testing.T) {
	in := "# Doc\n\n```md\n<!-- observer:org-begin policy=p -->\n```\n"
	d, err := ParseMarkdown([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	blocks, err := d.Blocks()
	if err != nil || len(blocks) != 0 {
		t.Fatalf("fenced marker counted: %v %v", blocks, err)
	}
	for _, bad := range []Block{
		{Kind: "org", Content: "x\n<!-- observer:org-end -->\n"},
		{Kind: "Org"},
		{Kind: "org", Policy: "a b"},
		{Kind: "org", Rev: "1-->"},
		{Kind: "org", Content: "nul\x00"},
	} {
		if err := d.UpsertBlock(bad); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	if string(d.Bytes()) != in {
		t.Fatal("refused upserts changed bytes")
	}
	// Two kinds / policies coexist; removal is targeted.
	if err := d.UpsertBlock(Block{Kind: "org", Policy: "a", Content: "A"}); err != nil {
		t.Fatal(err)
	}
	if err := d.UpsertBlock(Block{Kind: "org", Policy: "b", Content: "B"}); err != nil {
		t.Fatal(err)
	}
	if found, err := d.RemoveBlock("org", "a"); err != nil || !found {
		t.Fatal(found, err)
	}
	blocks, _ = d.Blocks()
	if len(blocks) != 1 || blocks[0].Policy != "b" {
		t.Fatalf("blocks = %+v", blocks)
	}
	if found, _ := d.RemoveBlock("org", "zzz"); found {
		t.Fatal("removed a block that does not exist")
	}
}
