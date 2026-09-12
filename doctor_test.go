package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// extractWithDiag runs the md parser over data with diagnostics enabled and
// returns the findings.
func extractWithDiag(t *testing.T, data string) []Finding {
	t.Helper()
	d := &Diagnostics{}
	p := &mdParser{}
	if _, _, err := p.Extract("f.md", []byte(data), ParseOpts{Diag: d}); err != nil {
		t.Fatal(err)
	}
	return d.findings
}

func findCategory(findings []Finding, category string) *Finding {
	for i := range findings {
		if findings[i].Category == category {
			return &findings[i]
		}
	}
	return nil
}

func TestDoctorClean(t *testing.T) {
	findings := extractWithDiag(t, "---\ntitle: hello\n---\n\n```yaml\nfoo: bar\nlist: [a, b]\n```\n")
	if len(findings) != 0 {
		t.Errorf("clean file should have no findings, got %v", findings)
	}
}

func TestDoctorYAMLError(t *testing.T) {
	findings := extractWithDiag(t, "text\n\n```yaml\nfoo: bar\n\tbad: tab indent\n```\n")
	f := findCategory(findings, "yaml-error")
	if f == nil {
		t.Fatalf("expected yaml-error finding, got %v", findings)
	}
	if f.Line != 3 {
		t.Errorf("yaml-error line: got %d, want 3", f.Line)
	}
	if !strings.Contains(f.Message, "fallback") {
		t.Errorf("message should mention the fallback, got %q", f.Message)
	}
}

func TestDoctorNonStringKeys(t *testing.T) {
	findings := extractWithDiag(t, "```yaml\nSource: {{src.home}}\n```\n")
	if findCategory(findings, "non-string-keys") == nil {
		t.Errorf("expected non-string-keys finding, got %v", findings)
	}
}

func TestDoctorNonFinite(t *testing.T) {
	findings := extractWithDiag(t, "```yaml\nvalue: .nan\n```\n")
	f := findCategory(findings, "non-finite")
	if f == nil {
		t.Fatalf("expected non-finite finding, got %v", findings)
	}
	if !strings.HasPrefix(f.Message, "value:") {
		t.Errorf("message should name the key, got %q", f.Message)
	}
}

func TestDoctorNestedMapping(t *testing.T) {
	findings := extractWithDiag(t, "---\nhosts:\n  mini:\n    home: /Users/x\n---\n")
	f := findCategory(findings, "nested-mapping")
	if f == nil {
		t.Fatalf("expected nested-mapping finding, got %v", findings)
	}
	if f.Line != 2 {
		t.Errorf("frontmatter finding line: got %d, want 2", f.Line)
	}
}

func TestDoctorDuplicateKey(t *testing.T) {
	findings := extractWithDiag(t, "```yaml\nfoo: 1\nfoo: 2\n```\n")
	if findCategory(findings, "duplicate-key") == nil {
		t.Errorf("expected duplicate-key finding, got %v", findings)
	}
}

func TestDoctorUnclosedFence(t *testing.T) {
	findings := extractWithDiag(t, "---\ntitle: x\n---\ntext\n\n```yaml\nfoo: bar\n\nprose, never closed\n")
	f := findCategory(findings, "unclosed-fence")
	if f == nil {
		t.Fatalf("expected unclosed-fence finding, got %v", findings)
	}
	if f.Line != 6 {
		t.Errorf("unclosed-fence line: got %d, want 6", f.Line)
	}
}

func TestDoctorBlockLineNumbers(t *testing.T) {
	// Second block is broken; the finding must point at its fence line.
	findings := extractWithDiag(t, "```yaml\nfoo: bar\n```\n\n```yaml\n\t{bad\n```\n")
	f := findCategory(findings, "yaml-error")
	if f == nil {
		t.Fatalf("expected yaml-error finding, got %v", findings)
	}
	if f.Line != 5 {
		t.Errorf("yaml-error line: got %d, want 5", f.Line)
	}
	if f.Path != "f.md" {
		t.Errorf("path: got %q", f.Path)
	}
}

