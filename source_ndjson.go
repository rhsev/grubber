package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// expandNDJSONSources resolves each path: files are used as-is, directories
// are expanded to their *.ndjson children (non-recursive, sorted by filename).
func expandNDJSONSources(srcs []string) ([]string, error) {
	var out []string
	for _, src := range srcs {
		info, err := os.Stat(src)
		if err != nil {
			return nil, fmt.Errorf("--from-ndjson: %w", err)
		}
		if !info.IsDir() {
			out = append(out, src)
			continue
		}
		matches, err := filepath.Glob(filepath.Join(src, "*.ndjson"))
		if err != nil {
			return nil, err
		}
		sort.Strings(matches)
		out = append(out, matches...)
	}
	return out, nil
}

// readNDJSONSource reads records from a single NDJSON file.
// Blank lines are skipped; malformed or non-object lines are warned and skipped.
// Preserve-else-inject: if a record lacks _note_file, it is set to srcPath
// (and _mtime to srcPath's mtime).
func readNDJSONSource(srcPath string) ([]Record, error) {
	f, err := os.Open(srcPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := os.Stat(srcPath)
	if err != nil {
		return nil, err
	}
	mtime := info.ModTime().UTC().Format(time.RFC3339)

	var records []Record
	scanner := bufio.NewScanner(f)
	lineno := 0
	for scanner.Scan() {
		lineno++
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		var raw any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: %s:%d: invalid JSON, skipping\n", srcPath, lineno)
			continue
		}
		obj, ok := raw.(map[string]any)
		if !ok {
			fmt.Fprintf(os.Stderr, "Warning: %s:%d: expected JSON object, skipping\n", srcPath, lineno)
			continue
		}
		r := Record(obj)
		if _, hasFile := r["_note_file"]; !hasFile {
			r["_note_file"] = srcPath
			r["_mtime"] = mtime
		}
		records = append(records, r)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return records, nil
}
