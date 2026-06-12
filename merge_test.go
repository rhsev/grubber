package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func md(id, binder string, extra map[string]any) Record {
	r := Record{"id": id, "binder": binder, "_note_file": "/notes/binder.md"}
	for k, v := range extra {
		r[k] = v
	}
	return r
}

func jl(id, binder string, extra map[string]any) Record {
	r := Record{"id": id, "binder": binder, "_note_file": "/notes/collections/inbox.jsonl"}
	for k, v := range extra {
		r[k] = v
	}
	return r
}

func TestMergeAnnotatedAndIndexedToOneRecord(t *testing.T) {
	out := mergeRecords(
		[]Record{md("abc", "Test", map[string]any{"title": "My Doc"})},
		[]Record{jl("abc", "Test", map[string]any{"filename": "doc.pdf", "kind": "pdf"})},
		[]string{"id", "binder"},
	)
	if len(out) != 1 {
		t.Fatalf("expected 1 record, got %d", len(out))
	}
	r := out[0]
	if r["_note_file"] != "/notes/binder.md" {
		t.Errorf("scanned _note_file must win, got %v", r["_note_file"])
	}
	if r["filename"] != "doc.pdf" || r["kind"] != "pdf" {
		t.Errorf("index fields not back-filled: %v", r)
	}
	if r["title"] != "My Doc" {
		t.Errorf("scanned field lost: %v", r)
	}
}

func TestMergeDoesNotOverwriteScannedField(t *testing.T) {
	out := mergeRecords(
		[]Record{md("abc", "Test", map[string]any{"kind": "custom"})},
		[]Record{jl("abc", "Test", map[string]any{"kind": "pdf"})},
		[]string{"id", "binder"},
	)
	if out[0]["kind"] != "custom" {
		t.Errorf("scanned field overwritten: %v", out[0]["kind"])
	}
}

func TestInboxOnlyRecordPassesThrough(t *testing.T) {
	out := mergeRecords(
		nil,
		[]Record{jl("xyz", "Inbox", map[string]any{"filename": "brief.pdf"})},
		[]string{"id", "binder"},
	)
	if len(out) != 1 || out[0]["filename"] != "brief.pdf" {
		t.Fatalf("inbox record lost: %v", out)
	}
}

func TestScannedOnlyRecordPassesThrough(t *testing.T) {
	out := mergeRecords(
		[]Record{md("abc", "Test", nil)},
		nil,
		[]string{"id", "binder"},
	)
	if len(out) != 1 {
		t.Fatalf("scanned record lost: %v", out)
	}
}

func TestDifferentBinderNotMerged(t *testing.T) {
	out := mergeRecords(
		[]Record{md("abc", "BinderA", nil)},
		[]Record{jl("abc", "BinderB", map[string]any{"filename": "f.pdf"})},
		[]string{"id", "binder"},
	)
	if len(out) != 2 {
		t.Fatalf("expected 2 records (different binders), got %d", len(out))
	}
}

func TestMissingPrimaryKeyPassesThrough(t *testing.T) {
	out := mergeRecords(
		[]Record{{"title": "no id", "_note_file": "/notes/log.md"}},
		[]Record{jl("abc", "Test", nil)},
		[]string{"id", "binder"},
	)
	if len(out) != 2 {
		t.Fatalf("expected 2 records, got %d", len(out))
	}
}

func TestMissingSecondaryKeyDefaultsToEmpty(t *testing.T) {
	scanned := []Record{{"id": "abc", "_note_file": "/n/a.md"}}
	jsonl := []Record{{"id": "abc", "_note_file": "/n/i.jsonl", "filename": "x.pdf"}}
	out := mergeRecords(scanned, jsonl, []string{"id", "binder"})
	if len(out) != 1 || out[0]["filename"] != "x.pdf" {
		t.Fatalf("records with absent binder should merge: %v", out)
	}
}

