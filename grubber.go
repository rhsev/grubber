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
	fromJSONL       []string
	mergeOn         []string
	// explode names a field whose array value is expanded into one record per
	// element before merge (see explodeRecords). Empty = disabled.
	explode string
	// postFilter holds the filters while --merge-on or --explode is active: they
	// must run against the *merged/exploded* records, otherwise a filter on an
	// annotation field would drop the index record before it can back-fill its
	// fields, or a filter on the exploded field would keep the wrong elements.
	postFilter *Filter
}

// SetExplode enables array explosion on the given field. Like --merge-on it
// defers filtering to after the explode step (filters then see the per-element
// rows). Safe to call when --merge-on already moved the filters to postFilter.
func (g *Grubber) SetExplode(field string) {
	g.explode = field
	if field != "" && g.filter != nil {
		g.postFilter, g.filter = g.filter, nil
	}
}

func NewGrubber(notesDir string, blocksOnly, frontmatterOnly, useMmd, noFill bool, depth *int, workers int, arrayFields, filters, extensions, fromJSONL, mergeOn []string) (*Grubber, error) {
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
	} else if len(fromJSONL) == 0 {
		return nil, fmt.Errorf("no notes directory or --from-jsonl source given")
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
	var postFilter *Filter
	if len(mergeOn) > 0 {
		postFilter, f = f, nil
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
		fromJSONL:       fromJSONL,
		mergeOn:         mergeOn,
		postFilter:      postFilter,
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

// collectRecords gathers the scan-path and JSONL-source records separately,
// applying array normalization and the per-record filter (nil while
// --merge-on is active; see postFilter).
func (g *Grubber) collectRecords(files []string) (scanned, jsonl []Record, err error) {
	if g.notesDir != "" {
		if files == nil {
			files, err = g.textFiles()
			if err != nil {
				return
			}
		}
		for fileRecords := range g.processFiles(files) {
			scanned = append(scanned, fileRecords...)
		}
	}

	if len(g.fromJSONL) > 0 {
		var srcPaths []string
		srcPaths, err = expandJSONLSources(g.fromJSONL)
		if err != nil {
			return
		}
		for _, srcPath := range srcPaths {
			srcRecords, rerr := readJSONLSource(srcPath)
			if rerr != nil {
				fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", srcPath, rerr)
				continue
			}
			for _, r := range srcRecords {
				if len(g.arrayFields) > 0 {
					g.normalizeArrays(r)
				}
				if g.filter != nil && !g.filter.Match(r) {
					continue
				}
				jsonl = append(jsonl, r)
			}
		}
	}
	return
}

// mergedRecords runs collect → merge → post-filter, the shared front half of
// Extract and the buffered StreamJSONL path.
func (g *Grubber) mergedRecords(files []string) ([]Record, error) {
	scanned, jsonl, err := g.collectRecords(files)
	if err != nil {
		return nil, err
	}
	if g.explode != "" {
		scanned = explodeRecords(scanned, g.explode)
		jsonl = explodeRecords(jsonl, g.explode)
	}
	var records []Record
	if len(g.mergeOn) > 0 {
		records = mergeRecords(scanned, jsonl, g.mergeOn)
	} else {
		records = append(scanned, jsonl...)
	}
	if g.postFilter != nil {
		kept := records[:0]
		for _, r := range records {
			if g.postFilter.Match(r) {
				kept = append(kept, r)
			}
		}
		records = kept
	}
	return records, nil
}

func (g *Grubber) Extract(files []string) (records []Record, keys []string, err error) {
	records, err = g.mergedRecords(files)
	if err != nil {
		return nil, nil, err
	}

	if len(records) == 0 {
		return nil, nil, nil
	}

	if !g.noFill {
		allKeys := make(map[string]struct{})
		for _, r := range records {
			for k := range r {
				allKeys[k] = struct{}{}
			}
		}
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

// StreamJSONL writes records as newline-delimited JSON as they are processed,
// without buffering all records in memory first. With --merge-on, merging
// needs the full record set, so that path buffers like Extract does.
func (g *Grubber) StreamJSONL(w io.Writer) error {
	enc := json.NewEncoder(w)

	if len(g.mergeOn) > 0 || g.explode != "" {
		records, err := g.mergedRecords(nil)
		if err != nil {
			return err
		}
		for _, r := range records {
			if err := enc.Encode(r); err != nil {
				return err
			}
		}
		return nil
	}

	if g.notesDir != "" {
		files, err := g.textFiles()
		if err != nil {
			return err
		}
		// On encode error keep draining the channel so the worker
		// goroutines can finish instead of blocking on send forever.
		var encErr error
		for fileRecords := range g.processFiles(files) {
			if encErr != nil {
				continue
			}
			for _, r := range fileRecords {
				if err := enc.Encode(r); err != nil {
					encErr = err
					break
				}
			}
		}
		if encErr != nil {
			return encErr
		}
	}

	if len(g.fromJSONL) > 0 {
		srcPaths, err := expandJSONLSources(g.fromJSONL)
		if err != nil {
			return err
		}
		for _, srcPath := range srcPaths {
			srcRecords, err := readJSONLSource(srcPath)
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
				for _, m := range matches {
					if !inHiddenDir(g.notesDir, m) {
						files = append(files, m)
					}
				}
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
					parts[j] = replacer.Replace(fmt.Sprint(a))
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
	if path == "~" {
		return os.UserHomeDir()
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, path[2:]), nil
	}
	return filepath.Abs(path)
}

// inHiddenDir reports whether path lies inside a hidden directory below root.
// Mirrors the WalkDir skip rule: hidden directories are excluded, hidden files
// themselves are not.
func inHiddenDir(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	parts := strings.Split(rel, string(filepath.Separator))
	for _, p := range parts[:len(parts)-1] {
		if strings.HasPrefix(p, ".") {
			return true
		}
	}
	return false
}

func containsStr(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
