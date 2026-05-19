package main

import (
	"reflect"
	"testing"
)

func TestTypstFindBlockSetDocument(t *testing.T) {
	data := []byte(`#set document(title: "Hello", author: "Alice")`)
	block := typstFindBlock(data, "#set document(")
	if block == nil {
		t.Fatal("expected block, got nil")
	}
	if string(block) != `title: "Hello", author: "Alice"` {
		t.Errorf("unexpected block: %q", block)
	}
}

func TestTypstFindBlockMetadata(t *testing.T) {
	data := []byte(`#metadata((title: "Hi")) <meta>`)
	block := typstFindBlock(data, "#metadata((")
	if block == nil {
		t.Fatal("expected block, got nil")
	}
	if string(block) != `title: "Hi"` {
		t.Errorf("unexpected block: %q", block)
	}
}

func TestTypstFindBlockNested(t *testing.T) {
	data := []byte(`#set document(title: "a (b)")`)
	block := typstFindBlock(data, "#set document(")
	if block == nil {
		t.Fatal("expected block, got nil")
	}
}

func TestTypstFindBlockNotFound(t *testing.T) {
	data := []byte(`no metadata here`)
	if block := typstFindBlock(data, "#set document("); block != nil {
		t.Errorf("expected nil, got %q", block)
	}
}

func TestTypstSplit(t *testing.T) {
	cases := []struct {
		input string
		want  []string
	}{
		{`a, b, c`, []string{"a", " b", " c"}},
		{`a, (b, c), d`, []string{"a", " (b, c)", " d"}},
		{`"a, b", c`, []string{`"a, b"`, " c"}},
	}
	for _, tc := range cases {
		got := typstSplit(tc.input, ',')
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("typstSplit(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestParseTypstValue(t *testing.T) {
	cases := []struct {
		input string
		want  any
	}{
		{`"hello"`, "hello"},
		{`("a", "b")`, []any{"a", "b"}},
		{`bare`, "bare"},
	}
	for _, tc := range cases {
		got := parseTypstValue(tc.input)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("parseTypstValue(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestParseTypstFieldsBasic(t *testing.T) {
	block := []byte(`title: "My Doc", author: "Alice"`)
	r := parseTypstFields(block)
	if r["title"] != "My Doc" {
		t.Errorf("title: got %v", r["title"])
	}
	if r["author"] != "Alice" {
		t.Errorf("author: got %v", r["author"])
	}
}

func TestParseTypstFieldsDate(t *testing.T) {
	block := []byte(`date: datetime(year: 2024, month: 3, day: 15)`)
	r := parseTypstFields(block)
	if r["date"] != "2024-03-15" {
		t.Errorf("date: got %v", r["date"])
	}
}

func TestTypstExtract(t *testing.T) {
	p := &typstParser{}
	data := []byte(`#set document(title: "Hello", author: "Alice")`)
	fm, blocks, err := p.Extract("f.typ", data, ParseOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if fm != nil {
		t.Errorf("typst returns no frontmatter, got %v", fm)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0]["title"] != "Hello" {
		t.Errorf("title: got %v", blocks[0]["title"])
	}
}

func TestTypstExtractNoBlock(t *testing.T) {
	p := &typstParser{}
	data := []byte(`// just a comment`)
	fm, blocks, err := p.Extract("f.typ", data, ParseOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if fm != nil || len(blocks) != 0 {
		t.Errorf("expected nil/empty, got fm=%v blocks=%v", fm, blocks)
	}
}
