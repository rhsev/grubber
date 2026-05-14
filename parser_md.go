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
	frontmatterRe = regexp.MustCompile("(?s)^---\n(.*?)\n---\n")
	yamlBlockRe   = regexp.MustCompile("(?s)```yaml\n(.*?)\n```")
	yamlMarker    = []byte("```yaml")
)

type mdParser struct{}

func (p *mdParser) Extract(path string, data []byte, opts ParseOpts) (Record, []Record, error) {
	var frontmatter Record
	var body []byte

	if m := frontmatterRe.FindSubmatch(data); m != nil {
		frontmatter = parseYAMLString(m[1])
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
	return frontmatter, parseYAMLBlocks(body), nil
}

func parseYAMLString(content []byte) Record {
	var node yaml.Node
	if err := yaml.Unmarshal(content, &node); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: YAML parse error, using line-by-line fallback: %v\n", err)
		return parseYAMLLenient(content)
	}
	if node.Kind == 0 || len(node.Content) == 0 {
		return nil
	}
	doc := node.Content[0]
	if doc.Kind != yaml.MappingNode {
		return nil
	}
	result := make(Record, len(doc.Content)/2)
	for i := 0; i+1 < len(doc.Content); i += 2 {
		key := doc.Content[i].Value
		var val any
		doc.Content[i+1].Decode(&val) //nolint:errcheck
		result[key] = stringifyDates(val)
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

func parseYAMLBlocks(body []byte) []Record {
	matches := yamlBlockRe.FindAllSubmatch(body, -1)
	records := make([]Record, 0, len(matches))
	for _, m := range matches {
		if r := parseYAMLString(m[1]); len(r) > 0 {
			records = append(records, r)
		}
	}
	return records
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
