# grubber – Architecture

grubber is about 2,900 lines of Go across thirteen files (tests not counted):

| File | What it does |
|------|--------------|
| `main.go` | CLI flags, config cascade (`--no-config` skips it), output routing |
| `grubber.go` | File discovery, parser dispatch, worker pool, output |
| `source_jsonl.go` | JSONL merge sources: dir expansion, line-parsing, provenance injection |
| `explode.go` | `--explode`: one record per element of an array field |
| `merge.go` | `--merge-on`: match JSONL records to scanned records on key fields (compared in NFC), back-fill missing fields |
| `parser.go` | FileParser interface and format registry |
| `parser_md.go` | Markdown parser: YAML frontmatter, YAML blocks, MultiMarkdown headers |
| `parser_typst.go` | Typst parser: `#metadata((...))` and `#set document(...)` |
| `parser_vcard.go` | vCard 3.0 parser: one flattened record per card |
| `json.go` | Pinned JSON semantics: the `encoding/json/v2` option set and the JSONL record encoder |
| `filter.go` | Filter expression parsing and matching |
| `config.go` | Config file loader |
| `doctor.go` | doctor subcommand: diagnostics collection and report |

## What happens on each run

1. Find all files with registered extensions (default: `.md`, `.typ`, `.vcf`) recursively, hidden dirs skipped
2. Parse each file in parallel: dispatch to the matching FileParser by extension, extract metadata and data records
3. Merge metadata and records into flat records, with frontmatter fields inherited into each block (`--inherit` limits which); add `_note_file` and `_mtime`
4. Apply filters, emit JSON / TSV / JSONL

Optionally, JSONL merge sources are read and added to the result set (see below).

No index, no cache, no state between runs.

## FileParser interface

Each file format is a self-contained file that registers itself via `init()`:

```go
type FileParser interface {
    Extract(path string, data []byte, opts ParseOpts) (frontmatter Record, blocks []Record, err error)
}
```

`grubber.go` dispatches by file extension (`filepath.Ext`) and knows nothing about individual formats. Adding a new format means adding one file, with no changes to core logic.

`ParseOpts` carries flags that only make sense for specific formats (e.g. `FrontmatterOnly` applies to Markdown, is ignored by Typst). This keeps parsers decoupled from the `Grubber` struct.

## Merge sources (`--from-jsonl`)

JSONL files named on `--from-jsonl` are a second input alongside the scan path. They are **not** discovered by walking the notes tree; sources are always explicit. This design is intentional:

- **JSONL is grubber's own output format.** Output of one run becomes input of the next, making grubber composable as a pipeline stage and enabling cheap replay from a cached scan.
- **Sources are decoupled from the tree.** A source can live anywhere; it does not have to sit inside `notesDir`.

**Union semantics.** Unless `--merge-on` is set, records from merge sources are concatenated with scanned records. There is no deduplication. If a fresh scan and a cache overlap, the output contains both. Consumers that need deduplication do it downstream (e.g., DuckDB `DISTINCT`) or use `--merge-on`.

**Preserve-else-inject provenance.** grubber guarantees every emitted record carries `_note_file`. For merge-source records:
- If a record already has `_note_file` (e.g., a grubber-produced cache), it is **preserved, never overwritten**, so the original Markdown provenance survives a round trip.
- If a record lacks `_note_file` (tool-authored lines), grubber injects the exact source file's path and `_mtime`.

**Not a parser.** `source_jsonl.go` is a separate input stage, not a FileParser registered in the format registry. It feeds the same filter/array-normalization/output path as scanned records. `-b`/`-m` (blocks-only/frontmatter-only) are scan-path concepts and do not filter merge-source records.

## Merge (`--merge-on`)

`merge.go` collapses a JSONL record into the scanned record that has the same values on the key fields (e.g. `--merge-on id,binder`). Keys are compared in NFC. Scanned records always pass through; a matching JSONL record is dropped after back-filling the fields the scanned record lacks (nil, missing, or `""`). Underscore fields (`_note_file`, `_mtime`) are never back-filled. JSONL records without a scanned counterpart, and records with no value for the first key field, pass through unchanged. Merging needs the full record set, so the filters move to `postFilter` and run after the merge.

## Explode (`--explode`)

`explode.go` adds one optional stage in `mergedRecords`, between collection and merge. A record whose named field holds an array becomes one record per element (the element as a scalar), other fields copied verbatim. Scalar/absent values pass through; an empty array yields one row without the field. Like `--merge-on`, enabling it (via `SetExplode`) moves the filters to `postFilter` so they run *after* the explode. Otherwise the non-matching elements of an exploded array would get past a filter on that field. The pipeline becomes **collect → explode → merge → post-filter**. Its purpose is to turn the one-record-per-file index (`binder` as an array) into the per-`(id, binder)` rows that `--merge-on id,binder` and downstream consumers expect. It is additive and off unless requested.

