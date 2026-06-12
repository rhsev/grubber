package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempJSONL(t *testing.T, dir, name string, lines []string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// Test 1: Pure replay — no directory, only --from-jsonl
func TestJSONLPureReplay(t *testing.T) {
	dir := t.TempDir()
	p := writeTempJSONL(t, dir, "test.jsonl", []string{
		`{"name":"alice","_note_file":"/notes/alice.md","_mtime":"2024-01-01T00:00:00Z"}`,
		`{"name":"bob","_note_file":"/notes/bob.md","_mtime":"2024-01-01T00:00:00Z"}`,
	})
	g, err := NewGrubber("", false, false, false, true, nil, 0, nil, nil, nil, []string{p}, nil)
	if err != nil {
		t.Fatal(err)
	}
	records, _, err := g.Extract(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
}

// Test 2: Merge — directory scan + JSONL source, counts add up
func TestJSONLMerge(t *testing.T) {
	if _, err := os.Stat("examples"); err != nil {
		t.Skip("examples dir not available")
	}
	tmp := t.TempDir()
	extraP := writeTempJSONL(t, tmp, "extra.jsonl", []string{
		`{"name":"extra-record","type":"test","_note_file":"/extra.md"}`,
	})

	gMerge, err := NewGrubber("examples", false, false, false, true, nil, 0, nil, nil, nil, []string{extraP}, nil)
	if err != nil {
		t.Fatal(err)
	}
	gScan, err := NewGrubber("examples", false, false, false, true, nil, 0, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	merged, _, err := gMerge.Extract(nil)
	if err != nil {
		t.Fatal(err)
	}
	scanned, _, err := gScan.Extract(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged) != len(scanned)+1 {
		t.Errorf("expected scanned+1=%d records, got %d", len(scanned)+1, len(merged))
	}
}

// Test 3: Preserve-else-inject — existing _note_file preserved; absent one injected
func TestJSONLPreserveElseInject(t *testing.T) {
	dir := t.TempDir()
	p := writeTempJSONL(t, dir, "test.jsonl", []string{
		`{"name":"alice","_note_file":"/original/alice.md","_mtime":"2020-01-01T00:00:00Z"}`,
		`{"name":"bob"}`,
	})
	g, err := NewGrubber("", false, false, false, true, nil, 0, nil, nil, nil, []string{p}, nil)
	if err != nil {
		t.Fatal(err)
	}
	records, _, err := g.Extract(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	var alice, bob Record
	for _, r := range records {
		if r["name"] == "alice" {
			alice = r
		} else if r["name"] == "bob" {
			bob = r
		}
	}
	if alice == nil || bob == nil {
		t.Fatal("expected both alice and bob records")
	}
	if alice["_note_file"] != "/original/alice.md" {
		t.Errorf("alice _note_file should be preserved, got %v", alice["_note_file"])
	}
	if alice["_mtime"] != "2020-01-01T00:00:00Z" {
		t.Errorf("alice _mtime should be preserved, got %v", alice["_mtime"])
	}
	if bob["_note_file"] != p {
		t.Errorf("bob _note_file should be injected as %q, got %v", p, bob["_note_file"])
	}
	if bob["_mtime"] == "" {
		t.Error("bob _mtime should be injected")
	}
}

// Test 4: Round-trip — extract → serialize to jsonl → replay → identical _note_file
func TestJSONLRoundTrip(t *testing.T) {
	if _, err := os.Stat("examples"); err != nil {
		t.Skip("examples dir not available")
	}
	g1, err := NewGrubber("examples", false, false, false, true, nil, 0, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	original, _, err := g1.Extract(nil)
	if err != nil {
		t.Fatal(err)
	}

	tmp := t.TempDir()
	jsonlPath := filepath.Join(tmp, "cache.jsonl")
	f, err := os.Create(jsonlPath)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	for _, r := range original {
		if err := enc.Encode(r); err != nil {
			f.Close()
			t.Fatal(err)
		}
	}
	f.Close()

	g2, err := NewGrubber("", false, false, false, true, nil, 0, nil, nil, nil, []string{jsonlPath}, nil)
	if err != nil {
		t.Fatal(err)
	}
	replayed, _, err := g2.Extract(nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(replayed) != len(original) {
		t.Fatalf("round-trip count mismatch: %d vs %d", len(replayed), len(original))
	}
	for i := range replayed {
		origFile, _ := original[i]["_note_file"].(string)
		replayFile, _ := replayed[i]["_note_file"].(string)
		if origFile != replayFile {
			t.Errorf("record %d: _note_file changed from %q to %q", i, origFile, replayFile)
		}
	}
}

// Test 5: Directory source — dir of two *.jsonl files, all records, _note_file per-file
func TestJSONLDirectorySource(t *testing.T) {
	dir := t.TempDir()
	p1 := writeTempJSONL(t, dir, "a.jsonl", []string{`{"name":"from-a"}`})
	p2 := writeTempJSONL(t, dir, "b.jsonl", []string{`{"name":"from-b"}`})

	g, err := NewGrubber("", false, false, false, true, nil, 0, nil, nil, nil, []string{dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	records, _, err := g.Extract(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	// Sorted by _note_file basename: a.jsonl < b.jsonl
	if records[0]["_note_file"] != p1 {
		t.Errorf("first record _note_file should be %q, got %v", p1, records[0]["_note_file"])
	}
	if records[1]["_note_file"] != p2 {
		t.Errorf("second record _note_file should be %q, got %v", p2, records[1]["_note_file"])
	}
}

// Test 6: Line-level rules — blank lines skipped, malformed warned+skipped, nested values preserved
func TestJSONLLineRules(t *testing.T) {
	dir := t.TempDir()
	p := writeTempJSONL(t, dir, "test.jsonl", []string{
		`{"name":"first","tags":["a","b"],"meta":{"key":"val"}}`,
		``,
		`not json{`,
		`[1,2,3]`,
		`{"name":"last"}`,
	})

	records, err := readJSONLSource(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 valid records, got %d", len(records))
	}
	if records[0]["name"] != "first" || records[1]["name"] != "last" {
		t.Errorf("unexpected records: %v %v", records[0]["name"], records[1]["name"])
	}
	tags, ok := records[0]["tags"].([]any)
	if !ok || len(tags) != 2 {
		t.Errorf("nested array not preserved: %v", records[0]["tags"])
	}
	meta, ok := records[0]["meta"].(map[string]any)
	if !ok || meta["key"] != "val" {
		t.Errorf("nested object not preserved: %v", records[0]["meta"])
	}
}

// Test 7: Filter applies to source records
func TestJSONLFilterApplied(t *testing.T) {
	dir := t.TempDir()
	p := writeTempJSONL(t, dir, "test.jsonl", []string{
		`{"type":"keep","name":"alice"}`,
		`{"type":"drop","name":"bob"}`,
	})
	g, err := NewGrubber("", false, false, false, true, nil, 0, nil, []string{"type=keep"}, nil, []string{p}, nil)
	if err != nil {
		t.Fatal(err)
	}
	records, _, err := g.Extract(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record after filter, got %d", len(records))
	}
	if records[0]["name"] != "alice" {
		t.Errorf("expected alice, got %v", records[0]["name"])
	}
}

// Test 8: No directory and no source is an error
func TestJSONLNoSourceNoDir(t *testing.T) {
	_, err := NewGrubber("", false, false, false, false, nil, 0, nil, nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error when no directory and no --from-jsonl source")
	}
}

// Test 9: -b/-m flags do not drop source records
func TestJSONLBlocksFrontmatterModeNoEffect(t *testing.T) {
	dir := t.TempDir()
	p := writeTempJSONL(t, dir, "test.jsonl", []string{
		`{"name":"alice","_note_file":"/notes/alice.md"}`,
	})

	gBlocks, err := NewGrubber("", true, false, false, true, nil, 0, nil, nil, nil, []string{p}, nil)
	if err != nil {
		t.Fatal(err)
	}
	records, _, err := gBlocks.Extract(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Errorf("blocks-only should not filter JSONL source records, got %d records", len(records))
	}

	gFM, err := NewGrubber("", false, true, false, true, nil, 0, nil, nil, nil, []string{p}, nil)
	if err != nil {
		t.Fatal(err)
	}
	records2, _, err := gFM.Extract(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(records2) != 1 {
		t.Errorf("frontmatter-only should not filter JSONL source records, got %d records", len(records2))
	}
}

func TestJSONLLongLine(t *testing.T) {
	dir := t.TempDir()
	big := `{"name":"big","data":"` + strings.Repeat("x", 100*1024) + `"}`
	p := writeTempJSONL(t, dir, "big.jsonl", []string{big, `{"name":"small"}`})
	records, err := readJSONLSource(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
}
