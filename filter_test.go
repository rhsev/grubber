package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/text/unicode/norm"
)

func TestParseConditionValid(t *testing.T) {
	cases := []struct {
		input string
		field string
		op    byte
		value string
	}{
		{"status=done", "status", '=', "done"},
		{"title~meeting", "title", '~', "meeting"},
		{"type^proj", "type", '^', "proj"},
		{"tag!archived", "tag", '!', "archived"},
		{"Status=Done", "Status", '=', "done"},
	}
	for _, tc := range cases {
		c, err := parseCondition(tc.input)
		if err != nil {
			t.Errorf("parseCondition(%q): unexpected error: %v", tc.input, err)
			continue
		}
		if c.field != tc.field || c.op != tc.op || c.value != tc.value {
			t.Errorf("parseCondition(%q) = {%q %c %q}, want {%q %c %q}",
				tc.input, c.field, c.op, c.value, tc.field, tc.op, tc.value)
		}
	}
}

func TestParseConditionInvalid(t *testing.T) {
	for _, s := range []string{"nodot", "", "  "} {
		if _, err := parseCondition(s); err == nil {
			t.Errorf("parseCondition(%q): expected error, got nil", s)
		}
	}
}

func filter(t *testing.T, exprs ...string) *Filter {
	t.Helper()
	f, err := NewFilter(exprs)
	if err != nil {
		t.Fatalf("NewFilter(%v): %v", exprs, err)
	}
	return f
}

func TestFilterEqual(t *testing.T) {
	f := filter(t, "status=done")
	if !f.Match(Record{"status": "done"}) {
		t.Error("exact match failed")
	}
	if f.Match(Record{"status": "open"}) {
		t.Error("non-match should fail")
	}
	if !f.Match(Record{"status": "Done"}) {
		t.Error("case-insensitive match failed")
	}
}

func TestFilterContains(t *testing.T) {
	f := filter(t, "title~meet")
	if !f.Match(Record{"title": "weekly meeting"}) {
		t.Error("contains match failed")
	}
	if f.Match(Record{"title": "standup"}) {
		t.Error("non-match should fail")
	}
}

func TestFilterPrefix(t *testing.T) {
	f := filter(t, "type^proj")
	if !f.Match(Record{"type": "project"}) {
		t.Error("prefix match failed")
	}
	if f.Match(Record{"type": "note"}) {
		t.Error("non-match should fail")
	}
}

func TestFilterNot(t *testing.T) {
	f := filter(t, "status!archived")
	if !f.Match(Record{"status": "open"}) {
		t.Error("not-equal should match other values")
	}
	if f.Match(Record{"status": "archived"}) {
		t.Error("not-equal should not match excluded value")
	}
}

func TestFilterMissingField(t *testing.T) {
	if filter(t, "tag=foo").Match(Record{}) {
		t.Error("missing field with = should not match")
	}
	if !filter(t, "tag!foo").Match(Record{}) {
		t.Error("missing field with ! should match")
	}
}

func TestFilterNilField(t *testing.T) {
	if filter(t, "tag=foo").Match(Record{"tag": nil}) {
		t.Error("nil field with = should not match")
	}
	if !filter(t, "tag!foo").Match(Record{"tag": nil}) {
		t.Error("nil field with ! should match")
	}
}

func TestFilterArray(t *testing.T) {
	f := filter(t, "tags=go")
	if !f.Match(Record{"tags": []any{"go", "cli"}}) {
		t.Error("array member should match")
	}
	if f.Match(Record{"tags": []any{"ruby", "cli"}}) {
		t.Error("non-member should not match")
	}
}

func TestFilterMultipleConditions(t *testing.T) {
	f := filter(t, "status=done", "type=task")
	if !f.Match(Record{"status": "done", "type": "task"}) {
		t.Error("all conditions met should match")
	}
	if f.Match(Record{"status": "done", "type": "note"}) {
		t.Error("not all conditions met should not match")
	}
}

