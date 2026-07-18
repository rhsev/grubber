# grubber – Architecture

grubber is about 800 lines of Go across nine small files:

| File | What it does |
|------|--------------|
| `main.go` | CLI flags, config cascade, output routing |
| `grubber.go` | File discovery, parser dispatch, worker pool, output |
| `source_jsonl.go` | JSONL merge sources: dir expansion, line-parsing, provenance injection |
| `parser.go` | FileParser interface and format registry |
| `parser_md.go` | Markdown parser: YAML frontmatter, YAML blocks, MultiMarkdown headers |
| `parser_typst.go` | Typst parser: `#metadata((...))` and `#set document(...)` |
| `filter.go` | Filter expression parsing and matching |
| `config.go` | Config file loader |
| `doctor.go` | doctor subcommand: diagnostics collection and report |

## What happens on each run

1. Find all files with registered extensions (default: `.md`, `.typ`) recursively, hidden dirs skipped
2. Parse each file in parallel: dispatch to the matching FileParser by extension, extract metadata and data records
3. Merge metadata and records into flat records; add `_note_file` and `_mtime`
4. Apply filters, emit JSON / TSV / JSONL

Optionally, JSONL merge sources are read and unioned into the result set (see below).

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

## Merge sources (`--from-jsonl`)

JSONL files named on `--from-jsonl` are a second input alongside the scan path. They are **not** discovered by walking the notes tree — sources are always explicit. This design is intentional:

- **JSONL is grubber's own output format.** Output of one run becomes input of the next, making grubber composable as a pipeline stage and enabling cheap replay from a cached scan.
- **Sources are decoupled from the tree.** A source can live anywhere; it does not have to sit inside `notesDir`.

**Union semantics.** Records from merge sources are concatenated with scanned records. No deduplication: if a fresh scan and a cache overlap, the output contains both by design. Consumers that need deduplication do it downstream (e.g., DuckDB `DISTINCT`).

**Preserve-else-inject provenance.** grubber guarantees every emitted record carries `_note_file`. For merge-source records:
- If a record already has `_note_file` (e.g., a grubber-produced cache): **preserved, never overwritten.** Round-trip fidelity — the original Markdown provenance is unchanged.
- If a record lacks `_note_file` (tool-authored lines): grubber injects the exact source file's path and `_mtime`.

**This is not a parser.** `source_jsonl.go` is a separate input stage, not a FileParser registered in the format registry. It feeds the same filter/array-normalization/output path as scanned records. `-b`/`-m` (blocks-only/frontmatter-only) are scan-path concepts and do not filter merge-source records.

## Explode (`--explode`)

`explode.go` adds one optional stage in `mergedRecords`, between collection and merge: a record whose named field holds an array becomes one record per element (the element as a scalar), other fields copied verbatim. Scalar/absent values pass through; an empty array yields one row without the field. Like `--merge-on`, enabling it (via `SetExplode`) moves the filters to `postFilter` so they run *after* the explode — otherwise the non-matching elements of an exploded array would leak past a filter on that field. The pipeline becomes **collect → explode → merge → post-filter**. Its purpose is the one-record-per-file index (`binder` as an array) projecting into the per-`(id, binder)` rows that `--merge-on id,binder` and downstream consumers expect; pure additive, off unless requested.

## A few non-obvious decisions

**YAML Node API.** yaml.v3 errors out on duplicate keys in a mapping. Real notes sometimes have them. The low-level `yaml.Node` API lets grubber walk key-value pairs manually and use last-value-wins, the same as most editors would.

**Doctor sees with extract's eyes.** `doctor` is not a second, stricter parser — it would drift from what extract actually does. Instead the regular parse functions take an optional `*Diagnostics` collector (`ParseOpts.Diag`), nil on the extract path: every finding is recorded at the exact spot where extract falls back, normalizes, or skips. A parser fix automatically updates both commands. The invisible-character scan (and its `--fix`) is the one part that runs on raw bytes before parsing — its fixed character list mirrors basekit's `frame.Sanitize`, minus tab replacement, plus CRLF→LF.

**reorderArgs.** Go's `flag` package stops parsing at the first non-flag argument, so `grubber extract ~/notes --format tsv` would silently ignore `--format`. `reorderArgs` moves positional arguments to the end before parsing, so flag order doesn't matter.

**--no-fill shortcut.** Normally, all records are padded with `nil` for missing keys (uniform schema for JSON/TSV). With `--no-fill`, the key-collection map and sort are skipped entirely — records come out with only the keys they actually have. Useful for `read_jsonl_auto` in DuckDB, which infers the schema itself.

**Config cascade.** Priority from low to high: built-in defaults → config file → named set → environment variables → CLI flags. `fs.Visit` detects which flags were explicitly passed (vs. at their zero value) so set values aren't overwritten by unset flags.

**Typst block extraction.** `#metadata((content))` uses nested parens — the outer call parens and an inner tuple. `typstFindBlock` scans byte-by-byte tracking paren depth, so nested structures like `datetime(year: 2024, month: 6, day: 1)` are handled without a full parser. The same function handles both `#metadata((` and `#set document(` via a prefix argument.

**Typst returns blocks, not frontmatter.** The Typst parser returns its metadata as a block record rather than frontmatter. This ensures files are included even when `blocks_only: true` — since Typst has no YAML-block concept, the `#metadata()` block IS the record.

## Performance

~28 ms for 2,000 files on Apple Silicon, I/O-bound. The worker pool (one goroutine per CPU by default) helps on larger corpora; at this scale the scan is fast enough that no index is warranted.

See [matterbase ARCHITECTURE.md](https://github.com/rhsev/matterbase/blob/main/ARCHITECTURE.md) for grubber's role in the broader stack.
