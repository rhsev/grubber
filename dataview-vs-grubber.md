# Data Structure Comparison: Dataview vs Grubber

## How data is stored

Dataview uses inline fields directly in the note body:

```
status:: active
due:: 2026-03-01
[assignee:: Anna]  ← can appear mid-sentence
```

Grubber uses YAML code blocks:

```yaml
type: project
status: active
due: 2026-03-01
assignee: Anna
```

Both read YAML frontmatter. The difference is how structured data lives in the body.


## Dataview inline fields

Advantages:
- Minimal syntax, very low friction to add a field
- Reads naturally in prose, especially the bracket variant
- No indentation rules, no quoting, no special characters to worry about
- Easy to teach to non-technical users

Disadvantages:
- Flat key-value only. No nesting, no structured lists
- One record per note. No way to store multiple records
- Proprietary syntax. Only Dataview can parse `key:: value`
- Mixed with prose. Extracting fields outside Obsidian requires a custom parser
- Two syntaxes for the same thing (`key::` vs `[key::]`) adds ambiguity
- No schema validation, no defined data types


## Grubber YAML blocks

Advantages:
- Standard format. Every programming language has a YAML parser
- Nesting, lists, multi-line values, complex structures
- Multiple records per note (e.g. several items in one inventory note)
- Clean separation of data and prose
- Same format as frontmatter — one syntax for everything
- Tool-independent. Works with jq, yq, Python, Ruby, or any JSON consumer
- Portable. The data is readable and usable without grubber

Disadvantages:
- More verbose for simple key-value pairs
- Indentation-sensitive. A wrong space breaks the record
- Looks technical in the note body (code block fences)
- Slightly higher barrier for non-technical users
- Requires familiarity with YAML syntax rules (quoting strings with colons, etc.)


## When to use what

Dataview inline fields work well when:
- All data is flat key-value
- Each note is one record
- You stay in Obsidian
- Users are non-technical

YAML blocks work well when:
- Data has structure (lists, nesting)
- A note contains multiple records
- You need the data outside Obsidian
- You want a standard format that outlives any tool
