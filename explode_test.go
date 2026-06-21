package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExplodeRecordsArray(t *testing.T) {
	in := []Record{
		{"id": "a", "binder": []any{"x", "y"}, "filename": "doc.pdf", "_note_file": "i.jsonl"},
	}
	out := explodeRecords(in, "binder")
	if len(out) != 2 {
		t.Fatalf("expected 2 rows, got %d: %v", len(out), out)
	}
	for i, want := range []string{"x", "y"} {
		if out[i]["binder"] != want {
			t.Errorf("row %d binder = %v, want %s", i, out[i]["binder"], want)
		}
		if out[i]["filename"] != "doc.pdf" {
			t.Errorf("row %d lost a copied field: %v", i, out[i])
		}
		if out[i]["_note_file"] != "i.jsonl" {
			t.Errorf("row %d lost provenance: %v", i, out[i])
		}
	}
	// originals must not be mutated (cloned)
	if arr, ok := in[0]["binder"].([]any); !ok || len(arr) != 2 {
		t.Errorf("source record was mutated: %v", in[0]["binder"])
	}
}

func TestExplodeRecordsScalarAndAbsent(t *testing.T) {
	in := []Record{
		{"id": "a", "binder": "solo"},
		{"id": "b"}, // field absent
	}
	out := explodeRecords(in, "binder")
	if len(out) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(out))
	}
	if out[0]["binder"] != "solo" {
		t.Errorf("scalar passthrough changed: %v", out[0])
	}
	if _, ok := out[1]["binder"]; ok {
		t.Errorf("absent field should stay absent: %v", out[1])
	}
}

func TestExplodeRecordsEmptyArray(t *testing.T) {
	in := []Record{{"id": "a", "binder": []any{}, "filename": "b.pdf"}}
	out := explodeRecords(in, "binder")
	if len(out) != 1 {
		t.Fatalf("empty array should yield one row, got %d", len(out))
	}
	if _, ok := out[0]["binder"]; ok {
		t.Errorf("empty array should drop the field (binderless row): %v", out[0])
	}
	if out[0]["filename"] != "b.pdf" {
		t.Errorf("other fields must survive: %v", out[0])
	}
}

func TestExplodeEmptyFieldIsNoop(t *testing.T) {
	in := []Record{{"id": "a", "binder": []any{"x", "y"}}}
	out := explodeRecords(in, "")
	if len(out) != 1 {
		t.Fatalf("empty field name should be a no-op, got %d rows", len(out))
	}
}

// Integration: a one-record-per-file index (binder as array) + a per-binder
// Markdown context block, collapsed via --explode binder --merge-on id,binder.
func TestExplodeWithMergeOn(t *testing.T) {
	dir := t.TempDir()
	notes := filepath.Join(dir, "notes")
	col := filepath.Join(notes, "collections")
	if err := os.MkdirAll(col, 0o755); err != nil {
		t.Fatal(err)
	}
	md := "## Refs\n\n```yaml\nid: ref-aaa\nbinder: Test\nstatus: annotated\n```\n"
	if err := os.WriteFile(filepath.Join(notes, "binder.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	index := `{"id": "ref-aaa", "binder": ["Test", "Inbox"], "filename": "vertrag.pdf", "kind": "pdf"}
{"id": "ref-bbb", "binder": ["Inbox"], "filename": "brief.pdf", "kind": "pdf"}
`
	if err := os.WriteFile(filepath.Join(col, "inbox.jsonl"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}

	// Filtered to binder=Test: only the merged ref-aaa/Test membership survives.
	g, err := NewGrubber(notes, false, false, false, true, nil, 0, nil,
		[]string{"binder=Test"}, nil, []string{col}, []string{"id", "binder"})
	if err != nil {
		t.Fatal(err)
	}
	g.SetExplode("binder")
	records, _, err := g.Extract(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record (ref-aaa in Test), got %d: %v", len(records), records)
	}
	r := records[0]
	if r["binder"] != "Test" {
		t.Errorf("binder should be the exploded scalar Test: %v", r["binder"])
	}
	if r["status"] != "annotated" {
		t.Errorf("annotation field missing (scanned record should win): %v", r)
	}
	if r["filename"] != "vertrag.pdf" {
		t.Errorf("index field should be back-filled into the annotation: %v", r)
	}

	// Unfiltered: ref-aaa explodes to Test+Inbox, ref-bbb stays Inbox → 3 rows
	// (the Test row merged with its annotation block).
	g2, err := NewGrubber(notes, false, false, false, true, nil, 0, nil,
		nil, nil, []string{col}, []string{"id", "binder"})
	if err != nil {
		t.Fatal(err)
	}
	g2.SetExplode("binder")
	all, _, err := g2.Extract(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 membership rows, got %d: %v", len(all), all)
	}
}
