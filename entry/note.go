package entry

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
)

// ErrStructure: a new prose would move a boundary or add a block, because it
// holds a heading, a --- line or a ```yaml block.
var ErrStructure = errors.New("the text would change the note's structure")

// Note is a Markdown note split into lines, with what it takes to write it
// back as it was: a byte order mark and \r\n line endings are kept.
type Note struct {
	Path  string
	Raw   []byte // the file as read
	Lines []string
	bom   bool
	crlf  bool
}

// ReadNote reads the note at path.
func ReadNote(path string) (*Note, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	n := &Note{Path: path, Raw: raw}
	data := raw
	if bytes.HasPrefix(data, []byte("\xef\xbb\xbf")) {
		n.bom = true
		data = data[3:]
	}
	s := string(data)
	if strings.Contains(s, "\r\n") {
		n.crlf = true
		s = strings.ReplaceAll(s, "\r\n", "\n")
	}
	n.Lines = strings.Split(s, "\n")
	return n, nil
}

// Hash is the hash of the note as read, the token for WriteInPlace.
func (n *Note) Hash() string { return Hash(n.Raw) }

// Encode joins lines in the note's own line endings and byte order mark.
func (n *Note) Encode(lines []string) []byte {
	s := strings.Join(lines, "\n")
	if n.crlf {
		s = strings.ReplaceAll(s, "\n", "\r\n")
	}
	if n.bom {
		s = "\xef\xbb\xbf" + s
	}
	return []byte(s)
}

// SetProse replaces the prose of the blocks at the given indexes of
// ParseBlocks(n.Lines) and returns the new lines and their blocks. It refuses
// a prose that would change where entries start or add a block.
func (n *Note) SetProse(blocks []Block, indexes []int, prose string) ([]string, []Block, error) {
	prose = strings.ReplaceAll(prose, "\r\n", "\n")
	prose = strings.TrimRight(strings.TrimLeft(prose, "\n"), " \t\n")

	// Bottom up, so the lines of the blocks still to do stay where they are.
	out := n.Lines
	for k := len(indexes) - 1; k >= 0; k-- {
		out = ReplaceProse(out, blocks[indexes[k]], prose)
	}
	after := ParseBlocks(out)
	if len(after) != len(blocks) {
		return nil, nil, fmt.Errorf("%w: it contains a yaml block", ErrStructure)
	}
	for _, i := range indexes {
		if after[i].Prose(out) != prose {
			return nil, nil, fmt.Errorf("%w: a heading or a --- line in it would end the entry early", ErrStructure)
		}
	}
	return out, after, nil
}
