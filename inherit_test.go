package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// inheritNote has two header fields and two blocks; the second block sets
// album itself.
const inheritNote = "---\nalbum: Safari\nurl: https://example.org\n---\n\n" +
	"```yaml\nid: a\nfilename: a.pdf\n```\n\n" +
	"```yaml\nid: b\nalbum: Eigenes\n```\n"

// extractNote writes content to a temp note, extracts it with the given
// inherit list (nil = flag absent) and returns the records in order.
func extractNote(t *testing.T, content string, frontmatterOnly bool, inherit []string) []Record {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "n.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := NewGrubber(dir, false, frontmatterOnly, false, true, nil, 0, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if inherit != nil {
		g.SetInherit(inherit)
	}
	recs, err := g.processFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return recs
}

// fields returns a record's keys, sorted, for a readable comparison.
func fields(r Record) []string {
	keys := make([]string, 0, len(r))
	for k := range r {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func TestInheritAbsentPassesEveryField(t *testing.T) {
	recs := extractNote(t, inheritNote, false, nil)
	if len(recs) != 2 {
		t.Fatalf("want 2 block records, got %d", len(recs))
	}
	if recs[0]["album"] != "Safari" || recs[0]["url"] != "https://example.org" {
		t.Errorf("without --inherit every header field must reach the block: %v", recs[0])
	}
	if recs[1]["album"] != "Eigenes" {
		t.Errorf("the block's own value must win over the header: %v", recs[1]["album"])
	}
}

func TestInheritEmptyPassesNone(t *testing.T) {
	recs := extractNote(t, inheritNote, false, []string{})
	want := []string{"_mtime", "_note_file", "filename", "id"}
	if got := fields(recs[0]); !slices.Equal(got, want) {
		t.Errorf("--inherit= : got fields %v, want %v (own fields plus provenance)", got, want)
	}
}

func TestInheritListPassesOnlyListed(t *testing.T) {
	recs := extractNote(t, inheritNote, false, []string{"album"})
	if recs[0]["album"] != "Safari" {
		t.Errorf("listed field album must be inherited: %v", recs[0])
	}
	if _, ok := recs[0]["url"]; ok {
		t.Errorf("unlisted field url must not be inherited: %v", recs[0])
	}
	if recs[1]["album"] != "Eigenes" {
		t.Errorf("a block setting album itself must keep its own value, got %v", recs[1]["album"])
	}
}

func TestInheritLeavesFrontmatterRecordWhole(t *testing.T) {
	// Without blocks, or under -m, the record is the frontmatter itself;
	// there is nothing to inherit into, so no list may take anything away.
	header := "---\nalbum: Safari\nurl: https://example.org\n---\n\nnur Prosa\n"
	cases := []struct {
		name            string
		content         string
		frontmatterOnly bool
	}{
		{"note without blocks", header, false},
		{"-m on a note with blocks", inheritNote, true},
	}
	for _, tc := range cases {
		for _, inherit := range [][]string{nil, {}, {"album"}} {
			recs := extractNote(t, tc.content, tc.frontmatterOnly, inherit)
			if len(recs) != 1 {
				t.Fatalf("%s, inherit %v: want 1 record, got %d", tc.name, inherit, len(recs))
			}
			if recs[0]["album"] != "Safari" || recs[0]["url"] != "https://example.org" {
				t.Errorf("%s, inherit %v: frontmatter must stay complete: %v", tc.name, inherit, recs[0])
			}
		}
	}
}

func TestResolveInherit(t *testing.T) {
	setWith := func(v []any) map[string]any { return map[string]any{"inherit": v} }
	cases := []struct {
		name   string
		dflt   []string
		set    map[string]any
		cliSet bool
		cli    string
		want   []string // nil = every field
	}{
		{"absent everywhere", nil, nil, false, "", nil},
		{"config default", []string{"album"}, nil, false, "", []string{"album"}},
		{"set overrides default", []string{"album"}, setWith([]any{"period"}), false, "", []string{"period"}},
		{"set inherit: [] means none", nil, setWith([]any{}), false, "", []string{}},
		{"set without the key keeps default", []string{"album"}, map[string]any{"path": "~/x"}, false, "", []string{"album"}},
		{"CLI list wins over set", nil, setWith([]any{"period"}), true, "album, url", []string{"album", "url"}},
		{"explicit --inherit= wins over set", nil, setWith([]any{"period"}), true, "", []string{}},
	}
	for _, tc := range cases {
		got := resolveInherit(tc.dflt, tc.set, tc.cliSet, tc.cli)
		if (got == nil) != (tc.want == nil) || !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %#v, want %#v", tc.name, got, tc.want)
		}
	}
}
