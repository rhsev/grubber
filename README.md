# grubber

A data retrieval tool for Markdown files. It just grubs fast through a big data field.

YAML code blocks in Markdown are usually treated as code examples, something to display or syntax-highlight. grubber treats them as structured data records that live next to their context, across an entire directory of files. Think dataview without Obsidian.

## Quick example

Input (`project-alpha.md`):

```markdown
---
title: Project Alpha
keywords: [project]
---

# Project Alpha

\```yaml
type: project
name: "Project Alpha"
org: "Northwind Corp"
status: active
start: 2025-01-15
end: 2025-06-30
owner: Jane Smith
\```

Kickoff completed. First milestone due end of February.
```

Output (`grubber extract examples/`):

```json
[
  {
    "title": "Project Alpha",
    "keywords": ["project"],
    "_note_file": "examples/project-alpha.md",
    "type": "project",
    "name": "Project Alpha",
    "org": "Northwind Corp",
    "status": "active",
    "start": "2025-01-15",
    "end": "2025-06-30",
    "owner": "Jane Smith"
  }
]
```

Frontmatter and YAML block are merged into one flat record. The prose stays in Markdown, untouched. Run this across a directory of 1,000 notes and you get a single JSON file with all your records.

## Installation

### Ruby

No dependencies beyond stdlib. Requires Ruby 3.1+.

```sh
chmod +x grubber
cp grubber /usr/local/bin/
```

### Crystal (optional)

A version written in the Crystal programming language is included. Crystal is a compiled language with a Ruby-like syntax. It produces a standalone binary and runs about 10x faster with the exact same features.

```sh
crystal build grubber.cr -o grubber_crystal --release
cp grubber_crystal /usr/local/bin/grubber
```

### Quick start

```sh
grubber extract ~/notes
grubber extract ~/notes -f "type=project" --format tsv
```

## Why

Structured data and the context around it usually live in different places. A database for the fields, a wiki or folder for the notes. grubber keeps them together: queryable YAML blocks inside Markdown files. The data stays where you read and write it.

- Standard Markdown. Any editor or renderer handles the format correctly. grubber just adds a read layer on top.
- Fast enough to skip the database. 1,000 files in under 0.5s (Ruby) or under 0.1s (Crystal). No index, no daemon, no setup.
- Only structure what you query. Put queryable fields in YAML blocks. Everything else stays in plain Markdown. This includes addresses, serial numbers, or meeting notes. If you'd never filter by it, don't put it in a code block.

### vs. databases

grubber is not a multi-dimensional database. But it covers many use cases: filtering, sorting, aggregating flat records across thousands of files. For personal data like contracts, contacts, inventory, or projects, that's often enough. And your data stays in plain text files that outlive any software.

### vs. Dataview (Obsidian)

| | Dataview | grubber |
|---|---|---|
| Editor lock-in | Obsidian only | Any editor |
| Query language | Proprietary DQL | Standard tools (jq, nushell, miller) |
| Output | In-note rendering | JSON/TSV for any pipeline |
| Live updates | Yes | No (run on demand) |
| Extensible | Plugin API | Shell scripts, any language |

grubber trades live updates for tool independence. No proprietary query language to learn. If you know jq or nushell, you already know how to query grubber output.

## How it works

grubber scans Markdown files for YAML frontmatter and fenced YAML code blocks, merges them into flat records, and outputs JSON or TSV. It does one thing: extract. All logic like filtering, sorting, or aggregating happens downstream with standard tools.

Multiple YAML blocks per file produce multiple records, each inheriting the frontmatter fields. The YAML block holds queryable data. The prose around it is context that doesn't need to be queried.

Available as a standalone CLI or as a Ruby library for use in other scripts:

```ruby
require_relative 'grubber'

grubber = DataGrubber::Grubber.new('~/notes', blocks_only: true)
result = grubber.extract
result[:records].each { |r| puts r['name'] }
```

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

# nushell: sort contacts by last interaction
grubber extract ~/notes -f type=person | from json | sort-by last_contact -r

# miller: TSV processing
grubber extract ~/notes --format tsv | mlr --tsv sort-by -nr amount
```

## Configuration

Optional config file at `~/.config/grubber/config.yaml`:

```yaml
defaults:
  blocks_only: true
  array_fields: [keywords, category]

sets:
  contracts:
    path: ~/notes
    filters: [type=contract]
    blocks_only: true

  people:
    path: ~/notes
    filters: [type=person]
```

Use sets with `grubber extract --set contracts`.

### Override hierarchy

CLI flags > Config set > Environment variables > Config defaults > Built-in defaults

### Environment variables

| Variable | Purpose |
|----------|---------|
| `GRUBBER_NOTES` | Default notes directory |
| `GRUBBER_ARRAY_FIELDS` | Fields to normalize to arrays (comma-separated) |

## Options

```
-o, --output FILE         Write to file instead of stdout
-s, --set NAME            Load options from config set
    --format FORMAT       json (default) or tsv
-b, --blocks-only         Only extract YAML blocks
-m, --frontmatter-only    Only extract frontmatter
    --array-fields FIELDS Normalize fields to arrays (comma-separated)
-f, --filter EXPR         Filter records (repeatable)
-h, --help                Show help
```

## Design

- Extract only. grubber reads and outputs. No transforms, no joins, no computed fields. Complexity belongs in downstream tools.
- Frontmatter provides note-level metadata (title, keywords, created date).
- YAML blocks hold structured records (each with a `type` and `name`).
- When both exist, each YAML block inherits the frontmatter fields.
- On field name collision, the YAML block wins.
- Dates are output as strings (`YYYY-MM-DD`) for safe JSON serialization.
- A `_note_file` field is added to each record for traceability.
- Valid Markdown. The format doesn't break any renderer. It extends Markdown with a queryable layer.

## License

MIT
