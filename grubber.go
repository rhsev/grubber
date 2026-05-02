package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

var (
	frontmatterRe = regexp.MustCompile("(?s)^---\n(.*?)\n---\n")
	yamlBlockRe   = regexp.MustCompile("(?s)```yaml\n(.*?)\n```")
	yamlMarker    = []byte("```yaml")
)

type Record map[string]any

type noteResult struct {
	metadata Record
	records  []Record
}

type Grubber struct {
	notesDir        string
	blocksOnly      bool
	frontmatterOnly bool
	useMmd          bool
	noFill          bool
	depth           *int
	workers         int
	arrayFields     []string
	filter          *Filter
	singleFile      bool
}

func NewGrubber(notesDir string, blocksOnly, frontmatterOnly, useMmd, noFill bool, depth *int, workers int, arrayFields, filters []string) (*Grubber, error) {
	expanded, err := expandPath(notesDir)
	if err != nil {
		return nil, fmt.Errorf("could not resolve path: %w", err)
	}
	info, err := os.Stat(expanded)
	if err != nil {
		return nil, fmt.Errorf("not found: %s", expanded)
	}
	var f *Filter
	if len(filters) > 0 {
		f, err = NewFilter(filters)
		if err != nil {
			return nil, err
		}
	}
	return &Grubber{
		notesDir:        expanded,
		blocksOnly:      blocksOnly,
		frontmatterOnly: frontmatterOnly,
		useMmd:          useMmd,
		noFill:          noFill,
		depth:           depth,
		workers:         workers,
		arrayFields:     arrayFields,
		filter:          f,
		singleFile:      !info.IsDir(),
	}, nil
}

func (g *Grubber) workerCount() int {
	if g.workers > 0 {
		return g.workers
	}
	return runtime.NumCPU()
}

func (g *Grubber) processFiles(files []string) <-chan []Record {
	wc := g.workerCount()
	fileCh := make(chan string, wc)
	resultCh := make(chan []Record)

	var wg sync.WaitGroup
	for range wc {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range fileCh {
				records, err := g.processFile(path)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error processing %s: %v\n", path, err)
					resultCh <- nil
					continue
				}
				resultCh <- records
			}
		}()
	}
	go func() {
		for _, f := range files {
			fileCh <- f
		}
		close(fileCh)
	}()
	go func() {
		wg.Wait()
		close(resultCh)
	}()
	return resultCh
}

func (g *Grubber) Extract(files []string) (records []Record, keys []string, err error) {
	if files == nil {
		files, err = g.markdownFiles()
		if err != nil {
			return
		}
	}
	if len(files) == 0 {
		return nil, nil, nil
	}

	var allKeys map[string]struct{}
	if !g.noFill {
		allKeys = make(map[string]struct{})
	}
	for fileRecords := range g.processFiles(files) {
		for _, r := range fileRecords {
			records = append(records, r)
			if allKeys != nil {
				for k := range r {
					allKeys[k] = struct{}{}
				}
			}
		}
	}

	if !g.noFill {
		keys = make([]string, 0, len(allKeys))
		for k := range allKeys {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for i, r := range records {
			if len(r) < len(keys) {
				normalized := make(Record, len(keys))
				for _, k := range keys {
					normalized[k] = r[k]
				}
				records[i] = normalized
			}
		}
	}

	// Sort by _note_file basename for deterministic output
	sort.SliceStable(records, func(i, j int) bool {
		bi, bj := "", ""
		if v, ok := records[i]["_note_file"].(string); ok {
			bi = filepath.Base(v)
		}
		if v, ok := records[j]["_note_file"].(string); ok {
			bj = filepath.Base(v)
		}
		return bi < bj
	})
	return
}

// StreamNDJSON writes records as newline-delimited JSON as they are processed,
// without buffering all records in memory first.
func (g *Grubber) StreamNDJSON(w io.Writer) error {
	files, err := g.markdownFiles()
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	for fileRecords := range g.processFiles(files) {
		for _, r := range fileRecords {
			if err := enc.Encode(r); err != nil {
				return err
			}
		}
	}
	return nil
}

func (g *Grubber) processFile(path string) ([]Record, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	mtime := info.ModTime().UTC().Format(time.RFC3339)

	note, err := g.parseNote(path)
	if err != nil {
		return nil, err
	}
	var result []Record
	for _, rec := range note.records {
		flat := make(Record, len(note.metadata)+len(rec)+1)
		for k, v := range note.metadata {
			flat[k] = v
		}
		for k, v := range rec {
			flat[k] = v
		}
		flat["_mtime"] = mtime
		if len(g.arrayFields) > 0 {
			g.normalizeArrays(flat)
		}
		if g.filter != nil && !g.filter.Match(flat) {
			continue
		}
		result = append(result, flat)
	}
	return result, nil
}

func (g *Grubber) parseNote(path string) (*noteResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var frontmatter Record
	var body []byte

	if m := frontmatterRe.FindSubmatch(data); m != nil {
		frontmatter = g.parseYAMLString(m[1])
		body = data[len(m[0]):]
	} else if g.useMmd {
		var bodyStr string
		frontmatter, bodyStr = g.parseMmdHeader(string(data))
		body = []byte(bodyStr)
	} else {
		body = data
	}

	if g.frontmatterOnly {
		return g.buildResult(path, frontmatter, nil), nil
	}
	if !bytes.Contains(body, yamlMarker) {
		return g.buildResult(path, frontmatter, nil), nil
	}

	return g.buildResult(path, frontmatter, g.parseYAMLBlocks(body)), nil
}

func (g *Grubber) buildResult(path string, frontmatter Record, yamlRecords []Record) *noteResult {
	metadata := make(Record, len(frontmatter)+1)
	for k, v := range frontmatter {
		metadata[k] = v
	}
	metadata["_note_file"] = path

	hasFrontmatter := len(frontmatter) > 0
	records := yamlRecords
	if len(records) == 0 && !g.blocksOnly && !g.frontmatterOnly && hasFrontmatter {
		records = []Record{{}}
	}
	if len(records) == 0 && g.frontmatterOnly && hasFrontmatter {
		records = []Record{{}}
	}
	return &noteResult{metadata: metadata, records: records}
}

func (g *Grubber) parseYAMLString(content []byte) Record {
	var node yaml.Node
	if err := yaml.Unmarshal(content, &node); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Could not parse YAML: %v\n", err)
		return nil
	}
	if node.Kind == 0 || len(node.Content) == 0 {
		return nil
	}
	doc := node.Content[0]
	if doc.Kind != yaml.MappingNode {
		return nil
	}
	// Walk pairs manually: last-value-wins for duplicate keys
	result := make(Record, len(doc.Content)/2)
	for i := 0; i+1 < len(doc.Content); i += 2 {
		key := doc.Content[i].Value
		var val any
		doc.Content[i+1].Decode(&val) //nolint:errcheck
		result[key] = stringifyDates(val)
	}
	return result
}

