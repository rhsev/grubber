// Package entry knows where an entry's prose starts and ends in a Markdown
// note, reads it, and replaces it. grubber reads the YAML blocks; this
// package, and the commenter command built on it, is the one place that
// decides about the text around them.
//
//   - A data block opens with a ```yaml line and closes with a ``` line.
//     ```yml and every other fence language is an example, never data.
//   - An entry ends with its block's closing fence.
//   - It starts after the nearest boundary above: a heading of any level, the
//     closing fence of another data block, a line of three or more `-`, the
//     end of the frontmatter, or the start of the file. Headings and `---`
//     inside code fences do not count.
//   - The prose is every line between boundary and opening fence, blank lines
//     at both ends dropped. Code fences in it belong to the prose.
//   - The entry's heading is the nearest heading above the block, even when
//     another block stands between them.
//   - Text after a block belongs to no entry; if another block follows without
//     a boundary, it is that block's prose.
//
// The cases are in testdata/entries: each note with a .jsonl file that lists
// its entries. Tools that read prose with code of their own can check
// themselves against the same files.
package entry

import (
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
	"gopkg.in/yaml.v3"
)

var (
	// The fences grubber reads: indentation and trailing blanks allowed.
	dataOpenRe  = regexp.MustCompile("^[ \t]*```yaml[ \t]*$")
	dataCloseRe = regexp.MustCompile("^[ \t]*```[ \t]*$")
	ruleRe      = regexp.MustCompile(`^-{3,}$`)
)

// Block is one data block and the entry around it. Indexes are 0-based lines.
type Block struct {
	Start       int    // first line after the boundary
	Open, Close int    // the fence lines
	Heading     string // text of the nearest heading above, "" if none
	Fields      map[string]any
}

// Prose returns the block's prose: the lines between boundary and opening
// fence, without blank lines at either end.
func (b Block) Prose(lines []string) string {
	from, to := b.Start, b.Open
	for from < to && strings.TrimSpace(lines[from]) == "" {
		from++
	}
	for to > from && strings.TrimSpace(lines[to-1]) == "" {
		to--
	}
	return strings.Join(lines[from:to], "\n")
}

// Matches reports whether every pair names a field of the block with that
// value. Values compare as text in NFC, as register compares binder names: a
// name pasted from the Finder may stand decomposed in a note. A list matches
// when one of its items does.
func (b Block) Matches(pairs [][2]string) bool {
	if b.Fields == nil {
		return false
	}
	for _, p := range pairs {
		v, ok := b.Fields[p[0]]
		if !ok || v == nil || !fieldEquals(v, p[1]) {
			return false
		}
	}
	return true
}

func fieldEquals(v any, want string) bool {
	if list, ok := v.([]any); ok {
		for _, item := range list {
			if fieldEquals(item, want) {
				return true
			}
		}
		return false
	}
	return norm.NFC.String(fmt.Sprint(v)) == norm.NFC.String(want)
}

// headingText returns the text of an ATX heading line, and whether it is one.
// Like register, a heading starts in column 0.
func headingText(line string) (string, bool) {
	n := 0
	for n < len(line) && line[n] == '#' {
		n++
	}
	if n < 1 || n > 6 || (n < len(line) && line[n] != ' ' && line[n] != '\t') {
		return "", false
	}
	return strings.TrimSpace(strings.TrimRight(strings.TrimSpace(line[n:]), "#")), true
}

func isHeadingLine(line string) bool {
	_, ok := headingText(line)
	return ok
}

// frontmatterEnd returns the index of the frontmatter's closing line, or -1.
// As in grubber, the file's first line must be exactly ---.
func frontmatterEnd(lines []string) int {
	if len(lines) == 0 || lines[0] != "---" {
		return -1
	}
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			return i
		}
	}
	return -1
}

// codeFence reports whether a line opens a code fence, with its marker.
func codeFence(line string) (string, bool) {
	t := strings.TrimSpace(line)
	for _, m := range []string{"```", "~~~"} {
		if strings.HasPrefix(t, m) {
			return m, true
		}
	}
	return "", false
}

// closesFence reports whether a line closes a fence opened with marker.
func closesFence(line, marker string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, marker) && strings.Trim(t, marker[:1]) == ""
}

// ParseBlocks returns every closed data block of a note, in order.
func ParseBlocks(lines []string) []Block {
	var blocks []Block
	boundary := 0
	if fm := frontmatterEnd(lines); fm >= 0 {
		boundary = fm + 1
	}
	heading := ""
	for i := boundary; i < len(lines); i++ {
		line := lines[i]
		if dataOpenRe.MatchString(line) {
			j := i + 1
			for j < len(lines) && !dataCloseRe.MatchString(lines[j]) {
				j++
			}
			if j < len(lines) {
				var fields map[string]any
				if yaml.Unmarshal([]byte(strings.Join(lines[i+1:j], "\n")), &fields) != nil {
					fields = nil
				}
				blocks = append(blocks, Block{Start: boundary, Open: i, Close: j, Heading: heading, Fields: fields})
				boundary = j + 1
				i = j
				continue
			}
			// An unclosed ```yaml is no block (grubber ignores it); read on.
			continue
		}
		if marker, ok := codeFence(line); ok {
			j := i + 1
			for j < len(lines) && !closesFence(lines[j], marker) {
				j++
			}
			i = j
			continue
		}
		if text, ok := headingText(line); ok {
			heading = text
			boundary = i + 1
			continue
		}
		if ruleRe.MatchString(strings.TrimSpace(line)) {
			boundary = i + 1
		}
	}
	return blocks
}

// ReplaceProse puts prose between b's boundary and its opening fence and
// returns the new lines. A heading is followed by the block directly when the
// prose is empty, as register writes a lean block; any other boundary keeps a
// blank line, so two blocks never touch.
func ReplaceProse(lines []string, b Block, prose string) []string {
	prose = strings.Trim(prose, "\n")
	var region []string
	afterHeading := b.Start > 0 && isHeadingLine(lines[b.Start-1])
	switch {
	case prose == "" && (b.Start == 0 || afterHeading):
	case prose == "":
		region = []string{""}
	case b.Start == 0:
		region = append(strings.Split(prose, "\n"), "")
	default:
		region = append([]string{""}, strings.Split(prose, "\n")...)
		region = append(region, "")
	}
	out := make([]string, 0, len(lines)-(b.Open-b.Start)+len(region))
	out = append(out, lines[:b.Start]...)
	out = append(out, region...)
	return append(out, lines[b.Open:]...)
}
