package entry

import (
	"reflect"
	"strings"
	"testing"
)

// The closing line is --- alone on its line. A --- inside a value, or a
// first line that is not exactly ---, is no frontmatter boundary.
func TestFrontmatter(t *testing.T) {
	cases := []struct {
		name, note string
		ok         bool
		fm, body   string
	}{
		{"closed", "---\ntitle: a\n---\nbody", true, "title: a", "body"},
		{"two fields", "---\na: 1\nb: 2\n---\n\nbody\n", true, "a: 1\nb: 2", "\nbody\n"},
		{"empty", "---\n---\nbody", true, "", "body"},
		{"one blank line", "---\n\n---\nbody", true, "", "body"},
		{"dashes inside a value", "---\ntitle: a --- b\nstatus: x\n---\nbody", true, "title: a --- b\nstatus: x", "body"},
		{"closed at the end of the file", "---\ntitle: a\n---", true, "title: a", ""},
		{"crlf", "---\r\ntitle: a\r\nb: 2\r\n---\r\nbody\r\n", true, "title: a\r\nb: 2", "body\r\n"},
		{"crlf, closed at the end of the file", "---\r\ntitle: a\r\n---\r", true, "title: a", ""},
		{"a longer line does not close", "---\ntitle: a\n---more\n---\nbody", true, "title: a\n---more", "body"},
		{"never closed", "---\ntitle: a\nbody", false, "", ""},
		{"only the opening line", "---", false, "", ""},
		{"only a longer closing line", "---\ntitle: a\n---more", false, "", ""},
		{"no frontmatter", "# heading\n---\ntext", false, "", ""},
		{"first line longer", "----\ntitle: a\n---\n", false, "", ""},
		{"blank after the dashes", "--- \ntitle: a\n---\n", false, "", ""},
		{"empty note", "", false, "", ""},
	}
	for _, c := range cases {
		fm, body, ok := Frontmatter([]byte(c.note))
		if !c.ok {
			c.body = c.note // without frontmatter the whole note is body
		}
		if ok != c.ok || string(fm) != c.fm || string(body) != c.body {
			t.Errorf("%s: Frontmatter = %q, %q, %v; want %q, %q, %v", c.name, fm, body, ok, c.fm, c.body, c.ok)
		}

		// The line form names the same closing line.
		lines := strings.Split(c.note, "\n")
		end := FrontmatterEnd(lines)
		if (end >= 0) != c.ok {
			t.Errorf("%s: FrontmatterEnd = %d, Frontmatter says %v", c.name, end, c.ok)
		}
		if end >= 0 {
			if rest := strings.Join(lines[end+1:], "\n"); rest != c.body {
				t.Errorf("%s: FrontmatterEnd leaves %q as body, want %q", c.name, rest, c.body)
			}
		}
	}
}

func TestMMDHeader(t *testing.T) {
	cases := []struct {
		name, note   string
		meta         map[string]any
		header, body string
	}{
		{"header and body", "Title: Hello World\nAuthor: Alice\n\nbody text",
			map[string]any{"title": "Hello World", "author": "Alice"}, "Title: Hello World\nAuthor: Alice", "body text"},
		{"a key with blanks", "Due Date: 2026-01-01\n\nbody",
			map[string]any{"due_date": "2026-01-01"}, "Due Date: 2026-01-01", "body"},
		{"continuation", "Title: Line one\n  continuation\n\nbody",
			map[string]any{"title": "Line one\ncontinuation"}, "Title: Line one\n  continuation", "body"},
		{"no blank line below", "Title: Hello\nTags: a", map[string]any{"title": "Hello", "tags": "a"}, "Title: Hello\nTags: a", ""},
		{"a colon inside the value", "Link: https://example.org\n\nbody",
			map[string]any{"link": "https://example.org"}, "Link: https://example.org", "body"},
		{"one line without a colon", "Title: Hello\nplain text\n\nbody", map[string]any{}, "", "Title: Hello\nplain text\n\nbody"},
		{"plain text first", "not a header\nTitle: Hello\n", map[string]any{}, "", "not a header\nTitle: Hello\n"},
		{"blank first line", "\n# Heading\ntext", map[string]any{}, "", "# Heading\ntext"},
		{"empty note", "", map[string]any{}, "", ""},
	}
	for _, c := range cases {
		meta, header, body := MMDHeader(c.note)
		if !reflect.DeepEqual(meta, c.meta) || header != c.header || body != c.body {
			t.Errorf("%s: MMDHeader = %v, %q, %q; want %v, %q, %q", c.name, meta, header, body, c.meta, c.header, c.body)
		}
	}
}