func TestFilterNotEqualsAlias(t *testing.T) {
	f, err := NewFilter([]string{"type!=vertrag"})
	if err != nil {
		t.Fatal(err)
	}
	if f.Match(Record{"type": "vertrag"}) {
		t.Error("type!=vertrag should reject type=vertrag")
	}
	if !f.Match(Record{"type": "meeting"}) {
		t.Error("type!=vertrag should accept type=meeting")
	}
}

// nfcNFD returns the composed and decomposed form of s and fails the test if
// they happen to be byte-equal, which would make every check below vacuous.
// The forms are built here rather than written as literals because editors
// tend to normalize source files silently.
func nfcNFD(t *testing.T, s string) (nfc, nfd string) {
	t.Helper()
	nfc, nfd = norm.NFC.String(s), norm.NFD.String(s)
	if nfc == nfd {
		t.Fatalf("%q has no decomposable characters", s)
	}
	return nfc, nfd
}

func TestFilterMatchesAcrossNormalizationForms(t *testing.T) {
	nfcVal, nfdVal := nfcNFD(t, "Ümläut Reise")
	nfcNeedle, nfdNeedle := nfcNFD(t, "Ümläut")

	cases := []struct {
		name   string
		value  string
		filter string
		want   bool
	}{
		{"= NFD value, NFC filter", nfdVal, "album=" + nfcVal, true},
		{"= NFC value, NFD filter", nfcVal, "album=" + nfdVal, true},
		{"~ NFD value, NFC filter", nfdVal, "album~" + nfcNeedle, true},
		{"~ NFC value, NFD filter", nfcVal, "album~" + nfdNeedle, true},
		{"^ NFD value, NFC filter", nfdVal, "album^" + nfcNeedle, true},
		{"^ NFC value, NFD filter", nfcVal, "album^" + nfdNeedle, true},
		// "!" must exclude the record whichever form either side is in.
		{"! NFD value, NFC filter", nfdVal, "album!" + nfcVal, false},
		{"! NFC value, NFD filter", nfcVal, "album!" + nfdVal, false},
	}
	for _, tc := range cases {
		if got := filter(t, tc.filter).Match(Record{"album": tc.value}); got != tc.want {
			t.Errorf("%s: Match = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestFilterResultDoesNotDependOnForm(t *testing.T) {
	// The point of normalizing is that the form a value happens to be stored
	// in stops mattering. That has to hold for plain ASCII filters too: NFD
	// spells ü as u plus a combining mark, so before normalization "~u"
	// matched it in NFD text but not in NFC text.
	nfcVal, nfdVal := nfcNFD(t, "Müller")
	for _, expr := range []string{"name~u", "name^mu", "name=müller", "name!müller", "name~ü"} {
		f := filter(t, expr)
		if a, b := f.Match(Record{"name": nfcVal}), f.Match(Record{"name": nfdVal}); a != b {
			t.Errorf("%s: NFC value matched %v, NFD value matched %v", expr, a, b)
		}
	}
}

func TestFilteredOutputKeepsOriginalForm(t *testing.T) {
	// Normalization is for comparing only: a value written in NFD must leave
	// grubber as NFD, byte for byte.
	nfcVal, nfdVal := nfcNFD(t, "Ümläut Reise")
	dir := t.TempDir()
	note := "---\nalbum: " + nfdVal + "\n---\n\n```yaml\ntype: ref\nid: 1\n```\n"
	if err := os.WriteFile(filepath.Join(dir, "n.md"), []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}

	g, err := NewGrubber(dir, false, false, false, true, nil, 0, nil,
		[]string{"album~" + nfcVal}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	records, _, err := g.Extract(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("NFC filter should find the NFD note, got %d records", len(records))
	}
	if records[0]["album"] != nfdVal {
		t.Errorf("record value was rewritten: got %q, want the NFD original", records[0]["album"])
	}

	var buf bytes.Buffer
	if err := g.OutputJSON(records, &buf); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(nfdVal)) || bytes.Contains(buf.Bytes(), []byte(nfcVal)) {
		t.Errorf("JSON output must carry the NFD bytes unchanged:\n%s", buf.String())
	}
}
