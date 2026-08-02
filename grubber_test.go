package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestNormalizeArraysComma(t *testing.T) {
	g := &Grubber{arrayFields: []string{"tags"}}
	r := Record{"tags": "go, cli, tools"}
	g.normalizeArrays(r)
	arr, ok := r["tags"].([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", r["tags"])
	}
	if len(arr) != 3 || arr[0] != "go" || arr[1] != "cli" || arr[2] != "tools" {
		t.Errorf("unexpected array: %v", arr)
	}
}

func TestNormalizeArraysSingle(t *testing.T) {
	g := &Grubber{arrayFields: []string{"tags"}}
	r := Record{"tags": "go"}
	g.normalizeArrays(r)
	arr, ok := r["tags"].([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", r["tags"])
	}
	if len(arr) != 1 || arr[0] != "go" {
		t.Errorf("unexpected array: %v", arr)
	}
}

func TestNormalizeArraysNonString(t *testing.T) {
	g := &Grubber{arrayFields: []string{"tags"}}
	r := Record{"tags": []any{"already", "array"}}
	g.normalizeArrays(r)
	arr, ok := r["tags"].([]any)
	if !ok || len(arr) != 2 {
		t.Errorf("pre-existing array should be unchanged: %v", r["tags"])
	}
}

func TestNormalizeArraysIgnoresOtherFields(t *testing.T) {
	g := &Grubber{arrayFields: []string{"tags"}}
	r := Record{"title": "hello, world"}
	g.normalizeArrays(r)
	if r["title"] != "hello, world" {
		t.Errorf("non-array field should be unchanged: %v", r["title"])
	}
}

func TestBuildResultWithFrontmatter(t *testing.T) {
	g := &Grubber{}
	fm := Record{"title": "hello"}
	blocks := []Record{{"foo": "bar"}}
	result := g.buildResult("notes/test.md", fm, blocks)
	if result.metadata["title"] != "hello" {
		t.Errorf("metadata title: got %v", result.metadata["title"])
	}
	if result.metadata["_note_file"] != "notes/test.md" {
		t.Errorf("_note_file: got %v", result.metadata["_note_file"])
	}
	if len(result.records) != 1 {
		t.Errorf("expected 1 record, got %d", len(result.records))
	}
}

func TestBuildResultNoBlocksFrontmatterFallback(t *testing.T) {
	g := &Grubber{}
	fm := Record{"title": "hello"}
	result := g.buildResult("test.md", fm, nil)
	if len(result.records) != 1 {
		t.Errorf("with frontmatter and no blocks, expected synthetic empty record; got %d records", len(result.records))
	}
}

func TestBuildResultNoBlocksBlocksOnly(t *testing.T) {
	g := &Grubber{blocksOnly: true}
	fm := Record{"title": "hello"}
	result := g.buildResult("test.md", fm, nil)
	if len(result.records) != 0 {
		t.Errorf("blocksOnly with no blocks should yield 0 records, got %d", len(result.records))
	}
}

func TestBuildResultNoFrontmatter(t *testing.T) {
	g := &Grubber{}
	result := g.buildResult("test.md", nil, nil)
	if len(result.records) != 0 {
		t.Errorf("no frontmatter, no blocks should yield 0 records, got %d", len(result.records))
	}
}

func TestOutputTSV(t *testing.T) {
	g := &Grubber{}
	records := []Record{
		{"name": "alice", "score": 10},
		{"name": "bob", "score": 20},
	}
	keys := []string{"name", "score"}
	var buf bytes.Buffer
	if err := g.OutputTSV(records, keys, &buf); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines (header + 2 rows), got %d", len(lines))
	}
	if lines[0] != "name\tscore" {
		t.Errorf("header: got %q", lines[0])
	}
	if lines[1] != "alice\t10" {
		t.Errorf("row 1: got %q", lines[1])
	}
}

func TestOutputTSVEmpty(t *testing.T) {
	g := &Grubber{}
	var buf bytes.Buffer
	if err := g.OutputTSV(nil, nil, &buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Errorf("empty records should produce no output, got %q", buf.String())
	}
}

func TestOutputTSVArrayField(t *testing.T) {
	g := &Grubber{}
	records := []Record{{"tags": []any{"go", "cli"}}}
	keys := []string{"tags"}
	var buf bytes.Buffer
	if err := g.OutputTSV(records, keys, &buf); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if lines[1] != "go, cli" {
		t.Errorf("array field: got %q", lines[1])
	}
}

func TestOutputTSVTabInValue(t *testing.T) {
	g := &Grubber{}
	records := []Record{{"title": "hello\tworld"}}
	keys := []string{"title"}
	var buf bytes.Buffer
	if err := g.OutputTSV(records, keys, &buf); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if strings.Contains(lines[1], "\t") {
		t.Errorf("tab in value should be replaced: got %q", lines[1])
	}
}

func TestOutputTSVTabInArrayValue(t *testing.T) {
	g := &Grubber{}
	records := []Record{{"tags": []any{"a\tb", "c\nd"}}}
	keys := []string{"tags"}
	var buf bytes.Buffer
	if err := g.OutputTSV(records, keys, &buf); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 || strings.Contains(lines[1], "\t") {
		t.Errorf("tabs/newlines in array values should be replaced: got %q", buf.String())
	}
}

func TestTextFilesDepthSkipsHiddenDirs(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{".hidden", "sub"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, sub, "note.md"), []byte("---\ntype: x\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	depth := 1
	g, err := NewGrubber(dir, false, false, false, true, &depth, 0, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	files, err := g.textFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || filepath.Base(filepath.Dir(files[0])) != "sub" {
		t.Errorf("expected only sub/note.md, got %v", files)
	}
}

// Records must come out in scan order regardless of which worker finished
// first — notes sharing a basename compare equal in the final sort, so an
// arrival-ordered pipeline would reshuffle them between runs.
func TestExtractOrderIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	// Same basename in several directories: the case where the stable sort
	// cannot distinguish records and the input order decides.
	for _, sub := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
		body := "---\ntitle: " + sub + "\n---\n"
		if err := os.WriteFile(filepath.Join(dir, sub, "note.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	titles := func() []string {
		g, err := NewGrubber(dir, false, false, false, true, nil, 0, nil, nil, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		records, _, err := g.Extract(nil)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]string, len(records))
		for i, r := range records {
			out[i], _ = r["title"].(string)
		}
		return out
	}

	first := titles()
	if len(first) != 8 {
		t.Fatalf("expected 8 records, got %d", len(first))
	}
	for i := range 20 {
		if got := titles(); !slices.Equal(got, first) {
			t.Fatalf("run %d reordered records: %v vs %v", i+2, got, first)
		}
	}
}

// The same guarantee on the streaming path, which has no final sort at all.
func TestStreamJSONLOrderIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	for i := range 60 {
		name := fmt.Sprintf("note-%02d.md", i)
		body := fmt.Sprintf("---\nn: %d\n---\n", i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	run := func() string {
		g, err := NewGrubber(dir, false, false, false, true, nil, 0, nil, nil, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if err := g.StreamJSONL(&buf); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}

	first := run()
	if strings.Count(first, "\n") != 60 {
		t.Fatalf("expected 60 lines, got %d", strings.Count(first, "\n"))
	}
	for i := range 20 {
		if got := run(); got != first {
			t.Fatalf("run %d produced a different order", i+2)
		}
	}
}

// Blocks within one note keep document order. Downstream tools rely on this:
// fileregister's album and mark-twin read the Nth block as the Nth item, so a
// reordering here would silently renumber their data.
func TestBlockOrderFollowsDocument(t *testing.T) {
	dir := t.TempDir()
	var body strings.Builder
	body.WriteString("---\ntitle: note\n---\n\nProse before the first block.\n")
	for i := range 12 {
		fmt.Fprintf(&body, "\n```yaml\nn: %d\n```\n\nProse between blocks.\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "note.md"), []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	want := make([]int, 12)
	for i := range want {
		want[i] = i
	}

	newG := func() *Grubber {
		g, err := NewGrubber(dir, false, false, false, true, nil, 0, nil, nil, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		return g
	}

	records, _, err := newG().Extract(nil)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]int, 0, len(records))
	for _, r := range records {
		if n, ok := r["n"].(int); ok {
			got = append(got, n)
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("Extract reordered blocks: got %v, want %v", got, want)
	}

	// The streaming path has no final sort at all, so it needs its own check.
	var buf bytes.Buffer
	if err := newG().StreamJSONL(&buf); err != nil {
		t.Fatal(err)
	}
	var streamed []int
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var rec struct {
			N *int `json:"n"`
		}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatal(err)
		}
		if rec.N != nil {
			streamed = append(streamed, *rec.N)
		}
	}
	if !slices.Equal(streamed, want) {
		t.Errorf("StreamJSONL reordered blocks: got %v, want %v", streamed, want)
	}
}

// The two output paths order notes differently, on purpose: json and tsv sort
// by filename so a note keeps its position when it moves between directories,
// while jsonl streams records as they are scanned and therefore follows the
// walk. Same three notes, two expected orders — if either side ever changes,
// this is the test that says so.
func TestNoteOrderDiffersPerFormat(t *testing.T) {
	dir := t.TempDir()
	// Directory name and filename deliberately disagree on the sort order.
	for _, f := range []struct{ sub, name, title string }{
		{"a", "zzz.md", "zzz"},
		{"b", "mmm.md", "mmm"},
		{"c", "aaa.md", "aaa"},
	} {
		if err := os.MkdirAll(filepath.Join(dir, f.sub), 0o755); err != nil {
			t.Fatal(err)
		}
		body := "---\ntitle: " + f.title + "\n---\n"
		if err := os.WriteFile(filepath.Join(dir, f.sub, f.name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	newG := func() *Grubber {
		g, err := NewGrubber(dir, false, false, false, false, nil, 0, nil, nil, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		return g
	}

	// json: by filename, ignoring the directory it sits in.
	records, keys, err := newG().Extract(nil)
	if err != nil {
		t.Fatal(err)
	}
	var byName []string
	for _, r := range records {
		title, _ := r["title"].(string)
		byName = append(byName, title)
	}
	if want := []string{"aaa", "mmm", "zzz"}; !slices.Equal(byName, want) {
		t.Errorf("json order: got %v, want %v (sorted by filename)", byName, want)
	}

	// tsv: same records, same order — the header plus one row per note.
	var tsv bytes.Buffer
	if err := newG().OutputTSV(records, keys, &tsv); err != nil {
		t.Fatal(err)
	}
	titleCol := slices.Index(keys, "title")
	if titleCol < 0 {
		t.Fatal("no title column")
	}
	var tsvOrder []string
	for _, line := range strings.Split(strings.TrimSpace(tsv.String()), "\n")[1:] {
		tsvOrder = append(tsvOrder, strings.Split(line, "\t")[titleCol])
	}
	if !slices.Equal(tsvOrder, byName) {
		t.Errorf("tsv order %v should match json order %v", tsvOrder, byName)
	}

	// jsonl: scan order, i.e. the directory walk a/ b/ c/.
	var buf bytes.Buffer
	if err := newG().StreamJSONL(&buf); err != nil {
		t.Fatal(err)
	}
	var scanned []string
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var rec struct {
			Title string `json:"title"`
		}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatal(err)
		}
		scanned = append(scanned, rec.Title)
	}
	if want := []string{"zzz", "mmm", "aaa"}; !slices.Equal(scanned, want) {
		t.Errorf("jsonl order: got %v, want %v (scan order)", scanned, want)
	}
}
