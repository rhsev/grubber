package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/rhsev/grubber/entry"
	"gopkg.in/yaml.v3"
)

func init() {
	RegisterParser(".md", &mdParser{})
}

var yamlMarker = []byte("```yaml")

type mdParser struct{}

func (p *mdParser) Extract(path string, data []byte, opts ParseOpts) (Record, []Record, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))

	var frontmatter Record
	var body []byte

	d := opts.Diag
	// Where the frontmatter ends, and what an MMD header is, stands in
	// package entry, for the tools that cut a note at the same place.
	if fm, rest, ok := entry.Frontmatter(data); ok {
		d.setPos(path, 2) // frontmatter content starts after the opening ---
		frontmatter = parseYAMLString(fm, d)
		body = rest
	} else if d != nil && opensFrontmatter(data) {
		d.setPos(path, 1)
		d.add("unclosed-frontmatter", "first line is --- but no closing --- follows, frontmatter ignored")
		body = data
	} else if opts.UseMmd {
		var bodyStr string
		frontmatter, bodyStr = parseMmdHeader(string(data))
		body = []byte(bodyStr)
	} else {
		body = data
	}

	if opts.FrontmatterOnly || !bytes.Contains(body, yamlMarker) {
		return frontmatter, nil, nil
	}
	d.setPos(path, 0)
	baseLine := bytes.Count(data[:len(data)-len(body)], []byte("\n"))
	return frontmatter, parseYAMLBlocks(body, baseLine, d), nil
}

// opensFrontmatter reports a file whose first line is ---. Without a closing
// line (entry.Frontmatter found none) it has no frontmatter for extract.
func opensFrontmatter(data []byte) bool {
	first, _, _ := bytes.Cut(data, []byte("\n"))
	return string(bytes.TrimSuffix(first, []byte("\r"))) == "---"
}

func parseYAMLString(content []byte, d *Diagnostics) Record {
	// What a block's text holds, and when it is read line by line, stands in
	// package entry, for the tools that edit the blocks read here.
	doc, err := entry.Mapping(content)
	if err != nil {
		if d != nil {
			d.add("yaml-error", "%v, line-by-line fallback used", err)
		} else {
			fmt.Fprintf(os.Stderr, "Warning: YAML parse error, using line-by-line fallback: %v\n", err)
		}
		return parseYAMLLenient(content)
	}
	if doc == nil {
		return nil
	}
	var seen map[string]bool
	if d != nil {
		seen = make(map[string]bool, len(doc.Content)/2)
	}
	result := make(Record, len(doc.Content)/2)
	for i := 0; i+1 < len(doc.Content); i += 2 {
		key := doc.Content[i].Value
		var val any
		keepTimeOfDay(doc.Content[i+1])
		doc.Content[i+1].Decode(&val) //nolint:errcheck
		if d != nil {
			if seen[key] {
				d.add("duplicate-key", "%s: duplicate key, last value wins", key)
			}
			seen[key] = true
			d.inspectValue(key, val)
			d.inspectRaw(key, doc.Content[i+1])
		}
		result[key] = normalizeValue(val)
	}
	return result
}

// keepTimeOfDay makes a timestamp with a time of day decode as the text the
// note holds. Decoded as a time it would go out as its date alone and lose
// the time; a plain date still decodes as a time and becomes YYYY-MM-DD.
func keepTimeOfDay(node *yaml.Node) {
	switch node.Kind {
	case yaml.ScalarNode:
		v := node.Value
		// Cheap test first: resolving the tag costs as much as decoding.
		if node.Style == 0 && len(v) > 10 && v[0] >= '0' && v[0] <= '9' &&
			strings.IndexByte(v, ':') > 0 && node.ShortTag() == "!!timestamp" {
			node.Tag = "!!str"
		}
	case yaml.SequenceNode:
		for _, c := range node.Content {
			keepTimeOfDay(c)
		}
	case yaml.MappingNode:
		for i := 1; i < len(node.Content); i += 2 {
			keepTimeOfDay(node.Content[i])
		}
	}
}

func parseYAMLLenient(content []byte) Record {
	return Record(entry.LenientFields(content))
}

func parseYAMLBlocks(body []byte, baseLine int, d *Diagnostics) []Record {
	// What a block is stands in entry.DataBlocks, for grubber and for the
	// tools that edit the blocks it reads.
	lines := strings.Split(string(body), "\n")
	spans := entry.DataBlocks(lines)
	records := make([]Record, 0, len(spans))
	for _, s := range spans {
		line := baseLine + s.Open + 1
		if d != nil {
			d.setLine(line)
		}
		text := ""
		if s.Close > s.Open+1 {
			text = strings.Join(lines[s.Open+1:s.Close], "\n") + "\n"
		}
		if r := parseYAMLString([]byte(text), d); len(r) > 0 {
			// The opening fence's line: with _note_file the block's address,
			// for tools that work on one entry (commenter --line).
			r["_line"] = line
			records = append(records, r)
		}
	}
	if d != nil {
		for _, i := range entry.UnclosedData(lines) {
			d.setLine(baseLine + i + 1)
			d.add("unclosed-fence", "```yaml fence without a closing fence, block ignored")
		}
	}
	return records
}

func parseMmdHeader(content string) (Record, string) {
	meta, _, body := entry.MMDHeader(content)
	return Record(meta), body
}
