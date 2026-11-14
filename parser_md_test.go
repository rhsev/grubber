package main

import (
	"encoding/json"
	"testing"
)

func TestParseYAMLStringBasic(t *testing.T) {
	r := parseYAMLString([]byte("title: hello\ncount: 42\n"), nil)
	if r["title"] != "hello" {
		t.Errorf("title: got %v", r["title"])
	}
	if r["count"] != 42 {
		t.Errorf("count: got %v", r["count"])
	}
}

func TestParseYAMLStringEmpty(t *testing.T) {
	if r := parseYAMLString([]byte(""), nil); r != nil {
		t.Errorf("empty input should return nil, got %v", r)
	}
}

func TestParseYAMLStringDate(t *testing.T) {
	r := parseYAMLString([]byte("due: 2024-03-15\n"), nil)
	if r["due"] != "2024-03-15" {
		t.Errorf("date should be stringified, got %v", r["due"])
	}
}

func TestParseYAMLStringFlowMappingKey(t *testing.T) {
	// An unquoted {{...}} placeholder decodes to a mapping with a non-string
	// key. The record must still survive json.Marshal.
	r := parseYAMLString([]byte("Source: {{src.home}}\n"), nil)
	if _, err := json.Marshal(r); err != nil {
		t.Errorf("record must be JSON-marshalable, got %v", err)
	}
}

func TestParseYAMLStringNaN(t *testing.T) {
	r := parseYAMLString([]byte("value: .nan\ninf: .inf\n"), nil)
	if r["value"] != nil || r["inf"] != nil {
		t.Errorf("NaN/Inf should normalize to nil, got %v / %v", r["value"], r["inf"])
	}
	if _, err := json.Marshal(r); err != nil {
		t.Errorf("record must be JSON-marshalable, got %v", err)
	}
}

func TestParseYAMLLenient(t *testing.T) {
	r := parseYAMLLenient([]byte("Title: hello\n# comment\nEmpty:\nkey: value\n"))
	if r["Title"] != "hello" {
		t.Errorf("Title: got %v", r["Title"])
	}
	if r["Empty"] != nil {
		t.Errorf("empty value should be nil, got %v", r["Empty"])
	}
	if r["key"] != "value" {
		t.Errorf("key: got %v", r["key"])
	}
}

func TestParseYAMLLenientEmpty(t *testing.T) {
	if r := parseYAMLLenient([]byte("")); r != nil {
		t.Errorf("empty input should return nil, got %v", r)
	}
}

func TestParseMmdHeaderBasic(t *testing.T) {
	input := "Title: Hello World\nAuthor: Alice\n\nbody text"
	meta, body := parseMmdHeader(input)
	if meta["title"] != "Hello World" {
		t.Errorf("title: got %v", meta["title"])
	}
	if meta["author"] != "Alice" {
		t.Errorf("author: got %v", meta["author"])
	}
	if body != "body text" {
		t.Errorf("body: got %q", body)
	}
}

func TestParseMmdHeaderContinuation(t *testing.T) {
	input := "Title: Line one\n  continuation\n\n"
	meta, _ := parseMmdHeader(input)
	if meta["title"] != "Line one\ncontinuation" {
		t.Errorf("continuation: got %v", meta["title"])
	}
}

func TestParseMmdHeaderNoBlankLine(t *testing.T) {
	input := "Title: Hello\n"
	meta, body := parseMmdHeader(input)
	if meta["title"] != "Hello" {
		t.Errorf("title: got %v", meta["title"])
	}
	if body != "" {
		t.Errorf("body should be empty, got %q", body)
	}
}

func TestParseMmdHeaderNotMetadata(t *testing.T) {
	input := "not a header\nTitle: Hello\n"
	meta, body := parseMmdHeader(input)
	if len(meta) != 0 {
		t.Errorf("invalid header should return empty meta, got %v", meta)
	}
	if body != input {
		t.Errorf("body should be full input")
	}
}

