package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"unicode/utf8"
)

// Finding is one doctor diagnostic: a place where extract silently degrades
// (lenient fallback, normalized value), skips data (unclosed fence), or where
// the file carries invisible characters that break search and pipelines.
type Finding struct {
	Path     string
	Line     int
	Col      int // 0 = whole line / unknown
	Category string
	Message  string
}

// Diagnostics collects findings while parsers run. All methods are safe on a
// nil receiver and record nothing — the extract path passes nil, so doctor
// sees exactly what extract does without touching its behavior.
type Diagnostics struct {
	path     string
	line     int
	findings []Finding
}

func (d *Diagnostics) setPos(path string, line int) {
	if d == nil {
		return
	}
	d.path, d.line = path, line
}

func (d *Diagnostics) setLine(line int) {
	if d == nil {
		return
	}
	d.line = line
}

func (d *Diagnostics) add(category, format string, args ...any) {
	if d == nil {
		return
	}
	d.findings = append(d.findings, Finding{Path: d.path, Line: d.line, Category: category, Message: fmt.Sprintf(format, args...)})
}

// inspectValue flags decoded YAML values that extract would normalize away
// (non-string map keys, NaN/Inf) or that carry more structure than a flat
// record wants (nested mappings). Runs on the raw decode, before
// normalizeValue erases the evidence.
func (d *Diagnostics) inspectValue(key string, v any) {
	if d == nil {
		return
	}
	switch val := v.(type) {
	case map[string]any:
		d.add("nested-mapping", "%s: value is a nested mapping", key)
	case map[any]any:
		d.add("non-string-keys", "%s: mapping with non-string keys, degrades on extract", key)
	case []any:
		nested := false
		for _, e := range val {
			switch e.(type) {
			case map[string]any, map[any]any:
				nested = true // one finding per field, not per element
			default:
				d.inspectValue(key, e)
			}
		}
		if nested {
			d.add("nested-mapping", "%s: value is a nested mapping", key)
		}
	case float64:
		if math.IsNaN(val) || math.IsInf(val, 0) {
			d.add("non-finite", "%s: NaN/Inf value, extracted as null", key)
		}
	}
}

// --- invisible characters -------------------------------------------------

// charNames covers the characters doctor knows by name; controls fall back
// to a generic label in charName.
var charNames = map[rune]string{
	0x00A0: "NO-BREAK SPACE",
	0x00AD: "SOFT HYPHEN",
	0x200B: "ZERO WIDTH SPACE",
	0x200C: "ZERO WIDTH NON-JOINER",
	0x200D: "ZERO WIDTH JOINER",
	0x200E: "LEFT-TO-RIGHT MARK",
	0x200F: "RIGHT-TO-LEFT MARK",
	0x2028: "LINE SEPARATOR",
	0x2029: "PARAGRAPH SEPARATOR",
	0x202A: "LEFT-TO-RIGHT EMBEDDING",
	0x202B: "RIGHT-TO-LEFT EMBEDDING",
	0x202C: "POP DIRECTIONAL FORMATTING",
	0x202D: "LEFT-TO-RIGHT OVERRIDE",
	0x202E: "RIGHT-TO-LEFT OVERRIDE",
	0x2060: "WORD JOINER",
	0x2061: "FUNCTION APPLICATION",
	0x2062: "INVISIBLE TIMES",
	0x2063: "INVISIBLE SEPARATOR",
	0x2064: "INVISIBLE PLUS",
	0xFEFF: "ZERO WIDTH NO-BREAK SPACE (BOM)",
}

func charName(r rune) string {
	if name, ok := charNames[r]; ok {
		return name
	}
	return "CONTROL CHARACTER"
}

// removableRune reports whether --fix strips r. The list is fixed (see
// AUFTRAG / basekit frame.Sanitize): tabs and NBSP are deliberately NOT here
// — tabs carry TaskPaper semantics, NBSP can be intentional typography.
// \r is handled separately (CRLF→LF normalization; lone \r is removed).
func removableRune(r rune) bool {
	switch {
	case r == 0x00AD || r == 0xFEFF:
		return true
	case r >= 0x200B && r <= 0x200F:
		return true
	case r >= 0x2028 && r <= 0x202E:
		return true
	case r >= 0x2060 && r <= 0x2064:
		return true
	case r >= 0x0080 && r <= 0x009F: // C1 controls
		return true
	case r < 0x20 && r != '\t' && r != '\n' && r != '\r': // C0 controls
		return true
	}
	return false
}

type charStat struct {
	count     int
	line, col int // first occurrence
}