func TestDoctorArrayOfMappings(t *testing.T) {
	findings := extractWithDiag(t, "```yaml\nitems:\n  - name: a\n  - name: b\n```\n")
	if len(findings) != 1 || findings[0].Category != "nested-mapping" {
		t.Errorf("expected exactly one nested-mapping finding for array of mappings, got %v", findings)
	}
}

func TestScanChars(t *testing.T) {
	// SHY, ZWSP, BOM (mid-file), C1 (U+0085), CRLF — plus tab and NBSP,
	// which must be counted (NBSP) or ignored (tab) but never fixed.
	data := []byte("Ge\u00adsch\u00e4fte\r\nzero\u200bwidth\ufeff\u0085x\nnbsp\u00a0\tTab bleibt hier\n")
	stats, crlf, invalid := scanChars(data)
	if invalid != nil {
		t.Errorf("valid UTF-8 must not report invalid bytes, got %v", invalid)
	}
	if crlf == nil || crlf.count != 1 || crlf.line != 1 {
		t.Errorf("crlf: got %+v", crlf)
	}
	if s := stats[0x00AD]; s == nil || s.count != 1 || s.line != 1 || s.col != 3 {
		t.Errorf("SOFT HYPHEN: got %+v", stats[0x00AD])
	}
	if s := stats[0x200B]; s == nil || s.line != 2 || s.col != 5 {
		t.Errorf("ZWSP: got %+v", stats[0x200B])
	}
	if stats[0xFEFF] == nil {
		t.Error("mid-file BOM not found")
	}
	if stats[0x0085] == nil {
		t.Error("C1 control not found")
	}
	if s := stats[0x00A0]; s == nil || s.count != 1 {
		t.Errorf("NBSP should be counted, got %+v", stats[0x00A0])
	}
	if _, ok := stats['\t']; ok {
		t.Error("tab must not be counted")
	}
}

func TestFixChars(t *testing.T) {
	data := []byte("Ge\u00adsch\u00e4fte\r\nzero\u200bwidth\ufeff\u0085x\nnbsp\u00a0\tTab bleibt hier\n")
	fixed := fixChars(data)
	want := "Gesch\u00e4fte\nzerowidthx\nnbsp\u00a0\tTab bleibt hier\n"
	if string(fixed) != want {
		t.Errorf("fixed: got %q, want %q", fixed, want)
	}
	if string(fixChars(fixed)) != want {
		t.Error("fixChars is not idempotent")
	}
}

func TestScanCharsInvalidUTF8(t *testing.T) {
	// A lone 0xFF, a truncated two-byte sequence (0xC3 without its
	// continuation), and a genuine U+FFFD, which is valid text and must not
	// be reported.
	data := []byte("ok\nvor\xffnach \xc3 ende\n� echt\n")
	_, _, invalid := scanChars(data)
	if len(invalid) != 2 {
		t.Fatalf("expected 2 distinct invalid bytes, got %v", invalid)
	}
	if s := invalid[0xFF]; s == nil || s.count != 1 || s.line != 2 || s.col != 4 {
		t.Errorf("0xFF: got %+v", invalid[0xFF])
	}
	if s := invalid[0xC3]; s == nil || s.count != 1 || s.line != 2 {
		t.Errorf("0xC3: got %+v", invalid[0xC3])
	}

	findings := charFindings("f.md", map[rune]*charStat{}, nil, invalid)
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %v", findings)
	}
	// Sorted by byte value, so 0xC3 comes before 0xFF.
	if findings[0].Category != "invalid-utf8" || !strings.Contains(findings[0].Message, "0xC3") {
		t.Errorf("first finding: %+v", findings[0])
	}
	if !strings.Contains(findings[1].Message, "0xFF") {
		t.Errorf("second finding: %+v", findings[1])
	}
}

