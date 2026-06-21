# Ideas

Deferred feature ideas. Nothing here is committed work — just notes so they
aren't lost. Each entry should be implementable from its own description.

## `block_marker`: opt-in harvesting of marked data blocks

**Problem.** grubber harvests *every* ` ```yaml ` block as a record
(`parser_md.go`). In notes that also contain illustrative YAML — docker-compose
snippets, config examples, tutorials — those blocks get pulled in as data too.
There is no way to say "this YAML is just an example, not a record."

**Idea.** A config option that switches the markdown parser into a stricter
opt-in mode:

```yaml
defaults:
  block_marker: all      # all (default) | data
```

- `all` — current behaviour, unchanged. Every ` ```yaml ` is a record.
- `data` — only ` ```yaml data ` is harvested; a bare ` ```yaml ` is treated as
  illustration and ignored.

**Why it's safe to defer.** Purely additive. The default stays `all`, so no
existing corpus changes behaviour. Can land any time.

**Notes / constraints.**
- Resolve through the normal tier `config → set → env → CLI` (like `extensions`),
  i.e. add a `defaults.block_marker` key + accessor in `config.go`, a
  `GRUBBER_BLOCK_MARKER` env var, and a `--block-marker` CLI flag.
- The current regex `(?s)```yaml\n(.*?)\n``` ` requires `yaml` immediately
  followed by a newline, so ` ```yaml data ` is **not** matched today. The
  parser needs info-string parsing (read the token after `yaml`) to recognise
  the marked form and, in `data` mode, skip unmarked blocks.
- Keep the marker word fixed (`data`); the option chooses the *policy*, not the
  vocabulary.
- Scope to the markdown parser only — code fences are markdown-specific;
  `parser_typst.go` has its own structure.
- Orthogonal to `blocks_only` / `frontmatter-only`; the marker only decides what
  counts as a block.
- Single YAML fence language throughout keeps editor/GitHub syntax highlighting
  on (no reliance on a ` ```yml ` alias).
- Tests: `all` mode unchanged; `data` mode harvests ` ```yaml data ` and skips
  bare ` ```yaml `; update README + ARCHITECTURE.
