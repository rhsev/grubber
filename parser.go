package main

// ParseOpts carries per-parse flags so parsers stay decoupled from Grubber.
type ParseOpts struct {
	UseMmd          bool
	FrontmatterOnly bool // meaningful only for formats that have a frontmatter concept
	// Diag, when non-nil, collects doctor findings during the parse. nil (the
	// extract path) records nothing; parse behavior is identical either way.
	Diag *Diagnostics
}

// FileParser extracts structured metadata from a single text file.
// It returns file-level frontmatter and zero or more data-block records.
type FileParser interface {
	Extract(path string, data []byte, opts ParseOpts) (frontmatter Record, blocks []Record, err error)
}

var parsers = map[string]FileParser{}

// RegisterParser associates a FileParser with a file extension (e.g. ".md").
// Called from init() in each parser file.
func RegisterParser(ext string, p FileParser) {
	parsers[ext] = p
}

// registeredExtensions returns the extensions of all registered parsers.
func registeredExtensions() []string {
	exts := make([]string, 0, len(parsers))
	for ext := range parsers {
		exts = append(exts, ext)
	}
	return exts
}