func TestFixCharsKeepsInvalidUTF8(t *testing.T) {
	// --fix must not silently repair or drop an undecodable byte: the right
	// repair depends on the original encoding. CRLF still gets normalized.
	data := []byte("vor\xffnach\r\n")
	got := fixChars(data)
	if string(got) != "vor\xffnach\n" {
		t.Errorf("invalid byte must survive --fix, got %q", got)
	}
}

func TestDoctorInvalidUTF8IsNotFixed(t *testing.T) {
	// A file whose only finding is an invalid byte must be reported and left
	// untouched, like suspect-char.
	dir := t.TempDir()
	md := filepath.Join(dir, "note.md")
	data := []byte("---\ntitle: vor\xffnach\n---\n")
	if err := os.WriteFile(md, data, 0o644); err != nil {
		t.Fatal(err)
	}

	res := doctorScan([]string{md}, true)
	if res.fixed != 0 {
		t.Errorf("invalid-utf8 alone must not trigger a rewrite, fixed=%d", res.fixed)
	}
	got, _ := os.ReadFile(md)
	if string(got) != string(data) {
		t.Errorf("file must be untouched, got %q", got)
	}
	if findCategory(res.findings, "invalid-utf8") == nil {
		t.Errorf("no invalid-utf8 finding, got %v", res.findings)
	}
}

func TestZWJSurvivesFix(t *testing.T) {
	// A composed emoji is held together by U+200D. --fix must leave it alone,
	// or 👨‍👩‍👧‍👦 falls apart into four people.
	family := "Familie: 👨‍👩‍👧‍👦 und 👩‍💻\n"
	if got := string(fixChars([]byte(family))); got != family {
		t.Errorf("ZWJ must survive --fix:\n got  %q\n want %q", got, family)
	}

	stats, _, _ := scanChars([]byte(family))
	s := stats[0x200D]
	if s == nil || s.count != 4 {
		t.Fatalf("ZWJ should be counted, got %+v", s)
	}
	findings := charFindings("f.md", stats, nil, nil)
	if len(findings) != 1 || findings[0].Category != "suspect-char" {
		t.Errorf("ZWJ must be reported as suspect-char, got %v", findings)
	}
}

func TestZeroWidthNeighboursStillRemoved(t *testing.T) {
	// Only ZWJ leaves the removable set; its neighbours in U+200B..U+200F
	// stay on it.
	for _, r := range []rune{0x200B, 0x200C, 0x200E, 0x200F} {
		if !removableRune(r) {
			t.Errorf("U+%04X should still be removable", r)
		}
	}
	if removableRune(0x200D) {
		t.Error("ZWJ must not be removable")
	}
}

func TestFixCharsLoneCR(t *testing.T) {
	if got := string(fixChars([]byte("a\rb\r\nc"))); got != "ab\nc" {
		t.Errorf("lone CR should be removed, CRLF normalized: got %q", got)
	}
}

func TestDoctorScanFix(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "note.md")
	jsonl := filepath.Join(dir, "index.jsonl")
	mdData := []byte("---\ntitle: x\n---\nGe\u00adsch\u00e4fte\r\n")
	jsonlData := []byte("{\"id\": \"a\u00adb\"}\n")
	if err := os.WriteFile(md, mdData, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jsonl, jsonlData, 0o644); err != nil {
		t.Fatal(err)
	}

	// Report pass: findings in both files, nothing written.
	res := doctorScan([]string{md, jsonl}, false)
	if res.affected != 2 || res.fixed != 0 {
		t.Fatalf("report pass: affected=%d fixed=%d, findings %v", res.affected, res.fixed, res.findings)
	}

	// Fix pass: md is rewritten, jsonl is reported but never written.
	res = doctorScan([]string{md, jsonl}, true)
	if res.fixed != 1 {
		t.Fatalf("fix pass: fixed=%d, findings %v", res.fixed, res.findings)
	}
	got, _ := os.ReadFile(md)
	if string(got) != "---\ntitle: x\n---\nGesch\u00e4fte\n" {
		t.Errorf("md after fix: %q", got)
	}
	jgot, _ := os.ReadFile(jsonl)
	if string(jgot) != string(jsonlData) {
		t.Errorf("jsonl must never be written, got %q", jgot)
	}

	// Second fix run: md is clean now, jsonl still reported.
	res = doctorScan([]string{md, jsonl}, true)
	if res.fixed != 0 {
		t.Errorf("fix must be idempotent, fixed=%d", res.fixed)
	}
	mdFindings := 0
	for _, f := range res.findings {
		if f.Path == md {
			mdFindings++
		}
	}
	if mdFindings != 0 {
		t.Errorf("md should be clean after fix, findings %v", res.findings)
	}
}

