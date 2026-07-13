package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

const version = "0.14.1"

type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ", ") }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

func main() {
	if len(os.Args) < 2 || os.Args[1] == "-h" || os.Args[1] == "--help" {
		printHelp()
		os.Exit(0)
	}
	if os.Args[1] == "-v" || os.Args[1] == "--version" {
		fmt.Printf("grubber %s (Go)\n", version)
		os.Exit(0)
	}

	command := os.Args[1]
	rest := os.Args[2:]

	switch command {
	case "extract":
		runExtract(rest, "")
	default:
		if _, err := os.Stat(command); err == nil {
			runExtract(rest, command)
		} else {
			fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
			os.Exit(1)
		}
	}
}

// reorderArgs moves positional args to the end so flags are parsed
// regardless of whether they appear before or after the path argument.
// valueFlags lists flag names that take a value (so the next token is
// consumed as that value); unknown or bool flags consume nothing, leaving
// positional args intact when the user mistypes a flag.
func reorderArgs(args []string, valueFlags map[string]bool) []string {
	var flagArgs, posArgs []string
	i := 0
	for i < len(args) {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			posArgs = append(posArgs, arg)
			i++
			continue
		}
		name := strings.TrimLeft(arg, "-")
		if eq := strings.Index(name, "="); eq >= 0 {
			name = name[:eq]
		}
		flagArgs = append(flagArgs, arg)
		if !strings.Contains(arg, "=") && valueFlags[name] && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			flagArgs = append(flagArgs, args[i+1])
			i += 2
		} else {
			i++
		}
	}
	return append(flagArgs, posArgs...)
}

// valueFlagNames returns the names of all registered flags that take a value,
// i.e. everything except bool flags.
func valueFlagNames(fs *flag.FlagSet) map[string]bool {
	names := make(map[string]bool)
	fs.VisitAll(func(f *flag.Flag) {
		if b, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && b.IsBoolFlag() {
			return
		}
		names[f.Name] = true
	})
	return names
}

// resolveNotesDir picks the directory to scan by precedence:
//
//	explicit CLI/positional > set path > $GRUBBER_NOTES > cwd
//
// cwd is the last resort and is used only when there are no --from-jsonl
// sources: a pure source-only run has no directory to scan, so it must return
// "". An empty set path is ignored rather than expanded — expandPath("")
// resolves to the cwd (filepath.Abs("")), which would silently turn a
// source-only run into a cwd scan. getwd is injected for testability.
func resolveNotesDir(cliDir, setPath, envNotes string, hasJSONL bool, getwd func() (string, error)) string {
	if cliDir != "" {
		return cliDir
	}
	if setPath != "" {
		expanded, _ := expandPath(setPath)
		return expanded
	}
	if envNotes != "" {
		return envNotes
	}
	if !hasJSONL {
		if wd, err := getwd(); err == nil {
			return wd
		}
	}
	return ""
}