// scanChars walks the raw file bytes and tallies removable runes, NBSP, and
// CRLF line endings, with the first occurrence of each.
func scanChars(data []byte) (stats map[rune]*charStat, crlf *charStat) {
	stats = make(map[rune]*charStat)
	line, col := 1, 1
	for i := 0; i < len(data); {
		r, size := utf8.DecodeRune(data[i:])
		if r == '\r' && i+1 < len(data) && data[i+1] == '\n' {
			if crlf == nil {
				crlf = &charStat{line: line, col: col}
			}
			crlf.count++
			i += 2
			line, col = line+1, 1
			continue
		}
		if removableRune(r) || r == 0x00A0 || r == '\r' { // lone \r: removable
			s := stats[r]
			if s == nil {
				s = &charStat{line: line, col: col}
				stats[r] = s
			}
			s.count++
		}
		i += size
		if r == '\n' {
			line, col = line+1, 1
		} else {
			col++
		}
	}
	return stats, crlf
}

// fixChars returns data with removable runes stripped and CRLF normalized to
// LF. Tabs and NBSP survive. Idempotent.
func fixChars(data []byte) []byte {
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); {
		r, size := utf8.DecodeRune(data[i:])
		if r == '\r' && i+1 < len(data) && data[i+1] == '\n' {
			out = append(out, '\n')
			i += 2
			continue
		}
		if !removableRune(r) && r != '\r' { // lone \r is removed too
			out = append(out, data[i:i+size]...)
		}
		i += size
	}
	return out
}

// charFindings converts a scan into findings, sorted by codepoint for
// deterministic output.
func charFindings(path string, stats map[rune]*charStat, crlf *charStat) []Finding {
	runes := make([]rune, 0, len(stats))
	for r := range stats {
		runes = append(runes, r)
	}
	sort.Slice(runes, func(i, j int) bool { return runes[i] < runes[j] })

	var findings []Finding
	if crlf != nil {
		findings = append(findings, Finding{path, crlf.line, crlf.col, "crlf",
			fmt.Sprintf("CRLF line endings ×%d, normalized to LF by --fix", crlf.count)})
	}
	for _, r := range runes {
		s := stats[r]
		category := "invisible-char"
		if r == 0x00A0 {
			category = "suspect-char" // reported, never fixed
		}
		findings = append(findings, Finding{path, s.line, s.col, category,
			fmt.Sprintf("U+%04X %s ×%d", r, charName(r), s.count)})
	}
	return findings
}

// --- scan and report ------------------------------------------------------

// doctorClasses maps the --only shorthand classes to their categories:
// "yaml" is the hand-work class (fix the note), "chars" the hygiene class
// (--fix handles it).
var doctorClasses = map[string][]string{
	"yaml":  {"yaml-error", "unclosed-fence", "non-string-keys", "non-finite", "duplicate-key", "nested-mapping"},
	"chars": {"invisible-char", "suspect-char", "crlf"},
}

// expandOnly resolves an --only list (class shorthands or exact category
// names) into a category set.
func expandOnly(items []string) (map[string]bool, error) {
	known := map[string]bool{"read-error": true, "parse-error": true, "write-error": true}
	for _, cats := range doctorClasses {
		for _, c := range cats {
			known[c] = true
		}
	}
	set := make(map[string]bool)
	for _, item := range items {
		if cats, ok := doctorClasses[item]; ok {
			for _, c := range cats {
				set[c] = true
			}
			continue
		}
		if !known[item] {
			return nil, fmt.Errorf("unknown category %q (classes: yaml, chars)", item)
		}
		set[item] = true
	}
	return set, nil
}

type doctorResult struct {
	findings []Finding
	scanned  int
	affected int
	fixed    int
}

// doctorScan runs the character scan and (where a parser exists) the parse
// diagnostics over files. With fix, files with removable characters or CRLF
// are rewritten atomically — except .jsonl, which is report-only: collection
// indexes belong to the register domain, a finding there is a hint, not a
// repair order.
func doctorScan(files []string, fix bool) doctorResult {
	var res doctorResult
	d := &Diagnostics{}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			d.setPos(path, 0)
			d.add("read-error", "%v", err)
			continue
		}
		res.scanned++
		before := len(d.findings)

		stats, crlf := scanChars(data)
		d.findings = append(d.findings, charFindings(path, stats, crlf)...)
		fixable := crlf != nil
		for r := range stats {
			if removableRune(r) || r == '\r' {
				fixable = true
			}
		}

		if parser, ok := parsers[filepath.Ext(path)]; ok {
			if _, _, err := parser.Extract(path, data, ParseOpts{Diag: d}); err != nil {
				d.setPos(path, 0)
				d.add("parse-error", "%v", err)
			}
		}

		if len(d.findings) > before {
			res.affected++
		}
		if fix && fixable && filepath.Ext(path) != ".jsonl" {
			if err := writeAtomic(path, fixChars(data)); err != nil {
				d.setPos(path, 0)
				d.add("write-error", "%v", err)
				continue
			}
			res.fixed++
		}
	}
	res.findings = d.findings
	return res
}

