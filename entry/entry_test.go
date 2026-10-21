package entry

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type golden struct {
	Line    int    `json:"line"`
	End     int    `json:"end"`
	Heading string `json:"heading"`
	Prose   string `json:"prose"`
}

// The notes in testdata/entries are the standard's cases; each .jsonl beside
// a note lists its entries in order.
func TestEntriesGolden(t *testing.T) {
	notes, _ := filepath.Glob("testdata/entries/*.md")
	if len(notes) == 0 {
		t.Fatal("no test notes")
	}
	for _, path := range notes {
		t.Run(filepath.Base(path), func(t *testing.T) {
			n, err := ReadNote(path)
			if err != nil {
				t.Fatal(err)
			}
			blocks := ParseBlocks(n.Lines)
			want := readGolden(t, strings.TrimSuffix(path, ".md")+".jsonl")
			if len(blocks) != len(want) {
				t.Fatalf("%d blocks, want %d", len(blocks), len(want))
			}
			for i, b := range blocks {
				got := golden{b.Open + 1, b.Close + 1, b.Heading, b.Prose(n.Lines)}
				if got != want[i] {
					t.Errorf("block %d:\n got %+v\nwant %+v", i+1, got, want[i])
				}
			}
		})
	}
}

func readGolden(t *testing.T, path string) []golden {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []golden
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var g golden
		if err := json.Unmarshal(sc.Bytes(), &g); err != nil {
			t.Fatal(err)
		}
		out = append(out, g)
	}
	return out
}

func noteOf(t *testing.T, content string) *Note {
	t.Helper()
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := ReadNote(path)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// set replaces the prose of block i and returns the note as it would be written.
func set(t *testing.T, content string, i int, prose string) (string, error) {
	t.Helper()
	n := noteOf(t, content)
	blocks := ParseBlocks(n.Lines)
	out, _, err := n.SetProse(blocks, []int{i}, prose)
	if err != nil {
		return "", err
	}
	return string(n.Encode(out)), nil
}

func TestSetProseReplacesOnlyItsRegion(t *testing.T) {
	got, err := set(t, "# Notes\n\n### Meeting\n\nOld text.\n\n```yaml\ntype: meeting\n```\n\nAfter the block.\n", 0, "New text.\nSecond line.")
	if err != nil {
		t.Fatal(err)
	}
	if want := "# Notes\n\n### Meeting\n\nNew text.\nSecond line.\n\n```yaml\ntype: meeting\n```\n\nAfter the block.\n"; got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestSetProseOnLeanBlock(t *testing.T) {
	lean := "### doc.pdf\n```yaml\ntype: ref\nid: '3'\n```\n"
	got, err := set(t, lean, 0, "About the document.")
	if err != nil {
		t.Fatal(err)
	}
	if want := "### doc.pdf\n\nAbout the document.\n\n```yaml\ntype: ref\nid: '3'\n```\n"; got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	// Clearing returns to the lean form.
	if got, _ = set(t, got, 0, ""); got != lean {
		t.Errorf("cleared:\n%s\nwant:\n%s", got, lean)
	}
}

func TestSetProseSecondBlockKeepsBlankLine(t *testing.T) {
	got, err := set(t, "### H\n\n```yaml\ntype: a\n```\n\nText B\n\n```yaml\ntype: b\n```\n", 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := "### H\n\n```yaml\ntype: a\n```\n\n```yaml\ntype: b\n```\n"; got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestSetProseRefusesStructure(t *testing.T) {
	for _, text := range []string{"### A heading", "line\n---\nline", "```yaml\nx: 1\n```"} {
		if _, err := set(t, "### H\n\nOld.\n\n```yaml\ntype: a\n```\n", 0, text); !errors.Is(err, ErrStructure) {
			t.Errorf("%q: err = %v, want ErrStructure", text, err)
		}
	}
}

func TestSetProseCodeFenceIsProse(t *testing.T) {
	text := "Example:\n\n```sh\n# a comment\n---\n```"
	got, err := set(t, "### H\n\n```yaml\ntype: a\n```\n", 0, text)
	if err != nil {
		t.Fatal(err)
	}
	n := noteOf(t, got)
	if p := ParseBlocks(n.Lines)[0].Prose(n.Lines); p != text {
		t.Errorf("prose = %q, want %q", p, text)
	}
}

func TestSetProseKeepsCRLFAndBOM(t *testing.T) {
	got, err := set(t, "\xef\xbb\xbf### H\r\n\r\nOld.\r\n\r\n```yaml\r\ntype: a\r\n```\r\n", 0, "New.")
	if err != nil {
		t.Fatal(err)
	}
	if want := "\xef\xbb\xbf### H\r\n\r\nNew.\r\n\r\n```yaml\r\ntype: a\r\n```\r\n"; got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestMatchesNFCAndLists(t *testing.T) {
	// "Müller" decomposed in the note, composed in the query.
	n := noteOf(t, "### A\n\n```yaml\nbinder: Mu\u0308ller\ntags: [a, b]\nid: '7'\n```\n")
	b := ParseBlocks(n.Lines)[0]
	if !b.Matches([][2]string{{"binder", "M\u00fcller"}, {"tags", "b"}, {"id", "7"}}) {
		t.Error("NFC, list item and quoted id should match")
	}
	if b.Matches([][2]string{{"tags", "c"}}) {
		t.Error("a list without the item must not match")
	}
}

func TestYmlIsNoBlock(t *testing.T) {
	n := noteOf(t, "### H\n\n```yml\ntype: a\n```\n")
	if blocks := ParseBlocks(n.Lines); len(blocks) != 0 {
		t.Errorf("a yml fence is an example, got %d blocks", len(blocks))
	}
}
