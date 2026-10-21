// Command commenter reads or replaces the prose of one entry in a Markdown
// note: the text between a ```yaml block and the nearest boundary above it.
// The rules are in package entry; every tool that shows or edits prose asks
// commenter rather than parsing the note itself.
//
//	commenter note.md --line 42
//	commenter note.md --where id=210322647 --where binder=safari
//	printf 'New text' | commenter note.md --line 42 --if-hash <hash> --set -
//
// Reading prints one JSON line per matching entry. --set replaces the prose of
// exactly one entry (--all: of every match), writes the note in place and
// prints the entries as they now stand. Exit codes: 2 nothing matches, 3
// several match without --all, 4 the note changed or a journal is left, 64
// the call is wrong, 1 reading or writing failed.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rhsev/grubber/entry"
)

var version = "dev"

const usage = "Usage: commenter FILE (--line N | --where key=value...) [--all] [--set TEXT|-] [--if-hash HASH]"

var (
	errNotFound  = errors.New("not_found")
	errAmbiguous = errors.New("ambiguous")
	errUsage     = errors.New("usage")
)

func exitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, errNotFound):
		return 2
	case errors.Is(err, errAmbiguous):
		return 3
	case errors.Is(err, entry.ErrConflict):
		return 4
	case errors.Is(err, errUsage), errors.Is(err, entry.ErrStructure):
		return 64
	}
	return 1
}

// Entry is what commenter prints for an entry. Lines are 1-based.
type Entry struct {
	File    string `json:"file"`
	Line    int    `json:"line"` // opening fence
	End     int    `json:"end"`  // closing fence
	Heading string `json:"heading,omitempty"`
	Prose   string `json:"prose,omitempty"`
	Hash    string `json:"hash"` // SHA-256 of the file, the token for --if-hash
}

type options struct {
	file   string
	line   int
	where  [][2]string
	all    bool
	set    *string
	ifHash string
}

func main() {
	opt, err := parseArgs(os.Args[1:])
	if err == nil {
		err = run(opt, os.Stdin, os.Stdout)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "commenter: %v\n", err)
		os.Exit(exitCode(err))
	}
}

func parseArgs(args []string) (options, error) {
	var opt options
	value := func(i int, flag string) (string, error) {
		if i+1 >= len(args) {
			return "", fmt.Errorf("%w: %s needs a value", errUsage, flag)
		}
		return args[i+1], nil
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "-h", "--help":
			fmt.Println(usage)
			os.Exit(0)
		case "-v", "--version":
			fmt.Println("commenter " + version)
			os.Exit(0)
		case "--line":
			v, err := value(i, a)
			if err != nil {
				return opt, err
			}
			n, cerr := strconv.Atoi(v)
			if cerr != nil || n < 1 {
				return opt, fmt.Errorf("%w: --line needs a line number, got %q", errUsage, v)
			}
			opt.line = n
			i++
		case "--where":
			v, err := value(i, a)
			if err != nil {
				return opt, err
			}
			k, val, ok := strings.Cut(v, "=")
			if !ok || k == "" {
				return opt, fmt.Errorf("%w: --where needs key=value, got %q", errUsage, v)
			}
			opt.where = append(opt.where, [2]string{k, val})
			i++
		case "--set":
			v, err := value(i, a)
			if err != nil {
				return opt, err
			}
			opt.set = &v
			i++
		case "--if-hash":
			v, err := value(i, a)
			if err != nil {
				return opt, err
			}
			opt.ifHash = v
			i++
		case "--all":
			opt.all = true
		default:
			if strings.HasPrefix(a, "-") || opt.file != "" {
				return opt, fmt.Errorf("%w: unexpected %q\n%s", errUsage, a, usage)
			}
			opt.file = expandHome(a)
		}
	}
	switch {
	case opt.file == "":
		return opt, fmt.Errorf("%w: no file given\n%s", errUsage, usage)
	case opt.line == 0 && len(opt.where) == 0:
		return opt, fmt.Errorf("%w: give --line or --where\n%s", errUsage, usage)
	case opt.line > 0 && len(opt.where) > 0:
		return opt, fmt.Errorf("%w: --line and --where exclude each other", errUsage)
	}
	return opt, nil
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

// selectBlocks returns the indexes of the blocks the options address. A line
// anywhere in a block, fences included, addresses it.
func selectBlocks(blocks []entry.Block, opt options) []int {
	var hits []int
	for i, b := range blocks {
		if opt.line > 0 {
			if opt.line-1 >= b.Open && opt.line-1 <= b.Close {
				hits = append(hits, i)
			}
		} else if b.Matches(opt.where) {
			hits = append(hits, i)
		}
	}
	return hits
}

func run(opt options, stdin io.Reader, stdout io.Writer) error {
	n, err := entry.ReadNote(opt.file)
	if err != nil {
		return err
	}
	blocks := entry.ParseBlocks(n.Lines)
	hits := selectBlocks(blocks, opt)
	if len(hits) == 0 {
		return fmt.Errorf("%w: %s: no yaml block %s", errNotFound, opt.file, describe(opt))
	}
	if opt.ifHash != "" && opt.ifHash != n.Hash() {
		return fmt.Errorf("%w: %s changed since it was read; nothing written", entry.ErrConflict, opt.file)
	}
	if opt.set == nil {
		return printEntries(stdout, opt.file, n.Hash(), n.Lines, blocks, hits)
	}

	if len(hits) > 1 && !opt.all {
		var at []string
		for _, h := range hits {
			at = append(at, strconv.Itoa(blocks[h].Open+1))
		}
		return fmt.Errorf("%w: %s: %d yaml blocks %s (line %s); add --all to change every one",
			errAmbiguous, opt.file, len(hits), describe(opt), strings.Join(at, ", "))
	}

	prose := *opt.set
	if prose == "-" {
		data, rerr := io.ReadAll(stdin)
		if rerr != nil {
			return rerr
		}
		prose = string(data)
	}
	out, after, err := n.SetProse(blocks, hits, prose)
	if err != nil {
		return fmt.Errorf("%w; nothing written", err)
	}
	data := n.Encode(out)
	hash := n.Hash()
	if string(data) != string(n.Raw) {
		if err := entry.WriteInPlace(opt.file, data, hash); err != nil {
			return err
		}
		hash = entry.Hash(data)
	}
	return printEntries(stdout, opt.file, hash, out, after, hits)
}

func describe(opt options) string {
	if opt.line > 0 {
		return fmt.Sprintf("at line %d", opt.line)
	}
	var parts []string
	for _, w := range opt.where {
		parts = append(parts, w[0]+"="+w[1])
	}
	return "with " + strings.Join(parts, ", ")
}

func printEntries(w io.Writer, file, hash string, lines []string, blocks []entry.Block, hits []int) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for _, h := range hits {
		b := blocks[h]
		if err := enc.Encode(Entry{
			File: file, Line: b.Open + 1, End: b.Close + 1,
			Heading: b.Heading, Prose: b.Prose(lines), Hash: hash,
		}); err != nil {
			return err
		}
	}
	return nil
}