func runExtract(args []string, pathOverride string) {
	fs := flag.NewFlagSet("extract", flag.ExitOnError)
	fs.Usage = printExtractHelp

	var (
		outputFile      string
		setName         string
		format          string
		blocksOnly      bool
		frontmatterOnly bool
		allFlag         bool
		useMmd          bool
		noFill          bool
		depth           int
		workers         int
		arrayFieldsStr  string
		extensionsStr   string
		mergeOnStr      string
		explodeStr      string
		filters         multiFlag
		fromJSONL       multiFlag
	)

	fs.StringVar(&outputFile, "o", "", "Write output to file")
	fs.StringVar(&outputFile, "output", "", "Write output to file")
	fs.StringVar(&setName, "s", "", "Load options from config set")
	fs.StringVar(&setName, "set", "", "Load options from config set")
	fs.StringVar(&format, "format", "", "Output format: json (default), tsv, or jsonl")
	fs.BoolVar(&blocksOnly, "b", false, "Only extract YAML blocks")
	fs.BoolVar(&blocksOnly, "blocks-only", false, "Only extract YAML blocks")
	fs.BoolVar(&frontmatterOnly, "m", false, "Only extract frontmatter")
	fs.BoolVar(&frontmatterOnly, "frontmatter-only", false, "Only extract frontmatter")
	fs.BoolVar(&allFlag, "a", false, "Extract everything, override config defaults")
	fs.BoolVar(&allFlag, "all", false, "Extract everything, override config defaults")
	fs.BoolVar(&useMmd, "mmd", false, "Also parse MultiMarkdown metadata headers")
	fs.BoolVar(&noFill, "no-fill", false, "Skip nil-filling missing keys (faster for duckdb)")
	fs.IntVar(&depth, "d", -1, "Limit directory recursion depth")
	fs.IntVar(&depth, "depth", -1, "Limit directory recursion depth")
	fs.IntVar(&workers, "workers", 0, "Number of parallel workers (default: NumCPU)")
	fs.StringVar(&arrayFieldsStr, "array-fields", "", "Normalize fields to arrays (comma-separated)")
	fs.StringVar(&extensionsStr, "extensions", "", "File extensions to scan (comma-separated, e.g. .md,.typ)")
	fs.Var(&filters, "f", "Filter records (can be used multiple times)")
	fs.Var(&filters, "filter", "Filter records (can be used multiple times)")
	fs.Var(&fromJSONL, "from-jsonl", "Read records from JSONL file or directory (repeatable)")
	fs.StringVar(&mergeOnStr, "merge-on", "", "Merge --from-jsonl records into scanned records on these key fields (comma-separated)")
	fs.StringVar(&explodeStr, "explode", "", "Expand a field's array value into one record per element (before merge)")

	fs.Parse(reorderArgs(args, valueFlagNames(fs))) //nolint:errcheck

	// Detect which flags were explicitly provided
	set := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	if pathOverride == "" && fs.NArg() > 0 {
		pathOverride = fs.Arg(0)
	}

	execute(execOpts{
		outputFile:         outputFile,
		setName:            setName,
		format:             format,
		blocksOnly:         blocksOnly,
		blocksOnlySet:      set["b"] || set["blocks-only"],
		frontmatterOnly:    frontmatterOnly,
		frontmatterOnlySet: set["m"] || set["frontmatter-only"],
		allSet:             set["a"] || set["all"],
		useMmd:             useMmd,
		useMmdSet:          set["mmd"],
		noFill:             noFill,
		depth:              depth,
		depthSet:           set["d"] || set["depth"],
		workers:            workers,
		arrayFieldsStr:     arrayFieldsStr,
		extensionsStr:      extensionsStr,
		mergeOnStr:         mergeOnStr,
		mergeOnSet:         set["merge-on"],
		explodeStr:         explodeStr,
		explodeSet:         set["explode"],
		filters:            []string(filters),
		notesDir:           pathOverride,
		fromJSONL:          []string(fromJSONL),
	})
}

type execOpts struct {
	outputFile         string
	setName            string
	format             string
	blocksOnly         bool
	blocksOnlySet      bool
	frontmatterOnly    bool
	frontmatterOnlySet bool
	allSet             bool
	useMmd             bool
	useMmdSet          bool
	noFill             bool
	depth              int
	depthSet           bool
	workers            int
	arrayFieldsStr     string
	extensionsStr      string
	mergeOnStr         string
	mergeOnSet         bool
	explodeStr         string
	explodeSet         bool
	filters            []string
	notesDir           string
	fromJSONL          []string
}

