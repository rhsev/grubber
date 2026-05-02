# grubber – Technical Architecture

## Overview

grubber is a stateless, multi-threaded extraction engine for structured data embedded in Markdown files. It scans a directory tree, parses YAML frontmatter and fenced YAML blocks, merges them into flat records, and emits JSON, NDJSON, or TSV.

```
CLI args
    ↓
reorderArgs + flag.FlagSet
    ↓
Config cascade (defaults → set → env → CLI)
    ↓
Grubber.Extract / StreamNDJSON
    ↓
Worker pool (goroutines + channels)
    ↓
parseNote per file
    ↓
Filter → Output
```

## Source Layout

| File | Responsibility |
|------|---------------|
| `main.go` | CLI parsing, config cascade, output routing |
| `grubber.go` | Core engine: file discovery, worker pool, parsing, output |
| `filter.go` | Filter expression parsing and matching |
| `config.go` | Config file loader (`~/.config/grubber/config.yaml`) |

## Parsing Pipeline

Each Markdown file goes through `parseNote`:

1. **Frontmatter** – matched via `^---\n(.*?)\n---\n` at file start. Parsed as YAML, stored as note-level metadata that is merged into every record from this file.
2. **MMD headers** – optionally parsed when `--mmd` is set, as a fallback if no frontmatter is found. MultiMarkdown-style `Key: Value` lines at the top of the file.
3. **YAML blocks** – all fenced ` ```yaml ` blocks in the body, each producing one record. A fast `bytes.Contains(body, []byte("```yaml"))` pre-check avoids the regex entirely when no blocks are present.
4. **Record assembly** – frontmatter fields are copied into each block record. Block fields win on collision. `_note_file` (path) and `_mtime` (RFC3339 UTC from `os.Stat`) are added unconditionally.

### YAML parsing: Node API

grubber uses `gopkg.in/yaml.v3`'s low-level `yaml.Node` API rather than direct unmarshalling into `map[string]any`. The reason: yaml.v3 returns an error (and an empty map) when a mapping contains duplicate keys. The Node API allows walking key-value pairs manually with last-value-wins semantics, which matches real-world note files that sometimes repeat keys.

Date values (parsed as `time.Time` by yaml.v3) are immediately stringified to `YYYY-MM-DD` via `stringifyDates` to produce safe, human-readable JSON output.

## Worker Pool

File processing is parallelised across all available CPUs (configurable via `--workers`):

```
markdownFiles()  →  []string
                        ↓
                  fileCh (buffered)
                   ↙   ↓   ↘
               worker worker worker   (NumCPU goroutines)
                   ↘   ↓   ↙
                  resultCh
                        ↓
                  caller drains
```

`processFiles` is the shared implementation used by both `Extract` (buffered JSON/TSV) and `StreamNDJSON` (streaming). Workers send `[]Record` per file; errors are printed to stderr and produce a nil slice so processing continues.

## Output Modes

| Format | Behaviour |
|--------|-----------|
| `json` | All records collected, nil-filled to a uniform key set, sorted by filename, emitted as a pretty-printed JSON array. |
| `tsv` | Same collection phase; keys become the header row. |
| `ndjson` | Records streamed directly from the worker pool via `json.Encoder`, one object per line. No buffering, no nil-filling, no sort. Ideal for large datasets and DuckDB ingestion. |

### nil-fill optimisation

For JSON and TSV output, every record is padded with `nil` values for keys it doesn't contain, so all records have a uniform schema. This requires collecting all keys across all records (`allKeys` map + sort).

When `--no-fill` is set, the `allKeys` map is never allocated and the sort is skipped entirely. The records are returned as-is with only the keys they actually contain — faster, and directly compatible with DuckDB's `read_ndjson_auto`.

## CLI Parsing

Go's `flag.FlagSet` stops parsing at the first non-flag argument. This would break the natural CLI form `grubber extract ~/notes --format tsv` because `~/notes` halts flag parsing.

`reorderArgs` solves this before `fs.Parse` is called: it walks the argument list, separates flag arguments (including their values for non-boolean flags) from positional arguments, and returns them with flags first. Boolean flag names are listed explicitly since `flag.FlagSet` does not expose flag types through its public API.

## Config Cascade

Options are resolved in increasing priority order:

```
Built-in defaults
    ↑
~/.config/grubber/config.yaml  (defaults section)
    ↑
Config set  (sets.<name> section, selected via --set)
    ↑
Environment variables  (GRUBBER_NOTES, GRUBBER_ARRAY_FIELDS)
    ↑
CLI flags
```

Tristate detection (did the user explicitly pass a flag, or is it at its zero value?) is implemented via `fs.Visit`, which only visits flags that were actually set on the command line.

## File Discovery

`markdownFiles` has two modes:

- **Depth-limited** (`--depth N`): uses `filepath.Glob` with repeated `*/` prefixes for each level — avoids a full `WalkDir` when only shallow scanning is needed.
- **Full recursion**: `filepath.WalkDir` with one rule: directories whose names start with `.` are skipped via `filepath.SkipDir`. This matches the behaviour of most Unix tools and avoids `.trash`, `.git`, etc.

Single-file mode is detected at startup via `os.Stat`; `markdownFiles` then returns a one-element slice without any directory traversal.

## Performance

~26 ms for a corpus of several thousand files on Apple Silicon (M-series), I/O-bound. Full scan on every invocation; no index, no cache, no daemon. At this scale, the overhead of maintaining an index would exceed the scan cost.

See [matterbase ARCHITECTURE.md](https://github.com/rhsev/matterbase/blob/main/ARCHITECTURE.md) for grubber's role in the broader stack.
