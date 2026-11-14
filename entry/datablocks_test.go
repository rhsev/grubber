package entry

import (
	"math/rand"
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
		{"a block in a longer fence is an example", "````\n```yaml\nx: 1\n```\n````\n```yaml\n```", []Span{{5, 6}}},
		{"so is one in a tilde fence", "~~~\n```yaml\nx: 1\n```\n~~~", nil},
		{"and one in a fence with a language", "````markdown\n```yaml\nx: 1\n```\n````", nil},
		{"a fence of three closes at the block's fence", "```sh\n```yaml\nx: 1\n```\n```yaml\ny: 2\n```", []Span{{4, 6}}},
		{"a fence never closed holds the rest", "~~~\n```yaml\nx: 1\n```\n", nil},
		{"an unclosed block hides no fence", "```yaml\nx: 1\n````\ntext\n````", nil},
		{"a fence needs its own line", "text ```yaml\nx: 1\n```", nil},
	}
	for _, c := range cases {
		if got := DataBlocks(strings.Split(c.note, "\n")); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// ParseBlocks finds the blocks DataBlocks finds. It looks at the same lines
// for the prose around them, with code of its own.
func TestParseBlocksFindsTheDataBlocks(t *testing.T) {
	pieces := []string{"```yaml", "```", "  ```yaml ", "\t```\t", "a: 1", "", "### h", "````", "`````", "~~~", "~~~~",
		"```yml", "```python", "````markdown", " ", "```yaml x", "x ```", "````yaml", "---", "text"}
	r := rand.New(rand.NewSource(1))
	for n := 0; n < 50000; n++ {
		lines := []string{"text"} // no frontmatter, which ParseBlocks cuts off itself
		for k := r.Intn(14); k > 0; k-- {
			lines = append(lines, pieces[r.Intn(len(pieces))])
		}
		var parsed []Span
		for _, b := range ParseBlocks(lines) {
			parsed = append(parsed, Span{b.Open, b.Close})
		}
		if data := DataBlocks(lines); !reflect.DeepEqual(parsed, data) {
			t.Fatalf("%q\nParseBlocks %v\n DataBlocks %v", lines, parsed, data)
		}
	}
}

func TestUnclosedData(t *testing.T) {
	note := "```yaml\n```\n````\n```yaml\n````\n```yaml\nx: 1"
	if got, want := UnclosedData(strings.Split(note, "\n")), []int{5}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
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

// A heading inside a code fence is no heading, and HeadingAt names the line
// of the one that is.
func TestHeadingAt(t *testing.T) {
	note := "```yaml\na: 1\n```\n### one\n~~~\n# no heading\n~~~\n```yaml\nb: 2\n```"
	blocks := ParseBlocks(strings.Split(note, "\n"))
	if len(blocks) != 2 || blocks[0].HeadingAt != -1 || blocks[1].HeadingAt != 3 || blocks[1].Heading != "one" {
		t.Errorf("got %+v", blocks)
	}
}
