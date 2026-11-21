package entry

import (
	"bytes"
	"strings"
)

// What a note carries above its text: YAML frontmatter between two --- lines,
// or a MultiMarkdown header. grubber reads its records by these rules, and a
// tool that shows or edits a note cuts it at the same place.

var fence = []byte("---")

// Frontmatter splits a note into the text of its frontmatter and the body
// below it. A note has frontmatter if its first line is exactly --- and a
// later line is exactly --- as well; that line closes it, and may be the
// file's last without a line break. A --- inside a value closes nothing, and
// a first line that is never closed is no frontmatter. The frontmatter may be
// empty. Both lines may end in \r. data is the note without a byte order mark.
func Frontmatter(data []byte) (fm, body []byte, ok bool) {
	open, rest, more := cutLine(data)
	if !bytes.Equal(open, fence) || !more {
		return nil, data, false
	}
	start := len(data) - len(rest)
	for more {
		var line []byte
		at := len(data) - len(rest)
		line, rest, more = cutLine(rest)
		if bytes.Equal(line, fence) {
			fm = data[start:at]
			fm = bytes.TrimSuffix(fm, []byte("\n"))
			fm = bytes.TrimSuffix(fm, []byte("\r"))
			return fm, rest, true
		}
	}
	return nil, data, false
}

// cutLine returns the first line of data without its line break or a \r
// before it, the rest, and whether a line break followed.
func cutLine(data []byte) (line, rest []byte, more bool) {
	line, rest, more = bytes.Cut(data, []byte("\n"))
	return bytes.TrimSuffix(line, []byte("\r")), rest, more
}

// FrontmatterEnd is Frontmatter for a note held as lines: the index of the
// frontmatter's closing line, or -1.
func FrontmatterEnd(lines []string) int {
	if len(lines) == 0 || strings.TrimSuffix(lines[0], "\r") != "---" {
		return -1
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSuffix(lines[i], "\r") == "---" {
			return i
		}
	}
	return -1
}

// MMDHeader splits a MultiMarkdown metadata header off a note: the fields,
// the header's lines as they stand, and the body below it.
//
// The header is the note's first lines up to the first blank one, which is
// dropped. Each line is `key: value`; a line that starts with a blank or a
// tab continues the value above it. A key is lowercased and its blanks become
// underscores, so `Due Date` is the field due_date. One line that is neither
// makes the whole note body: there is no header then, and meta is empty. A
// note that starts with a blank line has no header either, and body starts
// below that line.
func MMDHeader(content string) (meta map[string]any, header, body string) {
	meta = map[string]any{}
	lines := strings.Split(content, "\n")
	lastKey := ""
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			return meta, strings.Join(lines[:i], "\n"), strings.Join(lines[i+1:], "\n")
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if existing, ok := meta[lastKey].(string); ok && lastKey != "" {
				meta[lastKey] = existing + "\n" + strings.TrimSpace(line)
			}
		} else if idx := strings.Index(line, ":"); idx >= 0 {
			key := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(line[:idx]), " ", "_"))
			meta[key] = strings.TrimSpace(line[idx+1:])
			lastKey = key
		} else {
			return map[string]any{}, "", content
		}
	}
	return meta, content, ""
}
