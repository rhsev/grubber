# grubber – Architecture

grubber is about 450 lines of Go across four small files:

| File | What it does |
|------|--------------|
| `main.go` | CLI flags, config cascade, output routing |
| `grubber.go` | File discovery, parsing, worker pool, output |
| `filter.go` | Filter expression parsing and matching |
| `config.go` | Config file loader |

## What happens on each run

1. Find all `.md` files (recursive, hidden dirs skipped)
2. Parse each file in parallel: extract frontmatter + YAML blocks, merge into flat records
3. Add `_note_file` and `_mtime` to every record
4. Apply filters, emit JSON / TSV / NDJSON

No index, no cache, no state between runs.

## A few non-obvious decisions

**YAML Node API.** yaml.v3 errors out on duplicate keys in a mapping. Real notes sometimes have them. The low-level `yaml.Node` API lets grubber walk key-value pairs manually and use last-value-wins, the same as most editors would.

**reorderArgs.** Go's `flag` package stops parsing at the first non-flag argument, so `grubber extract ~/notes --format tsv` would silently ignore `--format`. `reorderArgs` moves positional arguments to the end before parsing, so flag order doesn't matter.

**--no-fill shortcut.** Normally, all records are padded with `nil` for missing keys (uniform schema for JSON/TSV). With `--no-fill`, the key-collection map and sort are skipped entirely — records come out with only the keys they actually have. Useful for `read_ndjson_auto` in DuckDB, which infers the schema itself.

**Config cascade.** Priority from low to high: built-in defaults → config file → named set → environment variables → CLI flags. `fs.Visit` detects which flags were explicitly passed (vs. at their zero value) so set values aren't overwritten by unset flags.

## Performance

~26 ms for several thousand files on Apple Silicon, I/O-bound. The worker pool (one goroutine per CPU by default) helps on larger corpora; at this scale the scan is fast enough that no index is warranted.

See [matterbase ARCHITECTURE.md](https://github.com/rhsev/matterbase/blob/main/ARCHITECTURE.md) for grubber's role in the broader stack.
