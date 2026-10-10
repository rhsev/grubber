# Why `doctor --fix` removes invisible characters

Every character on this list is invisible, carries no meaning for the
text, and makes equal things unequal. Search,
filters, diffs, and pipelines operate on bytes, not on the rendered image,
so a word with an invisible character inside is a different word.

They arrive almost exclusively through copy-paste: web clippings (newspaper
sites use soft hyphens for justification), chat UIs, PDFs, and old encoding
conversions. Nobody types them.

## Removed

**U+00AD SOFT HYPHEN.** An invisible hyphenation hint. The browser renders
`Ge­schäfte` indistinguishable from `Geschäfte`, but the text contains
`Ge<SHY>schäfte`. `grep Geschäfte` finds nothing, full-text filters miss the
record, and the character travels along with every copy. Main source:
clipped newspaper articles.

**U+200B–U+200C ZERO WIDTH SPACE / NON-JOINER.** Zero pixels wide, but a
real character in the string. Breaks words for search and comparison exactly
like the soft hyphen. Text copied out of chat UIs often carries these in
bulk. U+200D JOINER is the exception and is kept, see below.

**U+200E/200F and U+202A–202E bidi marks, embeddings, overrides.** Control
left-to-right/right-to-left rendering. Useless in plain notes, and
RIGHT-TO-LEFT OVERRIDE can visually reverse text, which is the classic
filename spoofing trick (`gpj.exe` displays as `exe.jpg`). Never legitimate
in a notes corpus.

**U+2028/2029 LINE / PARAGRAPH SEPARATOR.** Unicode line breaks that almost
no tool treats as line breaks. Editors show one line, line-based tools
(grep, JSONL) see something else; inside JSON strings they are known to
break JavaScript parsers. Typical source: copies from PDFs and Apple
apps.

**U+2060–2064 WORD JOINER and invisible math operators.** WORD JOINER is
the modern BOM-as-glue replacement: invisible, prevents line breaks, breaks
search. The INVISIBLE PLUS/TIMES/SEPARATOR characters come from mathematical
typesetting and only end up in text by copy-paste accident.

**U+FEFF BOM.** At the start of a file a legitimate (if unnecessary) UTF-8
signature; in the middle of a file it is debris from concatenating
files, acting like a zero-width space with the same search problems. The
extract parser tolerates a leading BOM; hygiene removes both cases.

**C1 controls U+0080–U+009F.** Almost always encoding corpses. The text was
Windows-1252 (curly quotes, en dash, bullet live there) but got
misinterpreted as Latin-1/UTF-8. U+0093/0094 are broken quotation marks,
U+0096 a broken dash. The characters are unprintable and the original
character is already lost, so removal is all that's left.

**C0 controls except `\t` and `\n`.** Terminal control codes, unprintable,
confuse terminals and parsers. A lone `\r` (old Mac line ending, still
produced by some apps) makes the file look multi-line in an editor while it
is a single line for grep and JSONL.

**CRLF → LF.** Not a bad character but a normalization. Mixed line endings
cause phantom diffs in git, `^M` artifacts, and tools that treat `\r` as
part of the line content.

## Deleted, not replaced

`--fix` deletes; the only replacement is CRLF → LF (a lone `\r` is
deleted). For nearly every class this follows from where the characters
sit. They have no visible counterpart and sit *between* letters that belong
together, so substituting a space would cut words apart.

The C1 range is the exception to consider. Behind an encoding corpse there
once was a real character (U+0093/0094 were curly quotes, U+0096 an en dash).
Translating them back to Windows-1252 would be a guess, right only if the
file actually took that specific mis-decoding path, and inventing characters
when wrong. Deletion adds nothing. `„Wort<U+0094>` becomes `„Wort`, visible
and local, while the control character was invisible *and* broken.

## Deliberately kept

**Tabs.** Carry indentation *semantics* in the TaskPaper world. Never
touched.

**U+00A0 NO-BREAK SPACE.** Can be intentional typography (`10 €`,
`Dr. Müller` without a break). Reported as its own category
(`suspect-char`), never fixed.

**U+200D ZERO WIDTH JOINER.** Same category, for a stronger reason. It is
what holds a composed emoji together. Removing it turns 👨‍👩‍👧‍👦 into four
separate people and 👩‍💻 into a woman beside a laptop, silently. A joiner in
the middle of prose is a paste artifact, but no rule separates that case
from an emoji reliably enough to delete on, so the report names it and the
file stays as it is.

**Bytes that are not valid UTF-8.** Reported as `invalid-utf8`, never fixed.
The same argument as the C1 range applies, one step earlier. An undecodable
byte still holds a character, but which one depends on the encoding the file
came from, and guessing would invent text. Deleting is no better here,
because unlike a C1 control the byte is not itself broken output, only
unread. So it stays, and the report names the byte and where it sits. In
extract, a value carrying such a byte reaches the output as
U+FFFD, and the exact escaping of that replacement differs between Go
versions, so a cached extract can churn without the note changing.

**Unicode normalization (NFC/NFD)** is not a defect, so `doctor` neither
reports nor fixes it. Extract compares filter values and merge keys in NFC.