// Config plumbing: merge_on in defaults, from_jsonl + merge_on in sets.
func TestConfigMergeOnAndFromJSONL(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgDir := filepath.Join(home, ".config", "grubber")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgYaml := `defaults:
  merge_on: [id, binder]
sets:
  notes:
    path: ~/notes
    from_jsonl: ["~/notes/collections/"]
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(cfgYaml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := NewConfig()
	if got := cfg.DefaultMergeOn(); len(got) != 2 || got[0] != "id" || got[1] != "binder" {
		t.Errorf("DefaultMergeOn = %v, want [id binder]", got)
	}
	set := cfg.GetSet("notes")
	if set == nil {
		t.Fatal("set 'notes' not found")
	}
	if got := cfgStrSlice(set, "from_jsonl"); len(got) != 1 || got[0] != "~/notes/collections/" {
		t.Errorf("from_jsonl = %v", got)
	}
}

// End-to-end: scan + --from-jsonl + --merge-on through Extract, with a filter
// on an annotation field — the back-filled index fields must survive.
func TestExtractMergeOnEndToEnd(t *testing.T) {
	dir := t.TempDir()
	notes := filepath.Join(dir, "notes")
	col := filepath.Join(notes, "collections")
	if err := os.MkdirAll(col, 0o755); err != nil {
		t.Fatal(err)
	}
	mdContent := "## Refs\n\n```yaml\nid: ref-aaa\nbinder: Test\nstatus: annotated\n```\n"
	if err := os.WriteFile(filepath.Join(notes, "binder.md"), []byte(mdContent), 0o644); err != nil {
		t.Fatal(err)
	}
	index := `{"id": "ref-aaa", "binder": "Test", "filename": "vertrag.pdf", "kind": "pdf"}
{"id": "ref-bbb", "binder": "Inbox", "filename": "brief.pdf", "kind": "pdf"}
`
	if err := os.WriteFile(filepath.Join(col, "inbox.jsonl"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}

	g, err := NewGrubber(notes, false, false, false, true, nil, 0, nil,
		[]string{"status=annotated"}, nil, []string{col}, []string{"id", "binder"})
	if err != nil {
		t.Fatal(err)
	}
	records, _, err := g.Extract(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record (merged, post-filtered), got %d: %v", len(records), records)
	}
	r := records[0]
	if r["filename"] != "vertrag.pdf" {
		t.Errorf("back-filled field missing after post-merge filter: %v", r)
	}
	if nf, _ := r["_note_file"].(string); !strings.HasSuffix(nf, "binder.md") {
		t.Errorf("_note_file should stay at the annotation: %v", r["_note_file"])
	}
}

func TestStreamJSONLMergeOn(t *testing.T) {
	dir := t.TempDir()
	notes := filepath.Join(dir, "notes")
	col := filepath.Join(notes, "collections")
	if err := os.MkdirAll(col, 0o755); err != nil {
		t.Fatal(err)
	}
	mdContent := "```yaml\nid: ref-aaa\nbinder: Test\nstatus: annotated\n```\n"
	if err := os.WriteFile(filepath.Join(notes, "binder.md"), []byte(mdContent), 0o644); err != nil {
		t.Fatal(err)
	}
	index := `{"id": "ref-aaa", "binder": "Test", "filename": "vertrag.pdf"}
{"id": "ref-bbb", "binder": "Inbox", "filename": "brief.pdf"}
`
	if err := os.WriteFile(filepath.Join(col, "inbox.jsonl"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}

	g, err := NewGrubber(notes, false, false, false, true, nil, 0, nil, nil, nil,
		[]string{col}, []string{"id", "binder"})
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	if err := g.StreamJSONL(&sb); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(sb.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 jsonl lines (1 merged + 1 inbox), got %d: %q", len(lines), sb.String())
	}
	var first Record
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if first["id"] == "ref-aaa" && first["filename"] != "vertrag.pdf" {
		t.Errorf("merged record lacks back-filled filename: %v", first)
	}
}
