package main

import "testing"

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