## A few non-obvious decisions

**YAML Node API.** yaml.v3 errors out on duplicate keys in a mapping. Real notes sometimes have them. The low-level `yaml.Node` API lets grubber walk key-value pairs manually and use last-value-wins, the same as most editors would.

**Doctor uses the extract parsers.** `doctor` is not a second, stricter parser, since that would drift from what extract actually does. Instead the regular parse functions take an optional `*Diagnostics` collector (`ParseOpts.Diag`), nil on the extract path. Every finding is recorded at the exact spot where extract falls back, normalizes, or skips, so a parser fix updates both commands. The invisible-character scan (and its `--fix`) is the one part that runs on raw bytes before parsing. Its fixed character list mirrors basekit's `frame.Sanitize`, minus tab replacement, plus CRLF→LF.

**reorderArgs.** Go's `flag` package stops parsing at the first non-flag argument, so `grubber extract ~/notes --format tsv` would silently ignore `--format`. `reorderArgs` moves positional arguments to the end before parsing, so flag order doesn't matter.

**--no-fill shortcut.** Normally, all records are padded with `nil` for missing keys (uniform schema for JSON/TSV). With `--no-fill`, the key-collection map and sort are skipped entirely, and records come out with only the keys they actually have. Useful for `read_jsonl_auto` in DuckDB, which infers the schema itself.

**Config cascade.** Priority from low to high: built-in defaults → config file defaults → environment variables (`GRUBBER_NOTES`, `GRUBBER_ARRAY_FIELDS`, `GRUBBER_EXTENSIONS`) → named set (`--set`) → CLI flags. A set named on the command line is a deliberate choice and therefore outranks the environment. Filters do not replace each other; those from config defaults, the set, and the CLI add up. `fs.Visit` detects which flags were explicitly passed (vs. at their zero value) so set values aren't overwritten by unset flags. `--no-config` ignores the config file and the `GRUBBER_*` variables, so only the command line counts; it refuses `--set` (exit 2).

**Inheritance is chosen per call.** Frontmatter flows
into every block by default, because for most readers the header describes
the group the blocks belong to. Readers that treat each block as a record of
its own need the opposite, and both readings concern the same files, so the
choice belongs to the call (`--inherit`), not to the note. Two exceptions
apply. `_note_file` is provenance and always passes, and a record that is the
frontmatter itself (a note without blocks, or `-m`) is never thinned out,
which is why `noteResult` records whether its records are real blocks. The
filter runs after inheritance, so it sees what the reader asked for. An empty
list and an absent one differ (none vs. all), so the config/CLI resolution in
`resolveInherit` keeps them apart where `splitTrim` alone would not. One
consequence of the precedence order is that a list in `defaults:` cannot be
widened back to "all" by a set or the CLI.

**Comparisons normalize, output does not.** Filter values and the field values
they are matched against both pass through `foldValue` (NFC, then lowercase),
and `--merge-on` keys through NFC alone. macOS file names arrive decomposed,
keyboard input composed, and without this a typed `folder~Verträge` found
none of the 3,073 mails whose folder name came from the file system. The
records themselves are never rewritten. A value leaves grubber in the form
the note or index holds. NFC comes from `golang.org/x/text`, the
second dependency next to yaml.v3; the standard library carries the same
package only as an internal vendor copy.

**JSON options are set explicitly.** grubber uses `encoding/json/v2`
directly and names its options in `json.go` rather than relying on defaults
that differ between the v1 and v2 APIs. `Deterministic` restores the sorted
map keys that record order depends on, `EscapeForHTML` keeps older cached
extracts diffing clean, and `AllowInvalidUTF8`/`AllowDuplicateNames` keep the
read and write paths degrading rather than failing on defects that `doctor`
reports. Each option has a test that fails if it is removed. The test files
still parse with v1 `encoding/json`, which checks the output against a second
implementation.

**vCard flattening is lossy by design.** Cards are read for querying, not round-tripping. TYPE parameters, photos, and `X-` properties are dropped, `EMAIL`/`TEL`/`URL` are always arrays for consistent typing across a corpus. `UID` maps to `uid`, not `id`, because `id` names a file in the fileregister convention, while a UID names one card among possibly many per file. Like Typst, every card is a block record, so `blocks_only: true` includes vCard files.

**Typst returns blocks, not frontmatter.** The Typst parser returns its metadata as a block record rather than frontmatter. This ensures files are included even when `blocks_only: true`. Since Typst has no YAML-block concept, the `#metadata()` block is the record.

## Performance

~28 ms for 2,000 files on Apple Silicon, I/O-bound. The worker pool (one goroutine per CPU by default) helps on larger corpora; at this scale the scan is fast enough that no index is warranted.

See [matterbase ARCHITECTURE.md](https://github.com/rhsev/matterbase/blob/main/ARCHITECTURE.md) for grubber's role in the broader stack.