func TestParseYAMLBlocks(t *testing.T) {
	body := []byte("text\n```yaml\nfoo: bar\n```\nmore\n```yaml\nbaz: qux\n```\n")
	blocks := parseYAMLBlocks(body, 0, nil)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0]["foo"] != "bar" {
		t.Errorf("block 0 foo: got %v", blocks[0]["foo"])
	}
	if blocks[1]["baz"] != "qux" {
		t.Errorf("block 1 baz: got %v", blocks[1]["baz"])
	}
}

func TestParseYAMLBlocksIndentedFence(t *testing.T) {
	// Block inside a list item: both fences indented. The indented closing
	// fence must terminate the block — prose below must not leak in.
	body := []byte("1. item:\n\n   ```yaml\n   sync_dir: ~/Sync\n   excludes:\n     - .git/\n   ```\n\nprose: with colon\n\n```bash\necho hi\n```\n")
	blocks := parseYAMLBlocks(body, 0, nil)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d: %v", len(blocks), blocks)
	}
	if blocks[0]["sync_dir"] != "~/Sync" {
		t.Errorf("sync_dir: got %v", blocks[0]["sync_dir"])
	}
	if _, leaked := blocks[0]["prose"]; leaked {
		t.Errorf("prose outside the block leaked into the record: %v", blocks[0])
	}
}

func TestParseYAMLBlocksInfoStringIgnored(t *testing.T) {
	if blocks := parseYAMLBlocks([]byte("```yaml title=x\nfoo: bar\n```\n"), 0, nil); len(blocks) != 0 {
		t.Errorf("fence with info string should not match, got %v", blocks)
	}
}

func TestParseYAMLBlocksEmpty(t *testing.T) {
	if blocks := parseYAMLBlocks([]byte("no blocks here"), 0, nil); len(blocks) != 0 {
		t.Errorf("expected 0 blocks, got %d", len(blocks))
	}
}

