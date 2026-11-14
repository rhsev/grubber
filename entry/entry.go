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

var ruleRe = regexp.MustCompile(`^-{3,}$`)

// IsDataOpen reports whether a line opens a data block: ```yaml alone on the
// line. Indentation, trailing blanks and a carriage return are allowed.
func IsDataOpen(line string) bool { return fenceText(line) == "```yaml" }

// IsDataClose reports whether a line closes a data block: ``` alone on the
// line, with the same allowances.
func IsDataClose(line string) bool { return fenceText(line) == "```" }

func fenceText(line string) string {
	return strings.Trim(strings.TrimSuffix(line, "\r"), " \t")
}

// Span is a data block by the lines of its fences, 0-based.
type Span struct{ Open, Close int }

// DataBlocks returns every data block grubber reads from the lines of a note,
// in order. This is the rule for what a data block is, and the one place it
// stands: grubber reads its records with it, and a tool that edits blocks
// finds them with it, so the two see the same blocks.
//
// A block runs from a ```yaml line to the next ``` line and may be empty. A
// ```yaml line without a closing fence below it is no block. A block inside
// another code fence is that fence's text, an example and no data: one in a
// fence of four backticks, in a ~~~ fence, or below a fence that was never
// closed. The lines are a note's body; frontmatter is the caller's to cut off.
func DataBlocks(lines []string) []Span {
	spans, _ := scanData(lines)
	return spans
}

// UnclosedData returns the lines that open a data block no fence closes,
// 0-based. grubber reads no record from there on, and doctor says so.
func UnclosedData(lines []string) []int {
	_, unclosed := scanData(lines)
	return unclosed
}

func scanData(lines []string) (spans []Span, unclosed []int) {
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if IsDataOpen(line) {
			j := i + 1
			for j < len(lines) && !IsDataClose(lines[j]) {
				j++
			}
			if j < len(lines) {
				spans = append(spans, Span{Open: i, Close: j})
				i = j
			} else {
				unclosed = append(unclosed, i)
			}
			continue
		}
		if marker, ok := codeFence(line); ok {
			i++
			for i < len(lines) && !closesFence(lines[i], marker) {
				i++
			}
		}
	}
	return spans, unclosed
}

// Mapping parses the text between a block's fences. A block that is a YAML
// mapping comes back as its node. One that is valid YAML of another kind, or
// empty, holds no record: nil without an error. An error means the text is
// not valid YAML, and the block is then read with LenientFields.
func Mapping(body []byte) (*yaml.Node, error) {
	var node yaml.Node
	if err := yaml.Unmarshal(body, &node); err != nil {
		return nil, err
	}
	if node.Kind == 0 || len(node.Content) == 0 || node.Content[0].Kind != yaml.MappingNode {
		return nil, nil
	}
	return node.Content[0], nil
}

// Fields reads the text between a block's fences into its fields, as grubber
// does: a mapping by its keys, the last one winning when a key stands twice,
// and text that is not valid YAML line by line. lenient reports the second
// case. nil means the block holds no record.
func Fields(body []byte) (fields map[string]any, lenient bool) {
	doc, err := Mapping(body)
	if err != nil {
		return LenientFields(body), true
	}
	if doc == nil {
		return nil, false
	}
	fields = make(map[string]any, len(doc.Content)/2)
	for i := 0; i+1 < len(doc.Content); i += 2 {
		var val any
		doc.Content[i+1].Decode(&val) //nolint:errcheck
		fields[doc.Content[i].Value] = val
	}
	return fields, false
}

// LenientFields reads a block that is not valid YAML line by line: every
// line of the form key: value is a field, whatever its indentation, and the
// value is the rest of the line as text. A value in quotes that close on the
// line is read without them, as YAML would read it, so that id: '42' is the
// id 42 here too. Lists, nested keys and values over several lines are not
// understood. nil when no line is a field.
func LenientFields(body []byte) map[string]any {
	result := make(map[string]any)
	for _, raw := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || line[0] == '#' {
			continue
		}
		idx := strings.IndexByte(line, ':')
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		if key == "" {
			continue
		}
		if val == "" {
			result[key] = nil
		} else {
			result[key] = unquoted(val)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// unquoted returns the text of a quoted YAML scalar, and v itself when v is
// anything else: the quote that opens v must close it at its end and nowhere
// before.
func unquoted(v string) string {
	if len(v) < 2 || (v[0] != '\'' && v[0] != '"') || v[len(v)-1] != v[0] {
		return v
	}
	inner := v[1 : len(v)-1]
	if v[0] == '\'' {
		if strings.Contains(strings.ReplaceAll(inner, "''", ""), "'") {
			return v
		}
		return strings.ReplaceAll(inner, "''", "'")
	}
	for i := 0; i < len(inner); i++ {
		switch inner[i] {
		case '\\':
			i++
			if i == len(inner) {
				return v // the closing quote is escaped
			}
		case '"':
			return v
		}
	}
	var text string
	if yaml.Unmarshal([]byte(v), &text) != nil {
		return v
	}
	return text
}

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

// codeFence reports whether a line opens a code fence, with its marker: the
// whole run of backticks or tildes, three or more.
func codeFence(line string) (string, bool) {
	t := strings.TrimSpace(line)
	for _, c := range []byte{'`', '~'} {
		n := 0
		for n < len(t) && t[n] == c {
			n++
		}
		if n >= 3 {
			return t[:n], true
		}
	}
	return "", false
}

// closesFence reports whether a line closes a fence opened with marker: the
// same character alone on the line, at least as many as opened it. A fence of
// four backticks can so hold an example with fences of three.
func closesFence(line, marker string) bool {
	t := strings.TrimSpace(line)
	return len(t) >= len(marker) && strings.Trim(t, marker[:1]) == ""
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
		if IsDataOpen(line) {
			j := i + 1
			for j < len(lines) && !IsDataClose(lines[j]) {
				j++
			}
			if j < len(lines) {
				fields, _ := Fields([]byte(strings.Join(lines[i+1:j], "\n")))
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
