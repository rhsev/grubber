package main

import (
	"strings"
)

func init() {
	RegisterParser(".vcf", &vcardParser{})
}

// vcardParser reads vCard 3.0 files as exported by Contacts.app. Each card
// becomes one block record; a file may hold one card or a whole export.
// The mapping is deliberately lossy: TYPE parameters (CELL/HOME/WORK) are
// dropped, structured fields are flattened, binary and X- properties are
// skipped. EMAIL, TEL and URL are always arrays so records type consistently
// across a corpus.
type vcardParser struct{}

// Properties that never become record fields: format plumbing, binary
// payloads, and the structured name (FN already carries the display name).
var vcardSkip = map[string]bool{
	"BEGIN": true, "END": true, "VERSION": true, "PRODID": true,
	"N": true, "PHOTO": true, "LOGO": true, "SOUND": true, "KEY": true,
	"REV": true,
}

func (p *vcardParser) Extract(path string, data []byte, opts ParseOpts) (Record, []Record, error) {
	var blocks []Record
	var rec Record

	for _, line := range unfoldVcard(data) {
		name, value, ok := splitVcardLine(line)
		if !ok {
			continue
		}
		switch {
		case name == "BEGIN" && strings.EqualFold(value, "VCARD"):
			rec = Record{}
			continue
		case name == "END" && strings.EqualFold(value, "VCARD"):
			if len(rec) > 0 {
				blocks = append(blocks, rec)
			}
			rec = nil
			continue
		}
		if rec == nil || value == "" || vcardSkip[name] || strings.HasPrefix(name, "X-") {
			continue
		}

		switch name {
		case "FN":
			rec["name"] = unescapeVcard(value)
		case "UID":
			// The card's identity — deliberately not "id", which names a
			// file in the fileregister convention while UID names one card
			// among possibly many per file. In a hand-maintained master
			// file typically a short slug, usable as the join key to a
			// companion Markdown record carrying the same uid.
			rec["uid"] = unescapeVcard(value)
		case "ORG":
			// Structured: org;unit;... — keep the organization only.
			rec["org"] = unescapeVcard(strings.SplitN(value, ";", 2)[0])
		case "TITLE":
			rec["title"] = unescapeVcard(value)
		case "NICKNAME":
			rec["nickname"] = unescapeVcard(value)
		case "BDAY":
			rec["bday"] = unescapeVcard(value)
		case "NOTE":
			rec["note"] = unescapeVcard(value)
		case "EMAIL", "TEL", "URL":
			key := strings.ToLower(name)
			arr, _ := rec[key].([]any)
			rec[key] = append(arr, unescapeVcard(value))
		case "CATEGORIES":
			// Commas inside a category are escaped, so a raw split is safe.
			var cats []any
			for _, c := range strings.Split(value, ",") {
				if c = unescapeVcard(c); c != "" {
					cats = append(cats, c)
				}
			}
			if len(cats) > 0 {
				rec["categories"] = cats
			}
		case "ADR":
			// Structured: pobox;ext;street;city;region;zip;country.
			var parts []string
			for _, c := range strings.Split(value, ";") {
				if c = unescapeVcard(c); c != "" {
					parts = append(parts, c)
				}
			}
			if len(parts) > 0 {
				rec["adr"] = strings.Join(parts, ", ")
			}
		}
	}
	return nil, blocks, nil
}

// unfoldVcard splits raw bytes into logical lines: a physical line starting
// with space or tab continues the previous one (RFC 2425 folding).
func unfoldVcard(data []byte) []string {
	physical := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	var lines []string
	for _, l := range physical {
		if (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")) && len(lines) > 0 {
			lines[len(lines)-1] += l[1:]
			continue
		}
		lines = append(lines, l)
	}
	return lines
}

// splitVcardLine parses "group.NAME;param=v:value" into the bare property
// name (group stripped, uppercased) and the raw value. The separating colon
// is the first one outside a quoted parameter value.
func splitVcardLine(line string) (name, value string, ok bool) {
	inQuotes := false
	colon := -1
	for i, r := range line {
		switch {
		case r == '"':
			inQuotes = !inQuotes
		case r == ':' && !inQuotes:
			colon = i
		}
		if colon >= 0 {
			break
		}
	}
	if colon <= 0 {
		return "", "", false
	}
	namePart := line[:colon]
	if dot := strings.IndexByte(namePart, '.'); dot >= 0 {
		namePart = namePart[dot+1:]
	}
	if semi := strings.IndexByte(namePart, ';'); semi >= 0 {
		namePart = namePart[:semi]
	}
	return strings.ToUpper(strings.TrimSpace(namePart)), line[colon+1:], true
}

// unescapeVcard resolves the RFC 2426 text escapes: \\ \, \; \n \N.
func unescapeVcard(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return strings.TrimSpace(s)
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n', 'N':
			b.WriteByte('\n')
		default:
			b.WriteByte(s[i])
		}
	}
	return strings.TrimSpace(b.String())
}
