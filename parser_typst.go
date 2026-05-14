package main

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
)

func init() {
	RegisterParser(".typ", &typstParser{})
}

var typstDateRe = regexp.MustCompile(`datetime\(\s*year:\s*(\d+),\s*month:\s*(\d+),\s*day:\s*(\d+)\s*\)`)

type typstParser struct{}

func (p *typstParser) Extract(path string, data []byte, opts ParseOpts) (Record, []Record, error) {
	// #metadata((...)) <label>  – custom per-document metadata tuple
	// #set document(...)        – standard Typst document metadata
	block := typstFindBlock(data, "#metadata((")
	if block == nil {
		block = typstFindBlock(data, "#set document(")
	}
	if block == nil {
		return nil, nil, nil
	}
	rec := parseTypstFields(block)
	if len(rec) == 0 {
		return nil, nil, nil
	}
	// Return as a block record so blocksOnly=true still includes Typst files.
	// There is no YAML-block concept in Typst; #set document() is the whole record.
	return nil, []Record{rec}, nil
}

// typstFindBlock finds the content after prefix up to the matching closing paren.
// prefix must end with the opening '(' already consumed (depth starts at 1).
// Works for both "#set document(" and "#metadata((" (double-paren tuple call).
func typstFindBlock(data []byte, prefix string) []byte {
	idx := bytes.Index(data, []byte(prefix))
	if idx < 0 {
		return nil
	}
	start := idx + len(prefix)
	depth := 1
	for i := start; i < len(data); i++ {
		switch data[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return data[start:i]
			}
		}
	}
	return nil
}

// parseTypstFields parses key: value pairs from a Typst argument list.
func parseTypstFields(content []byte) Record {
	// Replace datetime(...) with a quoted ISO date string before splitting on commas.
	s := typstDateRe.ReplaceAllStringFunc(string(content), func(m string) string {
		sub := typstDateRe.FindStringSubmatch(m)
		if sub == nil {
			return m
		}
		return fmt.Sprintf("%q", fmt.Sprintf("%s-%02s-%02s", sub[1], sub[2], sub[3]))
	})

	fields := typstSplit(s, ',')
	result := make(Record, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		colon := strings.Index(field, ":")
		if colon <= 0 {
			continue
		}
		key := strings.TrimSpace(field[:colon])
		val := strings.TrimSpace(field[colon+1:])
		if key == "" || val == "" {
			continue
		}
		result[key] = parseTypstValue(val)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// typstSplit splits s on sep only when not inside parentheses or double quotes.
func typstSplit(s string, sep rune) []string {
	var parts []string
	depth, start := 0, 0
	inStr := false
	for i, ch := range s {
		switch {
		case inStr && ch == '"':
			inStr = false
		case !inStr && ch == '"':
			inStr = true
		case !inStr && ch == '(':
			depth++
		case !inStr && ch == ')':
			depth--
		case !inStr && depth == 0 && ch == sep:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		parts = append(parts, s[start:])
	}
	return parts
}

// parseTypstValue converts a Typst literal to a Go value.
func parseTypstValue(s string) any {
	s = strings.TrimSpace(s)
	// Quoted string: "..."
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	// Array: ("a", "b", ...)
	if len(s) >= 2 && s[0] == '(' && s[len(s)-1] == ')' {
		parts := typstSplit(s[1:len(s)-1], ',')
		arr := make([]any, 0, len(parts))
		for _, p := range parts {
			if v := parseTypstValue(p); v != "" {
				arr = append(arr, v)
			}
		}
		return arr
	}
	return s
}
