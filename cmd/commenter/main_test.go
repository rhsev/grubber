package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rhsev/grubber/entry"
)

func tempNote(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func call(t *testing.T, opt options, stdin string) ([]Entry, error) {
	t.Helper()
	var out bytes.Buffer
	err := run(opt, strings.NewReader(stdin), &out)
	var entries []Entry
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var e Entry
		if jerr := json.Unmarshal([]byte(line), &e); jerr != nil {
			t.Fatalf("output is not JSON: %q", line)
		}
		entries = append(entries, e)
	}
	return entries, err
}

func ptr(s string) *string { return &s }

func TestReadByLineAndWhere(t *testing.T) {
	path := tempNote(t, "### Clip\n\nSummary.\n\n```yaml\ntype: link\nid: abc\n```\n")
	for _, opt := range []options{{file: path, line: 5}, {file: path, line: 7}, {file: path, where: [][2]string{{"id", "abc"}}}} {
		got, err := call(t, opt, "")
		if err != nil || len(got) != 1 || got[0].Prose != "Summary." || got[0].Heading != "Clip" || got[0].Line != 5 || got[0].End != 8 {
			t.Errorf("%+v: %+v, %v", opt, got, err)
		}
	}
}

func TestSetFromStdinWithHash(t *testing.T) {
	path := tempNote(t, "### H\n\nOld.\n\n```yaml\ntype: a\n```\n")
	read, _ := call(t, options{file: path, line: 5}, "")
	got, err := call(t, options{file: path, line: 5, set: ptr("-"), ifHash: read[0].Hash}, "New.\n")
	if err != nil || got[0].Prose != "New." || got[0].Hash == read[0].Hash {
		t.Fatalf("%+v, %v", got, err)
	}
	// The old hash no longer holds.
	if _, err := call(t, options{file: path, line: 5, set: ptr("Again."), ifHash: read[0].Hash}, ""); !errors.Is(err, entry.ErrConflict) || exitCode(err) != 4 {
		t.Errorf("stale hash: err = %v", err)
	}
}

func TestAmbiguousNeedsAll(t *testing.T) {
	path := tempNote(t, "### A\n\n```yaml\nid: '1'\n```\n\n### B\n\n```yaml\nid: '1'\n```\n")
	opt := options{file: path, where: [][2]string{{"id", "1"}}, set: ptr("T")}
	if _, err := call(t, opt, ""); !errors.Is(err, errAmbiguous) || exitCode(err) != 3 {
		t.Fatalf("err = %v, want ambiguous", err)
	}
	opt.all = true
	got, err := call(t, opt, "")
	if err != nil || len(got) != 2 || got[0].Prose != "T" || got[1].Prose != "T" {
		t.Errorf("%+v, %v", got, err)
	}
}

func TestExitCodes(t *testing.T) {
	path := tempNote(t, "### H\n\n```yml\ntype: a\n```\n\n```yaml\ntype: b\n```\n")
	if _, err := call(t, options{file: path, line: 3}, ""); exitCode(err) != 2 {
		t.Errorf("a yml block is no data: err = %v", err)
	}
	if _, err := call(t, options{file: path, line: 7, set: ptr("## Heading")}, ""); exitCode(err) != 64 {
		t.Errorf("structure: err = %v", err)
	}
	if _, err := parseArgs([]string{path}); exitCode(err) != 64 {
		t.Errorf("no address: err = %v", err)
	}
}
