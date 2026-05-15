# grubber – Architecture

grubber is about 600 lines of Go across seven small files:

| File | What it does |
|------|--------------|
| `main.go` | CLI flags, config cascade, output routing |
| `grubber.go` | File discovery, parser dispatch, worker pool, output |
| `parser.go` | FileParser interface and format registry |
| `parser_md.go` | Markdown parser: YAML frontmatter, YAML blocks, MultiMarkdown headers |
| `parser_typst.go` | Typst parser: `#metadata((...))` and `#set document(...)` |
| `filter.go` | Filter expression parsing and matching |
| `config.go` | Config file loader |

## What happens on each run

1. Find all files with registered extensions (default: `.md`, `.typ`) recursively, hidden dirs skipped
2. Parse each file in parallel: dispatch to the matching FileParser by extension, extract metadata and data records
3. Merge metadata and records into flat records; add `_note_file` and `_mtime`
4. Apply filters, emit JSON / TSV / NDJSON

No index, no cache, no state between runs.

## FileParser interface

Each file format is a self-contained file that registers itself via `init()`:

```go
type FileParser interface {
    Extract(path string, data []byte, opts ParseOpts) (frontmatter Record, blocks []Record, err error)
}
```

`grubber.go` dispatches by file extension (`filepath.Ext`) and knows nothing about individual formats. Adding a new format means adding one file — no changes to core logic.

`ParseOpts` carries flags that only make sense for specific formats (e.g. `FrontmatterOnly` applies to Markdown, is ignored by Typst). This keeps parsers decoupled from the `Grubber` struct.

## A few non-obvious decisions

**YAML Node API.** yaml.v3 errors out on duplicate keys in a mapping. Real notes sometimes have them. The low-level `yaml.Node` API lets grubber walk key-value pairs manually and use last-value-wins, the same as most editors would.

**reorderArgs.** Go's `flag` package stops parsing at the first non-flag argument, so `grubber extract ~/notes --format tsv` would silently ignore `--format`. `reorderArgs` moves positional arguments to the end before parsing, so flag order doesn't matter.

**--no-fill shortcut.** Normally, all records are padded with `nil` for missing keys (uniform schema for JSON/TSV). With `--no-fill`, the key-collection map and sort are skipped entirely — records come out with only the keys they actually have. Useful for `read_ndjson_auto` in DuckDB, which infers the schema itself.

**Config cascade.** Priority from low to high: built-in defaults → config file → named set → environment variables → CLI flags. `fs.Visit` detects which flags were explicitly passed (vs. at their zero value) so set values aren't overwritten by unset flags.

**Typst block extraction.** `#metadata((content))` uses nested parens — the outer call parens and an inner tuple. `typstFindBlock` scans byte-by-byte tracking paren depth, so nested structures like `datetime(year: 2024, month: 6, day: 1)` are handled without a full parser. The same function handles both `#metadata((` and `#set document(` via a prefix argument.

**Typst returns blocks, not frontmatter.** The Typst parser returns its metadata as a block record rather than frontmatter. This ensures files are included even when `blocks_only: true` — since Typst has no YAML-block concept, the `#metadata()` block IS the record.

## Performance

~26 ms for several thousand files on Apple Silicon, I/O-bound. The worker pool (one goroutine per CPU by default) helps on larger corpora; at this scale the scan is fast enough that no index is warranted.

See [matterbase ARCHITECTURE.md](https://github.com/rhsev/matterbase/blob/main/ARCHITECTURE.md) for grubber's role in the broader stack.
