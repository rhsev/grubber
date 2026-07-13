package main

import (
	"encoding/json"
	"testing"
)

func TestParseYAMLStringBasic(t *testing.T) {
	r := parseYAMLString([]byte("title: hello\ncount: 42\n"))
	if r["title"] != "hello" {
		t.Errorf("title: got %v", r["title"])
	}
	if r["count"] != 42 {
		t.Errorf("count: got %v", r["count"])
	}
}

func TestParseYAMLStringEmpty(t *testing.T) {
	if r := parseYAMLString([]byte("")); r != nil {
		t.Errorf("empty input should return nil, got %v", r)
	}
}

func TestParseYAMLStringDate(t *testing.T) {
	r := parseYAMLString([]byte("due: 2024-03-15\n"))
	if r["due"] != "2024-03-15" {
		t.Errorf("date should be stringified, got %v", r["due"])
	}
}

func TestParseYAMLStringFlowMappingKey(t *testing.T) {
	// An unquoted {{...}} placeholder decodes to a mapping with a non-string
	// key. The record must still survive json.Marshal.
	r := parseYAMLString([]byte("Source: {{src.home}}\n"))
	if _, err := json.Marshal(r); err != nil {
		t.Errorf("record must be JSON-marshalable, got %v", err)
	}
}

func TestParseYAMLStringNaN(t *testing.T) {
	r := parseYAMLString([]byte("value: .nan\ninf: .inf\n"))
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
	blocks := parseYAMLBlocks(body)
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
	blocks := parseYAMLBlocks(body)
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
	if blocks := parseYAMLBlocks([]byte("```yaml title=x\nfoo: bar\n```\n")); len(blocks) != 0 {
		t.Errorf("fence with info string should not match, got %v", blocks)
	}
}

func TestParseYAMLBlocksEmpty(t *testing.T) {
	if blocks := parseYAMLBlocks([]byte("no blocks here")); len(blocks) != 0 {
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