func TestExpandOnly(t *testing.T) {
	set, err := expandOnly([]string{"yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if !set["unclosed-fence"] || !set["duplicate-key"] || set["invisible-char"] {
		t.Errorf("yaml class: got %v", set)
	}
	set, err = expandOnly([]string{"chars", "yaml-error"})
	if err != nil {
		t.Fatal(err)
	}
	if !set["crlf"] || !set["yaml-error"] || set["nested-mapping"] {
		t.Errorf("chars+exact: got %v", set)
	}
	if _, err = expandOnly([]string{"typo"}); err == nil {
		t.Error("unknown category should error")
	}
}

func TestDoctorNilDiagnosticsSafe(t *testing.T) {
	var d *Diagnostics
	d.setPos("x", 1)
	d.setLine(2)
	d.add("cat", "msg")
	d.inspectValue("k", map[string]any{"a": 1})
}

func TestDoctorLeadingZeroNumber(t *testing.T) {
	findings := extractWithDiag(t, "---\nnumber: 01711234890\n---\n")
	f := findCategory(findings, "leading-zero-number")
	if f == nil {
		t.Fatalf("expected leading-zero-number, got %v", findings)
	}
	if !strings.Contains(f.Message, "1711234890") {
		t.Errorf("message should show what extract emits: %q", f.Message)
	}
}

// The dangerous case: every digit is octal-valid, so the value does not just
// lose its zero, it becomes a different number.
func TestDoctorLeadingZeroOctal(t *testing.T) {
	f := findCategory(extractWithDiag(t, "---\ncode: 0755\n---\n"), "leading-zero-number")
	if f == nil {
		t.Fatal("expected leading-zero-number for 0755")
	}
	if !strings.Contains(f.Message, "493") || !strings.Contains(f.Message, "octal") {
		t.Errorf("octal case should say so and show 493: %q", f.Message)
	}
}

func TestDoctorLeadingZeroQuotedIsClean(t *testing.T) {
	for _, doc := range []string{
		"---\nnumber: \"01711234890\"\n---\n", // double quoted
		"---\nnumber: '0755'\n---\n",          // single quoted
		"---\nnumber: 0.5\n---\n",             // the zero survives
		"---\nnumber: 0\n---\n",               // plain zero
		"---\nnumber: 0x1f\n---\n",            // deliberate hex
		"---\nnumber: 0o17\n---\n",            // deliberate octal
		"---\nnumber: 0b101\n---\n",           // deliberate binary
		"---\nnumber: 0abc\n---\n",            // stays a string
	} {
		if f := findCategory(extractWithDiag(t, doc), "leading-zero-number"); f != nil {
			t.Errorf("%q should not be flagged: %s", doc, f.Message)
		}
	}
}

func TestDoctorLeadingZeroInsideArray(t *testing.T) {
	findings := extractWithDiag(t, "---\ncodes: [0123, 456]\n---\n")
	if f := findCategory(findings, "leading-zero-number"); f == nil {
		t.Errorf("array elements should be inspected too, got %v", findings)
	}
}
