package main

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

func init() {
	RegisterParser(".md", &mdParser{})
}

var (
	frontmatterRe = regexp.MustCompile("(?s)^---\r?\n(.*?)\r?\n---\r?\n")
	// Fences must be fence-only lines, but may be indented (e.g. blocks
	// inside list items) — an indented closing fence must still terminate
	// the block, or the match runs on into unrelated prose.
	yamlBlockRe = regexp.MustCompile("(?ms)^[ \t]*```yaml[ \t]*\r?$\n(.*?)\r?\n[ \t]*```[ \t]*\r?$")
	// Opening fences alone, for doctor: an opener that is not part of a
	// yamlBlockRe match has no closing fence and its block is ignored.
	yamlFenceOpenRe = regexp.MustCompile("(?m)^[ \t]*```yaml[ \t]*\r?$")
	yamlMarker      = []byte("```yaml")
)

type mdParser struct{}

func (p *mdParser) Extract(path string, data []byte, opts ParseOpts) (Record, []Record, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))

	var frontmatter Record
	var body []byte

	d := opts.Diag
	if m := frontmatterRe.FindSubmatch(data); m != nil {
		d.setPos(path, 2) // frontmatter content starts after the opening ---
		frontmatter = parseYAMLString(m[1], d)
		body = data[len(m[0]):]
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

func parseYAMLString(content []byte, d *Diagnostics) Record {
	var node yaml.Node
	if err := yaml.Unmarshal(content, &node); err != nil {
		if d != nil {
			d.add("yaml-error", "%v, line-by-line fallback used", err)
		} else {
			fmt.Fprintf(os.Stderr, "Warning: YAML parse error, using line-by-line fallback: %v\n", err)
		}
		return parseYAMLLenient(content)
	}
	if node.Kind == 0 || len(node.Content) == 0 {
		return nil
	}
	doc := node.Content[0]
	if doc.Kind != yaml.MappingNode {
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
		doc.Content[i+1].Decode(&val) //nolint:errcheck
		if d != nil {
			if seen[key] {
				d.add("duplicate-key", "%s: duplicate key, last value wins", key)
			}
			seen[key] = true
			d.inspectValue(key, val)
		}
		result[key] = normalizeValue(val)
	}
	return result
}

func parseYAMLLenient(content []byte) Record {
	result := make(Record)
	for _, rawLine := range bytes.Split(content, []byte("\n")) {
		line := bytes.TrimSpace(rawLine)
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		idx := bytes.IndexByte(line, ':')
		if idx <= 0 {
			continue
		}
		key := string(bytes.TrimSpace(line[:idx]))
		val := string(bytes.TrimSpace(line[idx+1:]))
		if key == "" {
			continue
		}
		if val == "" {
			result[key] = nil
		} else {
			result[key] = val
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func parseYAMLBlocks(body []byte, baseLine int, d *Diagnostics) []Record {
	newline := []byte("\n")
	matches := yamlBlockRe.FindAllSubmatchIndex(body, -1)
	records := make([]Record, 0, len(matches))
	for _, m := range matches {
		if d != nil {
			d.setLine(baseLine + bytes.Count(body[:m[0]], newline) + 1)
		}
		if r := parseYAMLString(body[m[2]:m[3]], d); len(r) > 0 {
			records = append(records, r)
		}
	}
	if d != nil {
		for _, f := range yamlFenceOpenRe.FindAllIndex(body, -1) {
			if !insideSpan(f[0], matches) {
				d.setLine(baseLine + bytes.Count(body[:f[0]], newline) + 1)
				d.add("unclosed-fence", "```yaml fence without a closing fence, block ignored")
			}
		}
	}
	return records
}

func insideSpan(off int, spans [][]int) bool {
	for _, s := range spans {
		if off >= s[0] && off < s[1] {
			return true
		}
	}
	return false
}

func parseMmdHeader(content string) (Record, string) {
	metadata := make(Record)
	lines := strings.Split(content, "\n")
	lastKey := ""

	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			return metadata, strings.Join(lines[i+1:], "\n")
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if lastKey != "" {
				if existing, ok := metadata[lastKey].(string); ok {
					metadata[lastKey] = existing + "\n" + strings.TrimSpace(line)
				}
			}
		} else if idx := strings.Index(line, ":"); idx >= 0 {
			key := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(line[:idx]), " ", "_"))
			metadata[key] = strings.TrimSpace(line[idx+1:])
			lastKey = key
		} else {
			return make(Record), content
		}
	}
	return metadata, ""
}
