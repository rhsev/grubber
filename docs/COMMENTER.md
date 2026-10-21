# commenter

grubber reads only the YAML blocks. An entry is a YAML block with the text
that goes with it, and several programs built on grubber also need that text.
commenter reads and replaces it.

## Install

`make install` and `make link` build and install it together with grubber.
With Go:

```sh
go install github.com/rhsev/grubber/cmd/commenter@latest
```

Go programs can use the rules directly through the package
`github.com/rhsev/grubber/entry`.

## Reading and replacing

```sh
commenter note.md --line 42                     # the entry whose block opens at line 42
commenter note.md --where id=210322647 --where binder=safari
printf 'New text' | commenter note.md --line 42 --if-hash "$hash" --set -
```

`--line` takes the `_line` of a grubber record, so a record from `grubber
extract` leads straight to its entry. Reading prints one JSON line per entry:

```json
{"file":"/notes/collections/binder_safari.md","line":42,"end":46,"heading":"waterhole.jpg","prose":"At dawn.","hash":"9f2c…"}
```

`line` and `end` are the fence lines, counted from 1. `hash` is the SHA-256 of
the file.

`--where` compares field values as text, in NFC, and matches a list when one
of its items matches. `--set TEXT` (or `-` for stdin) replaces the prose of
exactly one entry. With `--all` it replaces it in every match. A text that
holds a heading, a `---` line or a ` ```yaml ` block is refused, because it
would change where entries start. With `--if-hash`, commenter writes nothing
if the note changed since that hash was read.

## The rules

- A data block opens with a ` ```yaml ` line and closes with a ` ``` ` line.
  ` ```yml ` and every other fence language is an example and never data.
- The prose is the text between the block and the nearest boundary above it.
  A boundary is a heading of any level, the end of another data block, a line
  of three or more `-`, the end of the frontmatter, or the start of the file.
- The entry's heading is the nearest heading above the block.
- Text after a block belongs to no entry. If another block follows without a
  boundary, the text is that block's prose.

The cases are in `entry/testdata/entries`. Each note there has a `.jsonl` file
that lists its entries, so a program with rules of its own can check itself
against them.

## Writing

commenter overwrites the note in place and does not replace it. The note keeps
its inode, so a bookmark to it still finds it after a rename. Its extended
attributes, creation date, flags, ACLs and hard links stay as well.

Before that, commenter writes the new content to `.<name>.journal` next to the
note, and it removes that file once the note is written. If a write stops
halfway, the journal holds the complete new content. commenter then refuses to
write that note again until the journal is gone.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | done |
| 1 | reading or writing failed |
| 2 | no entry matches |
| 3 | several entries match and `--all` is missing |
| 4 | the note changed, or a journal from an earlier write is left |
| 64 | the call is wrong |
