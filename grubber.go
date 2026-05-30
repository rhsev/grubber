package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
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
	extensions      []string
	filter          *Filter
	singleFile      bool
	fromNDJSON      []string
}

func NewGrubber(notesDir string, blocksOnly, frontmatterOnly, useMmd, noFill bool, depth *int, workers int, arrayFields, filters, extensions, fromNDJSON []string) (*Grubber, error) {
	var expanded string
	var singleFile bool
	if notesDir != "" {
		var err error
		expanded, err = expandPath(notesDir)
		if err != nil {
			return nil, fmt.Errorf("could not resolve path: %w", err)
		}
		info, err := os.Stat(expanded)
		if err != nil {
			return nil, fmt.Errorf("not found: %s", expanded)
		}
		singleFile = !info.IsDir()
	} else if len(fromNDJSON) == 0 {
		return nil, fmt.Errorf("no notes directory or --from-ndjson source given")
	}
	var f *Filter
	if len(filters) > 0 {
		var err error
		f, err = NewFilter(filters)
		if err != nil {
			return nil, err
		}
	}
	if len(extensions) == 0 {
		extensions = registeredExtensions()
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
		extensions:      extensions,
		filter:          f,
		singleFile:      singleFile,
		fromNDJSON:      fromNDJSON,
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
	var allKeys map[string]struct{}
	if !g.noFill {
		allKeys = make(map[string]struct{})
	}

	accumulate := func(r Record) {
		records = append(records, r)
		if allKeys != nil {
			for k := range r {
				allKeys[k] = struct{}{}
			}
		}
	}

	// Scan path
	if g.notesDir != "" {
		if files == nil {
			files, err = g.textFiles()
			if err != nil {
				return
			}
		}
		for fileRecords := range g.processFiles(files) {
			for _, r := range fileRecords {
				accumulate(r)
			}
		}
	}

	// NDJSON source path
	if len(g.fromNDJSON) > 0 {
		var srcPaths []string
		srcPaths, err = expandNDJSONSources(g.fromNDJSON)
		if err != nil {
			return
		}
		for _, srcPath := range srcPaths {
			var srcRecords []Record
			srcRecords, err = readNDJSONSource(srcPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", srcPath, err)
				err = nil
				continue
			}
			for _, r := range srcRecords {
				if len(g.arrayFields) > 0 {
					g.normalizeArrays(r)
				}
				if g.filter != nil && !g.filter.Match(r) {
					continue
				}
				accumulate(r)
			}
		}
	}

	if len(records) == 0 {
		return nil, nil, nil
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
	enc := json.NewEncoder(w)

	if g.notesDir != "" {
		files, err := g.textFiles()
		if err != nil {
			return err
		}
		for fileRecords := range g.processFiles(files) {
			for _, r := range fileRecords {
				if err := enc.Encode(r); err != nil {
					return err
				}
			}
		}
	}

	if len(g.fromNDJSON) > 0 {
		srcPaths, err := expandNDJSONSources(g.fromNDJSON)
		if err != nil {
			return err
		}
		for _, srcPath := range srcPaths {
			srcRecords, err := readNDJSONSource(srcPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", srcPath, err)
				continue
			}
			for _, r := range srcRecords {
				if len(g.arrayFields) > 0 {
					g.normalizeArrays(r)
				}
				if g.filter != nil && !g.filter.Match(r) {
					continue
				}
				if err := enc.Encode(r); err != nil {
					return err
				}
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

	ext := filepath.Ext(path)
	parser, ok := parsers[ext]
	if !ok {
		return g.buildResult(path, nil, nil), nil
	}

	opts := ParseOpts{UseMmd: g.useMmd, FrontmatterOnly: g.frontmatterOnly}
	frontmatter, blocks, err := parser.Extract(path, data, opts)
	if err != nil {
		return nil, err
	}
	return g.buildResult(path, frontmatter, blocks), nil
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

func (g *Grubber) textFiles() ([]string, error) {
	if g.singleFile {
		return []string{g.notesDir}, nil
	}

	extSet := make(map[string]bool, len(g.extensions))
	for _, e := range g.extensions {
		extSet[e] = true
	}

	if g.depth != nil {
		var files []string
		for d := range *g.depth + 1 {
			for _, ext := range g.extensions {
				pattern := g.notesDir + "/" + strings.Repeat("*/", d) + "*" + ext
				matches, err := filepath.Glob(pattern)
				if err != nil {
					return nil, err
				}
				files = append(files, matches...)
			}
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
		if !d.IsDir() && extSet[filepath.Ext(path)] {
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
