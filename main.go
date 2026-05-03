package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

const version = "0.8.2"

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
// boolFlags lists flag names that take no value (so the next token is not consumed).
func reorderArgs(args []string, boolFlags map[string]bool) []string {
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
		// If flag takes a value and next token isn't a flag, consume it as the value.
		if !strings.Contains(arg, "=") && !boolFlags[name] && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			flagArgs = append(flagArgs, args[i+1])
			i += 2
		} else {
			i++
		}
	}
	return append(flagArgs, posArgs...)
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
		filters         multiFlag
	)

	fs.StringVar(&outputFile, "o", "", "Write output to file")
	fs.StringVar(&outputFile, "output", "", "Write output to file")
	fs.StringVar(&setName, "s", "", "Load options from config set")
	fs.StringVar(&setName, "set", "", "Load options from config set")
	fs.StringVar(&format, "format", "", "Output format: json (default), tsv, or ndjson")
	fs.BoolVar(&blocksOnly, "b", false, "Only extract YAML blocks")
	fs.BoolVar(&blocksOnly, "blocks-only", false, "Only extract YAML blocks")
	fs.BoolVar(&frontmatterOnly, "m", false, "Only extract frontmatter")
	fs.BoolVar(&frontmatterOnly, "frontmatter-only", false, "Only extract frontmatter")
	fs.BoolVar(&allFlag, "a", false, "Extract everything, override config defaults")
	fs.BoolVar(&useMmd, "mmd", false, "Also parse MultiMarkdown metadata headers")
	fs.BoolVar(&noFill, "no-fill", false, "Skip nil-filling missing keys (faster for duckdb)")
	fs.IntVar(&depth, "d", -1, "Limit directory recursion depth")
	fs.IntVar(&depth, "depth", -1, "Limit directory recursion depth")
	fs.IntVar(&workers, "workers", 0, "Number of parallel workers (default: NumCPU)")
	fs.StringVar(&arrayFieldsStr, "array-fields", "", "Normalize fields to arrays (comma-separated)")
	fs.Var(&filters, "f", "Filter records (can be used multiple times)")
	fs.Var(&filters, "filter", "Filter records (can be used multiple times)")

	boolFlags := map[string]bool{
		"b": true, "blocks-only": true,
		"m": true, "frontmatter-only": true,
		"a": true, "mmd": true, "no-fill": true,
	}
	fs.Parse(reorderArgs(args, boolFlags)) //nolint:errcheck

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
		allSet:             set["a"],
		useMmd:             useMmd,
		useMmdSet:          set["mmd"],
		noFill:             noFill,
		depth:              depth,
		depthSet:           set["d"] || set["depth"],
		workers:            workers,
		arrayFieldsStr:     arrayFieldsStr,
		filters:            []string(filters),
		notesDir:           pathOverride,
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
	filters            []string
	notesDir           string
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

	// Notes dir: CLI > set path > env > cwd
	finalNotesDir := opts.notesDir
	if finalNotesDir == "" {
		finalNotesDir, _ = expandPath(cfgStr(setCfg, "path"))
	}
	if finalNotesDir == "" {
		finalNotesDir = os.Getenv("GRUBBER_NOTES")
	}
	if finalNotesDir == "" {
		finalNotesDir, _ = os.Getwd()
	}

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

	finalFilters := filters

	g, err := NewGrubber(finalNotesDir, blocksOnly, frontmatterOnly, useMmd, opts.noFill, depth, opts.workers, arrayFields, finalFilters)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

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

	// NDJSON streams directly without collecting all records first
	if finalFormat == "ndjson" {
		if err = g.StreamNDJSON(out); err != nil {
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
		fmt.Fprintf(os.Stderr, "No YAML records found in %s\n", finalNotesDir)
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
      --format=FORMAT       Output format: json (default), tsv, or ndjson
  -b, --blocks-only         Only extract YAML blocks, ignore frontmatter-only notes
  -m, --frontmatter-only    Only extract frontmatter, ignore YAML blocks
  -a, --all                 Extract everything, override config defaults
      --mmd                 Also parse MultiMarkdown metadata headers
  -d, --depth=N             Limit directory recursion depth (0 = no subdirectories)
      --workers=N           Number of parallel workers (default: NumCPU)
      --array-fields=FIELDS Normalize fields to arrays (comma-separated)
      --no-fill             Skip nil-filling missing keys (useful for DuckDB)
  -f, --filter=EXPR         Filter records (can be used multiple times)
                            Operators: = (equals), ~ (contains), ^ (starts with), ! (not equals)
                            Examples: type=vertrag, due^2025-02, name~versicher
  -h, --help                Show this help
`)
}
