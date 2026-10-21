package main

import "testing"

// _line is the 1-based line of a block's opening fence, counted in the whole
// file, frontmatter included. With _note_file it is the block's address for
// tools that work on one entry (commenter --line).
func TestLineOfEachBlock(t *testing.T) {
	note := "---\r\ntitle: x\r\n---\r\n\r\n### A\r\n```yaml\r\nid: 1\r\n```\r\n\r\n```yaml\r\nid: 2\r\n```\r\n"
	recs := extractNote(t, note, false, nil)
	if len(recs) != 2 {
		t.Fatalf("want 2 records, got %d", len(recs))
	}
	for i, want := range []int{6, 10} {
		if got := recs[i]["_line"]; got != want {
			t.Errorf("record %d: _line = %v, want %d", i+1, got, want)
		}
	}
}

func TestFrontmatterRecordHasNoLine(t *testing.T) {
	recs := extractNote(t, "---\ntitle: x\n---\n\nBody.\n", false, nil)
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	if _, ok := recs[0]["_line"]; ok {
		t.Errorf("a frontmatter record has no block, so no _line: %v", recs[0])
	}
}