func execute(opts execOpts) {
	cfg := NewConfig()

	var setCfg map[string]any
	if opts.setName != "" {
		setCfg = cfg.GetSet(opts.setName)
		if setCfg == nil {
			fmt.Fprintf(os.Stderr, "Error: Unknown set '%s'\n", opts.setName)
			if names := cfg.SetNames(); len(names) > 0 {
				fmt.Fprintf(os.Stderr, "Available sets: %s\n", strings.Join(names, ", "))
			}
			os.Exit(1)
		}
	}

	finalFormat := cliOr(opts.format, "json")
	switch finalFormat {
	case "json", "tsv", "jsonl":
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown format '%s' (expected json, tsv, or jsonl)\n", finalFormat)
		os.Exit(1)
	}

	// from_jsonl: set → CLI (CLI replaces). Set paths get ~ expansion —
	// the shell only expands CLI arguments. Resolved before the notes dir
	// so a source-only set (from_jsonl, no path) doesn't fall back to a
	// cwd scan.
	fromJSONL := opts.fromJSONL
	if len(fromJSONL) == 0 {
		for _, p := range cfgStrSlice(setCfg, "from_jsonl") {
			if expanded, err := expandPath(p); err == nil {
				fromJSONL = append(fromJSONL, expanded)
			}
		}
	}

	finalNotesDir := resolveNotesDir(
		opts.notesDir,
		cfgStr(setCfg, "path"),
		os.Getenv("GRUBBER_NOTES"),
		len(fromJSONL) > 0,
		os.Getwd,
	)

	// Bool options: config default → set → CLI (higher priority wins)
	blocksOnly := cfg.DefaultBlocksOnly()
	blocksOnly = cfgBool(setCfg, "blocks_only", blocksOnly)
	if opts.blocksOnlySet {
		blocksOnly = opts.blocksOnly
	}

	frontmatterOnly := cfgBool(setCfg, "frontmatter_only", false)
	if opts.frontmatterOnlySet {
		frontmatterOnly = opts.frontmatterOnly
	}

	useMmd := cfgBool(setCfg, "use_mmd", false)
	if opts.useMmdSet {
		useMmd = opts.useMmd
	}

	// --all overrides both block modes regardless of config
	if opts.allSet {
		blocksOnly, frontmatterOnly = false, false
	}

	// depth: set → CLI
	depth := cfgIntPtr(setCfg, "depth")
	if opts.depthSet {
		depth = &opts.depth
	}

	// array_fields: config default → set → env → CLI
	arrayFields := cfg.DefaultArrayFields()
	if af := cfgStrSlice(setCfg, "array_fields"); af != nil {
		arrayFields = af
	}
	if env := os.Getenv("GRUBBER_ARRAY_FIELDS"); env != "" {
		arrayFields = splitTrim(env, ",")
	}
	if opts.arrayFieldsStr != "" {
		arrayFields = splitTrim(opts.arrayFieldsStr, ",")
	}

	// filters: merge config defaults + set + CLI, deduplicated
	filters := dedupe(append(append(
		cfg.DefaultFilters(),
		cfgStrSlice(setCfg, "filters")...,
	), opts.filters...))

	// merge_on: config default → set → CLI. An explicitly given --merge-on
	// always wins, so --merge-on="" disables a configured default.
	mergeOn := cfg.DefaultMergeOn()
	if mo := cfgStrSlice(setCfg, "merge_on"); mo != nil {
		mergeOn = mo
	}
	if opts.mergeOnSet {
		mergeOn = splitTrim(opts.mergeOnStr, ",")
	}

	// explode: config default → set → CLI. --explode="" disables a configured
	// default. A single field name (not a list).
	explode := cfg.DefaultExplode()
	if ex, ok := setCfg["explode"].(string); ok {
		explode = ex
	}
	if opts.explodeSet {
		explode = strings.TrimSpace(opts.explodeStr)
	}

	// extensions: config default → set → env → CLI (nil = all registered parsers)
	extensions := cfg.DefaultExtensions()
	if exts := cfgStrSlice(setCfg, "extensions"); exts != nil {
		extensions = exts
	}
	if env := os.Getenv("GRUBBER_EXTENSIONS"); env != "" {
		extensions = splitTrim(env, ",")
	}
	if opts.extensionsStr != "" {
		extensions = splitTrim(opts.extensionsStr, ",")
	}

	g, err := NewGrubber(finalNotesDir, blocksOnly, frontmatterOnly, useMmd, opts.noFill, depth, opts.workers, arrayFields, filters, extensions, fromJSONL, mergeOn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	g.SetExplode(explode)

	var out *os.File
	if opts.outputFile != "" {
		out, err = os.Create(opts.outputFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		defer out.Close()
	} else {
		out = os.Stdout
	}

	// JSONL streams directly without collecting all records first
	if finalFormat == "jsonl" {
		if err = g.StreamJSONL(out); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	records, keys, err := g.Extract(nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if len(records) == 0 {
		if finalNotesDir != "" {
			fmt.Fprintf(os.Stderr, "No YAML records found in %s\n", finalNotesDir)
		} else {
			fmt.Fprintf(os.Stderr, "No records found\n")
		}
		os.Exit(0)
	}

	switch finalFormat {
	case "tsv":
		err = g.OutputTSV(records, keys, out)
	default:
		err = g.OutputJSON(records, out)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing output: %v\n", err)
		os.Exit(1)
	}

	if opts.outputFile != "" {
		fmt.Fprintf(os.Stderr, "Extracted %d records to %s\n", len(records), opts.outputFile)
	}
}

func cfgBool(m map[string]any, key string, fallback bool) bool {
	if m != nil {
		if v, ok := m[key].(bool); ok {
			return v
		}
	}
	return fallback
}

func cfgStr(m map[string]any, key string) string {
	if m != nil {
		if v, ok := m[key].(string); ok {
			return v
		}
	}
	return ""
}

func cfgIntPtr(m map[string]any, key string) *int {
	if m != nil {
		if v, ok := m[key].(int); ok {
			return &v
		}
	}
	return nil
}

func cfgStrSlice(m map[string]any, key string) []string {
	if m == nil {
		return nil
	}
	return toStringSlice(m[key])
}

func cliOr(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}

func splitTrim(s, sep string) []string {
	var result []string
	for _, part := range strings.Split(s, sep) {
		if p := strings.TrimSpace(part); p != "" {
			result = append(result, p)
		}
	}
	return result
}

func dedupe(s []string) []string {
	seen := make(map[string]bool, len(s))
	result := make([]string, 0, len(s))
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}

func printHelp() {
	fmt.Printf(`grubber v%s (Go) - Extract structured data from Markdown notes

USAGE:
  grubber [command] [options]

COMMANDS:
  extract      Extract YAML blocks from Markdown files

EXAMPLES:
  grubber extract ~/notes -o data.json
  grubber extract --set vertrag --format tsv
  grubber extract -f "type=vertrag" -f "due^2025-02"

CONFIG:
  ~/.config/grubber/config.yaml

ENVIRONMENT:
  GRUBBER_NOTES         Default notes directory
  GRUBBER_ARRAY_FIELDS  Default fields to normalize (comma-separated)
`, version)
}

func printExtractHelp() {
	fmt.Print(`Usage: grubber extract [directory] [options]

Options:
  -o, --output=FILE         Write output to file instead of stdout
  -s, --set=NAME            Load options from config set
      --format=FORMAT       Output format: json (default), tsv, or jsonl
  -b, --blocks-only         Only extract YAML blocks, ignore frontmatter-only notes
  -m, --frontmatter-only    Only extract frontmatter, ignore YAML blocks
  -a, --all                 Extract everything, override config defaults
      --mmd                 Also parse MultiMarkdown metadata headers
  -d, --depth=N             Limit directory recursion depth (0 = no subdirectories)
      --workers=N           Number of parallel workers (default: NumCPU)
      --array-fields=FIELDS Normalize fields to arrays (comma-separated)
      --extensions=EXTS     File extensions to scan (comma-separated, default: all registered)
      --no-fill             Skip nil-filling missing keys (useful for DuckDB)
  -f, --filter=EXPR         Filter records (can be used multiple times)
                            Operators: = (equals), ~ (contains), ^ (starts with), ! (not equals)
                            Examples: type=vertrag, due^2025-02, name~versicher
      --from-jsonl=PATH    Read records from an JSONL file (or directory of *.jsonl files)
                            and union them into the output. Repeatable. The notes directory
                            becomes optional when at least one --from-jsonl is given.
      --merge-on=KEYS      Merge --from-jsonl records into scanned records sharing the same
                            values on these fields (comma-separated, e.g. id,binder). The
                            scanned record wins; missing fields are back-filled from the
                            JSONL record. Filters apply after the merge.
      --explode=FIELD      Expand a field whose value is an array into one record per
                            element (the element as a scalar), before merge. Other fields
                            are copied to each row; an empty array yields one row without
                            the field. Pairs with --merge-on for one-record-per-file indexes.
  -h, --help                Show this help
`)
}