func TestMdExtractFrontmatterOnly(t *testing.T) {
	p := &mdParser{}
	data := []byte("---\ntitle: hello\n---\n```yaml\nfoo: bar\n```\n")
	fm, blocks, err := p.Extract("f.md", data, ParseOpts{FrontmatterOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if fm["title"] != "hello" {
		t.Errorf("frontmatter title: got %v", fm["title"])
	}
	if len(blocks) != 0 {
		t.Errorf("frontmatter-only should return no blocks, got %d", len(blocks))
	}
}

func TestMdExtractBlocks(t *testing.T) {
	p := &mdParser{}
	data := []byte("---\ntitle: hello\n---\n```yaml\nfoo: bar\n```\n")
	fm, blocks, err := p.Extract("f.md", data, ParseOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if fm["title"] != "hello" {
		t.Errorf("frontmatter title: got %v", fm["title"])
	}
	if len(blocks) != 1 || blocks[0]["foo"] != "bar" {
		t.Errorf("blocks: got %v", blocks)
	}
}

func TestMdExtractNoFrontmatter(t *testing.T) {
	p := &mdParser{}
	data := []byte("just text\n```yaml\nfoo: bar\n```\n")
	fm, blocks, err := p.Extract("f.md", data, ParseOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(fm) != 0 {
		t.Errorf("no frontmatter expected, got %v", fm)
	}
	if len(blocks) != 1 {
		t.Errorf("expected 1 block, got %d", len(blocks))
	}
}

func TestMdExtractCRLF(t *testing.T) {
	p := &mdParser{}
	data := []byte("---\r\ntitle: hello\r\n---\r\n\r\n```yaml\r\nfoo: bar\r\n```\r\n")
	fm, blocks, err := p.Extract("f.md", data, ParseOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if fm["title"] != "hello" {
		t.Errorf("frontmatter title: got %v", fm["title"])
	}
	if len(blocks) != 1 || blocks[0]["foo"] != "bar" {
		t.Errorf("blocks: got %v", blocks)
	}
}

func TestMdExtractBOM(t *testing.T) {
	p := &mdParser{}
	data := []byte("\xef\xbb\xbf---\ntitle: hello\n---\n")
	fm, _, err := p.Extract("f.md", data, ParseOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if fm["title"] != "hello" {
		t.Errorf("BOM-prefixed frontmatter: got %v", fm)
	}
}

// An empty block closes at its own fence. It used to reach for the next
// closing fence below and turn the prose in between into a record.
func TestParseYAMLBlocksEmptyBlockStaysEmpty(t *testing.T) {
	body := "```yaml\n```\n\nText with colon: yes\n\n```sh\necho: hi\n```\n\n```yaml\nid: 2\n```\n"
	records := parseYAMLBlocks([]byte(body), 0, nil)
	if len(records) != 1 {
		t.Fatalf("want the one real block, got %v", records)
	}
	if records[0]["id"] != 2 || records[0]["_line"] != 10 {
		t.Errorf("got %v", records[0])
	}
	d := &Diagnostics{}
	parseYAMLBlocks([]byte(body), 0, d)
	if len(d.findings) != 0 {
		t.Errorf("an empty block is no finding, got %v", d.findings)
	}
}

// A note that is frontmatter only may end with the closing --- and no newline.
func TestMdExtractFrontmatterAtEndOfFile(t *testing.T) {
	p := &mdParser{}
	for _, data := range []string{"---\ntitle: x\n---", "---\r\ntitle: x\r\n---", "---\r\ntitle: x\r\n---\r"} {
		fm, blocks, err := p.Extract("f.md", []byte(data), ParseOpts{})
		if err != nil {
			t.Fatal(err)
		}
		if fm["title"] != "x" || len(blocks) != 0 {
			t.Errorf("%q: frontmatter %v, blocks %v", data, fm, blocks)
		}
	}
	// A longer line that only starts with --- does not close the frontmatter.
	if fm, _, _ := p.Extract("f.md", []byte("---\ntitle: x\n---more"), ParseOpts{}); fm != nil {
		t.Errorf("---more must not close the frontmatter, got %v", fm)
	}
}

// A timestamp with a time of day keeps it; a plain date is still YYYY-MM-DD.
func TestParseYAMLStringTimestampKeepsTime(t *testing.T) {
	r := parseYAMLString([]byte("a: 2026-11-10 14:30:00\nb: 2026-11-10T14:30:00+01:00\nc: 2026-11-10\nd: [2026-11-10 08:00:00]\ne: \"2026-11-10 14:30:00\"\n"), nil)
	if r["a"] != "2026-11-10 14:30:00" || r["b"] != "2026-11-10T14:30:00+01:00" || r["e"] != "2026-11-10 14:30:00" {
		t.Errorf("time of day lost: %v", r)
	}
	if r["c"] != "2026-11-10" {
		t.Errorf("date: got %v", r["c"])
	}
	if arr, ok := r["d"].([]any); !ok || len(arr) != 1 || arr[0] != "2026-11-10 08:00:00" {
		t.Errorf("timestamp in a list: got %v", r["d"])
	}
}

// A block inside another code fence is an example in a note about notes, and
// no record. doctor does not take its opening fence for an unclosed one.
func TestParseYAMLBlocksSkipsABlockInAnotherFence(t *testing.T) {
	body := "````\n```yaml\nname: example\n```\n````\n\n~~~\n```yaml\nname: tilde\n~~~\n\n```yaml\nname: real\n```\n"
	d := &Diagnostics{}
	recs := parseYAMLBlocks([]byte(body), 0, d)
	if len(recs) != 1 || recs[0]["name"] != "real" || recs[0]["_line"] != 12 {
		t.Errorf("got %v, want the one block outside the fences, at line 12", recs)
	}
	if len(d.findings) != 0 {
		t.Errorf("no finding expected, got %v", d.findings)
	}
}
