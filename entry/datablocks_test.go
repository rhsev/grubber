package entry

import (
	"reflect"
	"strings"
	"testing"
)

func TestDataBlocks(t *testing.T) {
	cases := []struct {
		name, note string
		want       []Span
	}{
		{"plain", "a\n```yaml\nx: 1\n```\nb", []Span{{1, 3}}},
		{"empty block, then the next", "```yaml\n```\n\n```yaml\nx: 1\n```", []Span{{0, 1}, {3, 5}}},
		{"indented fences, trailing blanks", "- item\n  ```yaml \n  x: 1\n\t```\t", []Span{{1, 3}}},
		{"carriage returns", "```yaml\r\nx: 1\r\n```\r\n", []Span{{0, 2}}},
		{"no closing fence", "```yaml\nx: 1\n", nil},
		{"closed block, then an open one", "```yaml\n```\n```yaml\nx: 1", []Span{{0, 1}}},
		{"a second opener is the first block's text", "```yaml\n```yaml\n```", []Span{{0, 2}}},
		{"other languages are no data", "```yml\nx: 1\n```\n```yaml x\n```", nil},
		{"a longer fence does not hide a block", "````\n```yaml\nx: 1\n```\n````", []Span{{1, 3}}},
		{"nor does a tilde fence", "~~~\n```yaml\nx: 1\n```\n~~~", []Span{{1, 3}}},
		{"a fence needs its own line", "text ```yaml\nx: 1\n```", nil},
	}
	for _, c := range cases {
		if got := DataBlocks(strings.Split(c.note, "\n")); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// ParseBlocks and DataBlocks differ in one thing only: ParseBlocks leaves out
// a block that stands inside another code fence or in the frontmatter.
func TestParseBlocksIsDataBlocksOutsideOtherFences(t *testing.T) {
	note := "# a\n```yaml\nx: 1\n```\n````\n```yaml\ny: 2\n```\n````\n\n```yaml\n```\n"
	lines := strings.Split(note, "\n")
	var parsed []Span
	for _, b := range ParseBlocks(lines) {
		parsed = append(parsed, Span{b.Open, b.Close})
	}
	if want := []Span{{1, 3}, {10, 11}}; !reflect.DeepEqual(parsed, want) {
		t.Errorf("ParseBlocks: got %v, want %v", parsed, want)
	}
	if want := []Span{{1, 3}, {5, 7}, {10, 11}}; !reflect.DeepEqual(DataBlocks(lines), want) {
		t.Errorf("DataBlocks: got %v, want %v", DataBlocks(lines), want)
	}
}

func TestFields(t *testing.T) {
	cases := []struct {
		name, body string
		want       map[string]any
		lenient    bool
	}{
		{"mapping", "type: ref\nid: '42'\naka: [a, b]\n", map[string]any{"type": "ref", "id": "42", "aka": []any{"a", "b"}}, false},
		{"a key twice, the last wins", "id: 5\nid: 6\n", map[string]any{"id": 6}, false},
		{"empty", "", nil, false},
		{"a list is no record", "- one\n- two\n", nil, false},
		{"a colon in a value", "type: ref\nid: '42'\ncomment: see: there\n",
			map[string]any{"type": "ref", "id": "42", "comment": "see: there"}, true},
		{"an open quote", "id: 77\nbinder: \"My Docs\"\ncomment: \"open\n",
			map[string]any{"id": "77", "binder": "My Docs", "comment": "\"open"}, true},
		{"quotes inside quotes", "id: 'it''s'\nname: \"a \\\"b\\\"\"\nbad: x: y\n",
			map[string]any{"id": "it's", "name": "a \"b\"", "bad": "x: y"}, true},
		{"quotes that are not one scalar", "a: 'x' and 'y'\nbad: x: y\n",
			map[string]any{"a": "'x' and 'y'", "bad": "x: y"}, true},
		{"not valid, no field", "\t\"open\n", nil, true},
	}
	for _, c := range cases {
		got, lenient := Fields([]byte(c.body))
		if !reflect.DeepEqual(got, c.want) || lenient != c.lenient {
			t.Errorf("%s: got %#v (lenient %v), want %#v (lenient %v)", c.name, got, lenient, c.want, c.lenient)
		}
	}
}
