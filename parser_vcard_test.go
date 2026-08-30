package main

import (
	"reflect"
	"testing"
)

// Apple-style vCard 3.0 export: CRLF, groups (item1.), folded lines,
// TYPE parameters, binary PHOTO, X- noise.
const appleExport = "BEGIN:VCARD\r\n" +
	"VERSION:3.0\r\n" +
	"PRODID:-//Apple Inc.//macOS 15.0//EN\r\n" +
	"N:Smith;Jane;;;\r\n" +
	"FN:Jane Smith\r\n" +
	"ORG:Northwind Corp;Sales\r\n" +
	"TITLE:Account Manager\r\n" +
	"EMAIL;type=INTERNET;type=WORK;type=pref:jane@northwind.example\r\n" +
	"item1.EMAIL;type=INTERNET:jane@home.example\r\n" +
	"item1.X-ABLabel:_$!<Other>!$_\r\n" +
	"TEL;type=CELL;type=VOICE;type=pref:+49 170 1234567\r\n" +
	"TEL;type=HOME;type=VOICE:+49 30 7654321\r\n" +
	"item2.ADR;type=HOME;type=pref:;;Musterstr. 1;Berlin;;10115;Germany\r\n" +
	"item2.X-ABADR:de\r\n" +
	"BDAY:1985-04-12\r\n" +
	"CATEGORIES:Friends,Work\r\n" +
	"NOTE:Met at FOSDEM\\, 2024.\\nPrefers email.\r\n" +
	"item3.URL;type=pref:https://jane.example\r\n" +
	"PHOTO;ENCODING=b;TYPE=JPEG:TU9DSy1KUEVHLURBVEEtTU9DSy1KUEVH\r\n" +
	" TU9DSy1KUEVHLURBVEEtTU9DSy1KUEVH\r\n" +
	"UID:12345-ABCDE\r\n" +
	"END:VCARD\r\n" +
	"BEGIN:VCARD\r\n" +
	"VERSION:3.0\r\n" +
	"N:;;;;\r\n" +
	"FN:ACME Support\r\n" +
	"ORG:ACME GmbH\r\n" +
	"TEL;type=WORK:+49 800 1111111\r\n" +
	"END:VCARD\r\n"

func TestVcardExtract(t *testing.T) {
	p := &vcardParser{}
	fm, blocks, err := p.Extract("contacts.vcf", []byte(appleExport), ParseOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if fm != nil {
		t.Errorf("frontmatter = %v, want nil", fm)
	}
	if len(blocks) != 2 {
		t.Fatalf("got %d records, want 2", len(blocks))
	}

	want := Record{
		"uid":        "12345-ABCDE",
		"name":       "Jane Smith",
		"org":        "Northwind Corp",
		"title":      "Account Manager",
		"email":      []any{"jane@northwind.example", "jane@home.example"},
		"tel":        []any{"+49 170 1234567", "+49 30 7654321"},
		"url":        []any{"https://jane.example"},
		"adr":        "Musterstr. 1, Berlin, 10115, Germany",
		"bday":       "1985-04-12",
		"categories": []any{"Friends", "Work"},
		"note":       "Met at FOSDEM, 2024.\nPrefers email.",
	}
	if !reflect.DeepEqual(blocks[0], want) {
		t.Errorf("record 0:\n got  %v\n want %v", blocks[0], want)
	}

	want2 := Record{
		"name": "ACME Support",
		"org":  "ACME GmbH",
		"tel":  []any{"+49 800 1111111"},
	}
	if !reflect.DeepEqual(blocks[1], want2) {
		t.Errorf("record 1:\n got  %v\n want %v", blocks[1], want2)
	}
}

func TestVcardSingleCardLF(t *testing.T) {
	// One contact per file, plain LF line endings.
	src := "BEGIN:VCARD\nVERSION:3.0\nFN:Max Mustermann\nTEL;type=CELL:+49 151 0000000\nEND:VCARD\n"
	_, blocks, err := (&vcardParser{}).Extract("max.vcf", []byte(src), ParseOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 1 {
		t.Fatalf("got %d records, want 1", len(blocks))
	}
	if blocks[0]["name"] != "Max Mustermann" {
		t.Errorf("name = %v", blocks[0]["name"])
	}
	if !reflect.DeepEqual(blocks[0]["tel"], []any{"+49 151 0000000"}) {
		t.Errorf("tel = %v", blocks[0]["tel"])
	}
}

func TestVcardFoldedProperty(t *testing.T) {
	// A NOTE folded across three physical lines must reassemble seamlessly.
	src := "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Folded\r\n" +
		"NOTE:This is a long note that has been fol\r\n ded across multiple lines by the ex\r\n porter.\r\nEND:VCARD\r\n"
	_, blocks, err := (&vcardParser{}).Extract("f.vcf", []byte(src), ParseOpts{})
	if err != nil {
		t.Fatal(err)
	}
	got := blocks[0]["note"]
	want := "This is a long note that has been folded across multiple lines by the exporter."
	if got != want {
		t.Errorf("note = %q, want %q", got, want)
	}
}

func TestVcardGarbageIgnored(t *testing.T) {
	// Lines outside BEGIN/END and non-property junk must not create records.
	src := "just some text\nFN:Orphan Property\n\nBEGIN:VCARD\nVERSION:3.0\nEND:VCARD\n"
	_, blocks, err := (&vcardParser{}).Extract("g.vcf", []byte(src), ParseOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 0 {
		t.Errorf("got %d records, want 0 (empty card dropped)", len(blocks))
	}
}

func TestVcardEscapedSeparators(t *testing.T) {
	// RFC 2426: a backslash-escaped separator is part of the field, not a
	// structural split point.
	src := "BEGIN:VCARD\n" +
		"VERSION:3.0\n" +
		"FN:Escape Case\n" +
		"ORG:ACME\\; Sub;Unit\n" +
		"CATEGORIES:Friends\\,Berlin,Work\n" +
		"ADR:;;123 Main\\; Suite 4;City;;12345;US\n" +
		"END:VCARD\n"
	_, blocks, err := (&vcardParser{}).Extract("e.vcf", []byte(src), ParseOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 1 {
		t.Fatalf("got %d records, want 1", len(blocks))
	}
	r := blocks[0]
	if r["org"] != "ACME; Sub" {
		t.Errorf("org = %q, want %q", r["org"], "ACME; Sub")
	}
	if want := []any{"Friends,Berlin", "Work"}; !reflect.DeepEqual(r["categories"], want) {
		t.Errorf("categories = %v, want %v", r["categories"], want)
	}
	if want := "123 Main; Suite 4, City, 12345, US"; r["adr"] != want {
		t.Errorf("adr = %q, want %q", r["adr"], want)
	}
}