// writeAtomic replaces path via temp file + rename in the same directory.
// The file gets a new inode and mtime — sync tools and backups will see it
// as changed.
func writeAtomic(path string, data []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".doctor-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, info.Mode()); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

func runDoctor(args []string) {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	fs.Usage = printDoctorHelp
	var (
		setName       string
		extensionsStr string
		onlyStr       string
		fix           bool
	)
	fs.StringVar(&setName, "s", "", "Load options from config set")
	fs.StringVar(&setName, "set", "", "Load options from config set")
	fs.StringVar(&extensionsStr, "extensions", "", "File extensions to scan (comma-separated)")
	fs.StringVar(&onlyStr, "only", "", "Report only these categories or classes (yaml, chars; comma-separated)")
	fs.BoolVar(&fix, "fix", false, "Remove invisible characters in place (never touches .jsonl)")
	fs.Parse(reorderArgs(args, valueFlagNames(fs))) //nolint:errcheck

	var only map[string]bool
	if onlyStr != "" {
		var err error
		only, err = expandOnly(splitTrim(onlyStr, ","))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	cfg := NewConfig()
	var setCfg map[string]any
	if setName != "" {
		setCfg = cfg.GetSet(setName)
		if setCfg == nil {
			fmt.Fprintf(os.Stderr, "Error: Unknown set '%s'\n", setName)
			os.Exit(1)
		}
	}

	dir := resolveNotesDir(fs.Arg(0), cfgStr(setCfg, "path"), os.Getenv("GRUBBER_NOTES"), false, os.Getwd)

	extensions := cfg.DefaultExtensions()
	if exts := cfgStrSlice(setCfg, "extensions"); exts != nil {
		extensions = exts
	}
	if env := os.Getenv("GRUBBER_EXTENSIONS"); env != "" {
		extensions = splitTrim(env, ",")
	}
	if extensionsStr != "" {
		extensions = splitTrim(extensionsStr, ",")
	}

	g, err := NewGrubber(dir, false, false, false, false, nil, 0, nil, nil, extensions, nil, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	files, err := g.textFiles()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// JSONL sources from the set are scanned (report-only, doctorScan never
	// fixes .jsonl) — a collection index with invisible characters is worth
	// knowing about even if register owns the file.
	for _, p := range cfgStrSlice(setCfg, "from_jsonl") {
		expanded, err := expandPath(p)
		if err != nil {
			continue
		}
		srcPaths, err := expandJSONLSources([]string{expanded})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			continue
		}
		files = append(files, srcPaths...)
	}

	res := doctorScan(files, fix)
	// --only filters the report and the exit code; it does not change what
	// --fix touches.
	if only != nil {
		kept := res.findings[:0]
		affected := make(map[string]bool)
		for _, f := range res.findings {
			if only[f.Category] {
				kept = append(kept, f)
				affected[f.Path] = true
			}
		}
		res.findings = kept
		res.affected = len(affected)
	}
	for _, f := range res.findings {
		pos := fmt.Sprintf("%s:%d", f.Path, f.Line)
		if f.Col > 0 {
			pos = fmt.Sprintf("%s:%d:%d", f.Path, f.Line, f.Col)
		}
		fmt.Printf("%s\t%s\t%s\n", pos, f.Category, f.Message)
	}
	if res.fixed > 0 {
		fmt.Fprintf(os.Stderr, "fixed %d file(s) in place — new mtime/inode, sync tools and backups will see them as changed\n", res.fixed)
	}
	if len(res.findings) == 0 {
		fmt.Fprintf(os.Stderr, "clean: %d files scanned\n", res.scanned)
		return
	}
	fmt.Fprintf(os.Stderr, "%d finding(s) in %d of %d files scanned\n", len(res.findings), res.affected, res.scanned)
	os.Exit(1)
}

func printDoctorHelp() {
	fmt.Print(`Usage: grubber doctor [directory] [options]

Reports what extract only handles by degrading it, and invisible characters
that break search and pipelines. Categories:

  yaml-error       block is not valid YAML, line-by-line fallback used
  unclosed-fence   ` + "```" + `yaml fence without a closing fence, block ignored
  non-string-keys  mapping with non-string keys, degrades on extract
  non-finite       NaN/Inf value, extracted as null
  duplicate-key    duplicate key in one mapping, last value wins
  nested-mapping   value is a nested mapping in a flat record
  invisible-char   soft hyphen, zero-width, bidi, BOM, C0/C1 controls
  suspect-char     NBSP — reported only, never fixed (may be intentional)
  crlf             CRLF line endings, normalized to LF by --fix

Output is one line per finding: file:line[:col], category, message
(tab-separated). Exit code 0 means clean, 1 means findings.

Options:
  -s, --set=NAME          Load path/extensions from config set; its JSONL
                          sources are scanned too (report-only)
      --extensions=EXTS   File extensions to scan (comma-separated, default: all registered)
      --only=LIST         Report only these categories, comma-separated. Two
                          class shorthands: yaml (the hand-work findings) and
                          chars (the hygiene findings). Filters report and
                          exit code; does not change what --fix touches
      --fix               Remove invisible characters and normalize CRLF, in
                          place (atomic replace; never touches .jsonl; tabs
                          and NBSP always survive)
  -h, --help              Show this help
`)
}
