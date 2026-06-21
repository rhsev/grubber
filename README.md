# grubber

[![CI](https://github.com/rhsev/grubber/actions/workflows/ci.yml/badge.svg)](https://github.com/rhsev/grubber/actions/workflows/ci.yml)

A data retrieval tool for Markdown and other text files with metadata. It just grubs fast through a big data field.

YAML code blocks in Markdown are usually used as code examples. grubber treats them as structured data records that live next to their context, across an entire directory of files. Think dataview without Obsidian.

## Quick example

Input (`project-alpha.md`):

````markdown
---
title: Project Alpha
keywords: [project]
---

# Project Alpha

```yaml
type: project
name: "Project Alpha"
org: "Northwind Corp"
status: active
start: 2025-01-15
end: 2025-06-30
owner: Jane Smith
```

Kickoff completed. First milestone due end of February.
````

Output (`grubber extract examples/`):

```json
[
  {
    "_note_file": "examples/project-alpha.md",
    "end": "2025-06-30",
    "keywords": ["project"],
    "name": "Project Alpha",
    "org": "Northwind Corp",
    "owner": "Jane Smith",
    "start": "2025-01-15",
    "status": "active",
    "title": "Project Alpha",
    "type": "project"
  }
]
```

Frontmatter and YAML block are merged into one flat record. The prose stays in Markdown, untouched. Run this across a directory of 1,000 notes and you get a single JSON file with all your records.

## Installation

Download the pre-built binary for macOS (Apple Silicon) from [Releases](https://github.com/rhsev/grubber/releases), or build from source:

```sh
go build -o grubber .
cp grubber /usr/local/bin/
```

Requires Go 1.22+.

### Quick start

```sh
grubber extract ~/notes
grubber extract ~/notes -f "type=project" --format tsv
grubber extract ~/notes/project.md
```

## Why

Structured data and the context around it usually live in different places. A database for the fields, a wiki or folder for the notes. grubber keeps them together: queryable YAML blocks inside Markdown files. The data stays where you read and write it.

- Standard Markdown. Any editor or renderer handles the format correctly. grubber just adds a read layer on top.
- Fast enough to skip the database. 2,000 notes in under 30ms. No index, no daemon, no setup.
- Only structure what you query. Put queryable fields in YAML blocks. Everything else stays in plain Markdown. If you'd never filter by it, don't put it in a code block.

### vs. databases

grubber is not a multi-dimensional database. But it covers many use cases: filtering, sorting, aggregating flat records across thousands of files. For personal data like contracts, contacts, inventory, or projects, that's often enough. And your data stays in plain text files that outlive any software.

### vs. Dataview (Obsidian)

| | Dataview | grubber |
|---|---|---|
| Editor lock-in | Obsidian only | Any editor |
| Query language | Proprietary DQL | Standard tools (jq, nushell, miller) |
| Output | In-note rendering | JSON/TSV for any pipeline |
| Live updates | Yes | No (run on demand) |
| Formats | Markdown only | Markdown, Typst, extensible |
| Extensible | Plugin API | Shell scripts, any language |

grubber trades live updates for tool independence. No proprietary query language to learn. If you know jq or nushell, you already know how to query grubber output.

## How it works

grubber scans files for metadata and structured data, merges everything into flat records, and outputs JSON or TSV. It does one thing: extract. All logic like filtering, sorting, or aggregating happens downstream with standard tools.

The primary format is Markdown: grubber reads YAML frontmatter and fenced YAML code blocks, merges them into flat records. Multiple YAML blocks per file produce multiple records, each inheriting the frontmatter fields.

Beyond Markdown, grubber has a file-format registry that can handle other text files with metadata. Typst is currently implemented: grubber reads `#metadata((...))` and `#set document(...)` blocks. Which file types are scanned is configurable via `--extensions`. Other formats with native metadata conventions — such as Org-mode, AsciiDoc, or plain YAML — are natural candidates for future parsers.

## Usage

```sh
# Extract all records from a directory
grubber extract ~/notes

# Output as TSV
grubber extract ~/notes --format tsv

# Filter records
grubber extract ~/notes -f "type=contract"
grubber extract ~/notes -f "type=contract" -f "end^2025"

# Only YAML blocks (skip frontmatter-only notes)
grubber extract ~/notes --blocks-only

# Only frontmatter (ignore YAML blocks)
grubber extract ~/notes --frontmatter-only

# Write to file
grubber extract ~/notes -o data.json

# Use a config set
grubber extract --set contracts
```

### Filter operators

| Operator | Meaning | Example |
|----------|---------|---------|
| `=` | equals | `type=contract` |
| `~` | contains | `name~hosting` |
| `^` | starts with | `end^2025` |
| `!` | not equals | `status!archived` |

Filters are case-insensitive and work on arrays (matches if any element matches).

## Piping to other tools

grubber outputs JSON by default, designed for piping. The downstream tool does the thinking:

```sh
# jq: contracts expiring in 2025
grubber extract ~/notes -f type=contract | jq '[.[] | select(.end | startswith("2025"))]'

# sql (duckdb): sort contacts by last interaction
grubber extract ~/notes -f type=person | duckdb -c "SELECT name, last_contact FROM read_json_auto('/dev/stdin') ORDER BY last_contact DESC"

# jsonl: stream into duckdb without buffering the full array
grubber extract ~/notes --format jsonl --no-fill | duckdb -c "SELECT name, status FROM read_jsonl_auto('/dev/stdin')"

# miller: TSV processing
grubber extract ~/notes --format tsv | mlr --tsv sort-by -nr amount
```

## JSONL sources (`--from-jsonl`)

grubber can read pre-computed records from one or more `.jsonl` files and union them into the output alongside (or instead of) a fresh scan.

```sh
# Cache an expensive scan once …
grubber extract ~/notes --format jsonl -o cache.jsonl

# … then replay it with no Markdown read at all:
grubber extract --from-jsonl cache.jsonl

# … or merge a fresh scan of a small dir with a large cached baseline:
grubber extract ~/new-notes --from-jsonl cache.jsonl

# Multiple sources (files or directories of *.jsonl):
grubber extract --from-jsonl /path/to/cache1.jsonl --from-jsonl /path/to/dir/
```

`--from-jsonl` is repeatable. If a path is a directory, every `*.jsonl` file directly inside it is read (non-recursive, sorted by filename).

**Union semantics.** By default source records are concatenated with scanned records — no deduplication. Scanned records come first, then sources in the order given.

**Merging (`--merge-on`).** When a JSONL index and scanned annotation files describe the same logical records in two layers, `--merge-on KEYS` collapses them:

```sh
# fileregister: collection index + Markdown annotations, one record per file
grubber extract ~/notes --from-jsonl ~/notes/collections/ --merge-on id,binder
```

A source record that matches a scanned record on all key fields is dropped after back-filling any fields the scanned record lacks (the scanned record wins; `_note_file`/`_mtime` are never touched). Unmatched source records pass through. The first key field is the primary identity — records without it are never merge candidates; later keys default to `""` when absent. Filters run *after* the merge, so they see back-filled fields. With `--format jsonl` the merge buffers instead of streaming.

**Exploding (`--explode FIELD`).** When the index keeps a single record per file with a field holding an array (e.g. fileregister stores `binder: [projekt-a, lesen]`), `--explode FIELD` expands each such record into one row per element — the element as a scalar — *before* the merge. Per-binder Markdown blocks carry a single `binder`, so the exploded index rows then line up and collapse on `(id, binder)`:

```sh
grubber extract ~/notes --from-jsonl ~/notes/collections/ --explode binder --merge-on id,binder -f binder=projekt-a
```

Scalar or absent values pass through unchanged; an empty array yields one row without the field (a binderless row). All other fields, including provenance, are copied to every row. Like `--merge-on`, filters run *after* the explode (so `-f binder=projekt-a` keeps only the matching membership), and `--explode FIELD` can live in `defaults:`/a set (an explicit `--explode=` disables it). For a **per-file** view, omit `--explode`: a filter already matches a value *inside* an array (`-f binder=lesen` matches `[projekt-a, lesen]`).

`merge_on` can live in the config (`defaults:` or a set), so a schema you always use needs no flag; an explicit `--merge-on=` (empty) disables it for one run. Together with `from_jsonl` in a set, the whole database definition moves into config and the command shrinks to the query:

```sh
grubber extract -s notes -f binder=project-alpha
```

**Provenance (`_note_file` / `_mtime`).** grubber guarantees every emitted record carries `_note_file`:
- If a source record already has `_note_file` (e.g., from a prior grubber run): **preserved unchanged.** Round-trip fidelity — the original Markdown path is kept.
- If a source record has no `_note_file` (tool-authored lines): grubber injects the source file path.

**Filters and `--array-fields`** apply to source records exactly as they do to scanned records. `-b`/`-m` (blocks-only / frontmatter-only) are scan-path concepts and do not filter source records.

The notes directory is optional when at least one `--from-jsonl` is given.

## Configuration

Optional config file at `~/.config/grubber/config.yaml`:

```yaml
defaults:
  blocks_only: true
  array_fields: [keywords, category]
  extensions: [.md, .typ]
  merge_on: [id, binder]

sets:
  contracts:
    path: ~/notes
    filters: [type=contract]
    blocks_only: true

  people:
    path: ~/notes
    filters: [type=person]

  # a full database definition: scan + JSONL index + dedup
  notes:
    path: ~/notes
    from_jsonl: ["~/notes/collections/"]
    merge_on: [id, binder]
```

Use sets with `grubber extract --set contracts`.

### Override hierarchy

CLI flags > Config set > Environment variables > Config defaults > Built-in defaults

### Environment variables

| Variable | Purpose |
|----------|---------|
| `GRUBBER_NOTES` | Default notes directory |
| `GRUBBER_ARRAY_FIELDS` | Fields to normalize to arrays (comma-separated) |
| `GRUBBER_EXTENSIONS` | File extensions to scan (comma-separated, e.g. `.md,.typ`) |

## Options

```
-o, --output FILE         Write to file instead of stdout
-s, --set NAME            Load options from config set
    --format FORMAT       json (default), tsv, or jsonl
-b, --blocks-only         Only extract YAML blocks
-m, --frontmatter-only    Only extract frontmatter
-a, --all                 Extract everything, override config defaults
    --array-fields FIELDS Normalize fields to arrays (splits comma-separated values)
    --extensions EXTS     File extensions to scan (comma-separated, default: all registered)
    --mmd                 Also read MultiMarkdown metadata headers
-d, --depth N             Limit directory recursion depth (0 = no subdirectories)
    --workers N           Number of parallel workers (default: NumCPU)
    --no-fill             Skip nil-filling missing keys (useful for DuckDB)
-f, --filter EXPR         Filter records (repeatable)
    --from-jsonl PATH    Read records from JSONL file or directory; union into output (repeatable)
    --merge-on KEYS       Merge --from-jsonl records into scanned records sharing these key fields
    --explode FIELD       Expand a field's array value into one record per element (before merge)
-h, --help                Show help
```

`--array-fields` normalizes string values to arrays. Comma-separated strings like `a, b, c` are split into `["a", "b", "c"]`. YAML arrays are kept as-is. Single string values in array fields are wrapped in a one-element array. Values that contain commas as part of their meaning (e.g. `"Smith, John"`) should be written as a YAML array instead.

## How to structure your notes

### Markdown

grubber reads two things from a Markdown file: YAML frontmatter and fenced YAML code blocks (` ```yaml `). Everything else is ignored.

- Frontmatter holds note-level metadata (title, keywords, created date). These fields are merged into every record from that file.
- YAML code blocks hold structured data records. Only ` ```yaml ` blocks are read — other fenced blocks are ignored.
- Multiple YAML blocks in one note produce multiple records. Each inherits the frontmatter fields.
- On field name collision, the YAML block wins over frontmatter.
- Notes without YAML blocks are extracted as frontmatter-only records (unless `--blocks-only`).

### Typst

grubber reads metadata from two Typst constructs:

- `#metadata((key: "value", ...)) <label>` — custom per-document metadata tuple, takes priority
- `#set document(title: "...", author: "...", date: datetime(...))` — standard Typst document metadata

Only the first matching block per file is read. `datetime(year:, month:, day:)` values are converted to `YYYY-MM-DD` strings. Arrays are supported: `("a", "b", "c")` becomes a JSON array.

### General

- A `_note_file` field is added automatically to every record for traceability.
- A `_mtime` field (RFC3339 UTC) is added automatically with the file's last-modified time.
- grubber scans directories recursively. Hidden directories (starting with `.`) are skipped.

See [examples/SCHEMA.md](examples/SCHEMA.md) for an example schema.

## YAML notes

grubber follows YAML 1.2. Values that look like numbers are parsed as numbers. If you want a value like a phone number or ID with a leading zero to remain a string, quote it explicitly:

```yaml
number: "01711234567"   # string
number: 01711234567     # parsed as a number
```

## Design

- Extract only. grubber reads and outputs. No transforms, no joins, no computed fields. Complexity belongs in downstream tools.
- Valid Markdown. The format doesn't break any renderer. grubber adds a queryable layer on top.
- Dates are output as strings (`YYYY-MM-DD`) for safe JSON serialization.
- Schema-agnostic. grubber extracts whatever YAML it finds. Field names and record types are up to you.
- JSON keys are sorted alphabetically for deterministic output.

## See also

[matterbase](https://github.com/rhsev/matterbase) — a terminal UI built on top of grubber. Its [ARCHITECTURE.md](https://github.com/rhsev/matterbase/blob/main/ARCHITECTURE.md) also covers grubber's role in the overall stack.

## License

MIT