func (g *Grubber) parseYAMLBlocks(body []byte) []Record {
	matches := yamlBlockRe.FindAllSubmatch(body, -1)
	records := make([]Record, 0, len(matches))
	for _, m := range matches {
		if r := g.parseYAMLString(m[1]); len(r) > 0 {
			records = append(records, r)
		}
	}
	return records
}

func (g *Grubber) markdownFiles() ([]string, error) {
	if g.singleFile {
		return []string{g.notesDir}, nil
	}
	if g.depth != nil {
		var files []string
		for d := range *g.depth + 1 {
			pattern := g.notesDir + "/" + strings.Repeat("*/", d) + "*.md"
			matches, err := filepath.Glob(pattern)
			if err != nil {
				return nil, err
			}
			files = append(files, matches...)
		}
		return files, nil
	}
	var files []string
	err := filepath.WalkDir(g.notesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && path != g.notesDir {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(path, ".md") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func (g *Grubber) normalizeArrays(r Record) {
	for k, v := range r {
		if !containsStr(g.arrayFields, k) {
			continue
		}
		s, ok := v.(string)
		if !ok {
			continue
		}
		if strings.Contains(s, ",") {
			parts := strings.Split(s, ",")
			arr := make([]any, 0, len(parts))
			for _, p := range parts {
				if p = strings.TrimSpace(p); p != "" {
					arr = append(arr, p)
				}
			}
			r[k] = arr
		} else {
			r[k] = []any{v}
		}
	}
}

func (g *Grubber) parseMmdHeader(content string) (Record, string) {
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

func (g *Grubber) OutputJSON(records []Record, w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(records)
}

func (g *Grubber) OutputTSV(records []Record, keys []string, w io.Writer) error {
	if len(records) == 0 {
		return nil
	}
	fmt.Fprintln(w, strings.Join(keys, "\t"))
	replacer := strings.NewReplacer("\t", " ", "\n", " ", "\r", " ")
	for _, r := range records {
		row := make([]string, len(keys))
		for i, k := range keys {
			switch val := r[k].(type) {
			case nil:
				row[i] = ""
			case []any:
				parts := make([]string, len(val))
				for j, a := range val {
					parts[j] = fmt.Sprint(a)
				}
				row[i] = strings.Join(parts, ", ")
			default:
				row[i] = replacer.Replace(fmt.Sprint(val))
			}
		}
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}
	return nil
}

func stringifyDates(v any) any {
	switch val := v.(type) {
	case time.Time:
		return val.Format("2006-01-02")
	case map[string]any:
		result := make(map[string]any, len(val))
		for k, vv := range val {
			result[k] = stringifyDates(vv)
		}
		return result
	case []any:
		result := make([]any, len(val))
		for i, vv := range val {
			result[i] = stringifyDates(vv)
		}
		return result
	}
	return v
}

func expandPath(path string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, path[2:]), nil
	}
	return filepath.Abs(path)
}

func containsStr(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
